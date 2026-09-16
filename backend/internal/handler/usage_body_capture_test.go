package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

type staticUsageBodyCaptureSettingRepo struct {
	values map[string]string
}

func (f *staticUsageBodyCaptureSettingRepo) Get(ctx context.Context, key string) (*service.Setting, error) {
	return nil, service.ErrSettingNotFound
}

func (f *staticUsageBodyCaptureSettingRepo) GetValue(ctx context.Context, key string) (string, error) {
	return f.values[key], nil
}

func (f *staticUsageBodyCaptureSettingRepo) Set(ctx context.Context, key, value string) error {
	return nil
}

func (f *staticUsageBodyCaptureSettingRepo) GetMultiple(ctx context.Context, keys []string) (map[string]string, error) {
	out := make(map[string]string, len(keys))
	for _, k := range keys {
		out[k] = f.values[k]
	}
	return out, nil
}

func (f *staticUsageBodyCaptureSettingRepo) SetMultiple(ctx context.Context, settings map[string]string) error {
	return nil
}

func (f *staticUsageBodyCaptureSettingRepo) GetAll(ctx context.Context) (map[string]string, error) {
	return f.values, nil
}

func (f *staticUsageBodyCaptureSettingRepo) Delete(ctx context.Context, key string) error { return nil }

func newUsageBodyCaptureEnabledService(t *testing.T) *service.SettingService {
	t.Helper()
	return service.NewSettingService(&staticUsageBodyCaptureSettingRepo{values: map[string]string{
		service.SettingKeyUsageBodyCaptureEnabled: "true",
	}}, nil)
}

func TestUsageBodyCaptureMiddleware_DisabledNoCapture(t *testing.T) {
	gin.SetMode(gin.TestMode)
	// nil repo 读取失败 fail-closed：开关视为关闭，不注入捕获缓冲、不读请求体。
	router := gin.New()
	var captured *service.UsageBodyCapture
	router.POST("/v1/messages", UsageBodyCaptureMiddleware(service.NewSettingService(nil, nil)), func(c *gin.Context) {
		captured = service.UsageBodyCaptureFromContext(c.Request.Context())
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"gpt-5"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", w.Code)
	}
	if captured != nil {
		t.Fatal("capture must be absent when the feature is disabled")
	}
}

func TestUsageBodyCaptureMiddleware_CapturesAndRestoresBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	var captured *service.UsageBodyCapture
	router.POST("/v1/messages", UsageBodyCaptureMiddleware(newUsageBodyCaptureEnabledService(t)), func(c *gin.Context) {
		captured = service.UsageBodyCaptureFromContext(c.Request.Context())
		// handler 正常重读请求体（回填后零损耗），并照常写出响应。
		body, err := httputil.ReadRequestBodyWithPrealloc(c.Request)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"echo_len": len(body)})
	})

	const reqBody = `{"model":"gpt-5","messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", w.Code, w.Body.String())
	}
	if captured == nil {
		t.Fatal("capture must be present when enabled")
	}
	requestBody, _, requestBodyTruncated, responseBody, _, responseBodyTruncated := captured.Snapshot()
	if requestBody != reqBody || requestBodyTruncated {
		t.Fatalf("unexpected request snapshot: %q trunc=%v", requestBody, requestBodyTruncated)
	}
	if !strings.Contains(responseBody, `"echo_len"`) || responseBodyTruncated {
		t.Fatalf("unexpected response snapshot: %q trunc=%v", responseBody, responseBodyTruncated)
	}
}

func TestUsageBodyCaptureMiddleware_SkipsBinaryResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	var captured *service.UsageBodyCapture
	router.POST("/v1/messages", UsageBodyCaptureMiddleware(newUsageBodyCaptureEnabledService(t)), func(c *gin.Context) {
		captured = service.UsageBodyCaptureFromContext(c.Request.Context())
		c.Data(http.StatusOK, "image/png", []byte{0x89, 0x50, 0x4e, 0x47, 0x00, 0xff})
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"gpt-5"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if captured == nil {
		t.Fatal("capture buffer should exist")
	}
	_, _, _, responseBody, responseCaptured, _ := captured.Snapshot()
	if responseCaptured || responseBody != "" {
		t.Fatalf("binary response must not be captured: captured=%v body=%q", responseCaptured, responseBody)
	}
}

func TestUsageBodyCaptureMiddleware_SkipsNonJSONBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	var captured *service.UsageBodyCapture
	router.POST("/v1/messages", UsageBodyCaptureMiddleware(newUsageBodyCaptureEnabledService(t)), func(c *gin.Context) {
		captured = service.UsageBodyCaptureFromContext(c.Request.Context())
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader("binary-audio-payload"))
	req.Header.Set("Content-Type", "audio/mpeg")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if captured == nil {
		t.Fatal("capture buffer should still exist for response capture")
	}
	requestBody, _, _, _, _, _ := captured.Snapshot()
	if requestBody != "" {
		t.Fatalf("non-JSON request body must not be captured, got %q", requestBody)
	}
}
