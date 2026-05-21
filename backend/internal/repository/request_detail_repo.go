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
	id, request_id, created_at, completed_at, duration_ms, status_code, success,
	platform, endpoint, upstream_endpoint, model, upstream_model, stream,
	user_id, api_key_id, account_id, group_id, subscription_id, ip_address, user_agent,
	request_headers, %s AS request_body, %s AS upstream_request_body, response_headers, %s AS response_content, %s AS response_body,
	response_truncated, error_message,
	COALESCE(octet_length(request_body), 0), COALESCE(octet_length(response_body), 0)
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

	requestHeaders, err := json.Marshal(nonNilHeaderMap(detail.RequestHeaders))
	if err != nil {
		return fmt.Errorf("marshal request headers: %w", err)
	}
	responseHeaders, err := json.Marshal(nonNilHeaderMap(detail.ResponseHeaders))
	if err != nil {
		return fmt.Errorf("marshal response headers: %w", err)
	}

	query := `
		INSERT INTO request_details (
			request_id, created_at, completed_at, duration_ms, status_code, success,
			platform, endpoint, upstream_endpoint, model, upstream_model, stream,
			user_id, api_key_id, account_id, group_id, subscription_id,
			ip_address, user_agent, request_headers, request_body, upstream_request_body,
			response_headers, response_content, response_body, response_truncated, error_message
		) VALUES (
			$1, $2, $3, $4, $5, $6,
			$7, $8, $9, $10, $11, $12,
			$13, $14, $15, $16, $17,
			$18, $19, $20::jsonb, $21, $22,
			$23::jsonb, $24, $25, $26, $27
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
			error_message = EXCLUDED.error_message
		RETURNING id, created_at
	`

	err = scanSingleRow(
		ctx,
		r.sql,
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
			detail.RequestBody,
			detail.UpstreamRequestBody,
			string(responseHeaders),
			detail.ResponseContent,
			detail.ResponseBody,
			detail.ResponseTruncated,
			detail.ErrorMessage,
		},
		&detail.ID,
		&detail.CreatedAt,
	)
	if err != nil {
		return err
	}
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
	selectColumns := fmt.Sprintf(requestDetailSelectColumns, "''", "''", "''", "''")
	query := fmt.Sprintf(`
		SELECT %s
		FROM request_details %s
		ORDER BY %s %s, id DESC
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
	selectColumns := fmt.Sprintf(requestDetailSelectColumns, "request_body", "upstream_request_body", "response_content", "response_body")
	query := `
		SELECT ` + selectColumns + `
		FROM request_details
		WHERE id = $1`

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
	selectColumns := fmt.Sprintf(requestDetailSelectColumns, "request_body", "upstream_request_body", "response_content", "response_body")
	query := `
		SELECT ` + selectColumns + `
		FROM request_details ` + where + `
		ORDER BY COALESCE(completed_at, created_at) ASC, id ASC
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
			SELECT request_id
			FROM request_details
			WHERE COALESCE(completed_at, created_at) < $1
			ORDER BY COALESCE(completed_at, created_at) ASC, id ASC
			LIMIT $2
		),
		deleted_artifacts AS (
			DELETE FROM request_detail_image_artifacts
			WHERE request_id IN (SELECT request_id FROM victims)
		),
		deleted_details AS (
			DELETE FROM request_details
			WHERE request_id IN (SELECT request_id FROM victims)
			RETURNING 1
		)
		SELECT COUNT(*) FROM deleted_details
	`
	var deleted int64
	if err := scanSingleRow(ctx, r.sql, query, []any{before, limit}, &deleted); err != nil {
		return 0, err
	}
	return deleted, nil
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
		detail              service.RequestDetail
		completedAt         sql.NullTime
		durationMS          sql.NullInt64
		userID              sql.NullInt64
		apiKeyID            sql.NullInt64
		accountID           sql.NullInt64
		groupID             sql.NullInt64
		subscriptionID      sql.NullInt64
		requestHeadersRaw   []byte
		requestBody         string
		upstreamRequestBody string
		responseHeadersRaw  []byte
		responseContent     string
		responseBody        string
		requestBodyBytes    int
		responseBodyBytes   int
	)

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
		&upstreamRequestBody,
		&responseHeadersRaw,
		&responseContent,
		&responseBody,
		&detail.ResponseTruncated,
		&detail.ErrorMessage,
		&requestBodyBytes,
		&responseBodyBytes,
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
	detail.RequestBody = requestBody
	detail.UpstreamRequestBody = upstreamRequestBody
	detail.ResponseContent = responseContent
	detail.ResponseBody = responseBody
	if detail.ResponseContent == "" && detail.ResponseBody != "" {
		detail.ResponseContent = service.ExtractRequestDetailResponseContent(
			service.RequestDetailContext{
				Platform: detail.Platform,
			},
			detail.ResponseBody,
		)
	}
	detail.RequestBodyBytes = requestBodyBytes
	detail.ResponseBodyBytes = responseBodyBytes
	return &detail, nil
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
		add("COALESCE(completed_at, created_at) >= $%d", *filters.StartTime)
	}
	if filters.EndTime != nil {
		add("COALESCE(completed_at, created_at) < $%d", *filters.EndTime)
	}
	if requestID := strings.TrimSpace(filters.RequestID); requestID != "" {
		add("request_id = $%d", requestID)
	}
	if filters.UserID != nil {
		add("user_id = $%d", *filters.UserID)
	}
	if user := strings.TrimSpace(filters.User); user != "" {
		addRepeated("user_id IN (SELECT id FROM users WHERE deleted_at IS NULL AND (email ILIKE '%%' || $%d || '%%' OR username ILIKE '%%' || $%d || '%%'))", user)
	}
	if filters.APIKeyID != nil {
		add("api_key_id = $%d", *filters.APIKeyID)
	}
	if apiKey := strings.TrimSpace(filters.APIKey); apiKey != "" {
		addRepeated("api_key_id IN (SELECT id FROM api_keys WHERE deleted_at IS NULL AND (key ILIKE '%%' || $%d || '%%' OR name ILIKE '%%' || $%d || '%%'))", apiKey)
	}
	if filters.AccountID != nil {
		add("account_id = $%d", *filters.AccountID)
	}
	if filters.GroupID != nil {
		add("group_id = $%d", *filters.GroupID)
	}
	if platform := strings.TrimSpace(filters.Platform); platform != "" {
		add("platform = $%d", platform)
	}
	if model := strings.TrimSpace(filters.Model); model != "" {
		add("model ILIKE '%%' || $%d || '%%'", model)
	}
	if endpoint := strings.TrimSpace(filters.Endpoint); endpoint != "" {
		add("endpoint ILIKE '%%' || $%d || '%%'", endpoint)
	}
	if filters.StatusCode != nil {
		add("status_code = $%d", *filters.StatusCode)
	}
	if filters.Success != nil {
		add("success = $%d", *filters.Success)
	}
	if filters.Stream != nil {
		add("stream = $%d", *filters.Stream)
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
		return "id"
	case "created_at":
		return "created_at"
	case "completed_at":
		return "COALESCE(completed_at, created_at)"
	case "status_code":
		return "status_code"
	case "duration_ms":
		return "duration_ms"
	case "model":
		return "model"
	case "platform":
		return "platform"
	default:
		return "COALESCE(completed_at, created_at)"
	}
}

func normalizeSortOrder(value string) string {
	if strings.EqualFold(strings.TrimSpace(value), pagination.SortOrderAsc) {
		return pagination.SortOrderAsc
	}
	return pagination.SortOrderDesc
}
