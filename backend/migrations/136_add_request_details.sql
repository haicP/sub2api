-- Migration: 136_add_request_details
-- 创建请求明细表，用于按 request_id 保存单次请求的调试信息。

CREATE TABLE IF NOT EXISTS request_details (
    id                    BIGSERIAL   PRIMARY KEY,
    request_id            VARCHAR(64) NOT NULL UNIQUE,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at          TIMESTAMPTZ NULL,
    duration_ms           INTEGER     NULL,
    status_code           INTEGER     NOT NULL DEFAULT 0,
    success               BOOLEAN     NOT NULL DEFAULT FALSE,
    platform              VARCHAR(32) NOT NULL DEFAULT '',
    endpoint              VARCHAR(255) NOT NULL DEFAULT '',
    upstream_endpoint     VARCHAR(1024) NOT NULL DEFAULT '',
    model                 VARCHAR(255) NOT NULL DEFAULT '',
    upstream_model        VARCHAR(255) NOT NULL DEFAULT '',
    stream                BOOLEAN     NOT NULL DEFAULT FALSE,
    user_id               BIGINT      NULL,
    api_key_id            BIGINT      NULL,
    account_id            BIGINT      NULL,
    group_id              BIGINT      NULL,
    subscription_id       BIGINT      NULL,
    ip_address            VARCHAR(45) NOT NULL DEFAULT '',
    user_agent            VARCHAR(512) NOT NULL DEFAULT '',
    request_headers       JSONB       NOT NULL DEFAULT '{}'::jsonb,
    response_headers      JSONB       NOT NULL DEFAULT '{}'::jsonb,
    request_body          TEXT        NOT NULL DEFAULT '',
    upstream_request_body TEXT        NOT NULL DEFAULT '',
    response_body         TEXT        NOT NULL DEFAULT '',
    error_message         TEXT        NOT NULL DEFAULT '',
    response_truncated    BOOLEAN     NOT NULL DEFAULT FALSE
);

CREATE INDEX IF NOT EXISTS idx_request_details_created_at
    ON request_details (created_at DESC);

CREATE INDEX IF NOT EXISTS idx_request_details_user_created_at
    ON request_details (user_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_request_details_api_key_created_at
    ON request_details (api_key_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_request_details_account_created_at
    ON request_details (account_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_request_details_model_created_at
    ON request_details (model, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_request_details_platform_created_at
    ON request_details (platform, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_request_details_status_created_at
    ON request_details (status_code, created_at DESC);
