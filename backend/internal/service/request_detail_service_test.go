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
	mu            sync.Mutex
	created       []RequestDetail
	deletedBefore []time.Time
	deleteLimit   []int
	deleteCount   int64
	migrateLimit  []int
	migrateCounts []int64
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

func (s *requestDetailRepoStub) CreateImageArtifacts(context.Context, []RequestDetailImageArtifact) error {
	return nil
}

func (s *requestDetailRepoStub) ListImageArtifactsByRequestID(context.Context, string) ([]RequestDetailImageArtifact, error) {
	return nil, nil
}

func (s *requestDetailRepoStub) GetImageArtifact(context.Context, string, int64) (*RequestDetailImageArtifact, error) {
	return nil, nil
}

func (s *requestDetailRepoStub) DeleteBefore(_ context.Context, before time.Time, limit int) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deletedBefore = append(s.deletedBefore, before)
	s.deleteLimit = append(s.deleteLimit, limit)
	deleted := s.deleteCount
	s.deleteCount = 0
	return deleted, nil
}

func (s *requestDetailRepoStub) MigrateLegacyBodies(_ context.Context, limit int) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.migrateLimit = append(s.migrateLimit, limit)
	if len(s.migrateCounts) == 0 {
		return 0, nil
	}
	migrated := s.migrateCounts[0]
	s.migrateCounts = s.migrateCounts[1:]
	return migrated, nil
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

func TestRequestDetailServiceCleansExpiredDetailsOnStart(t *testing.T) {
	repo := &requestDetailRepoStub{}
	svc := NewRequestDetailService(repo, RequestDetailRetentionConfig{
		RetentionDays:    7,
		CleanupInterval:  time.Hour,
		CleanupBatchSize: 123,
	})

	startedAt := time.Now()
	svc.Start()
	defer svc.Stop()

	require.Eventually(t, func() bool {
		repo.mu.Lock()
		defer repo.mu.Unlock()
		return len(repo.deletedBefore) == 1
	}, time.Second, 10*time.Millisecond)

	repo.mu.Lock()
	defer repo.mu.Unlock()
	require.Len(t, repo.deleteLimit, 1)
	require.Equal(t, 123, repo.deleteLimit[0])
	require.WithinDuration(t, startedAt.AddDate(0, 0, -7), repo.deletedBefore[0], 2*time.Second)
}

func TestRequestDetailServiceMigratesOneLegacyBodyBatchPerTick(t *testing.T) {
	repo := &requestDetailRepoStub{migrateCounts: []int64{100, 100}}
	svc := NewRequestDetailService(repo)

	svc.migrateLegacyBodiesOnce()

	repo.mu.Lock()
	defer repo.mu.Unlock()
	require.Equal(t, []int{100}, repo.migrateLimit)
	require.Equal(t, []int64{100}, repo.migrateCounts)
}
