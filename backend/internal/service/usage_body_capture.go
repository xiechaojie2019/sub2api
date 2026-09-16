package service

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
)

// 使用记录请求/响应体捕获的系统设置键。捕获默认关闭（opt-in）：
// 请求/响应体包含完整对话内容，开启前管理员必须明确知晓存储与隐私影响。
const (
	// SettingKeyUsageBodyCaptureEnabled 开关：true 时网关路由按上限截断捕获请求/响应体并随 usage_logs 落库。
	SettingKeyUsageBodyCaptureEnabled = "usage_body_capture_enabled"
	// SettingKeyUsageBodyCaptureMaxBytes 单侧（请求体/响应体各自）捕获上限字节数。
	SettingKeyUsageBodyCaptureMaxBytes = "usage_body_capture_max_bytes"
)

const (
	// UsageBodyCaptureDefaultMaxBytes 未配置时的默认单侧上限（64KB）。
	UsageBodyCaptureDefaultMaxBytes = 64 * 1024
	// UsageBodyCaptureMinMaxBytes / UsageBodyCaptureMaxMaxBytes 限制管理员可配置的范围。
	UsageBodyCaptureMinMaxBytes = 1024
	UsageBodyCaptureMaxMaxBytes = 1024 * 1024
)

// usageBodyCaptureCacheTTL 网关热路径每个请求都会读取该开关，
// 禁止直接访问 DB；TTL 内的改动延迟生效。
const usageBodyCaptureCacheTTL = 5 * time.Second

// UsageBodyCaptureRuntime 是用量体捕获的运行时开关快照。
type UsageBodyCaptureRuntime struct {
	Enabled  bool
	MaxBytes int
}

type cachedUsageBodyCaptureRuntime struct {
	runtime   UsageBodyCaptureRuntime
	expiresAt int64
}

// ClampUsageBodyCaptureMaxBytes 把配置值收敛到安全范围。
func ClampUsageBodyCaptureMaxBytes(raw string) int {
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n <= 0 {
		return UsageBodyCaptureDefaultMaxBytes
	}
	if n < UsageBodyCaptureMinMaxBytes {
		return UsageBodyCaptureMinMaxBytes
	}
	if n > UsageBodyCaptureMaxMaxBytes {
		return UsageBodyCaptureMaxMaxBytes
	}
	return n
}

// UsageBodyCaptureRuntime 读取捕获开关与上限（TTL 缓存；读取失败 fail-closed 关闭）。
func (s *SettingService) UsageBodyCaptureRuntime(ctx context.Context) UsageBodyCaptureRuntime {
	if cached, ok := s.usageBodyCaptureCache.Load().(*cachedUsageBodyCaptureRuntime); ok && cached != nil {
		if time.Now().UnixNano() < cached.expiresAt {
			return cached.runtime
		}
	}

	result, _, _ := s.usageBodyCaptureSF.Do("usage_body_capture", func() (any, error) {
		if cached, ok := s.usageBodyCaptureCache.Load().(*cachedUsageBodyCaptureRuntime); ok && cached != nil {
			if time.Now().UnixNano() < cached.expiresAt {
				return cached.runtime, nil
			}
		}

		runtime := UsageBodyCaptureRuntime{Enabled: false, MaxBytes: UsageBodyCaptureDefaultMaxBytes}
		if s.settingRepo != nil {
			vals, err := s.settingRepo.GetMultiple(ctx, []string{SettingKeyUsageBodyCaptureEnabled, SettingKeyUsageBodyCaptureMaxBytes})
			if err == nil {
				runtime.Enabled = strings.EqualFold(strings.TrimSpace(vals[SettingKeyUsageBodyCaptureEnabled]), "true")
				runtime.MaxBytes = ClampUsageBodyCaptureMaxBytes(vals[SettingKeyUsageBodyCaptureMaxBytes])
			}
		}

		s.usageBodyCaptureCache.Store(&cachedUsageBodyCaptureRuntime{
			runtime:   runtime,
			expiresAt: time.Now().Add(usageBodyCaptureCacheTTL).UnixNano(),
		})
		return runtime, nil
	})

	runtime, _ := result.(UsageBodyCaptureRuntime)
	return runtime
}

// InvalidateUsageBodyCaptureCache 设置保存后的主动失效钩子；TTL 是兜底。
func (s *SettingService) InvalidateUsageBodyCaptureCache() {
	if s == nil {
		return
	}
	s.usageBodyCaptureSF.Forget("usage_body_capture")
	if cached, ok := s.usageBodyCaptureCache.Load().(*cachedUsageBodyCaptureRuntime); ok && cached != nil {
		s.usageBodyCaptureCache.Store(&cachedUsageBodyCaptureRuntime{
			runtime:   cached.runtime,
			expiresAt: 0,
		})
	}
}

// UsageBodyCapture 汇集单次请求的请求体/响应体捕获缓冲。
// 中间件在响应写出过程中追加响应分片，用量记录任务在响应完成后读取快照；
// 两者以互斥锁界定，快照读取不必关心写时序。
type UsageBodyCapture struct {
	mu sync.Mutex

	requestBody          string
	requestBodyCaptured  bool
	requestBodyTruncated bool

	responseBuf           []byte
	responseBodyCaptured  bool
	responseBodyTruncated bool

	limit int
}

// NewUsageBodyCapture limit 为单侧捕获上限（调用方需已收敛到安全范围）。
func NewUsageBodyCapture(limit int) *UsageBodyCapture {
	if limit <= 0 {
		limit = UsageBodyCaptureDefaultMaxBytes
	}
	return &UsageBodyCapture{limit: limit}
}

// SetRequestBody 记录请求体快照；超限部分丢弃并置截断标记。
func (u *UsageBodyCapture) SetRequestBody(body []byte) {
	if u == nil {
		return
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	u.requestBodyCaptured = true
	if len(body) > u.limit {
		u.requestBody = string(body[:u.limit])
		u.requestBodyTruncated = true
		return
	}
	u.requestBody = string(body)
	u.requestBodyTruncated = false
}

// AppendResponseChunk 追加响应分片；达到上限后静默丢弃后续字节。
func (u *UsageBodyCapture) AppendResponseChunk(chunk []byte) {
	if u == nil || len(chunk) == 0 {
		return
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	u.responseBodyCaptured = true
	remaining := u.limit - len(u.responseBuf)
	if remaining <= 0 {
		u.responseBodyTruncated = true
		return
	}
	if len(chunk) > remaining {
		chunk = chunk[:remaining]
		u.responseBodyTruncated = true
	}
	u.responseBuf = append(u.responseBuf, chunk...)
}

// Snapshot 返回请求/响应体快照、是否捕获及截断标记；空字符串也可能是已捕获的空 body。
func (u *UsageBodyCapture) Snapshot() (requestBody string, requestCaptured bool, requestBodyTruncated bool, responseBody string, responseCaptured bool, responseBodyTruncated bool) {
	if u == nil {
		return "", false, false, "", false, false
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.requestBody, u.requestBodyCaptured, u.requestBodyTruncated, string(u.responseBuf), u.responseBodyCaptured, u.responseBodyTruncated
}

// WithUsageBodyCapture 把捕获缓冲挂到请求 context 上，供用量记录链路读取。
func WithUsageBodyCapture(ctx context.Context, capture *UsageBodyCapture) context.Context {
	return context.WithValue(ctx, ctxkey.UsageBodyCapture, capture)
}

// UsageBodyCaptureFromContext 从 context 取出捕获缓冲；未注入时返回 nil。
func UsageBodyCaptureFromContext(ctx context.Context) *UsageBodyCapture {
	if ctx == nil {
		return nil
	}
	capture, _ := ctx.Value(ctxkey.UsageBodyCapture).(*UsageBodyCapture)
	return capture
}

// ApplyUsageBodyCapture 把捕获的请求/响应体写入用量日志（落库前调用）。
// 未开启捕获或该路径无捕获缓冲时为 no-op，日志行的两个字段保持 NULL。
func ApplyUsageBodyCapture(ctx context.Context, log *UsageLog) {
	if ctx == nil || log == nil {
		return
	}
	capture := UsageBodyCaptureFromContext(ctx)
	if capture == nil {
		return
	}
	requestBody, requestCaptured, requestBodyTruncated, responseBody, responseCaptured, responseBodyTruncated := capture.Snapshot()
	if requestCaptured {
		body := requestBody
		log.RequestBody = &body
		log.RequestBodyTruncated = requestBodyTruncated
	}
	if responseCaptured {
		body := responseBody
		log.ResponseBody = &body
		log.ResponseBodyTruncated = responseBodyTruncated
	}
}
