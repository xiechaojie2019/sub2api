package service

import (
	"context"
	"testing"
)

type fakeUsageBodyCaptureSettingRepo struct {
	values map[string]string
}

func (f *fakeUsageBodyCaptureSettingRepo) Get(ctx context.Context, key string) (*Setting, error) {
	return nil, ErrSettingNotFound
}

func (f *fakeUsageBodyCaptureSettingRepo) GetValue(ctx context.Context, key string) (string, error) {
	return f.values[key], nil
}

func (f *fakeUsageBodyCaptureSettingRepo) Set(ctx context.Context, key, value string) error {
	return nil
}

func (f *fakeUsageBodyCaptureSettingRepo) GetMultiple(ctx context.Context, keys []string) (map[string]string, error) {
	out := make(map[string]string, len(keys))
	for _, k := range keys {
		out[k] = f.values[k]
	}
	return out, nil
}

func (f *fakeUsageBodyCaptureSettingRepo) SetMultiple(ctx context.Context, settings map[string]string) error {
	return nil
}

func (f *fakeUsageBodyCaptureSettingRepo) GetAll(ctx context.Context) (map[string]string, error) {
	return f.values, nil
}

func (f *fakeUsageBodyCaptureSettingRepo) Delete(ctx context.Context, key string) error { return nil }

func TestClampUsageBodyCaptureMaxBytes(t *testing.T) {
	cases := []struct {
		raw  string
		want int
	}{
		{"", UsageBodyCaptureDefaultMaxBytes},
		{"abc", UsageBodyCaptureDefaultMaxBytes},
		{"0", UsageBodyCaptureDefaultMaxBytes},
		{"-5", UsageBodyCaptureDefaultMaxBytes},
		{"100", UsageBodyCaptureMinMaxBytes},
		{"8192", 8192},
		{"99999999", UsageBodyCaptureMaxMaxBytes},
	}
	for _, tc := range cases {
		if got := ClampUsageBodyCaptureMaxBytes(tc.raw); got != tc.want {
			t.Fatalf("ClampUsageBodyCaptureMaxBytes(%q) = %d, want %d", tc.raw, got, tc.want)
		}
	}
}

func TestUsageBodyCaptureRuntime_FailClosedAndEnabled(t *testing.T) {
	// repo 错误时 fail-closed：开关默认关闭。
	svc := NewSettingService(&failingUsageBodyCaptureRepo{}, nil)
	runtime := svc.UsageBodyCaptureRuntime(context.Background())
	if runtime.Enabled {
		t.Fatal("expected capture disabled when settings read fails")
	}
	if runtime.MaxBytes != UsageBodyCaptureDefaultMaxBytes {
		t.Fatalf("unexpected default max bytes: %d", runtime.MaxBytes)
	}

	// 开启 + 自定义上限。
	svc2 := NewSettingService(&fakeUsageBodyCaptureSettingRepo{values: map[string]string{
		SettingKeyUsageBodyCaptureEnabled:  "true",
		SettingKeyUsageBodyCaptureMaxBytes: "4096",
	}}, nil)
	runtime2 := svc2.UsageBodyCaptureRuntime(context.Background())
	if !runtime2.Enabled || runtime2.MaxBytes != 4096 {
		t.Fatalf("unexpected runtime: %+v", runtime2)
	}
}

type failingUsageBodyCaptureRepo struct {
	fakeUsageBodyCaptureSettingRepo
}

func (f *failingUsageBodyCaptureRepo) GetMultiple(ctx context.Context, keys []string) (map[string]string, error) {
	return nil, context.DeadlineExceeded
}

func TestUsageBodyCapture_SnapshotAndTruncation(t *testing.T) {
	capture := NewUsageBodyCapture(8)
	capture.SetRequestBody([]byte("0123456789"))
	request, requestCaptured, reqTrunc, _, _, _ := capture.Snapshot()
	if request != "01234567" || !requestCaptured || !reqTrunc {
		t.Fatalf("unexpected request snapshot: %q captured=%v trunc=%v", request, requestCaptured, reqTrunc)
	}

	capture.AppendResponseChunk([]byte("abcdefghij"))
	capture.AppendResponseChunk([]byte("klmno"))
	_, _, _, resp, responseCaptured, respTrunc := capture.Snapshot()
	if resp != "abcdefgh" || !responseCaptured || !respTrunc {
		t.Fatalf("unexpected response snapshot: %q trunc=%v", resp, respTrunc)
	}
}

func TestApplyUsageBodyCapture(t *testing.T) {
	// 无捕获缓冲时 no-op。
	log := &UsageLog{}
	ApplyUsageBodyCapture(context.Background(), log)
	if log.RequestBody != nil || log.ResponseBody != nil {
		t.Fatal("expected no-op without capture in context")
	}

	capture := NewUsageBodyCapture(32)
	capture.SetRequestBody([]byte(`{"model":"gpt-5"}`))
	capture.AppendResponseChunk([]byte(`{"id":"resp"}`))
	ctx := WithUsageBodyCapture(context.Background(), capture)

	log2 := &UsageLog{}
	ApplyUsageBodyCapture(ctx, log2)
	if log2.RequestBody == nil || *log2.RequestBody != `{"model":"gpt-5"}` {
		t.Fatalf("unexpected request body: %+v", log2.RequestBody)
	}
	if log2.ResponseBody == nil || *log2.ResponseBody != `{"id":"resp"}` {
		t.Fatalf("unexpected response body: %+v", log2.ResponseBody)
	}
	if log2.RequestBodyTruncated || log2.ResponseBodyTruncated {
		t.Fatal("unexpected truncation flags")
	}
}
