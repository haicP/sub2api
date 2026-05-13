-- Migration: 138_add_request_detail_image_artifacts
-- 请求详情图片附件元数据。图片二进制保存到 S3，本表只保存可审计引用。

CREATE TABLE IF NOT EXISTS request_detail_image_artifacts (
    id              BIGSERIAL   PRIMARY KEY,
    request_id      VARCHAR(64) NOT NULL,
    direction       VARCHAR(16) NOT NULL DEFAULT '',
    source          VARCHAR(64) NOT NULL DEFAULT '',
    status          VARCHAR(16) NOT NULL DEFAULT '',
    s3_key          TEXT        NOT NULL DEFAULT '',
    original_url    TEXT        NOT NULL DEFAULT '',
    content_type    VARCHAR(128) NOT NULL DEFAULT '',
    file_name       VARCHAR(255) NOT NULL DEFAULT '',
    size_bytes      BIGINT      NOT NULL DEFAULT 0,
    sha256          VARCHAR(64) NOT NULL DEFAULT '',
    image_index     INTEGER     NULL,
    metadata        JSONB       NOT NULL DEFAULT '{}'::jsonb,
    error_message   TEXT        NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_request_detail_image_artifacts_request_id
    ON request_detail_image_artifacts (request_id, id);

CREATE INDEX IF NOT EXISTS idx_request_detail_image_artifacts_created_at
    ON request_detail_image_artifacts (created_at DESC);

