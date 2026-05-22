package repository

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestRequestDetailRepositoryCreateListAndGet(t *testing.T) {
	ctx := context.Background()
	db, mock := newSQLMock(t)
	repo := NewRequestDetailRepository(nil, db)

	createdAt := time.Date(2026, 5, 12, 8, 0, 0, 0, time.UTC)
	completedAt := createdAt.Add(1500 * time.Millisecond)
	durationMs := 1500
	statusCode := 200
	userID := int64(101)
	apiKeyID := int64(202)
	accountID := int64(303)
	groupID := int64(404)
	subscriptionID := int64(505)

	requestHeaders := map[string][]string{"Authorization": {"Bearer redacted"}, "Content-Type": {"application/json"}}
	responseHeaders := map[string][]string{"X-Request-ID": {"upstream-1"}}
	requestBody := `{"messages":[{"role":"user","content":"full request body"}]}`
	upstreamRequestBody := `{"prompt":"full upstream body"}`
	responseBody := `{"choices":[{"message":{"content":"full response body"}}]}`
	responseContent := "full response body"
	largeRequestBody := strings.Repeat("x", service.RequestDetailBodyCompressionMinSize+32)

	detail := &service.RequestDetail{
		RequestID:           "req-detail-1",
		CreatedAt:           createdAt,
		CompletedAt:         &completedAt,
		DurationMS:          &durationMs,
		StatusCode:          statusCode,
		Success:             true,
		Platform:            "openai",
		Endpoint:            "/v1/chat/completions",
		UpstreamEndpoint:    "/v1/responses",
		Model:               "gpt-5.1",
		UpstreamModel:       "gpt-5.1-upstream",
		Stream:              true,
		UserID:              userID,
		APIKeyID:            apiKeyID,
		AccountID:           accountID,
		GroupID:             &groupID,
		SubscriptionID:      &subscriptionID,
		IPAddress:           "127.0.0.1",
		UserAgent:           "sub2api-test",
		RequestHeaders:      requestHeaders,
		ResponseHeaders:     responseHeaders,
		RequestBody:         largeRequestBody,
		UpstreamRequestBody: largeRequestBody,
		ResponseContent:     responseContent,
		ResponseBody:        responseBody,
		ResponseTruncated:   true,
	}

	mock.ExpectBegin()
	mock.ExpectQuery("INSERT INTO request_detail_body_blobs").
		WithArgs(sqlmock.AnyArg(), service.RequestDetailBodyBlobCodecGzip, len(largeRequestBody), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(91)))
	mock.ExpectQuery("INSERT INTO request_details").
		WithArgs(
			detail.RequestID,
			detail.CreatedAt,
			detail.CompletedAt,
			detail.DurationMS,
			detail.StatusCode,
			detail.Success,
			detail.Platform,
			detail.Endpoint,
			detail.UpstreamEndpoint,
			detail.Model,
			detail.UpstreamModel,
			detail.Stream,
			nullableInt64Arg(&userID),
			nullableInt64Arg(&apiKeyID),
			nullableInt64Arg(&accountID),
			nullableInt64Arg(&groupID),
			nullableInt64Arg(&subscriptionID),
			detail.IPAddress,
			detail.UserAgent,
			requestDetailJSONArg(t, requestHeaders),
			"",
			"",
			requestDetailJSONArg(t, responseHeaders),
			responseContent,
			responseBody,
			detail.ResponseTruncated,
			detail.ErrorMessage,
			int64(91), sqlmock.AnyArg(), len(largeRequestBody),
			int64(91), sqlmock.AnyArg(), len(largeRequestBody),
			nil, "", len(responseContent),
			nil, "", len(responseBody),
		).
		WillReturnRows(sqlmock.NewRows([]string{"id", "created_at"}).AddRow(int64(11), createdAt))
	mock.ExpectCommit()

	require.NoError(t, repo.Create(ctx, detail))
	require.Equal(t, int64(11), detail.ID)
	require.Equal(t, createdAt, detail.CreatedAt)
	require.NotNil(t, detail.RequestBodyRef.BlobID)
	require.NotNil(t, detail.UpstreamRequestRef.BlobID)
	require.Equal(t, *detail.RequestBodyRef.BlobID, *detail.UpstreamRequestRef.BlobID)

	listRows := requestDetailRows().
		AddRow(requestDetailRowValues(t, int64(11), "req-detail-1", createdAt, completedAt, durationMs, statusCode, true, "openai", "/v1/chat/completions", "/v1/responses", "gpt-5.1", "gpt-5.1-upstream", true, userID, apiKeyID, accountID, groupID, subscriptionID, "127.0.0.1", "sub2api-test", requestHeaders, "", "", responseHeaders, "", "", true, "request failed", len(largeRequestBody), len(largeRequestBody), len(responseContent), len(responseBody))...)

	mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM request_details WHERE request_details\\.request_id = \\$1 AND request_details\\.user_id = \\$2 AND request_details\\.platform = \\$3").
		WithArgs("req-detail-1", userID, "openai").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(int64(1)))
	mock.ExpectQuery(regexp.QuoteMeta("'' AS request_body")).
		WithArgs("req-detail-1", userID, "openai", 10, 0).
		WillReturnRows(listRows)

	items, page, err := repo.List(ctx, pagination.PaginationParams{Page: 1, PageSize: 10, SortBy: "created_at", SortOrder: "desc"}, service.RequestDetailFilters{
		RequestID: "req-detail-1",
		Platform:  "openai",
		UserID:    &userID,
	})
	require.NoError(t, err)
	require.NotNil(t, page)
	require.Equal(t, int64(1), page.Total)
	require.Len(t, items, 1)
	require.Empty(t, items[0].RequestBody)
	require.Empty(t, items[0].UpstreamRequestBody)
	require.Empty(t, items[0].ResponseBody)
	require.Equal(t, len(largeRequestBody), items[0].RequestBodyBytes)
	require.Equal(t, len(responseBody), items[0].ResponseBodyBytes)

	getRows := requestDetailRows().
		AddRow(requestDetailRowValues(t, int64(11), "req-detail-1", createdAt, completedAt, durationMs, statusCode, true, "openai", "/v1/chat/completions", "/v1/responses", "gpt-5.1", "gpt-5.1-upstream", true, userID, apiKeyID, accountID, groupID, subscriptionID, "127.0.0.1", "sub2api-test", requestHeaders, requestBody, upstreamRequestBody, responseHeaders, responseContent, responseBody, true, "request failed", len(requestBody), len(upstreamRequestBody), len(responseContent), len(responseBody))...)

	mock.ExpectQuery("FROM request_details").
		WithArgs(int64(11)).
		WillReturnRows(getRows)
	mock.ExpectQuery("FROM request_detail_image_artifacts").
		WithArgs("req-detail-1").
		WillReturnRows(requestDetailImageArtifactRows())

	got, err := repo.GetByID(ctx, 11)
	require.NoError(t, err)
	require.Equal(t, requestBody, got.RequestBody)
	require.Equal(t, upstreamRequestBody, got.UpstreamRequestBody)
	require.Equal(t, responseContent, got.ResponseContent)
	require.Equal(t, responseBody, got.ResponseBody)
	require.Equal(t, requestHeaders, got.RequestHeaders)
	require.Equal(t, responseHeaders, got.ResponseHeaders)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRequestDetailRepositoryListUsesCompletedTimeForDefaultSortAndDateFilters(t *testing.T) {
	ctx := context.Background()
	db, mock := newSQLMock(t)
	repo := NewRequestDetailRepository(nil, db)

	start := time.Date(2026, 5, 14, 5, 12, 0, 0, time.UTC)
	end := start.Add(time.Minute)
	createdAt := start.Add(-30 * time.Second)
	completedAt := start.Add(27 * time.Second)

	mock.ExpectQuery(regexp.QuoteMeta("SELECT COUNT(*) FROM request_details WHERE COALESCE(request_details.completed_at, request_details.created_at) >= $1 AND COALESCE(request_details.completed_at, request_details.created_at) < $2")).
		WithArgs(start, end).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(int64(1)))
	mock.ExpectQuery(regexp.QuoteMeta("ORDER BY COALESCE(request_details.completed_at, request_details.created_at) desc, request_details.id DESC")).
		WithArgs(start, end, 10, 0).
		WillReturnRows(requestDetailRows().
			AddRow(requestDetailRowValues(t, int64(32), "req-image", createdAt, completedAt, 38877, 200, true, "openai", "/v1/images/generations", "/v1/images/generations", "gpt-image-2", "gpt-image-2", false, int64(1), int64(1), nil, nil, nil, "127.0.0.1", "ua", map[string][]string{}, "", "", map[string][]string{}, "", "", false, "", 49, 0, 0, 748)...))

	items, page, err := repo.List(ctx, pagination.PaginationParams{Page: 1, PageSize: 10, SortOrder: "desc"}, service.RequestDetailFilters{
		StartTime: &start,
		EndTime:   &end,
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), page.Total)
	require.Len(t, items, 1)
	require.Equal(t, "req-image", items[0].RequestID)
	require.Equal(t, completedAt, *items[0].CompletedAt)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRequestDetailRepositoryListFiltersByAPIKeyAndUserText(t *testing.T) {
	ctx := context.Background()
	db, mock := newSQLMock(t)
	repo := NewRequestDetailRepository(nil, db)

	mock.ExpectQuery(regexp.QuoteMeta("SELECT COUNT(*) FROM request_details WHERE request_details.user_id IN (SELECT id FROM users WHERE deleted_at IS NULL AND (email ILIKE '%' || $1 || '%' OR username ILIKE '%' || $1 || '%')) AND request_details.api_key_id IN (SELECT id FROM api_keys WHERE deleted_at IS NULL AND (key ILIKE '%' || $2 || '%' OR name ILIKE '%' || $2 || '%'))")).
		WithArgs("admin@example.com", "prod-key").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(int64(0)))

	items, page, err := repo.List(ctx, pagination.PaginationParams{Page: 1, PageSize: 10}, service.RequestDetailFilters{
		User:   "admin@example.com",
		APIKey: "prod-key",
	})
	require.NoError(t, err)
	require.Empty(t, items)
	require.Equal(t, int64(0), page.Total)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRequestDetailRepositoryImageArtifacts(t *testing.T) {
	ctx := context.Background()
	db, mock := newSQLMock(t)
	repo := NewRequestDetailRepository(nil, db)

	now := time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC)
	imageIndex := 2
	artifact := service.RequestDetailImageArtifact{
		RequestID:   "req-img",
		Direction:   "response",
		Source:      "$.data.0.b64_json",
		Status:      "stored",
		S3Key:       "backups/request-detail-images/2026/05/14/req-img/artifact-1.png",
		ContentType: "image/png",
		FileName:    "out.png",
		SizeBytes:   123,
		SHA256:      "abc",
		ImageIndex:  &imageIndex,
		Metadata:    map[string]any{"artifact_ref": "artifact-1"},
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	mock.ExpectExec("DELETE FROM request_detail_image_artifacts").
		WithArgs("req-img").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO request_detail_image_artifacts").
		WithArgs(
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
			requestDetailJSONArg(t, artifact.Metadata),
			artifact.ErrorMessage,
			artifact.CreatedAt,
			artifact.UpdatedAt,
		).
		WillReturnResult(sqlmock.NewResult(1, 1))
	require.NoError(t, repo.CreateImageArtifacts(ctx, []service.RequestDetailImageArtifact{artifact}))

	mock.ExpectQuery("FROM request_detail_image_artifacts").
		WithArgs("req-img").
		WillReturnRows(requestDetailImageArtifactRows().
			AddRow(int64(9), "req-img", "response", "$.data.0.b64_json", "stored", artifact.S3Key, "", "image/png", "out.png", int64(123), "abc", imageIndex, requestDetailMustJSON(t, artifact.Metadata), "", now, now))

	items, err := repo.ListImageArtifactsByRequestID(ctx, "req-img")
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, int64(9), items[0].ID)
	require.Equal(t, artifact.S3Key, items[0].S3Key)
	require.NotNil(t, items[0].ImageIndex)
	require.Equal(t, imageIndex, *items[0].ImageIndex)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRequestDetailRepositoryCreateRejectsEmptyRequestID(t *testing.T) {
	ctx := context.Background()
	db, mock := newSQLMock(t)
	repo := NewRequestDetailRepository(nil, db)

	err := repo.Create(ctx, &service.RequestDetail{RequestID: "   "})
	require.ErrorIs(t, err, service.ErrRequestDetailRequestIDRequired)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRequestDetailRepositoryGetByIDNotFound(t *testing.T) {
	ctx := context.Background()
	db, mock := newSQLMock(t)
	repo := NewRequestDetailRepository(nil, db)

	mock.ExpectQuery("FROM request_details").
		WithArgs(int64(99)).
		WillReturnRows(requestDetailRows())

	got, err := repo.GetByID(ctx, 99)
	require.Nil(t, got)
	require.ErrorIs(t, err, service.ErrRequestDetailNotFound)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRequestDetailRepositoryStreamAllReturnsFullBodies(t *testing.T) {
	ctx := context.Background()
	db, mock := newSQLMock(t)
	repo := NewRequestDetailRepository(nil, db)

	createdAt := time.Date(2026, 5, 12, 9, 0, 0, 0, time.UTC)
	rows := requestDetailRows().
		AddRow(requestDetailRowValues(t, int64(1), "req-1", createdAt, nil, nil, 200, true, "anthropic", "/v1/messages", "/v1/messages", "claude", "claude", false, nil, nil, nil, nil, nil, "127.0.0.1", "ua", map[string][]string{"Content-Type": {"application/json"}}, `{"input":"x"}`, `{"upstream":"y"}`, map[string][]string{"X-Request-ID": {"1"}}, `hello`, `{"output":"z"}`, false, "", 13, len(`{"upstream":"y"}`), len(`hello`), 14)...)

	mock.ExpectQuery("(?s)FROM request_details\\s+LEFT JOIN request_detail_body_blobs rb ON rb\\.id = request_details\\.request_body_blob_id.*ORDER BY request_details\\.created_at ASC, request_details\\.id ASC").
		WillReturnRows(rows)
	mock.ExpectQuery("FROM request_detail_image_artifacts").
		WithArgs("req-1").
		WillReturnRows(requestDetailImageArtifactRows())

	var streamed []service.RequestDetail
	err := repo.StreamAll(ctx, service.RequestDetailFilters{}, func(detail service.RequestDetail) error {
		streamed = append(streamed, detail)
		return nil
	})
	require.NoError(t, err)
	require.Len(t, streamed, 1)
	require.Equal(t, `{"input":"x"}`, streamed[0].RequestBody)
	require.Equal(t, `{"upstream":"y"}`, streamed[0].UpstreamRequestBody)
	require.Equal(t, `hello`, streamed[0].ResponseContent)
	require.Equal(t, `{"output":"z"}`, streamed[0].ResponseBody)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRequestDetailRepositoryDeleteBeforeDeletesDetailsAndArtifacts(t *testing.T) {
	ctx := context.Background()
	db, mock := newSQLMock(t)
	repo := &requestDetailRepository{sql: db}

	cutoff := time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC)
	mock.ExpectQuery(regexp.QuoteMeta("WITH victims AS")).
		WithArgs(cutoff, 5000).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(int64(3)))

	deleted, err := repo.DeleteBefore(ctx, cutoff, 0)
	require.NoError(t, err)
	require.Equal(t, int64(3), deleted)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRequestDetailRepositoryMigrateLegacyBodiesCompressesLargeBodies(t *testing.T) {
	ctx := context.Background()
	db, mock := newSQLMock(t)
	repo := &requestDetailRepository{sql: db}

	largeBody := strings.Repeat("legacy-body-", service.RequestDetailBodyCompressionMinSize/len("legacy-body-")+16)
	mock.ExpectBegin()
	mock.ExpectQuery("(?s)SELECT id,.*raw_size_bytes").
		WithArgs(service.RequestDetailBodyCompressionMinSize, 100).
		WillReturnRows(sqlmock.NewRows([]string{"id", "raw_size_bytes"}).
			AddRow(int64(7), int64(len(largeBody)*2+len("small response content")+len("small response body"))))
	mock.ExpectQuery("SELECT request_body, upstream_request_body, response_content, response_body").
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"request_body", "upstream_request_body", "response_content", "response_body"}).
			AddRow(largeBody, largeBody, "small response content", "small response body"))
	mock.ExpectQuery("INSERT INTO request_detail_body_blobs").
		WithArgs(sqlmock.AnyArg(), service.RequestDetailBodyBlobCodecGzip, len(largeBody), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(91)))
	mock.ExpectExec("UPDATE request_details SET").
		WithArgs(
			int64(7),
			"", int64(91), sqlmock.AnyArg(), len(largeBody),
			"", int64(91), sqlmock.AnyArg(), len(largeBody),
			"small response content", nil, "", len("small response content"),
			"small response body", nil, "", len("small response body"),
		).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	migrated, err := repo.MigrateLegacyBodies(ctx, 100)
	require.NoError(t, err)
	require.Equal(t, int64(1), migrated)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRequestDetailRepositoryMigrateLegacyBodiesLimitsRawBytesPerBatch(t *testing.T) {
	ctx := context.Background()
	db, mock := newSQLMock(t)
	repo := &requestDetailRepository{sql: db}

	largeBody := strings.Repeat("x", service.RequestDetailBodyCompressionMinSize+1)
	mock.ExpectBegin()
	mock.ExpectQuery("(?s)SELECT id,.*raw_size_bytes").
		WithArgs(service.RequestDetailBodyCompressionMinSize, 100).
		WillReturnRows(sqlmock.NewRows([]string{"id", "raw_size_bytes"}).
			AddRow(int64(7), int64(requestDetailLegacyBodyMigrationMaxBatchRawBytes+1)).
			AddRow(int64(8), int64(1024)))
	mock.ExpectQuery("SELECT request_body, upstream_request_body, response_content, response_body").
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"request_body", "upstream_request_body", "response_content", "response_body"}).
			AddRow(largeBody, "", "", ""))
	mock.ExpectQuery("INSERT INTO request_detail_body_blobs").
		WithArgs(sqlmock.AnyArg(), service.RequestDetailBodyBlobCodecGzip, len(largeBody), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(91)))
	mock.ExpectExec("UPDATE request_details SET").
		WithArgs(
			int64(7),
			"", int64(91), sqlmock.AnyArg(), len(largeBody),
			"", nil, "", 0,
			"", nil, "", 0,
			"", nil, "", 0,
		).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	migrated, err := repo.MigrateLegacyBodies(ctx, 100)
	require.NoError(t, err)
	require.Equal(t, int64(1), migrated)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRequestDetailRepositoryDecodeBlobRows(t *testing.T) {
	ctx := context.Background()
	db, mock := newSQLMock(t)
	repo := NewRequestDetailRepository(nil, db)

	createdAt := time.Date(2026, 5, 22, 10, 0, 0, 0, time.UTC)
	largeBody := strings.Repeat("stored-large-body-", 1024)
	ref, err := service.BuildRequestDetailBodyBlob(largeBody)
	require.NoError(t, err)
	blobID := int64(91)

	rows := requestDetailRows().
		AddRow(requestDetailRowValuesWithRefs(t, requestDetailRowInput{
			ID:                         12,
			RequestID:                  "req-blob",
			CreatedAt:                  createdAt,
			StatusCode:                 200,
			Success:                    true,
			Platform:                   "openai",
			Endpoint:                   "/v1/chat/completions",
			UpstreamEndpoint:           "/v1/chat/completions",
			Model:                      "gpt-5.1",
			UpstreamModel:              "gpt-5.1",
			Stream:                     false,
			IPAddress:                  "127.0.0.1",
			UserAgent:                  "ua",
			RequestHeaders:             map[string][]string{},
			ResponseHeaders:            map[string][]string{},
			RequestBodyBytes:           len(largeBody),
			UpstreamRequestBodyBytes:   len(largeBody),
			ResponseContentBytes:       0,
			ResponseBodyBytes:          0,
			RequestBodyBlobID:          blobID,
			RequestBodySHA:             ref.SHA256,
			RequestBodyBlobContent:     ref.Content,
			RequestBodyBlobCodec:       ref.Codec,
			RequestBodyBlobRawSize:     ref.RawSizeBytes,
			RequestBodyBlobCompressed:  ref.CompressedSizeBytes,
			UpstreamBodyBlobID:         blobID,
			UpstreamBodySHA:            ref.SHA256,
			UpstreamBodyBlobContent:    ref.Content,
			UpstreamBodyBlobCodec:      ref.Codec,
			UpstreamBodyBlobRawSize:    ref.RawSizeBytes,
			UpstreamBodyBlobCompressed: ref.CompressedSizeBytes,
			ResponseTruncated:          false,
			ErrorMessage:               "",
		})...)

	mock.ExpectQuery("FROM request_details").
		WithArgs(int64(12)).
		WillReturnRows(rows)
	mock.ExpectQuery("FROM request_detail_image_artifacts").
		WithArgs("req-blob").
		WillReturnRows(requestDetailImageArtifactRows())

	got, err := repo.GetByID(ctx, 12)
	require.NoError(t, err)
	require.Equal(t, largeBody, got.RequestBody)
	require.Equal(t, largeBody, got.UpstreamRequestBody)
	require.Equal(t, len(largeBody), got.RequestBodyBytes)
	require.Equal(t, len(largeBody), got.UpstreamRequestBodyBytes)
	require.NotNil(t, got.RequestBodyRef.BlobID)
	require.Equal(t, blobID, *got.RequestBodyRef.BlobID)
	require.NoError(t, mock.ExpectationsWereMet())
}

func requestDetailRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id",
		"request_id",
		"created_at",
		"completed_at",
		"duration_ms",
		"status_code",
		"success",
		"platform",
		"endpoint",
		"upstream_endpoint",
		"model",
		"upstream_model",
		"stream",
		"user_id",
		"api_key_id",
		"account_id",
		"group_id",
		"subscription_id",
		"ip_address",
		"user_agent",
		"request_headers",
		"request_body",
		"request_body_blob_content",
		"request_body_blob_codec",
		"request_body_blob_raw_size",
		"request_body_blob_compressed_size",
		"upstream_request_body",
		"upstream_request_body_blob_content",
		"upstream_request_body_blob_codec",
		"upstream_request_body_blob_raw_size",
		"upstream_request_body_blob_compressed_size",
		"response_headers",
		"response_content",
		"response_content_blob_content",
		"response_content_blob_codec",
		"response_content_blob_raw_size",
		"response_content_blob_compressed_size",
		"response_body",
		"response_body_blob_content",
		"response_body_blob_codec",
		"response_body_blob_raw_size",
		"response_body_blob_compressed_size",
		"response_truncated",
		"error_message",
		"request_body_bytes",
		"upstream_request_body_bytes",
		"response_content_bytes",
		"response_body_bytes",
		"request_body_blob_id",
		"request_body_sha256",
		"upstream_request_body_blob_id",
		"upstream_request_body_sha256",
		"response_content_blob_id",
		"response_content_sha256",
		"response_body_blob_id",
		"response_body_sha256",
	})
}

func requestDetailRowValues(t *testing.T, values ...any) []driver.Value {
	t.Helper()
	if len(values) != 32 {
		t.Fatalf("requestDetailRowValues got %d values, want 32", len(values))
	}
	return requestDetailRowValuesWithRefs(t, requestDetailRowInput{
		ID:                       values[0],
		RequestID:                values[1],
		CreatedAt:                values[2],
		CompletedAt:              values[3],
		DurationMS:               values[4],
		StatusCode:               values[5],
		Success:                  values[6],
		Platform:                 values[7],
		Endpoint:                 values[8],
		UpstreamEndpoint:         values[9],
		Model:                    values[10],
		UpstreamModel:            values[11],
		Stream:                   values[12],
		UserID:                   values[13],
		APIKeyID:                 values[14],
		AccountID:                values[15],
		GroupID:                  values[16],
		SubscriptionID:           values[17],
		IPAddress:                values[18],
		UserAgent:                values[19],
		RequestHeaders:           values[20],
		RequestBody:              values[21],
		UpstreamRequestBody:      values[22],
		ResponseHeaders:          values[23],
		ResponseContent:          values[24],
		ResponseBody:             values[25],
		ResponseTruncated:        values[26],
		ErrorMessage:             values[27],
		RequestBodyBytes:         values[28],
		UpstreamRequestBodyBytes: values[29],
		ResponseContentBytes:     values[30],
		ResponseBodyBytes:        values[31],
	})
}

type requestDetailRowInput struct {
	ID                            any
	RequestID                     any
	CreatedAt                     any
	CompletedAt                   any
	DurationMS                    any
	StatusCode                    any
	Success                       any
	Platform                      any
	Endpoint                      any
	UpstreamEndpoint              any
	Model                         any
	UpstreamModel                 any
	Stream                        any
	UserID                        any
	APIKeyID                      any
	AccountID                     any
	GroupID                       any
	SubscriptionID                any
	IPAddress                     any
	UserAgent                     any
	RequestHeaders                any
	RequestBody                   any
	RequestBodyBlobContent        any
	RequestBodyBlobCodec          any
	RequestBodyBlobRawSize        any
	RequestBodyBlobCompressed     any
	UpstreamRequestBody           any
	UpstreamBodyBlobContent       any
	UpstreamBodyBlobCodec         any
	UpstreamBodyBlobRawSize       any
	UpstreamBodyBlobCompressed    any
	ResponseHeaders               any
	ResponseContent               any
	ResponseContentBlobContent    any
	ResponseContentBlobCodec      any
	ResponseContentBlobRawSize    any
	ResponseContentBlobCompressed any
	ResponseBody                  any
	ResponseBodyBlobContent       any
	ResponseBodyBlobCodec         any
	ResponseBodyBlobRawSize       any
	ResponseBodyBlobCompressed    any
	ResponseTruncated             any
	ErrorMessage                  any
	RequestBodyBytes              any
	UpstreamRequestBodyBytes      any
	ResponseContentBytes          any
	ResponseBodyBytes             any
	RequestBodyBlobID             any
	RequestBodySHA                any
	UpstreamBodyBlobID            any
	UpstreamBodySHA               any
	ResponseContentBlobID         any
	ResponseContentSHA            any
	ResponseBodyBlobID            any
	ResponseBodySHA               any
}

func requestDetailRowValuesWithRefs(t *testing.T, in requestDetailRowInput) []driver.Value {
	t.Helper()
	requestHeaders := requestDetailMustJSON(t, in.RequestHeaders)
	responseHeaders := requestDetailMustJSON(t, in.ResponseHeaders)
	return []driver.Value{
		in.ID, in.RequestID, in.CreatedAt, in.CompletedAt, in.DurationMS, in.StatusCode, in.Success, in.Platform, in.Endpoint, in.UpstreamEndpoint,
		in.Model, in.UpstreamModel, in.Stream, in.UserID, in.APIKeyID, in.AccountID, in.GroupID, in.SubscriptionID, in.IPAddress, in.UserAgent,
		requestHeaders,
		valueOrDefault(in.RequestBody, ""), valueOrDefault(in.RequestBodyBlobContent, []byte{}), valueOrDefault(in.RequestBodyBlobCodec, ""), valueOrDefault(in.RequestBodyBlobRawSize, 0), valueOrDefault(in.RequestBodyBlobCompressed, 0),
		valueOrDefault(in.UpstreamRequestBody, ""), valueOrDefault(in.UpstreamBodyBlobContent, []byte{}), valueOrDefault(in.UpstreamBodyBlobCodec, ""), valueOrDefault(in.UpstreamBodyBlobRawSize, 0), valueOrDefault(in.UpstreamBodyBlobCompressed, 0),
		responseHeaders,
		valueOrDefault(in.ResponseContent, ""), valueOrDefault(in.ResponseContentBlobContent, []byte{}), valueOrDefault(in.ResponseContentBlobCodec, ""), valueOrDefault(in.ResponseContentBlobRawSize, 0), valueOrDefault(in.ResponseContentBlobCompressed, 0),
		valueOrDefault(in.ResponseBody, ""), valueOrDefault(in.ResponseBodyBlobContent, []byte{}), valueOrDefault(in.ResponseBodyBlobCodec, ""), valueOrDefault(in.ResponseBodyBlobRawSize, 0), valueOrDefault(in.ResponseBodyBlobCompressed, 0),
		in.ResponseTruncated, in.ErrorMessage, in.RequestBodyBytes, in.UpstreamRequestBodyBytes, in.ResponseContentBytes, in.ResponseBodyBytes,
		valueOrDefault(in.RequestBodyBlobID, nil), valueOrDefault(in.RequestBodySHA, ""),
		valueOrDefault(in.UpstreamBodyBlobID, nil), valueOrDefault(in.UpstreamBodySHA, ""),
		valueOrDefault(in.ResponseContentBlobID, nil), valueOrDefault(in.ResponseContentSHA, ""),
		valueOrDefault(in.ResponseBodyBlobID, nil), valueOrDefault(in.ResponseBodySHA, ""),
	}
}

func valueOrDefault(value any, fallback any) any {
	if value == nil {
		return fallback
	}
	return value
}

func requestDetailImageArtifactRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id",
		"request_id",
		"direction",
		"source",
		"status",
		"s3_key",
		"original_url",
		"content_type",
		"file_name",
		"size_bytes",
		"sha256",
		"image_index",
		"metadata",
		"error_message",
		"created_at",
		"updated_at",
	})
}

type requestDetailJSONMatcher string

func (m requestDetailJSONMatcher) Match(v driver.Value) bool {
	b, ok := v.([]byte)
	if !ok {
		s, ok := v.(string)
		if !ok {
			return false
		}
		b = []byte(s)
	}
	var got any
	var want any
	if err := json.Unmarshal(b, &got); err != nil {
		return false
	}
	if err := json.Unmarshal([]byte(m), &want); err != nil {
		return false
	}
	return reflect.DeepEqual(want, got)
}

func requestDetailJSONArg(t *testing.T, value any) requestDetailJSONMatcher {
	t.Helper()
	return requestDetailJSONMatcher(requestDetailMustJSON(t, value))
}

func requestDetailMustJSON(t *testing.T, value any) string {
	t.Helper()
	b, err := json.Marshal(value)
	require.NoError(t, err)
	return string(b)
}
