-- usage_logs.request_body / response_body 存储网关捕获的客户端请求体与上游响应体
-- （受系统设置 usage_body_capture_enabled 开关控制，按 usage_body_capture_max_bytes 截断）。
-- *_truncated 标记内容因超出捕获上限被截断。
-- NULL 表示未开启捕获时的历史行或未捕获的路径；可空无默认，走 metadata-only 加列。
ALTER TABLE usage_logs
    ADD COLUMN IF NOT EXISTS request_body TEXT;
ALTER TABLE usage_logs
    ADD COLUMN IF NOT EXISTS response_body TEXT;
ALTER TABLE usage_logs
    ADD COLUMN IF NOT EXISTS request_body_truncated BOOLEAN;
ALTER TABLE usage_logs
    ADD COLUMN IF NOT EXISTS response_body_truncated BOOLEAN;
