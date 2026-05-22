-- Migration: 141_request_detail_body_blob_indexes_notx
-- Build request detail blob indexes concurrently to avoid blocking production writes.

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_request_detail_body_blobs_created_at
    ON request_detail_body_blobs (created_at DESC);

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_request_details_request_body_blob_id
    ON request_details (request_body_blob_id)
    WHERE request_body_blob_id IS NOT NULL;

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_request_details_upstream_request_body_blob_id
    ON request_details (upstream_request_body_blob_id)
    WHERE upstream_request_body_blob_id IS NOT NULL;

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_request_details_response_content_blob_id
    ON request_details (response_content_blob_id)
    WHERE response_content_blob_id IS NOT NULL;

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_request_details_response_body_blob_id
    ON request_details (response_body_blob_id)
    WHERE response_body_blob_id IS NOT NULL;
