package service

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"sync/atomic"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
)

var ErrRequestDetailNotFound = infraerrors.NotFound("REQUEST_DETAIL_NOT_FOUND", "request detail not found")
var ErrRequestDetailRequestIDRequired = infraerrors.BadRequest("REQUEST_DETAIL_REQUEST_ID_REQUIRED", "request_id is required")

type RequestDetail struct {
	ID                  int64                        `json:"id"`
	RequestID           string                       `json:"request_id"`
	CreatedAt           time.Time                    `json:"created_at"`
	CompletedAt         *time.Time                   `json:"completed_at,omitempty"`
	DurationMS          *int                         `json:"duration_ms,omitempty"`
	StatusCode          int                          `json:"status_code"`
	Success             bool                         `json:"success"`
	Platform            string                       `json:"platform"`
	Endpoint            string                       `json:"endpoint"`
	UpstreamEndpoint    string                       `json:"upstream_endpoint"`
	Model               string                       `json:"model"`
	UpstreamModel       string                       `json:"upstream_model"`
	Stream              bool                         `json:"stream"`
	UserID              int64                        `json:"user_id,omitempty"`
	APIKeyID            int64                        `json:"api_key_id,omitempty"`
	AccountID           int64                        `json:"account_id,omitempty"`
	GroupID             *int64                       `json:"group_id,omitempty"`
	SubscriptionID      *int64                       `json:"subscription_id,omitempty"`
	IPAddress           string                       `json:"ip_address"`
	UserAgent           string                       `json:"user_agent"`
	RequestHeaders      map[string][]string          `json:"request_headers,omitempty"`
	RequestBody         string                       `json:"request_body,omitempty"`
	UpstreamRequestBody string                       `json:"upstream_request_body,omitempty"`
	ResponseHeaders     map[string][]string          `json:"response_headers,omitempty"`
	ResponseContent     string                       `json:"response_content,omitempty"`
	ResponseBody        string                       `json:"response_body,omitempty"`
	ResponseTruncated   bool                         `json:"response_truncated"`
	ErrorMessage        string                       `json:"error_message,omitempty"`
	RequestBodyBytes    int                          `json:"request_body_bytes,omitempty"`
	ResponseBodyBytes   int                          `json:"response_body_bytes,omitempty"`
	ImageArtifacts      []RequestDetailImageArtifact `json:"image_artifacts,omitempty"`
}

type RequestDetailImageArtifact struct {
	ID           int64          `json:"id"`
	RequestID    string         `json:"request_id"`
	Direction    string         `json:"direction"`
	Source       string         `json:"source"`
	Status       string         `json:"status"`
	S3Key        string         `json:"s3_key"`
	OriginalURL  string         `json:"original_url,omitempty"`
	ContentType  string         `json:"content_type"`
	FileName     string         `json:"file_name,omitempty"`
	SizeBytes    int64          `json:"size_bytes"`
	SHA256       string         `json:"sha256,omitempty"`
	ImageIndex   *int           `json:"image_index,omitempty"`
	Metadata     map[string]any `json:"metadata,omitempty"`
	ErrorMessage string         `json:"error_message,omitempty"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
}

type RequestDetailFilters struct {
	StartTime *time.Time
	EndTime   *time.Time

	RequestID string
	User      string
	UserID    *int64
	APIKey    string
	APIKeyID  *int64
	AccountID *int64
	GroupID   *int64

	Platform   string
	Model      string
	Endpoint   string
	StatusCode *int
	Success    *bool
	Stream     *bool
}

type RequestDetailRepository interface {
	Create(ctx context.Context, detail *RequestDetail) error
	List(ctx context.Context, params pagination.PaginationParams, filters RequestDetailFilters) ([]RequestDetail, *pagination.PaginationResult, error)
	GetByID(ctx context.Context, id int64) (*RequestDetail, error)
	StreamAll(ctx context.Context, filters RequestDetailFilters, write func(RequestDetail) error) error
	CreateImageArtifacts(ctx context.Context, artifacts []RequestDetailImageArtifact) error
	ListImageArtifactsByRequestID(ctx context.Context, requestID string) ([]RequestDetailImageArtifact, error)
	GetImageArtifact(ctx context.Context, requestID string, artifactID int64) (*RequestDetailImageArtifact, error)
}

type RequestDetailService struct {
	repo          RequestDetailRepository
	backupService *BackupService
	queue         chan *RequestDetail
	stopCh        chan struct{}
	doneCh        chan struct{}
	started       atomic.Bool
	stopped       atomic.Bool
}

func NewRequestDetailService(repo RequestDetailRepository) *RequestDetailService {
	return &RequestDetailService{
		repo:   repo,
		queue:  make(chan *RequestDetail, 1024),
		stopCh: make(chan struct{}),
		doneCh: make(chan struct{}),
	}
}

func (s *RequestDetailService) SetBackupService(backupService *BackupService) {
	if s == nil {
		return
	}
	s.backupService = backupService
}

func (s *RequestDetailService) Start() {
	if s == nil || !s.started.CompareAndSwap(false, true) {
		return
	}
	s.stopped.Store(false)
	go s.run()
}

func (s *RequestDetailService) Stop() {
	if s == nil || !s.started.CompareAndSwap(true, false) {
		return
	}
	s.stopped.Store(true)
	close(s.stopCh)
	<-s.doneCh
}

func (s *RequestDetailService) Enqueue(detail *RequestDetail) bool {
	if s == nil || detail == nil || s.stopped.Load() {
		return false
	}
	select {
	case s.queue <- detail:
		return true
	default:
		return false
	}
}

func (s *RequestDetailService) Create(ctx context.Context, detail *RequestDetail) error {
	if s == nil || s.repo == nil || detail == nil {
		return nil
	}
	if strings.TrimSpace(detail.RequestID) == "" {
		return ErrRequestDetailRequestIDRequired
	}
	return s.repo.Create(ctx, detail)
}

func (s *RequestDetailService) List(ctx context.Context, params pagination.PaginationParams, filters RequestDetailFilters) ([]RequestDetail, *pagination.PaginationResult, error) {
	return s.repo.List(ctx, params, filters)
}

func (s *RequestDetailService) GetByID(ctx context.Context, id int64) (*RequestDetail, error) {
	return s.repo.GetByID(ctx, id)
}

func (s *RequestDetailService) GetImageArtifactDownloadURL(ctx context.Context, requestID string, artifactID int64) (string, error) {
	if s == nil || s.repo == nil || s.backupService == nil {
		return "", ErrBackupS3NotConfigured
	}
	artifact, err := s.repo.GetImageArtifact(ctx, requestID, artifactID)
	if err != nil {
		return "", err
	}
	if artifact == nil || strings.TrimSpace(artifact.S3Key) == "" || artifact.Status != "stored" {
		return "", ErrRequestDetailNotFound
	}
	store, _, err := s.backupService.NewConfiguredObjectStore(ctx)
	if err != nil {
		return "", err
	}
	return store.PresignURL(ctx, artifact.S3Key, time.Hour)
}

func (s *RequestDetailService) StreamAll(ctx context.Context, filters RequestDetailFilters, write func(RequestDetail) error) error {
	if s == nil || s.repo == nil {
		return nil
	}
	return s.repo.StreamAll(ctx, filters, write)
}

func (s *RequestDetailService) WriteBackupNDJSON(ctx context.Context, filters RequestDetailFilters, w io.Writer) error {
	enc := json.NewEncoder(w)
	return s.repo.StreamAll(ctx, filters, func(detail RequestDetail) error {
		return enc.Encode(detail)
	})
}

func (s *RequestDetailService) run() {
	defer close(s.doneCh)
	for {
		select {
		case detail, ok := <-s.queue:
			if !ok {
				return
			}
			s.persist(detail)
		case <-s.stopCh:
			for {
				select {
				case detail, ok := <-s.queue:
					if !ok {
						return
					}
					s.persist(detail)
				default:
					return
				}
			}
		}
	}
}

func (s *RequestDetailService) persist(detail *RequestDetail) {
	if s == nil || s.repo == nil || detail == nil {
		return
	}
	s.prepareImageArtifacts(detail)
	if err := s.repo.Create(context.Background(), detail); err != nil {
		logger.LegacyPrintf("service.request_detail", "persist request detail failed request_id=%s err=%v", detail.RequestID, err)
		return
	}
	if len(detail.ImageArtifacts) > 0 {
		if err := s.repo.CreateImageArtifacts(context.Background(), detail.ImageArtifacts); err != nil {
			logger.LegacyPrintf("service.request_detail", "persist request detail image artifacts failed request_id=%s err=%v", detail.RequestID, err)
		}
	}
}
