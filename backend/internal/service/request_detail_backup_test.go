package service

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type requestDetailBackupRepoStub struct {
	RequestDetailRepository
	mu      sync.Mutex
	items   []RequestDetail
	filters []RequestDetailFilters
}

func (s *requestDetailBackupRepoStub) StreamAll(_ context.Context, filters RequestDetailFilters, write func(RequestDetail) error) error {
	s.mu.Lock()
	s.filters = append(s.filters, filters)
	s.mu.Unlock()
	for _, item := range s.items {
		if err := write(item); err != nil {
			return err
		}
	}
	return nil
}

type requestDetailBackupSettingRepoStub struct {
	mu   sync.Mutex
	data map[string]string
}

func newRequestDetailBackupSettingRepoStub() *requestDetailBackupSettingRepoStub {
	return &requestDetailBackupSettingRepoStub{data: map[string]string{}}
}

func (s *requestDetailBackupSettingRepoStub) Get(_ context.Context, key string) (*Setting, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.data[key]
	if !ok {
		return nil, ErrSettingNotFound
	}
	return &Setting{Key: key, Value: value}, nil
}

func (s *requestDetailBackupSettingRepoStub) GetValue(_ context.Context, key string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data[key], nil
}

func (s *requestDetailBackupSettingRepoStub) Set(_ context.Context, key string, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[key] = value
	return nil
}

func (s *requestDetailBackupSettingRepoStub) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]string{}
	for _, key := range keys {
		if value, ok := s.data[key]; ok {
			out[key] = value
		}
	}
	return out, nil
}

func (s *requestDetailBackupSettingRepoStub) SetMultiple(_ context.Context, settings map[string]string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, value := range settings {
		s.data[key] = value
	}
	return nil
}

func (s *requestDetailBackupSettingRepoStub) GetAll(_ context.Context) (map[string]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]string, len(s.data))
	for key, value := range s.data {
		out[key] = value
	}
	return out, nil
}

func (s *requestDetailBackupSettingRepoStub) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, key)
	return nil
}

type requestDetailBackupPlainEncryptor struct{}

func (e requestDetailBackupPlainEncryptor) Encrypt(plaintext string) (string, error) {
	return "ENC:" + plaintext, nil
}

func (e requestDetailBackupPlainEncryptor) Decrypt(ciphertext string) (string, error) {
	return strings.TrimPrefix(ciphertext, "ENC:"), nil
}

func TestRequestDetailBackupPartSizeIs200MB(t *testing.T) {
	require.Equal(t, int64(200*1024*1024), requestDetailBackupPartSizeBytes)
}

func useFastRequestDetailBackupRetry(t *testing.T) {
	originalBackoff := requestDetailBackupUploadRetryBaseBackoff
	requestDetailBackupUploadRetryBaseBackoff = time.Millisecond
	t.Cleanup(func() { requestDetailBackupUploadRetryBaseBackoff = originalBackoff })
}

func TestRequestDetailBackupWritesGzipNDJSON(t *testing.T) {
	largeBody := strings.Repeat("large-body-", 1024)
	responseContent := "full response content"
	ref, err := BuildRequestDetailBodyBlob(largeBody)
	require.NoError(t, err)
	repo := &requestDetailBackupRepoStub{items: []RequestDetail{{
		RequestID:           "req-backup",
		CreatedAt:           time.Now(),
		RequestBody:         largeBody,
		UpstreamRequestBody: largeBody,
		RequestBodyRef:      *ref,
		UpstreamRequestRef:  *ref,
		ResponseContent:     responseContent,
		ResponseBody:        `{"b":2}`,
	}}}
	svc := NewRequestDetailService(repo)

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	require.NoError(t, svc.WriteBackupNDJSON(context.Background(), RequestDetailFilters{}, gz))
	require.NoError(t, gz.Close())

	reader, err := gzip.NewReader(bytes.NewReader(buf.Bytes()))
	require.NoError(t, err)
	out, err := io.ReadAll(reader)
	require.NoError(t, err)
	body := string(out)
	require.Contains(t, body, `"type":"request_detail"`)
	require.Contains(t, body, `"format_version":3`)
	require.Contains(t, body, `"request_id":"req-backup"`)
	require.NotContains(t, body, `"type":"body_blob"`)
	require.NotContains(t, body, `"request_body_ref"`)
	require.NotContains(t, body, `"upstream_request_body_ref"`)
	require.NotContains(t, body, `"response_content_ref"`)
	require.NotContains(t, body, `"response_body_ref"`)
	require.True(t, strings.HasSuffix(string(out), "\n"))

	var record requestDetailBackupDetailRecord
	require.NoError(t, json.Unmarshal(bytes.TrimSpace(out), &record))
	require.Equal(t, "request_detail", record.Type)
	require.Equal(t, 3, record.FormatVersion)
	require.Equal(t, largeBody, record.Detail.RequestBody)
	require.Equal(t, largeBody, record.Detail.UpstreamRequestBody)
	require.Equal(t, responseContent, record.Detail.ResponseContent)
	require.Equal(t, `{"b":2}`, record.Detail.ResponseBody)
}

func TestRequestDetailBackupWritesLegacyInlineLargeBodies(t *testing.T) {
	largeBody := strings.Repeat("legacy-large-body-", 1024)
	repo := &requestDetailBackupRepoStub{items: []RequestDetail{{
		RequestID:           "req-legacy-backup",
		CreatedAt:           time.Now(),
		RequestBody:         largeBody,
		UpstreamRequestBody: largeBody,
	}}}
	svc := NewRequestDetailService(repo)

	var buf bytes.Buffer
	require.NoError(t, svc.WriteBackupNDJSON(context.Background(), RequestDetailFilters{}, &buf))
	body := buf.String()
	require.Contains(t, body, `"type":"request_detail"`)
	require.Contains(t, body, `"format_version":3`)
	require.Contains(t, body, `"request_id":"req-legacy-backup"`)
	require.Contains(t, body, largeBody)
	require.NotContains(t, body, `"type":"body_blob"`)
	require.NotContains(t, body, `"request_body_ref"`)
	require.NotContains(t, body, `"upstream_request_body_ref"`)
	require.NotContains(t, body, `"response_content_ref"`)
	require.NotContains(t, body, `"response_body_ref"`)
	require.NotContains(t, body, `"inline":"legacy-large-body-`)
}

func TestRequestDetailBackupUploadUsesV3FullBodyArchiveFormat(t *testing.T) {
	largeBody := strings.Repeat("upload-large-body-", 1024)
	blockCh := make(chan struct{})
	repo := &requestDetailBackupBlockingRepoStub{
		blockCh: blockCh,
		items: []RequestDetail{{
			RequestID:           "req-upload-archive",
			CreatedAt:           time.Now(),
			RequestBody:         largeBody,
			UpstreamRequestBody: largeBody,
		}},
	}
	store := newRequestDetailBackupStoreStub()
	svc := newRequestDetailBackupTestService(repo, store, nil)

	record, err := svc.StartBackup(context.Background(), "manual")
	require.NoError(t, err)

	close(blockCh)
	require.Eventually(t, func() bool {
		final, err := svc.GetBackupRecord(context.Background(), record.ID)
		return err == nil && final.Status == "completed" && len(final.Parts) == 1
	}, time.Second, 10*time.Millisecond)

	final, err := svc.GetBackupRecord(context.Background(), record.ID)
	require.NoError(t, err)
	data := store.objects[final.Parts[0].S3Key]
	reader, err := gzip.NewReader(bytes.NewReader(data))
	require.NoError(t, err)
	out, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.NoError(t, reader.Close())
	body := string(out)
	require.Contains(t, body, `"type":"request_detail"`)
	require.Contains(t, body, `"format_version":3`)
	require.Contains(t, body, largeBody)
	require.NotContains(t, body, `"type":"body_blob"`)
	require.NotContains(t, body, `"request_body_ref"`)
	require.NotContains(t, body, `"upstream_request_body_ref"`)
	require.NotContains(t, body, `"response_content_ref"`)
	require.NotContains(t, body, `"response_body_ref"`)

	var recordLine requestDetailBackupDetailRecord
	require.NoError(t, json.Unmarshal(bytes.TrimSpace(out), &recordLine))
	require.Equal(t, "request_detail", recordLine.Type)
	require.Equal(t, 3, recordLine.FormatVersion)
	require.Equal(t, largeBody, recordLine.Detail.RequestBody)
	require.Equal(t, largeBody, recordLine.Detail.UpstreamRequestBody)
}

type requestDetailBackupBlockingRepoStub struct {
	RequestDetailRepository
	mu      sync.Mutex
	blockCh chan struct{}
	items   []RequestDetail
	filters []RequestDetailFilters
}

func (s *requestDetailBackupBlockingRepoStub) StreamAll(ctx context.Context, filters RequestDetailFilters, write func(RequestDetail) error) error {
	s.mu.Lock()
	s.filters = append(s.filters, filters)
	s.mu.Unlock()
	select {
	case <-s.blockCh:
	case <-ctx.Done():
		return ctx.Err()
	}
	for _, item := range s.items {
		if err := write(item); err != nil {
			return err
		}
	}
	return nil
}

type requestDetailBackupStoreStub struct {
	mu                  sync.Mutex
	uploadCh            chan struct{}
	uploadNotified      bool
	uploadErr           error
	uploadHook          func(key string) error
	deleteErr           error
	headObjectErr       error
	headObjectSizeDelta int64
	objects             map[string][]byte
	deleted             []string
}

func newRequestDetailBackupStoreStub() *requestDetailBackupStoreStub {
	return &requestDetailBackupStoreStub{
		uploadCh: make(chan struct{}),
		objects:  map[string][]byte{},
	}
}

func (s *requestDetailBackupStoreStub) Upload(_ context.Context, key string, body io.Reader, _ string) (int64, error) {
	data, err := io.ReadAll(body)
	if err != nil {
		return 0, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.uploadNotified {
		close(s.uploadCh)
		s.uploadNotified = true
	}
	if s.uploadHook != nil {
		if err := s.uploadHook(key); err != nil {
			return 0, err
		}
	}
	if s.uploadErr != nil {
		return 0, s.uploadErr
	}
	s.objects[key] = data
	if s.headObjectErr != nil {
		return 0, fmt.Errorf("S3 HeadObject after PutObject: %w", s.headObjectErr)
	}
	sizeBytes := int64(len(data)) + s.headObjectSizeDelta
	if sizeBytes != int64(len(data)) {
		return 0, fmt.Errorf("S3 HeadObject size mismatch after PutObject: uploaded=%d stored=%d key=%s", len(data), sizeBytes, key)
	}
	return sizeBytes, nil
}

func (s *requestDetailBackupStoreStub) Download(_ context.Context, key string) (io.ReadCloser, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, ok := s.objects[key]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (s *requestDetailBackupStoreStub) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deleted = append(s.deleted, key)
	if s.deleteErr != nil {
		return s.deleteErr
	}
	delete(s.objects, key)
	return nil
}

func (s *requestDetailBackupStoreStub) PresignURL(_ context.Context, key string, _ time.Duration) (string, error) {
	return "https://example.com/" + key, nil
}

func (s *requestDetailBackupStoreStub) HeadBucket(_ context.Context) error { return nil }

func (s *requestDetailBackupStoreStub) HeadObject(_ context.Context, key string) (*BackupObjectInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.headObjectErr != nil {
		return nil, s.headObjectErr
	}
	data, ok := s.objects[key]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	return &BackupObjectInfo{SizeBytes: int64(len(data)) + s.headObjectSizeDelta}, nil
}

func newRequestDetailBackupTestService(repo RequestDetailRepository, store BackupObjectStore, settings *requestDetailBackupSettingRepoStub) *RequestDetailBackupService {
	if settings == nil {
		settings = newRequestDetailBackupSettingRepoStub()
	}
	cfg := BackupS3Config{
		Bucket:          "test-bucket",
		AccessKeyID:     "AKID",
		SecretAccessKey: "ENC:secret123",
		Prefix:          "backups",
	}
	data, _ := json.Marshal(cfg)
	_ = settings.Set(context.Background(), settingKeyBackupS3Config, string(data))
	backup := NewBackupService(settings, &config.Config{}, requestDetailBackupPlainEncryptor{}, func(context.Context, *BackupS3Config) (BackupObjectStore, error) {
		return store, nil
	}, nil)
	backup.storeFactory = func(context.Context, *BackupS3Config) (BackupObjectStore, error) {
		return store, nil
	}
	requestDetailSvc := NewRequestDetailService(repo)
	requestDetailSvc.SetBackupService(backup)
	svc := NewRequestDetailBackupService(requestDetailSvc, backup, settings)
	if cacheDir, err := os.MkdirTemp("", "sub2api-request-detail-backup-test-*"); err == nil {
		svc.SetCacheDir(cacheDir)
	}
	return svc
}

func TestRequestDetailBackupStartBackupReturnsRunningAndCompletesAsync(t *testing.T) {
	fixedNow := time.Date(2026, 5, 22, 9, 30, 0, 0, time.Local)
	originalNow := requestDetailBackupNow
	requestDetailBackupNow = func() time.Time { return fixedNow }
	t.Cleanup(func() { requestDetailBackupNow = originalNow })

	blockCh := make(chan struct{})
	detailCreatedAt := time.Date(2026, 5, 22, 22, 10, 0, 0, time.Local)
	repo := &requestDetailBackupBlockingRepoStub{
		blockCh: blockCh,
		items: []RequestDetail{{
			RequestID:    "req-async",
			CreatedAt:    detailCreatedAt,
			RequestBody:  `{"a":1}`,
			ResponseBody: `{"b":2}`,
		}},
	}
	store := newRequestDetailBackupStoreStub()
	svc := newRequestDetailBackupTestService(repo, store, nil)

	record, err := svc.StartBackup(context.Background(), "manual")
	require.NoError(t, err)
	require.Equal(t, "running", record.Status)
	require.Empty(t, record.FileName)
	require.Empty(t, record.S3Key)

	close(blockCh)
	require.Eventually(t, func() bool {
		final, err := svc.GetBackupRecord(context.Background(), record.ID)
		return err == nil && final.Status == "completed" && final.SizeBytes > 0 && len(final.Parts) == 1
	}, time.Second, 10*time.Millisecond)

	final, err := svc.GetBackupRecord(context.Background(), record.ID)
	require.NoError(t, err)
	require.Len(t, final.Parts, 1)
	require.Equal(t, 1, final.Parts[0].Index)
	require.Equal(t, "request_details_20260522_093000_2210_2210_part001.ndjson.gz", final.Parts[0].FileName)
	require.Equal(t, "backups/request-details/20260522/request_details_20260522_093000_2210_2210_part001.ndjson.gz", final.Parts[0].S3Key)
	require.Equal(t, final.FileName, final.Parts[0].FileName)
	require.Equal(t, final.S3Key, final.Parts[0].S3Key)
	require.Equal(t, final.SizeBytes, final.Parts[0].SizeBytes)

	repo.mu.Lock()
	defer repo.mu.Unlock()
	require.Len(t, repo.filters, 1)
	require.Nil(t, repo.filters[0].StartTime)
	require.Nil(t, repo.filters[0].EndTime)
}

func TestRequestDetailBackupUploadsReadmeSummaryAfterCompletion(t *testing.T) {
	fixedNow := time.Date(2026, 5, 22, 9, 30, 0, 0, time.Local)
	originalNow := requestDetailBackupNow
	requestDetailBackupNow = func() time.Time { return fixedNow }
	t.Cleanup(func() { requestDetailBackupNow = originalNow })

	blockCh := make(chan struct{})
	repo := &requestDetailBackupBlockingRepoStub{
		blockCh: blockCh,
		items: []RequestDetail{
			{RequestID: "req-readme-1", CreatedAt: fixedNow.Add(time.Minute), RequestBody: `{"a":1}`},
			{RequestID: "req-readme-2", CreatedAt: fixedNow.Add(2 * time.Minute), RequestBody: `{"b":2}`},
		},
	}
	store := newRequestDetailBackupStoreStub()
	svc := newRequestDetailBackupTestService(repo, store, nil)

	record, err := svc.StartBackup(context.Background(), "manual")
	require.NoError(t, err)
	close(blockCh)
	require.Eventually(t, func() bool {
		final, err := svc.GetBackupRecord(context.Background(), record.ID)
		return err == nil && final.Status == "completed" && final.ReadmeS3Key != ""
	}, time.Second, 10*time.Millisecond)

	final, err := svc.GetBackupRecord(context.Background(), record.ID)
	require.NoError(t, err)
	require.Equal(t, "readme_20260522_093000.txt", final.ReadmeFileName)
	require.Equal(t, "backups/request-details/20260522/readme_20260522_093000.txt", final.ReadmeS3Key)
	require.Greater(t, final.ReadmeSizeBytes, int64(0))

	readme := string(store.objects[final.ReadmeS3Key])
	require.Contains(t, readme, "request_detail_backup_summary\n")
	require.Contains(t, readme, "backup_id: "+record.ID+"\n")
	require.Contains(t, readme, "date: 2026-05-22\n")
	require.Contains(t, readme, "started_at: "+fixedNow.Format(time.RFC3339)+"\n")
	require.Contains(t, readme, "finished_at: "+fixedNow.Format(time.RFC3339)+"\n")
	require.Contains(t, readme, "record_count: 2\n")
	require.Contains(t, readme, "part_count: 1\n")
	require.Contains(t, readme, fmt.Sprintf("total_file_size_bytes: %d\n", final.SizeBytes))
	require.Contains(t, readme, final.Parts[0].FileName)
	require.Contains(t, readme, final.Parts[0].S3Key)
}

func TestRequestDetailBackupScheduledBackupUsesPreviousDayFilter(t *testing.T) {
	loc := time.Local
	fixedNow := time.Date(2026, 5, 21, 10, 30, 0, 0, loc)
	originalNow := requestDetailBackupNow
	requestDetailBackupNow = func() time.Time { return fixedNow }
	t.Cleanup(func() { requestDetailBackupNow = originalNow })

	blockCh := make(chan struct{})
	repo := &requestDetailBackupBlockingRepoStub{
		blockCh: blockCh,
		items: []RequestDetail{{
			RequestID: "req-scheduled",
			CreatedAt: fixedNow,
		}},
	}
	store := newRequestDetailBackupStoreStub()
	svc := newRequestDetailBackupTestService(repo, store, nil)

	record, err := svc.StartScheduledBackup(context.Background())
	require.NoError(t, err)
	require.Equal(t, "scheduled", record.TriggeredBy)

	close(blockCh)
	require.Eventually(t, func() bool {
		final, err := svc.GetBackupRecord(context.Background(), record.ID)
		return err == nil && final.Status == "completed"
	}, time.Second, 10*time.Millisecond)

	expectedEnd := time.Date(2026, 5, 21, 0, 0, 0, 0, loc)
	expectedStart := expectedEnd.AddDate(0, 0, -1)
	repo.mu.Lock()
	defer repo.mu.Unlock()
	require.Len(t, repo.filters, 1)
	require.NotNil(t, repo.filters[0].StartTime)
	require.NotNil(t, repo.filters[0].EndTime)
	require.Equal(t, expectedStart, *repo.filters[0].StartTime)
	require.Equal(t, expectedEnd, *repo.filters[0].EndTime)
}

func TestRequestDetailBackupSplitsIntoIndependentGzipParts(t *testing.T) {
	fixedNow := time.Date(2026, 5, 22, 9, 30, 0, 0, time.Local)
	originalNow := requestDetailBackupNow
	requestDetailBackupNow = func() time.Time { return fixedNow }
	t.Cleanup(func() { requestDetailBackupNow = originalNow })
	originalPartSize := requestDetailBackupPartSizeBytes
	requestDetailBackupPartSizeBytes = 1
	t.Cleanup(func() { requestDetailBackupPartSizeBytes = originalPartSize })

	blockCh := make(chan struct{})
	repo := &requestDetailBackupBlockingRepoStub{
		blockCh: blockCh,
		items: []RequestDetail{
			{RequestID: "req-part-1", CreatedAt: fixedNow, RequestBody: `{"a":1}`},
			{RequestID: "req-part-2", CreatedAt: fixedNow, RequestBody: `{"b":2}`},
		},
	}
	store := newRequestDetailBackupStoreStub()
	svc := newRequestDetailBackupTestService(repo, store, nil)

	record, err := svc.StartBackup(context.Background(), "manual")
	require.NoError(t, err)

	close(blockCh)
	require.Eventually(t, func() bool {
		final, err := svc.GetBackupRecord(context.Background(), record.ID)
		return err == nil && final.Status == "completed" && len(final.Parts) >= 2
	}, time.Second, 10*time.Millisecond)

	final, err := svc.GetBackupRecord(context.Background(), record.ID)
	require.NoError(t, err)
	require.Len(t, final.Parts, 2)
	require.Equal(t, "backups/request-details/20260522/request_details_20260522_093000_0930_0930_part001.ndjson.gz", final.Parts[0].S3Key)
	require.Equal(t, "backups/request-details/20260522/request_details_20260522_093000_0930_0930_part002.ndjson.gz", final.Parts[1].S3Key)
	require.Equal(t, final.Parts[0].FileName, final.FileName)
	require.Equal(t, final.Parts[0].S3Key, final.S3Key)

	var total int64
	for _, part := range final.Parts {
		total += part.SizeBytes
		data := store.objects[part.S3Key]
		require.NotEmpty(t, data)
		reader, err := gzip.NewReader(bytes.NewReader(data))
		require.NoError(t, err)
		out, err := io.ReadAll(reader)
		require.NoError(t, err)
		require.NoError(t, reader.Close())
		require.Contains(t, string(out), fmt.Sprintf(`"request_id":"req-part-%d"`, part.Index))
		require.True(t, strings.HasSuffix(string(out), "\n"))
	}
	require.Equal(t, total, final.SizeBytes)
}

func TestRequestDetailBackupPartFileNameUsesFirstAndLastRequestMinute(t *testing.T) {
	fixedNow := time.Date(2026, 5, 22, 9, 30, 0, 0, time.Local)
	originalNow := requestDetailBackupNow
	requestDetailBackupNow = func() time.Time { return fixedNow }
	t.Cleanup(func() { requestDetailBackupNow = originalNow })

	first := time.Date(2026, 5, 22, 22, 10, 30, 0, time.Local)
	last := time.Date(2026, 5, 22, 23, 40, 0, 0, time.Local)
	blockCh := make(chan struct{})
	repo := &requestDetailBackupBlockingRepoStub{
		blockCh: blockCh,
		items: []RequestDetail{
			{RequestID: "req-window-1", CreatedAt: first, RequestBody: `{"a":1}`},
			{RequestID: "req-window-2", CreatedAt: last, RequestBody: `{"b":2}`},
		},
	}
	store := newRequestDetailBackupStoreStub()
	svc := newRequestDetailBackupTestService(repo, store, nil)

	record, err := svc.StartBackup(context.Background(), "manual")
	require.NoError(t, err)
	close(blockCh)
	require.Eventually(t, func() bool {
		final, err := svc.GetBackupRecord(context.Background(), record.ID)
		return err == nil && final.Status == "completed" && len(final.Parts) == 1
	}, time.Second, 10*time.Millisecond)

	final, err := svc.GetBackupRecord(context.Background(), record.ID)
	require.NoError(t, err)
	require.Equal(t, "request_details_20260522_093000_2210_2340_part001.ndjson.gz", final.Parts[0].FileName)
	require.Contains(t, final.Parts[0].S3Key, "request_details_20260522_093000_2210_2340_part001.ndjson.gz")
}

func TestRequestDetailBackupContinuesExportWhileUploadIsBlocked(t *testing.T) {
	originalPartSize := requestDetailBackupPartSizeBytes
	requestDetailBackupPartSizeBytes = 1
	t.Cleanup(func() { requestDetailBackupPartSizeBytes = originalPartSize })

	blockCh := make(chan struct{})
	now := time.Date(2026, 5, 22, 10, 0, 0, 0, time.Local)
	repo := &requestDetailBackupBlockingRepoStub{
		blockCh: blockCh,
		items: []RequestDetail{
			{RequestID: "req-parallel-1", CreatedAt: now},
			{RequestID: "req-parallel-2", CreatedAt: now.Add(time.Minute)},
			{RequestID: "req-parallel-3", CreatedAt: now.Add(2 * time.Minute)},
		},
	}
	store := newRequestDetailBackupStoreStub()
	uploadStarted := make(chan struct{})
	releaseUpload := make(chan struct{})
	firstUpload := true
	store.uploadHook = func(string) error {
		if firstUpload {
			firstUpload = false
			close(uploadStarted)
			<-releaseUpload
		}
		return nil
	}
	svc := newRequestDetailBackupTestService(repo, store, nil)

	record, err := svc.StartBackup(context.Background(), "manual")
	require.NoError(t, err)
	close(blockCh)

	select {
	case <-uploadStarted:
	case <-time.After(time.Second):
		t.Fatal("first upload did not start")
	}
	require.Eventually(t, func() bool {
		final, err := svc.GetBackupRecord(context.Background(), record.ID)
		return err == nil && strings.Contains(final.Progress, "exported part 3")
	}, time.Second, 10*time.Millisecond)

	close(releaseUpload)
	require.Eventually(t, func() bool {
		final, err := svc.GetBackupRecord(context.Background(), record.ID)
		return err == nil && final.Status == "completed" && len(final.Parts) == 3
	}, time.Second, 10*time.Millisecond)
}

func TestRequestDetailBackupKeyUsesDateFolderAndTrimmedPrefix(t *testing.T) {
	cfg := &BackupS3Config{Prefix: "sub2api/backups/"}
	key := buildRequestDetailBackupKey(cfg, "20260522", "request_details_20260522_093000_part001.ndjson.gz")
	require.Equal(t, "sub2api/backups/request-details/20260522/request_details_20260522_093000_part001.ndjson.gz", key)

	cfg.Prefix = ""
	key = buildRequestDetailBackupKey(cfg, "20260522", "request_details_20260522_093000_part001.ndjson.gz")
	require.Equal(t, "backups/request-details/20260522/request_details_20260522_093000_part001.ndjson.gz", key)
}

func TestRequestDetailBackupRecoverStaleRecords(t *testing.T) {
	fixedNow := time.Date(2026, 5, 22, 9, 30, 0, 0, time.Local)
	originalNow := requestDetailBackupNow
	requestDetailBackupNow = func() time.Time { return fixedNow }
	t.Cleanup(func() { requestDetailBackupNow = originalNow })

	settings := newRequestDetailBackupSettingRepoStub()
	svc := NewRequestDetailBackupService(nil, nil, settings)
	require.NoError(t, svc.saveRecord(context.Background(), &BackupRecord{
		ID:          "stale-1",
		Status:      "running",
		BackupType:  "request_details",
		Progress:    "uploading",
		StartedAt:   fixedNow.Add(-time.Hour).Format(time.RFC3339),
		TriggeredBy: "manual",
	}))
	require.NoError(t, svc.saveRecord(context.Background(), &BackupRecord{
		ID:          "done-1",
		Status:      "completed",
		BackupType:  "request_details",
		StartedAt:   fixedNow.Add(-2 * time.Hour).Format(time.RFC3339),
		FinishedAt:  fixedNow.Add(-time.Hour).Format(time.RFC3339),
		TriggeredBy: "manual",
	}))

	svc.recoverStaleRecords(context.Background())

	stale, err := svc.GetBackupRecord(context.Background(), "stale-1")
	require.NoError(t, err)
	require.Equal(t, "failed", stale.Status)
	require.Empty(t, stale.Progress)
	require.Equal(t, fixedNow.Format(time.RFC3339), stale.FinishedAt)
	require.Contains(t, stale.ErrorMsg, "server restart")

	done, err := svc.GetBackupRecord(context.Background(), "done-1")
	require.NoError(t, err)
	require.Equal(t, "completed", done.Status)
	require.Empty(t, done.ErrorMsg)
}

func TestRequestDetailBackupStartBackupUploadFailureIsRecordedAsync(t *testing.T) {
	useFastRequestDetailBackupRetry(t)
	blockCh := make(chan struct{})
	repo := &requestDetailBackupBlockingRepoStub{
		blockCh: blockCh,
		items: []RequestDetail{{
			RequestID: "req-fail",
			CreatedAt: time.Now(),
		}},
	}
	store := newRequestDetailBackupStoreStub()
	store.uploadErr = fmt.Errorf("S3 PutObject failed")
	svc := newRequestDetailBackupTestService(repo, store, nil)

	record, err := svc.StartBackup(context.Background(), "manual")
	require.NoError(t, err)
	require.Equal(t, "running", record.Status)

	close(blockCh)
	require.Eventually(t, func() bool {
		final, err := svc.GetBackupRecord(context.Background(), record.ID)
		return err == nil && final.Status == "failed" && strings.Contains(final.ErrorMsg, "S3 PutObject failed")
	}, time.Second, 10*time.Millisecond)
}

func TestRequestDetailBackupRetriesCurrentPartAndDeletesCacheAfterSuccess(t *testing.T) {
	useFastRequestDetailBackupRetry(t)
	blockCh := make(chan struct{})
	repo := &requestDetailBackupBlockingRepoStub{
		blockCh: blockCh,
		items: []RequestDetail{{
			RequestID: "req-retry",
			CreatedAt: time.Now(),
		}},
	}
	store := newRequestDetailBackupStoreStub()
	uploadCount := 0
	store.uploadHook = func(key string) error {
		if strings.Contains(key, "part001") {
			uploadCount++
		}
		if strings.Contains(key, "part001") && uploadCount <= 2 {
			return fmt.Errorf("temporary S3 PutObject failed")
		}
		return nil
	}
	svc := newRequestDetailBackupTestService(repo, store, nil)
	cacheRoot := t.TempDir()
	svc.SetCacheDir(cacheRoot)

	record, err := svc.StartBackup(context.Background(), "manual")
	require.NoError(t, err)
	close(blockCh)
	require.Eventually(t, func() bool {
		final, err := svc.GetBackupRecord(context.Background(), record.ID)
		return err == nil && final.Status == "completed" && len(final.Parts) == 1
	}, time.Second, 10*time.Millisecond)

	require.Equal(t, 3, uploadCount)
	entries, err := os.ReadDir(cacheRoot)
	require.NoError(t, err)
	require.Empty(t, entries)
}

func TestRequestDetailBackupUploadFailureDeletesUploadedParts(t *testing.T) {
	useFastRequestDetailBackupRetry(t)
	originalPartSize := requestDetailBackupPartSizeBytes
	requestDetailBackupPartSizeBytes = 1
	t.Cleanup(func() { requestDetailBackupPartSizeBytes = originalPartSize })

	blockCh := make(chan struct{})
	repo := &requestDetailBackupBlockingRepoStub{
		blockCh: blockCh,
		items: []RequestDetail{
			{RequestID: "req-upload-ok", CreatedAt: time.Now()},
			{RequestID: "req-upload-fail", CreatedAt: time.Now()},
		},
	}
	store := newRequestDetailBackupStoreStub()
	uploadCount := 0
	store.uploadHook = func(string) error {
		uploadCount++
		if uploadCount >= 2 {
			return fmt.Errorf("S3 PutObject failed")
		}
		return nil
	}
	svc := newRequestDetailBackupTestService(repo, store, nil)

	record, err := svc.StartBackup(context.Background(), "manual")
	require.NoError(t, err)

	close(blockCh)
	require.Eventually(t, func() bool {
		final, err := svc.GetBackupRecord(context.Background(), record.ID)
		return err == nil && final.Status == "failed" && strings.Contains(final.ErrorMsg, "S3 PutObject failed")
	}, time.Second, 10*time.Millisecond)

	store.mu.Lock()
	defer store.mu.Unlock()
	require.Len(t, store.deleted, 1)
	require.Contains(t, store.deleted[0], "part001")
}

func TestRequestDetailBackupDeleteRemovesPartsAndRecord(t *testing.T) {
	store := newRequestDetailBackupStoreStub()
	svc := newRequestDetailBackupTestService(&requestDetailBackupRepoStub{}, store, nil)
	require.NoError(t, svc.saveRecord(context.Background(), &BackupRecord{
		ID:              "backup-delete",
		Status:          "completed",
		BackupType:      "request_details",
		FileName:        "part001.ndjson.gz",
		S3Key:           "backups/request-details/20260522/part001.ndjson.gz",
		ReadmeFileName:  "readme_20260522_093000.txt",
		ReadmeS3Key:     "backups/request-details/20260522/readme_20260522_093000.txt",
		ReadmeSizeBytes: 100,
		Parts: []BackupRecordPart{
			{Index: 1, FileName: "part001.ndjson.gz", S3Key: "backups/request-details/20260522/part001.ndjson.gz", SizeBytes: 10},
			{Index: 2, FileName: "part002.ndjson.gz", S3Key: "backups/request-details/20260522/part002.ndjson.gz", SizeBytes: 20},
		},
		StartedAt:   time.Now().Format(time.RFC3339),
		TriggeredBy: "manual",
	}))

	require.NoError(t, svc.DeleteBackup(context.Background(), "backup-delete"))
	_, err := svc.GetBackupRecord(context.Background(), "backup-delete")
	require.ErrorIs(t, err, ErrBackupNotFound)
	require.ElementsMatch(t, []string{
		"backups/request-details/20260522/part001.ndjson.gz",
		"backups/request-details/20260522/part002.ndjson.gz",
		"backups/request-details/20260522/readme_20260522_093000.txt",
	}, store.deleted)
}

func TestRequestDetailBackupDeleteIgnoresMissingS3Object(t *testing.T) {
	store := newRequestDetailBackupStoreStub()
	store.deleteErr = fmt.Errorf("not found")
	svc := newRequestDetailBackupTestService(&requestDetailBackupRepoStub{}, store, nil)
	require.NoError(t, svc.saveRecord(context.Background(), &BackupRecord{
		ID:          "backup-missing-object",
		Status:      "completed",
		BackupType:  "request_details",
		FileName:    "part001.ndjson.gz",
		S3Key:       "backups/request-details/20260522/part001.ndjson.gz",
		SizeBytes:   10,
		StartedAt:   time.Now().Format(time.RFC3339),
		TriggeredBy: "manual",
	}))

	require.NoError(t, svc.DeleteBackup(context.Background(), "backup-missing-object"))
	_, err := svc.GetBackupRecord(context.Background(), "backup-missing-object")
	require.ErrorIs(t, err, ErrBackupNotFound)
	require.Equal(t, []string{"backups/request-details/20260522/part001.ndjson.gz"}, store.deleted)
}

func TestRequestDetailBackupStartBackupHeadObjectFailureIsRecordedAsync(t *testing.T) {
	useFastRequestDetailBackupRetry(t)
	blockCh := make(chan struct{})
	repo := &requestDetailBackupBlockingRepoStub{
		blockCh: blockCh,
		items: []RequestDetail{{
			RequestID: "req-head-fail",
			CreatedAt: time.Now(),
		}},
	}
	store := newRequestDetailBackupStoreStub()
	store.headObjectErr = fmt.Errorf("not found")
	svc := newRequestDetailBackupTestService(repo, store, nil)

	record, err := svc.StartBackup(context.Background(), "manual")
	require.NoError(t, err)
	require.Equal(t, "running", record.Status)

	close(blockCh)
	require.Eventually(t, func() bool {
		final, err := svc.GetBackupRecord(context.Background(), record.ID)
		return err == nil && final.Status == "failed" && strings.Contains(final.ErrorMsg, "HeadObject")
	}, time.Second, 10*time.Millisecond)
}

func TestRequestDetailBackupStartBackupHeadObjectSizeMismatchIsRecordedAsync(t *testing.T) {
	useFastRequestDetailBackupRetry(t)
	blockCh := make(chan struct{})
	repo := &requestDetailBackupBlockingRepoStub{
		blockCh: blockCh,
		items: []RequestDetail{{
			RequestID: "req-head-mismatch",
			CreatedAt: time.Now(),
		}},
	}
	store := newRequestDetailBackupStoreStub()
	store.headObjectSizeDelta = 1
	svc := newRequestDetailBackupTestService(repo, store, nil)

	record, err := svc.StartBackup(context.Background(), "manual")
	require.NoError(t, err)
	require.Equal(t, "running", record.Status)

	close(blockCh)
	require.Eventually(t, func() bool {
		final, err := svc.GetBackupRecord(context.Background(), record.ID)
		return err == nil && final.Status == "failed" && strings.Contains(final.ErrorMsg, "size mismatch")
	}, time.Second, 10*time.Millisecond)
}
