package service

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type requestDetailBackupRepoStub struct {
	RequestDetailRepository
	items []RequestDetail
}

func (s *requestDetailBackupRepoStub) StreamAll(_ context.Context, _ RequestDetailFilters, write func(RequestDetail) error) error {
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

func TestRequestDetailBackupWritesGzipNDJSON(t *testing.T) {
	repo := &requestDetailBackupRepoStub{items: []RequestDetail{{
		RequestID:    "req-backup",
		CreatedAt:    time.Now(),
		RequestBody:  `{"a":1}`,
		ResponseBody: `{"b":2}`,
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
	require.Contains(t, string(out), `"request_id":"req-backup"`)
	require.True(t, strings.HasSuffix(string(out), "\n"))
}

type requestDetailBackupBlockingRepoStub struct {
	RequestDetailRepository
	blockCh chan struct{}
	items   []RequestDetail
}

func (s *requestDetailBackupBlockingRepoStub) StreamAll(ctx context.Context, _ RequestDetailFilters, write func(RequestDetail) error) error {
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
	mu        sync.Mutex
	uploadCh  chan struct{}
	uploadErr error
	objects   map[string][]byte
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
	close(s.uploadCh)
	if s.uploadErr != nil {
		return 0, s.uploadErr
	}
	s.objects[key] = data
	return int64(len(data)), nil
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

func (s *requestDetailBackupStoreStub) Delete(_ context.Context, key string) error { return nil }

func (s *requestDetailBackupStoreStub) PresignURL(_ context.Context, key string, _ time.Duration) (string, error) {
	return "https://example.com/" + key, nil
}

func (s *requestDetailBackupStoreStub) HeadBucket(_ context.Context) error { return nil }

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
	return NewRequestDetailBackupService(requestDetailSvc, backup, settings)
}

func TestRequestDetailBackupStartBackupReturnsRunningAndCompletesAsync(t *testing.T) {
	blockCh := make(chan struct{})
	repo := &requestDetailBackupBlockingRepoStub{
		blockCh: blockCh,
		items: []RequestDetail{{
			RequestID:    "req-async",
			CreatedAt:    time.Now(),
			RequestBody:  `{"a":1}`,
			ResponseBody: `{"b":2}`,
		}},
	}
	store := newRequestDetailBackupStoreStub()
	svc := newRequestDetailBackupTestService(repo, store, nil)

	record, err := svc.StartBackup(context.Background(), "manual")
	require.NoError(t, err)
	require.Equal(t, "running", record.Status)
	require.NotEmpty(t, record.S3Key)

	close(blockCh)
	require.Eventually(t, func() bool {
		final, err := svc.GetBackupRecord(context.Background(), record.ID)
		return err == nil && final.Status == "completed" && final.SizeBytes > 0
	}, time.Second, 10*time.Millisecond)
}

func TestRequestDetailBackupStartBackupUploadFailureIsRecordedAsync(t *testing.T) {
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
