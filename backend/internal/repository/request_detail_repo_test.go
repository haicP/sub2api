package repository

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"reflect"
	"regexp"
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
		RequestBody:         requestBody,
		UpstreamRequestBody: upstreamRequestBody,
		ResponseBody:        responseBody,
		ResponseTruncated:   true,
	}

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
			requestBody,
			upstreamRequestBody,
			requestDetailJSONArg(t, responseHeaders),
			responseBody,
			detail.ResponseTruncated,
			detail.ErrorMessage,
		).
		WillReturnRows(sqlmock.NewRows([]string{"id", "created_at"}).AddRow(int64(11), createdAt))

	require.NoError(t, repo.Create(ctx, detail))
	require.Equal(t, int64(11), detail.ID)
	require.Equal(t, createdAt, detail.CreatedAt)

	listRows := requestDetailRows().
		AddRow(int64(11), "req-detail-1", createdAt, completedAt, durationMs, statusCode, true, "openai", "/v1/chat/completions", "/v1/responses", "gpt-5.1", "gpt-5.1-upstream", true, userID, apiKeyID, accountID, groupID, subscriptionID, "127.0.0.1", "sub2api-test", requestDetailMustJSON(t, requestHeaders), "", "", requestDetailMustJSON(t, responseHeaders), "", true, "request failed", int64(len(requestBody)), int64(len(responseBody)))

	mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM request_details WHERE request_id = \\$1 AND user_id = \\$2 AND platform = \\$3").
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
	require.Equal(t, len(requestBody), items[0].RequestBodyBytes)
	require.Equal(t, len(responseBody), items[0].ResponseBodyBytes)

	getRows := requestDetailRows().
		AddRow(int64(11), "req-detail-1", createdAt, completedAt, durationMs, statusCode, true, "openai", "/v1/chat/completions", "/v1/responses", "gpt-5.1", "gpt-5.1-upstream", true, userID, apiKeyID, accountID, groupID, subscriptionID, "127.0.0.1", "sub2api-test", requestDetailMustJSON(t, requestHeaders), requestBody, upstreamRequestBody, requestDetailMustJSON(t, responseHeaders), responseBody, true, "request failed", int64(len(requestBody)), int64(len(responseBody)))

	mock.ExpectQuery("FROM request_details WHERE id = \\$1").
		WithArgs(int64(11)).
		WillReturnRows(getRows)

	got, err := repo.GetByID(ctx, 11)
	require.NoError(t, err)
	require.Equal(t, requestBody, got.RequestBody)
	require.Equal(t, upstreamRequestBody, got.UpstreamRequestBody)
	require.Equal(t, responseBody, got.ResponseBody)
	require.Equal(t, requestHeaders, got.RequestHeaders)
	require.Equal(t, responseHeaders, got.ResponseHeaders)
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

	mock.ExpectQuery("FROM request_details WHERE id = \\$1").
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
		AddRow(int64(1), "req-1", createdAt, nil, nil, 200, true, "anthropic", "/v1/messages", "/v1/messages", "claude", "claude", false, nil, nil, nil, nil, nil, "127.0.0.1", "ua", requestDetailMustJSON(t, map[string][]string{"Content-Type": {"application/json"}}), `{"input":"x"}`, `{"upstream":"y"}`, requestDetailMustJSON(t, map[string][]string{"X-Request-ID": {"1"}}), `{"output":"z"}`, false, "", int64(13), int64(14))

	mock.ExpectQuery("FROM request_details\\s+ORDER BY created_at ASC, id ASC").
		WillReturnRows(rows)

	var streamed []service.RequestDetail
	err := repo.StreamAll(ctx, service.RequestDetailFilters{}, func(detail service.RequestDetail) error {
		streamed = append(streamed, detail)
		return nil
	})
	require.NoError(t, err)
	require.Len(t, streamed, 1)
	require.Equal(t, `{"input":"x"}`, streamed[0].RequestBody)
	require.Equal(t, `{"upstream":"y"}`, streamed[0].UpstreamRequestBody)
	require.Equal(t, `{"output":"z"}`, streamed[0].ResponseBody)
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
		"upstream_request_body",
		"response_headers",
		"response_body",
		"response_truncated",
		"error_message",
		"request_body_bytes",
		"response_body_bytes",
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
