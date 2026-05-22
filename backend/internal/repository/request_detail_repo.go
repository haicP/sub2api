package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

type requestDetailRepository struct {
	client *dbent.Client
	sql    sqlExecutor
}

const requestDetailSelectColumns = `
	request_details.id, request_details.request_id, request_details.created_at, request_details.completed_at,
	request_details.duration_ms, request_details.status_code, request_details.success,
	request_details.platform, request_details.endpoint, request_details.upstream_endpoint,
	request_details.model, request_details.upstream_model, request_details.stream,
	request_details.user_id, request_details.api_key_id, request_details.account_id,
	request_details.group_id, request_details.subscription_id, request_details.ip_address, request_details.user_agent,
	request_details.request_headers, %s, request_details.response_headers, %s,
	request_details.response_truncated, request_details.error_message,
	COALESCE(NULLIF(request_details.request_body_raw_bytes, 0), octet_length(request_details.request_body), 0),
	COALESCE(NULLIF(request_details.upstream_request_body_raw_bytes, 0), octet_length(request_details.upstream_request_body), 0),
	COALESCE(NULLIF(request_details.response_content_raw_bytes, 0), octet_length(request_details.response_content), 0),
	COALESCE(NULLIF(request_details.response_body_raw_bytes, 0), octet_length(request_details.response_body), 0),
	request_details.request_body_blob_id, request_details.request_body_sha256,
	request_details.upstream_request_body_blob_id, request_details.upstream_request_body_sha256,
	request_details.response_content_blob_id, request_details.response_content_sha256,
	request_details.response_body_blob_id, request_details.response_body_sha256
`

const requestDetailRequestBodyListColumns = `
	'' AS request_body,
	''::bytea AS request_body_blob_content,
	'' AS request_body_blob_codec,
	0 AS request_body_blob_raw_size,
	0 AS request_body_blob_compressed_size,
	'' AS upstream_request_body,
	''::bytea AS upstream_request_body_blob_content,
	'' AS upstream_request_body_blob_codec,
	0 AS upstream_request_body_blob_raw_size,
	0 AS upstream_request_body_blob_compressed_size
`

const requestDetailResponseBodyListColumns = `
	'' AS response_content,
	''::bytea AS response_content_blob_content,
	'' AS response_content_blob_codec,
	0 AS response_content_blob_raw_size,
	0 AS response_content_blob_compressed_size,
	'' AS response_body,
	''::bytea AS response_body_blob_content,
	'' AS response_body_blob_codec,
	0 AS response_body_blob_raw_size,
	0 AS response_body_blob_compressed_size
`

const requestDetailRequestBodyFullColumns = `
	request_details.request_body AS request_body,
	COALESCE(rb.content, ''::bytea) AS request_body_blob_content,
	COALESCE(rb.codec, '') AS request_body_blob_codec,
	COALESCE(rb.raw_size_bytes, 0) AS request_body_blob_raw_size,
	COALESCE(rb.compressed_size_bytes, 0) AS request_body_blob_compressed_size,
	request_details.upstream_request_body AS upstream_request_body,
	COALESCE(urb.content, ''::bytea) AS upstream_request_body_blob_content,
	COALESCE(urb.codec, '') AS upstream_request_body_blob_codec,
	COALESCE(urb.raw_size_bytes, 0) AS upstream_request_body_blob_raw_size,
	COALESCE(urb.compressed_size_bytes, 0) AS upstream_request_body_blob_compressed_size
`

const requestDetailResponseBodyFullColumns = `
	request_details.response_content AS response_content,
	COALESCE(rcb.content, ''::bytea) AS response_content_blob_content,
	COALESCE(rcb.codec, '') AS response_content_blob_codec,
	COALESCE(rcb.raw_size_bytes, 0) AS response_content_blob_raw_size,
	COALESCE(rcb.compressed_size_bytes, 0) AS response_content_blob_compressed_size,
	request_details.response_body AS response_body,
	COALESCE(rsb.content, ''::bytea) AS response_body_blob_content,
	COALESCE(rsb.codec, '') AS response_body_blob_codec,
	COALESCE(rsb.raw_size_bytes, 0) AS response_body_blob_raw_size,
	COALESCE(rsb.compressed_size_bytes, 0) AS response_body_blob_compressed_size
`

const requestDetailBodyJoins = `
	LEFT JOIN request_detail_body_blobs rb ON rb.id = request_details.request_body_blob_id
	LEFT JOIN request_detail_body_blobs urb ON urb.id = request_details.upstream_request_body_blob_id
	LEFT JOIN request_detail_body_blobs rcb ON rcb.id = request_details.response_content_blob_id
	LEFT JOIN request_detail_body_blobs rsb ON rsb.id = request_details.response_body_blob_id
`

func NewRequestDetailRepository(client *dbent.Client, sqlDB *sql.DB) service.RequestDetailRepository {
	return &requestDetailRepository{client: client, sql: sqlDB}
}

func (r *requestDetailRepository) Create(ctx context.Context, detail *service.RequestDetail) error {
	if detail == nil {
		return nil
	}
	if strings.TrimSpace(detail.RequestID) == "" {
		return service.ErrRequestDetailRequestIDRequired
	}
	db, ok := r.sql.(*sql.DB)
	if !ok {
		return r.createWithExecutor(ctx, r.sql, detail)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := r.createWithExecutor(ctx, tx, detail); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		_ = tx.Rollback()
		return err
	}
	return nil
}

func (r *requestDetailRepository) createWithExecutor(ctx context.Context, exec sqlExecutor, detail *service.RequestDetail) error {
	requestHeaders, err := json.Marshal(nonNilHeaderMap(detail.RequestHeaders))
	if err != nil {
		return fmt.Errorf("marshal request headers: %w", err)
	}
	responseHeaders, err := json.Marshal(nonNilHeaderMap(detail.ResponseHeaders))
	if err != nil {
		return fmt.Errorf("marshal response headers: %w", err)
	}
	preparedBodies, err := prepareRequestDetailBodies(ctx, exec, detail)
	if err != nil {
		return err
	}

	query := `
		INSERT INTO request_details (
			request_id, created_at, completed_at, duration_ms, status_code, success,
			platform, endpoint, upstream_endpoint, model, upstream_model, stream,
			user_id, api_key_id, account_id, group_id, subscription_id,
			ip_address, user_agent, request_headers, request_body, upstream_request_body,
			response_headers, response_content, response_body, response_truncated, error_message,
			request_body_blob_id, request_body_sha256, request_body_raw_bytes,
			upstream_request_body_blob_id, upstream_request_body_sha256, upstream_request_body_raw_bytes,
			response_content_blob_id, response_content_sha256, response_content_raw_bytes,
			response_body_blob_id, response_body_sha256, response_body_raw_bytes
		) VALUES (
			$1, $2, $3, $4, $5, $6,
			$7, $8, $9, $10, $11, $12,
			$13, $14, $15, $16, $17,
			$18, $19, $20::jsonb, $21, $22,
			$23::jsonb, $24, $25, $26, $27,
			$28, $29, $30,
			$31, $32, $33,
			$34, $35, $36,
			$37, $38, $39
		)
		ON CONFLICT (request_id) DO UPDATE SET
			completed_at = EXCLUDED.completed_at,
			duration_ms = EXCLUDED.duration_ms,
			status_code = EXCLUDED.status_code,
			success = EXCLUDED.success,
			platform = EXCLUDED.platform,
			endpoint = EXCLUDED.endpoint,
			upstream_endpoint = EXCLUDED.upstream_endpoint,
			model = EXCLUDED.model,
			upstream_model = EXCLUDED.upstream_model,
			stream = EXCLUDED.stream,
			user_id = EXCLUDED.user_id,
			api_key_id = EXCLUDED.api_key_id,
			account_id = EXCLUDED.account_id,
			group_id = EXCLUDED.group_id,
			subscription_id = EXCLUDED.subscription_id,
			ip_address = EXCLUDED.ip_address,
			user_agent = EXCLUDED.user_agent,
			request_headers = EXCLUDED.request_headers,
			request_body = EXCLUDED.request_body,
			upstream_request_body = EXCLUDED.upstream_request_body,
			response_headers = EXCLUDED.response_headers,
			response_content = EXCLUDED.response_content,
			response_body = EXCLUDED.response_body,
			response_truncated = EXCLUDED.response_truncated,
			error_message = EXCLUDED.error_message,
			request_body_blob_id = EXCLUDED.request_body_blob_id,
			request_body_sha256 = EXCLUDED.request_body_sha256,
			request_body_raw_bytes = EXCLUDED.request_body_raw_bytes,
			upstream_request_body_blob_id = EXCLUDED.upstream_request_body_blob_id,
			upstream_request_body_sha256 = EXCLUDED.upstream_request_body_sha256,
			upstream_request_body_raw_bytes = EXCLUDED.upstream_request_body_raw_bytes,
			response_content_blob_id = EXCLUDED.response_content_blob_id,
			response_content_sha256 = EXCLUDED.response_content_sha256,
			response_content_raw_bytes = EXCLUDED.response_content_raw_bytes,
			response_body_blob_id = EXCLUDED.response_body_blob_id,
			response_body_sha256 = EXCLUDED.response_body_sha256,
			response_body_raw_bytes = EXCLUDED.response_body_raw_bytes
		RETURNING id, created_at
	`

	err = scanSingleRow(
		ctx,
		exec,
		query,
		[]any{
			detail.RequestID,
			detail.CreatedAt,
			detail.CompletedAt,
			nullInt(detail.DurationMS),
			detail.StatusCode,
			detail.Success,
			detail.Platform,
			detail.Endpoint,
			detail.UpstreamEndpoint,
			detail.Model,
			detail.UpstreamModel,
			detail.Stream,
			nullableInt64Value(detail.UserID),
			nullableInt64Value(detail.APIKeyID),
			nullableInt64Value(detail.AccountID),
			detail.GroupID,
			detail.SubscriptionID,
			detail.IPAddress,
			detail.UserAgent,
			string(requestHeaders),
			preparedBodies.requestBody.inline,
			preparedBodies.upstreamRequestBody.inline,
			string(responseHeaders),
			preparedBodies.responseContent.inline,
			preparedBodies.responseBody.inline,
			detail.ResponseTruncated,
			detail.ErrorMessage,
			nullableInt64Ptr(preparedBodies.requestBody.ref.BlobID),
			preparedBodies.requestBody.ref.SHA256,
			preparedBodies.requestBody.ref.RawSizeBytes,
			nullableInt64Ptr(preparedBodies.upstreamRequestBody.ref.BlobID),
			preparedBodies.upstreamRequestBody.ref.SHA256,
			preparedBodies.upstreamRequestBody.ref.RawSizeBytes,
			nullableInt64Ptr(preparedBodies.responseContent.ref.BlobID),
			preparedBodies.responseContent.ref.SHA256,
			preparedBodies.responseContent.ref.RawSizeBytes,
			nullableInt64Ptr(preparedBodies.responseBody.ref.BlobID),
			preparedBodies.responseBody.ref.SHA256,
			preparedBodies.responseBody.ref.RawSizeBytes,
		},
		&detail.ID,
		&detail.CreatedAt,
	)
	if err != nil {
		return err
	}
	detail.RequestBodyRef = preparedBodies.requestBody.ref
	detail.UpstreamRequestRef = preparedBodies.upstreamRequestBody.ref
	detail.ResponseContentRef = preparedBodies.responseContent.ref
	detail.ResponseBodyRef = preparedBodies.responseBody.ref
	detail.RequestBodyBytes = preparedBodies.requestBody.ref.RawSizeBytes
	detail.UpstreamRequestBodyBytes = preparedBodies.upstreamRequestBody.ref.RawSizeBytes
	detail.ResponseContentBytes = preparedBodies.responseContent.ref.RawSizeBytes
	detail.ResponseBodyBytes = preparedBodies.responseBody.ref.RawSizeBytes
	return nil
}

func (r *requestDetailRepository) List(ctx context.Context, params pagination.PaginationParams, filters service.RequestDetailFilters) ([]service.RequestDetail, *pagination.PaginationResult, error) {
	where, args := buildRequestDetailWhere(filters)
	total, err := countRequestDetails(ctx, r.sql, where, args)
	if err != nil {
		return nil, nil, err
	}
	if total == 0 {
		return []service.RequestDetail{}, paginationResultFromTotal(0, params), nil
	}

	sortBy := normalizeRequestDetailSort(params.SortBy)
	sortOrder := normalizeSortOrder(params.SortOrder)
	selectColumns := fmt.Sprintf(requestDetailSelectColumns, requestDetailRequestBodyListColumns, requestDetailResponseBodyListColumns)
	query := fmt.Sprintf(`
		SELECT %s
		FROM request_details %s
		ORDER BY %s %s, request_details.id DESC
		LIMIT $%d OFFSET $%d
	`, selectColumns, where, sortBy, sortOrder, len(args)+1, len(args)+2)
	queryArgs := append(append([]any{}, args...), params.Limit(), params.Offset())

	rows, err := r.sql.QueryContext(ctx, query, queryArgs...)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = rows.Close() }()

	items, err := scanRequestDetailRows(rows)
	if err != nil {
		return nil, nil, err
	}
	return items, paginationResultFromTotal(total, params), nil
}

func (r *requestDetailRepository) GetByID(ctx context.Context, id int64) (*service.RequestDetail, error) {
	selectColumns := fmt.Sprintf(requestDetailSelectColumns, requestDetailRequestBodyFullColumns, requestDetailResponseBodyFullColumns)
	query := `
		SELECT ` + selectColumns + `
		FROM request_details
		` + requestDetailBodyJoins + `
		WHERE request_details.id = $1`

	rows, err := r.sql.QueryContext(ctx, query, id)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, err
		}
		return nil, service.ErrRequestDetailNotFound
	}

	detail, err := scanRequestDetail(rows)
	if err != nil {
		return nil, err
	}
	artifacts, err := r.ListImageArtifactsByRequestID(ctx, detail.RequestID)
	if err != nil {
		return nil, err
	}
	detail.ImageArtifacts = artifacts
	return detail, nil
}

func (r *requestDetailRepository) StreamAll(ctx context.Context, filters service.RequestDetailFilters, write func(service.RequestDetail) error) error {
	where, args := buildRequestDetailWhere(filters)
	selectColumns := fmt.Sprintf(requestDetailSelectColumns, requestDetailRequestBodyFullColumns, requestDetailResponseBodyFullColumns)
	query := `
		SELECT ` + selectColumns + `
		FROM request_details
		` + requestDetailBodyJoins + `
		` + where + `
		ORDER BY COALESCE(request_details.completed_at, request_details.created_at) ASC, request_details.id ASC
	`
	rows, err := r.sql.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		detail, err := scanRequestDetail(rows)
		if err != nil {
			return err
		}
		artifacts, err := r.ListImageArtifactsByRequestID(ctx, detail.RequestID)
		if err != nil {
			return err
		}
		detail.ImageArtifacts = artifacts
		if err := write(*detail); err != nil {
			return err
		}
	}
	return rows.Err()
}

func (r *requestDetailRepository) DeleteBefore(ctx context.Context, before time.Time, limit int) (int64, error) {
	if limit <= 0 {
		limit = 5000
	}
	query := `
		WITH victims AS (
			SELECT request_id, request_body_blob_id, upstream_request_body_blob_id,
				response_content_blob_id, response_body_blob_id
			FROM request_details
			WHERE COALESCE(request_details.completed_at, request_details.created_at) < $1
			ORDER BY COALESCE(request_details.completed_at, request_details.created_at) ASC, request_details.id ASC
			LIMIT $2
		),
		victim_blobs AS (
			SELECT DISTINCT blob_id
			FROM (
				SELECT request_body_blob_id AS blob_id FROM victims
				UNION ALL
				SELECT upstream_request_body_blob_id AS blob_id FROM victims
				UNION ALL
				SELECT response_content_blob_id AS blob_id FROM victims
				UNION ALL
				SELECT response_body_blob_id AS blob_id FROM victims
			) ids
			WHERE blob_id IS NOT NULL
		),
		deleted_artifacts AS (
			DELETE FROM request_detail_image_artifacts
			WHERE request_id IN (SELECT request_id FROM victims)
		),
		deleted_details AS (
			DELETE FROM request_details
			WHERE request_id IN (SELECT request_id FROM victims)
			RETURNING 1
		),
		cleanup_blobs AS (
			DELETE FROM request_detail_body_blobs b
			WHERE b.id IN (SELECT blob_id FROM victim_blobs)
			  AND NOT EXISTS (
				SELECT 1
				FROM request_details d
				WHERE (
					d.request_body_blob_id = b.id
					OR d.upstream_request_body_blob_id = b.id
					OR d.response_content_blob_id = b.id
					OR d.response_body_blob_id = b.id
				)
				AND d.request_id NOT IN (SELECT request_id FROM victims)
			)
		)
		SELECT COUNT(*) FROM deleted_details
	`
	var deleted int64
	if err := scanSingleRow(ctx, r.sql, query, []any{before, limit}, &deleted); err != nil {
		return 0, err
	}
	return deleted, nil
}

func (r *requestDetailRepository) MigrateLegacyBodies(ctx context.Context, limit int) (int64, error) {
	if limit <= 0 {
		limit = 100
	}
	db, ok := r.sql.(*sql.DB)
	if !ok {
		return r.migrateLegacyBodiesWithExecutor(ctx, r.sql, limit)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	migrated, err := r.migrateLegacyBodiesWithExecutor(ctx, tx, limit)
	if err != nil {
		_ = tx.Rollback()
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		_ = tx.Rollback()
		return 0, err
	}
	return migrated, nil
}

func (r *requestDetailRepository) migrateLegacyBodiesWithExecutor(ctx context.Context, exec sqlExecutor, limit int) (int64, error) {
	rows, err := exec.QueryContext(ctx, `
		SELECT id, request_body, upstream_request_body, response_content, response_body
		FROM request_details
		WHERE (request_body_blob_id IS NULL AND octet_length(request_body) >= $1)
		   OR (upstream_request_body_blob_id IS NULL AND octet_length(upstream_request_body) >= $1)
		   OR (response_content_blob_id IS NULL AND octet_length(response_content) >= $1)
		   OR (response_body_blob_id IS NULL AND octet_length(response_body) >= $1)
		ORDER BY id ASC
		LIMIT $2
		FOR UPDATE SKIP LOCKED
	`, service.RequestDetailBodyCompressionMinSize, limit)
	if err != nil {
		return 0, err
	}

	type legacyRow struct {
		id                  int64
		requestBody         string
		upstreamRequestBody string
		responseContent     string
		responseBody        string
	}
	var items []legacyRow
	for rows.Next() {
		var item legacyRow
		if err := rows.Scan(&item.id, &item.requestBody, &item.upstreamRequestBody, &item.responseContent, &item.responseBody); err != nil {
			_ = rows.Close()
			return 0, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return 0, err
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}

	var migrated int64
	for _, item := range items {
		detail := &service.RequestDetail{
			RequestBody:         item.requestBody,
			UpstreamRequestBody: item.upstreamRequestBody,
			ResponseContent:     item.responseContent,
			ResponseBody:        item.responseBody,
		}
		prepared, err := prepareRequestDetailBodies(ctx, exec, detail)
		if err != nil {
			return migrated, err
		}
		if _, err := exec.ExecContext(ctx, `
			UPDATE request_details SET
				request_body = $2,
				request_body_blob_id = $3,
				request_body_sha256 = $4,
				request_body_raw_bytes = $5,
				upstream_request_body = $6,
				upstream_request_body_blob_id = $7,
				upstream_request_body_sha256 = $8,
				upstream_request_body_raw_bytes = $9,
				response_content = $10,
				response_content_blob_id = $11,
				response_content_sha256 = $12,
				response_content_raw_bytes = $13,
				response_body = $14,
				response_body_blob_id = $15,
				response_body_sha256 = $16,
				response_body_raw_bytes = $17
			WHERE id = $1
		`,
			item.id,
			prepared.requestBody.inline,
			nullableInt64Ptr(prepared.requestBody.ref.BlobID),
			prepared.requestBody.ref.SHA256,
			prepared.requestBody.ref.RawSizeBytes,
			prepared.upstreamRequestBody.inline,
			nullableInt64Ptr(prepared.upstreamRequestBody.ref.BlobID),
			prepared.upstreamRequestBody.ref.SHA256,
			prepared.upstreamRequestBody.ref.RawSizeBytes,
			prepared.responseContent.inline,
			nullableInt64Ptr(prepared.responseContent.ref.BlobID),
			prepared.responseContent.ref.SHA256,
			prepared.responseContent.ref.RawSizeBytes,
			prepared.responseBody.inline,
			nullableInt64Ptr(prepared.responseBody.ref.BlobID),
			prepared.responseBody.ref.SHA256,
			prepared.responseBody.ref.RawSizeBytes,
		); err != nil {
			return migrated, err
		}
		migrated++
	}
	return migrated, nil
}

func (r *requestDetailRepository) CreateImageArtifacts(ctx context.Context, artifacts []service.RequestDetailImageArtifact) error {
	if len(artifacts) == 0 {
		return nil
	}
	requestIDs := make(map[string]struct{})
	for _, artifact := range artifacts {
		requestID := strings.TrimSpace(artifact.RequestID)
		if requestID != "" {
			requestIDs[requestID] = struct{}{}
		}
	}
	for requestID := range requestIDs {
		if _, err := r.sql.ExecContext(ctx, `DELETE FROM request_detail_image_artifacts WHERE request_id = $1`, requestID); err != nil {
			return err
		}
	}
	query := `
		INSERT INTO request_detail_image_artifacts (
			request_id, direction, source, status, s3_key, original_url,
			content_type, file_name, size_bytes, sha256, image_index,
			metadata, error_message, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6,
			$7, $8, $9, $10, $11,
			$12::jsonb, $13, $14, $15
		)
	`
	for _, artifact := range artifacts {
		if strings.TrimSpace(artifact.RequestID) == "" {
			continue
		}
		metadata, err := json.Marshal(nonNilMetadataMap(artifact.Metadata))
		if err != nil {
			return fmt.Errorf("marshal image artifact metadata: %w", err)
		}
		createdAt := artifact.CreatedAt
		if createdAt.IsZero() {
			createdAt = timeNowUTC()
		}
		updatedAt := artifact.UpdatedAt
		if updatedAt.IsZero() {
			updatedAt = createdAt
		}
		if _, err := r.sql.ExecContext(
			ctx,
			query,
			artifact.RequestID,
			artifact.Direction,
			artifact.Source,
			artifact.Status,
			artifact.S3Key,
			artifact.OriginalURL,
			artifact.ContentType,
			artifact.FileName,
			artifact.SizeBytes,
			artifact.SHA256,
			artifact.ImageIndex,
			string(metadata),
			artifact.ErrorMessage,
			createdAt,
			updatedAt,
		); err != nil {
			return err
		}
	}
	return nil
}

func (r *requestDetailRepository) ListImageArtifactsByRequestID(ctx context.Context, requestID string) ([]service.RequestDetailImageArtifact, error) {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return []service.RequestDetailImageArtifact{}, nil
	}
	rows, err := r.sql.QueryContext(ctx, `
		SELECT id, request_id, direction, source, status, s3_key, original_url,
			content_type, file_name, size_bytes, sha256, image_index,
			metadata, error_message, created_at, updated_at
		FROM request_detail_image_artifacts
		WHERE request_id = $1
		ORDER BY id ASC
	`, requestID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	return scanRequestDetailImageArtifactRows(rows)
}

func (r *requestDetailRepository) GetImageArtifact(ctx context.Context, requestID string, artifactID int64) (*service.RequestDetailImageArtifact, error) {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" || artifactID <= 0 {
		return nil, service.ErrRequestDetailNotFound
	}
	rows, err := r.sql.QueryContext(ctx, `
		SELECT id, request_id, direction, source, status, s3_key, original_url,
			content_type, file_name, size_bytes, sha256, image_index,
			metadata, error_message, created_at, updated_at
		FROM request_detail_image_artifacts
		WHERE request_id = $1 AND id = $2
	`, requestID, artifactID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, err
		}
		return nil, service.ErrRequestDetailNotFound
	}
	item, err := scanRequestDetailImageArtifact(rows)
	if err != nil {
		return nil, err
	}
	return item, rows.Err()
}

type requestDetailScanner interface {
	Scan(dest ...any) error
}

type requestDetailImageArtifactScanner interface {
	Scan(dest ...any) error
}

type preparedRequestDetailBody struct {
	inline string
	ref    service.RequestDetailBodyRef
}

type preparedRequestDetailBodies struct {
	requestBody         preparedRequestDetailBody
	upstreamRequestBody preparedRequestDetailBody
	responseContent     preparedRequestDetailBody
	responseBody        preparedRequestDetailBody
}

func prepareRequestDetailBodies(ctx context.Context, exec sqlExecutor, detail *service.RequestDetail) (*preparedRequestDetailBodies, error) {
	cache := map[string]service.RequestDetailBodyRef{}
	prepare := func(raw string) (preparedRequestDetailBody, error) {
		ref := service.RequestDetailBodyRef{RawSizeBytes: len([]byte(raw))}
		if raw == "" || len([]byte(raw)) < service.RequestDetailBodyCompressionMinSize {
			return preparedRequestDetailBody{inline: raw, ref: ref}, nil
		}
		built, err := service.BuildRequestDetailBodyBlob(raw)
		if err != nil {
			return preparedRequestDetailBody{}, err
		}
		cacheKey := built.SHA256 + ":" + fmt.Sprintf("%d", built.RawSizeBytes) + ":" + built.Codec
		if cached, ok := cache[cacheKey]; ok {
			return preparedRequestDetailBody{ref: cached}, nil
		}
		blobID, err := upsertRequestDetailBodyBlob(ctx, exec, *built)
		if err != nil {
			return preparedRequestDetailBody{}, err
		}
		built.BlobID = &blobID
		built.Content = nil
		cache[cacheKey] = *built
		return preparedRequestDetailBody{ref: *built}, nil
	}

	requestBody, err := prepare(detail.RequestBody)
	if err != nil {
		return nil, fmt.Errorf("prepare request body: %w", err)
	}
	upstreamRequestBody, err := prepare(detail.UpstreamRequestBody)
	if err != nil {
		return nil, fmt.Errorf("prepare upstream request body: %w", err)
	}
	responseContent, err := prepare(detail.ResponseContent)
	if err != nil {
		return nil, fmt.Errorf("prepare response content: %w", err)
	}
	responseBody, err := prepare(detail.ResponseBody)
	if err != nil {
		return nil, fmt.Errorf("prepare response body: %w", err)
	}
	return &preparedRequestDetailBodies{
		requestBody:         requestBody,
		upstreamRequestBody: upstreamRequestBody,
		responseContent:     responseContent,
		responseBody:        responseBody,
	}, nil
}

func upsertRequestDetailBodyBlob(ctx context.Context, exec sqlExecutor, ref service.RequestDetailBodyRef) (int64, error) {
	var id int64
	query := `
		INSERT INTO request_detail_body_blobs (sha256, codec, raw_size_bytes, compressed_size_bytes, content)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (sha256, raw_size_bytes, codec) DO UPDATE SET
			compressed_size_bytes = request_detail_body_blobs.compressed_size_bytes
		RETURNING id
	`
	if err := scanSingleRow(ctx, exec, query, []any{
		ref.SHA256,
		ref.Codec,
		ref.RawSizeBytes,
		ref.CompressedSizeBytes,
		ref.Content,
	}, &id); err != nil {
		return 0, err
	}
	return id, nil
}

func scanRequestDetailRows(rows *sql.Rows) ([]service.RequestDetail, error) {
	items := make([]service.RequestDetail, 0)
	for rows.Next() {
		item, err := scanRequestDetail(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func scanRequestDetailImageArtifactRows(rows *sql.Rows) ([]service.RequestDetailImageArtifact, error) {
	items := make([]service.RequestDetailImageArtifact, 0)
	for rows.Next() {
		item, err := scanRequestDetailImageArtifact(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func scanRequestDetailImageArtifact(scanner requestDetailImageArtifactScanner) (*service.RequestDetailImageArtifact, error) {
	var (
		item        service.RequestDetailImageArtifact
		imageIndex  sql.NullInt64
		metadataRaw []byte
	)
	if err := scanner.Scan(
		&item.ID,
		&item.RequestID,
		&item.Direction,
		&item.Source,
		&item.Status,
		&item.S3Key,
		&item.OriginalURL,
		&item.ContentType,
		&item.FileName,
		&item.SizeBytes,
		&item.SHA256,
		&imageIndex,
		&metadataRaw,
		&item.ErrorMessage,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if imageIndex.Valid {
		v := int(imageIndex.Int64)
		item.ImageIndex = &v
	}
	if len(metadataRaw) > 0 {
		item.Metadata = map[string]any{}
		if err := json.Unmarshal(metadataRaw, &item.Metadata); err != nil {
			return nil, fmt.Errorf("unmarshal image artifact metadata: %w", err)
		}
	}
	return &item, nil
}

func scanRequestDetail(scanner requestDetailScanner) (*service.RequestDetail, error) {
	var (
		detail                    service.RequestDetail
		completedAt               sql.NullTime
		durationMS                sql.NullInt64
		userID                    sql.NullInt64
		apiKeyID                  sql.NullInt64
		accountID                 sql.NullInt64
		groupID                   sql.NullInt64
		subscriptionID            sql.NullInt64
		requestHeadersRaw         []byte
		requestBody               string
		upstreamRequestBody       string
		responseHeadersRaw        []byte
		responseContent           string
		responseBody              string
		requestBodyBytes          int
		upstreamRequestBodyBytes  int
		responseContentBytes      int
		responseBodyBytes         int
		requestBodyBlobID         sql.NullInt64
		requestBodySHA            string
		upstreamRequestBodyBlobID sql.NullInt64
		upstreamRequestBodySHA    string
		responseContentBlobID     sql.NullInt64
		responseContentSHA        string
		responseBodyBlobID        sql.NullInt64
		responseBodySHA           string
	)
	requestBlob := requestDetailScannedBody{}
	upstreamBlob := requestDetailScannedBody{}
	responseContentBlob := requestDetailScannedBody{}
	responseBlob := requestDetailScannedBody{}

	if err := scanner.Scan(
		&detail.ID,
		&detail.RequestID,
		&detail.CreatedAt,
		&completedAt,
		&durationMS,
		&detail.StatusCode,
		&detail.Success,
		&detail.Platform,
		&detail.Endpoint,
		&detail.UpstreamEndpoint,
		&detail.Model,
		&detail.UpstreamModel,
		&detail.Stream,
		&userID,
		&apiKeyID,
		&accountID,
		&groupID,
		&subscriptionID,
		&detail.IPAddress,
		&detail.UserAgent,
		&requestHeadersRaw,
		&requestBody,
		&requestBlob.content,
		&requestBlob.codec,
		&requestBlob.rawSize,
		&requestBlob.compressedSize,
		&upstreamRequestBody,
		&upstreamBlob.content,
		&upstreamBlob.codec,
		&upstreamBlob.rawSize,
		&upstreamBlob.compressedSize,
		&responseHeadersRaw,
		&responseContent,
		&responseContentBlob.content,
		&responseContentBlob.codec,
		&responseContentBlob.rawSize,
		&responseContentBlob.compressedSize,
		&responseBody,
		&responseBlob.content,
		&responseBlob.codec,
		&responseBlob.rawSize,
		&responseBlob.compressedSize,
		&detail.ResponseTruncated,
		&detail.ErrorMessage,
		&requestBodyBytes,
		&upstreamRequestBodyBytes,
		&responseContentBytes,
		&responseBodyBytes,
		&requestBodyBlobID,
		&requestBodySHA,
		&upstreamRequestBodyBlobID,
		&upstreamRequestBodySHA,
		&responseContentBlobID,
		&responseContentSHA,
		&responseBodyBlobID,
		&responseBodySHA,
	); err != nil {
		return nil, err
	}

	if completedAt.Valid {
		detail.CompletedAt = &completedAt.Time
	}
	if durationMS.Valid {
		v := int(durationMS.Int64)
		detail.DurationMS = &v
	}
	if userID.Valid {
		detail.UserID = userID.Int64
	}
	if apiKeyID.Valid {
		detail.APIKeyID = apiKeyID.Int64
	}
	if accountID.Valid {
		detail.AccountID = accountID.Int64
	}
	if groupID.Valid {
		v := groupID.Int64
		detail.GroupID = &v
	}
	if subscriptionID.Valid {
		v := subscriptionID.Int64
		detail.SubscriptionID = &v
	}
	if len(requestHeadersRaw) > 0 {
		detail.RequestHeaders = map[string][]string{}
		if err := json.Unmarshal(requestHeadersRaw, &detail.RequestHeaders); err != nil {
			return nil, fmt.Errorf("unmarshal request headers: %w", err)
		}
	}
	if len(responseHeadersRaw) > 0 {
		detail.ResponseHeaders = map[string][]string{}
		if err := json.Unmarshal(responseHeadersRaw, &detail.ResponseHeaders); err != nil {
			return nil, fmt.Errorf("unmarshal response headers: %w", err)
		}
	}
	detail.RequestBodyRef = buildScannedBodyRef(requestBodyBlobID, requestBodySHA, requestBodyBytes, requestBlob)
	detail.UpstreamRequestRef = buildScannedBodyRef(upstreamRequestBodyBlobID, upstreamRequestBodySHA, upstreamRequestBodyBytes, upstreamBlob)
	detail.ResponseContentRef = buildScannedBodyRef(responseContentBlobID, responseContentSHA, responseContentBytes, responseContentBlob)
	detail.ResponseBodyRef = buildScannedBodyRef(responseBodyBlobID, responseBodySHA, responseBodyBytes, responseBlob)
	var err error
	if detail.RequestBody, err = decodeScannedBody(requestBody, detail.RequestBodyRef); err != nil {
		return nil, fmt.Errorf("decode request body: %w", err)
	}
	if detail.UpstreamRequestBody, err = decodeScannedBody(upstreamRequestBody, detail.UpstreamRequestRef); err != nil {
		return nil, fmt.Errorf("decode upstream request body: %w", err)
	}
	if detail.ResponseContent, err = decodeScannedBody(responseContent, detail.ResponseContentRef); err != nil {
		return nil, fmt.Errorf("decode response content: %w", err)
	}
	if detail.ResponseBody, err = decodeScannedBody(responseBody, detail.ResponseBodyRef); err != nil {
		return nil, fmt.Errorf("decode response body: %w", err)
	}
	if detail.ResponseContent == "" && detail.ResponseBody != "" {
		detail.ResponseContent = service.ExtractRequestDetailResponseContent(
			service.RequestDetailContext{
				Platform: detail.Platform,
			},
			detail.ResponseBody,
		)
	}
	detail.RequestBodyBytes = requestBodyBytes
	detail.UpstreamRequestBodyBytes = upstreamRequestBodyBytes
	detail.ResponseContentBytes = responseContentBytes
	detail.ResponseBodyBytes = responseBodyBytes
	return &detail, nil
}

type requestDetailScannedBody struct {
	content        []byte
	codec          string
	rawSize        int
	compressedSize int
}

func buildScannedBodyRef(blobID sql.NullInt64, sha string, fallbackRawSize int, body requestDetailScannedBody) service.RequestDetailBodyRef {
	ref := service.RequestDetailBodyRef{
		SHA256:              sha,
		RawSizeBytes:        fallbackRawSize,
		CompressedSizeBytes: body.compressedSize,
		Codec:               body.codec,
		Content:             body.content,
	}
	if body.rawSize > 0 {
		ref.RawSizeBytes = body.rawSize
	}
	if blobID.Valid {
		v := blobID.Int64
		ref.BlobID = &v
	}
	return ref
}

func decodeScannedBody(inline string, ref service.RequestDetailBodyRef) (string, error) {
	if len(ref.Content) == 0 {
		return inline, nil
	}
	return service.DecodeRequestDetailBodyBlob(ref)
}

func buildRequestDetailWhere(filters service.RequestDetailFilters) (string, []any) {
	conditions := make([]string, 0, 14)
	args := make([]any, 0, 14)
	add := func(format string, value any) {
		args = append(args, value)
		conditions = append(conditions, fmt.Sprintf(format, len(args)))
	}
	addRepeated := func(format string, value any) {
		args = append(args, value)
		conditions = append(conditions, fmt.Sprintf(format, len(args), len(args)))
	}

	if filters.StartTime != nil {
		add("COALESCE(request_details.completed_at, request_details.created_at) >= $%d", *filters.StartTime)
	}
	if filters.EndTime != nil {
		add("COALESCE(request_details.completed_at, request_details.created_at) < $%d", *filters.EndTime)
	}
	if requestID := strings.TrimSpace(filters.RequestID); requestID != "" {
		add("request_details.request_id = $%d", requestID)
	}
	if filters.UserID != nil {
		add("request_details.user_id = $%d", *filters.UserID)
	}
	if user := strings.TrimSpace(filters.User); user != "" {
		addRepeated("request_details.user_id IN (SELECT id FROM users WHERE deleted_at IS NULL AND (email ILIKE '%%' || $%d || '%%' OR username ILIKE '%%' || $%d || '%%'))", user)
	}
	if filters.APIKeyID != nil {
		add("request_details.api_key_id = $%d", *filters.APIKeyID)
	}
	if apiKey := strings.TrimSpace(filters.APIKey); apiKey != "" {
		addRepeated("request_details.api_key_id IN (SELECT id FROM api_keys WHERE deleted_at IS NULL AND (key ILIKE '%%' || $%d || '%%' OR name ILIKE '%%' || $%d || '%%'))", apiKey)
	}
	if filters.AccountID != nil {
		add("request_details.account_id = $%d", *filters.AccountID)
	}
	if filters.GroupID != nil {
		add("request_details.group_id = $%d", *filters.GroupID)
	}
	if platform := strings.TrimSpace(filters.Platform); platform != "" {
		add("request_details.platform = $%d", platform)
	}
	if model := strings.TrimSpace(filters.Model); model != "" {
		add("request_details.model ILIKE '%%' || $%d || '%%'", model)
	}
	if endpoint := strings.TrimSpace(filters.Endpoint); endpoint != "" {
		add("request_details.endpoint ILIKE '%%' || $%d || '%%'", endpoint)
	}
	if filters.StatusCode != nil {
		add("request_details.status_code = $%d", *filters.StatusCode)
	}
	if filters.Success != nil {
		add("request_details.success = $%d", *filters.Success)
	}
	if filters.Stream != nil {
		add("request_details.stream = $%d", *filters.Stream)
	}

	if len(conditions) == 0 {
		return "", args
	}
	return "WHERE " + strings.Join(conditions, " AND "), args
}

func countRequestDetails(ctx context.Context, sqlq sqlQueryer, where string, args []any) (int64, error) {
	query := "SELECT COUNT(*) FROM request_details"
	if where != "" {
		query += " " + where
	}
	var total int64
	if err := scanSingleRow(ctx, sqlq, query, args, &total); err != nil {
		return 0, err
	}
	return total, nil
}

func nullableInt64Value(value int64) any {
	if value == 0 {
		return nil
	}
	return value
}

func nullableInt64Ptr(value *int64) any {
	if value == nil || *value == 0 {
		return nil
	}
	return *value
}

func nonNilHeaderMap(value map[string][]string) map[string][]string {
	if value == nil {
		return map[string][]string{}
	}
	return value
}

func nonNilMetadataMap(value map[string]any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	return value
}

func timeNowUTC() time.Time {
	return time.Now().UTC()
}

func normalizeRequestDetailSort(value string) string {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "id":
		return "request_details.id"
	case "created_at":
		return "request_details.created_at"
	case "completed_at":
		return "COALESCE(request_details.completed_at, request_details.created_at)"
	case "status_code":
		return "request_details.status_code"
	case "duration_ms":
		return "request_details.duration_ms"
	case "model":
		return "request_details.model"
	case "platform":
		return "request_details.platform"
	default:
		return "COALESCE(request_details.completed_at, request_details.created_at)"
	}
}

func normalizeSortOrder(value string) string {
	if strings.EqualFold(strings.TrimSpace(value), pagination.SortOrderAsc) {
		return pagination.SortOrderAsc
	}
	return pagination.SortOrderDesc
}
