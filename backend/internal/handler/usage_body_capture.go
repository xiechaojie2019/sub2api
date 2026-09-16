package handler

import (
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	"github.com/Wei-Shaw/sub2api/internal/pkg/requestmodel"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// usageBodyCaptureWriter 包装 gin.ResponseWriter，把写出的响应字节（含 SSE 分片）
// 按上限截断捕获，供 usage_logs 落库；其余方法全部委托给内层 writer。
type usageBodyCaptureWriter struct {
	gin.ResponseWriter
	capture                *service.UsageBodyCapture
	responseCaptureKnown   bool
	responseCaptureAllowed bool
}

func (w *usageBodyCaptureWriter) Write(b []byte) (int, error) {
	w.ensureResponseCaptureAllowed(b)
	if w.responseCaptureAllowed {
		w.capture.AppendResponseChunk(b)
	}
	return w.ResponseWriter.Write(b)
}

func (w *usageBodyCaptureWriter) WriteString(s string) (int, error) {
	w.ensureResponseCaptureAllowed([]byte(s))
	if w.responseCaptureAllowed {
		w.capture.AppendResponseChunk([]byte(s))
	}
	return w.ResponseWriter.WriteString(s)
}

func (w *usageBodyCaptureWriter) ensureResponseCaptureAllowed(firstChunk []byte) {
	if w.responseCaptureKnown {
		return
	}
	contentType := w.Header().Get("Content-Type")
	if strings.TrimSpace(contentType) == "" && len(firstChunk) > 0 {
		contentType = http.DetectContentType(firstChunk)
	}
	w.responseCaptureAllowed = isCaptureResponseContentType(contentType)
	w.responseCaptureKnown = true
}

// isCaptureResponseContentType 只允许可读文本响应进入 usage_logs；媒体、下载和
// application/octet-stream 等二进制响应必须跳过，避免非法 UTF-8 破坏落库。
func isCaptureResponseContentType(contentType string) bool {
	mediaType := strings.ToLower(strings.TrimSpace(contentType))
	if idx := strings.IndexByte(mediaType, ';'); idx >= 0 {
		mediaType = strings.TrimSpace(mediaType[:idx])
	}
	return mediaType == "application/json" ||
		strings.HasSuffix(mediaType, "+json") ||
		mediaType == "text/event-stream" ||
		strings.HasPrefix(mediaType, "text/")
}

// isJSONContentType 仅 JSON 请求体值得入库；multipart/二进制（音频等）原样跳过。
func isJSONContentType(contentType string) bool {
	mediaType := strings.ToLower(strings.TrimSpace(contentType))
	if idx := strings.IndexByte(mediaType, ';'); idx >= 0 {
		mediaType = strings.TrimSpace(mediaType[:idx])
	}
	return strings.HasSuffix(mediaType, "+json") || mediaType == "application/json" ||
		mediaType == "text/json"
}

// UsageBodyCaptureMiddleware 在系统开关开启时捕获网关请求的请求体与响应体快照。
// 快照挂到 request context（ctxkey.UsageBodyCapture），由用量落库链路
// service.ApplyUsageBodyCapture 读取；开关关闭时零开销直通。
//
// 必须注册在认证之后（未认证请求不值得付出读取/缓冲代价）、
// OpsErrorLoggerMiddleware 之内（其 defer 会把 c.Writer 还原为本中间件
// 包裹前的 writer，随后本中间件再还原，互不冲突）。
func UsageBodyCaptureMiddleware(settingService *service.SettingService) gin.HandlerFunc {
	return func(c *gin.Context) {
		if settingService == nil {
			c.Next()
			return
		}
		runtime := settingService.UsageBodyCaptureRuntime(c.Request.Context())
		if !runtime.Enabled || c.Request == nil {
			c.Next()
			return
		}

		capture := service.NewUsageBodyCapture(runtime.MaxBytes)

		// 读全量请求体再回填（与 composite 路由中间件同模式），超过捕获上限
		// 只保留前 limit 字节；解码 gzip/zstd 后的明文同步生效（与转发一致）。
		if c.Request.Body != nil && isJSONContentType(c.Request.Header.Get("Content-Type")) {
			if body, err := httputil.ReadRequestBodyWithPrealloc(c.Request); err == nil {
				capture.SetRequestBody(body)
				requestmodel.ResetRequestBody(c.Request, body)
			}
			// 读取失败（含 MaxBytesReader 超限）：不回填，交由 handler 按既有错误路径处理。
		}

		originalWriter := c.Writer
		c.Writer = &usageBodyCaptureWriter{ResponseWriter: originalWriter, capture: capture}
		defer func() {
			if c.Writer == originalWriter {
				return
			}
			if w, ok := c.Writer.(*usageBodyCaptureWriter); ok && w.capture == capture {
				c.Writer = originalWriter
			}
		}()

		c.Request = c.Request.WithContext(service.WithUsageBodyCapture(c.Request.Context(), capture))
		c.Next()
	}
}
