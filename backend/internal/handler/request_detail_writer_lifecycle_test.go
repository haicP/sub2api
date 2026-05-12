package handler

import (
	"context"
	"fmt"
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
	details []*service.RequestDetail
}

func (s *requestDetailRepoStub) Create(_ context.Context, detail *service.RequestDetail) error {
	if detail != nil {
		copied := *detail
		s.details = append(s.details, &copied)
	}
	return nil
}

func (s *requestDetailRepoStub) List(context.Context, pagination.PaginationParams, service.RequestDetailFilters) ([]service.RequestDetail, *pagination.PaginationResult, error) {
	return nil, nil, nil
}

func (s *requestDetailRepoStub) GetByID(context.Context, int64) (*service.RequestDetail, error) {
	return nil, nil
}

func (s *requestDetailRepoStub) StreamAll(context.Context, service.RequestDetailFilters, func(service.RequestDetail) error) error {
	return nil
}

func writerStatusProbe() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
		_ = c.Writer.Status()
		_ = c.Writer.Written()
	}
}

func TestRequestDetailMiddleware_RestoresOriginalWriter_AfterOpsWrapper_JSON(t *testing.T) {
	gin.SetMode(gin.TestMode)

	repo := &requestDetailRepoStub{}
	svc := service.NewRequestDetailService(repo)
	svc.Start()
	defer svc.Stop()

	r := gin.New()
	r.Use(writerStatusProbe())
	r.Use(OpsErrorLoggerMiddleware(nil))
	r.Use(service.RequestDetailMiddleware(svc))
	r.GET("/json", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/json", nil)
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.JSONEq(t, `{"ok":true}`, rec.Body.String())
	require.Eventually(t, func() bool { return len(repo.details) == 1 }, time.Second, 10*time.Millisecond)
	require.Equal(t, `{"ok":true}`, repo.details[0].ResponseBody)
}

func TestRequestDetailMiddleware_RestoresOriginalWriter_AfterOpsWrapper_SSE(t *testing.T) {
	gin.SetMode(gin.TestMode)

	repo := &requestDetailRepoStub{}
	svc := service.NewRequestDetailService(repo)
	svc.Start()
	defer svc.Stop()

	r := gin.New()
	r.Use(writerStatusProbe())
	r.Use(OpsErrorLoggerMiddleware(nil))
	r.Use(service.RequestDetailMiddleware(svc))
	r.GET("/stream", func(c *gin.Context) {
		c.Header("Content-Type", "text/event-stream")
		c.Status(http.StatusOK)
		for i := 0; i < 2; i++ {
			_, err := fmt.Fprintf(c.Writer, "data: {\"idx\":%d}\n\n", i)
			require.NoError(t, err)
			c.Writer.Flush()
		}
		_, err := fmt.Fprint(c.Writer, "data: [DONE]\n\n")
		require.NoError(t, err)
		c.Writer.Flush()
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/stream", nil)
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "data: {\"idx\":0}")
	require.Contains(t, rec.Body.String(), "data: {\"idx\":1}")
	require.Contains(t, rec.Body.String(), "data: [DONE]")
	require.Eventually(t, func() bool { return len(repo.details) == 1 }, time.Second, 10*time.Millisecond)
	require.Contains(t, repo.details[0].ResponseBody, "data: {\"idx\":0}")
	require.Contains(t, repo.details[0].ResponseBody, "data: [DONE]")
}
