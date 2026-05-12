package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type requestDetailRepoStub struct {
	listFilters service.RequestDetailFilters
}

func (s *requestDetailRepoStub) Create(context.Context, *service.RequestDetail) error {
	return nil
}

func (s *requestDetailRepoStub) List(_ context.Context, params pagination.PaginationParams, filters service.RequestDetailFilters) ([]service.RequestDetail, *pagination.PaginationResult, error) {
	s.listFilters = filters
	return []service.RequestDetail{{
		ID:        1,
		RequestID: "req-1",
		CreatedAt: time.Now(),
		Platform:  "openai",
		Model:     "gpt-test",
	}}, &pagination.PaginationResult{Total: 1, Page: params.Page, PageSize: params.PageSize, Pages: 1}, nil
}

func (s *requestDetailRepoStub) GetByID(context.Context, int64) (*service.RequestDetail, error) {
	return &service.RequestDetail{ID: 1, RequestID: "req-1", RequestBody: `{"input":"hello"}`, ResponseBody: `{"output":"hi"}`}, nil
}

func (s *requestDetailRepoStub) StreamAll(context.Context, service.RequestDetailFilters, func(service.RequestDetail) error) error {
	return nil
}

func TestRequestDetailHandlerListParsesFilters(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &requestDetailRepoStub{}
	h := NewRequestDetailHandler(service.NewRequestDetailService(repo))
	r := gin.New()
	r.GET("/admin/request-details", h.List)

	req := httptest.NewRequest(http.MethodGet, "/admin/request-details?platform=openai&model=gpt-test&success=true&stream=false", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "openai", repo.listFilters.Platform)
	require.Equal(t, "gpt-test", repo.listFilters.Model)
	require.NotNil(t, repo.listFilters.Success)
	require.True(t, *repo.listFilters.Success)
	require.NotNil(t, repo.listFilters.Stream)
	require.False(t, *repo.listFilters.Stream)
}
