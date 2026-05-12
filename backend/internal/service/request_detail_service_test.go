package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/stretchr/testify/require"
)

type requestDetailRepoStub struct {
	mu      sync.Mutex
	created []RequestDetail
}

func (s *requestDetailRepoStub) Create(_ context.Context, detail *RequestDetail) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.created = append(s.created, *detail)
	return nil
}

func (s *requestDetailRepoStub) List(context.Context, pagination.PaginationParams, RequestDetailFilters) ([]RequestDetail, *pagination.PaginationResult, error) {
	return nil, nil, nil
}

func (s *requestDetailRepoStub) GetByID(context.Context, int64) (*RequestDetail, error) {
	return nil, nil
}

func (s *requestDetailRepoStub) StreamAll(context.Context, RequestDetailFilters, func(RequestDetail) error) error {
	return nil
}

func TestRequestDetailServiceCreateAsyncFlushesOnStop(t *testing.T) {
	repo := &requestDetailRepoStub{}
	svc := NewRequestDetailService(repo)

	svc.Start()
	require.True(t, svc.Enqueue(&RequestDetail{RequestID: "req-async", CreatedAt: time.Now()}))
	svc.Stop()

	require.Len(t, repo.created, 1)
	require.Equal(t, "req-async", repo.created[0].RequestID)
}

func TestRequestDetailServiceEnqueueReturnsFalseAfterStop(t *testing.T) {
	repo := &requestDetailRepoStub{}
	svc := NewRequestDetailService(repo)

	svc.Start()
	svc.Stop()

	require.False(t, svc.Enqueue(&RequestDetail{RequestID: "req-after-stop", CreatedAt: time.Now()}))
	require.Len(t, repo.created, 0)
}
