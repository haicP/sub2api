-- Migration: 140_request_detail_body_blobs
-- Store large request detail bodies as compressed, deduplicated database blobs.

CREATE TABLE IF NOT EXISTS request_detail_body_blobs (
    id                    BIGSERIAL PRIMARY KEY,
    sha256                VARCHAR(64) NOT NULL,
    codec                 VARCHAR(16) NOT NULL DEFAULT 'gzip',
    raw_size_bytes        INTEGER NOT NULL,
    compressed_size_bytes INTEGER NOT NULL,
    content               BYTEA NOT NULL,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_request_detail_body_blobs_sha_size_codec
    ON request_detail_body_blobs (sha256, raw_size_bytes, codec);

ALTER TABLE request_details
    ADD COLUMN IF NOT EXISTS request_body_blob_id BIGINT NULL,
    ADD COLUMN IF NOT EXISTS request_body_sha256 VARCHAR(64) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS request_body_raw_bytes INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS upstream_request_body_blob_id BIGINT NULL,
    ADD COLUMN IF NOT EXISTS upstream_request_body_sha256 VARCHAR(64) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS upstream_request_body_raw_bytes INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS response_content_blob_id BIGINT NULL,
    ADD COLUMN IF NOT EXISTS response_content_sha256 VARCHAR(64) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS response_content_raw_bytes INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS response_body_blob_id BIGINT NULL,
    ADD COLUMN IF NOT EXISTS response_body_sha256 VARCHAR(64) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS response_body_raw_bytes INTEGER NOT NULL DEFAULT 0;
