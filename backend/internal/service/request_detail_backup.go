package service

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/robfig/cron/v3"

	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
)

const (
	settingKeyRequestDetailBackupSchedule = "request_detail_backup_schedule"
	settingKeyRequestDetailBackupRecords  = "request_detail_backup_records"
	requestDetailBackupContentType        = "application/gzip"
)

var requestDetailBackupNow = time.Now
var requestDetailBackupPartSizeBytes int64 = 1 << 30

// RequestDetailBackupDownloadPart includes a presigned URL for one backup part.
type RequestDetailBackupDownloadPart struct {
	BackupRecordPart
	URL string `json:"url"`
}

// RequestDetailBackupDownloadURLs is the download response for request detail backups.
type RequestDetailBackupDownloadURLs struct {
	URL   string                            `json:"url,omitempty"`
	URLs  []string                          `json:"urls"`
	Parts []RequestDetailBackupDownloadPart `json:"parts"`
}

type countingWriter struct {
	w io.Writer
	n int64
}

func (w *countingWriter) Write(p []byte) (int, error) {
	n, err := w.w.Write(p)
	w.n += int64(n)
	return n, err
}

type requestDetailBackupTempPart struct {
	BackupRecordPart
	tempPath string
}

func recordRequestDetailBackupFailure(record *BackupRecord, err error) {
	record.Status = "failed"
	record.ErrorMsg = err.Error()
	record.Progress = ""
	record.FinishedAt = time.Now().Format(time.RFC3339)
}

func cleanupRequestDetailBackupTempParts(parts []requestDetailBackupTempPart) {
	for _, part := range parts {
		if part.tempPath != "" {
			_ = os.Remove(part.tempPath)
		}
	}
}

func cleanupUploadedRequestDetailBackupParts(ctx context.Context, store BackupObjectStore, parts []BackupRecordPart) {
	for _, part := range parts {
		if strings.TrimSpace(part.S3Key) != "" {
			_ = store.Delete(ctx, part.S3Key)
		}
	}
}

type RequestDetailBackupService struct {
	requestDetailService *RequestDetailService
	backupService        *BackupService
	settingRepo          SettingRepository

	mu          sync.Mutex
	cronSched   *cron.Cron
	cronEntryID cron.EntryID
	backupMu    sync.Mutex
	backingUp   bool
}

func NewRequestDetailBackupService(requestDetailService *RequestDetailService, backupService *BackupService, settingRepo SettingRepository) *RequestDetailBackupService {
	return &RequestDetailBackupService{
		requestDetailService: requestDetailService,
		backupService:        backupService,
		settingRepo:          settingRepo,
	}
}

func (s *RequestDetailBackupService) Start() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cronSched != nil {
		return
	}
	s.cronSched = cron.New()
	s.cronSched.Start()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s.recoverStaleRecords(ctx)
	schedule, err := s.GetSchedule(ctx)
	if err == nil && schedule.Enabled && schedule.CronExpr != "" {
		_ = s.applyCronScheduleLocked(schedule)
	}
}

func (s *RequestDetailBackupService) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cronSched != nil {
		s.cronSched.Stop()
		s.cronSched = nil
		s.cronEntryID = 0
	}
}

func (s *RequestDetailBackupService) GetSchedule(ctx context.Context) (*BackupScheduleConfig, error) {
	raw, err := s.settingRepo.GetValue(ctx, settingKeyRequestDetailBackupSchedule)
	if err != nil || raw == "" {
		return &BackupScheduleConfig{}, nil
	}
	var cfg BackupScheduleConfig
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return &BackupScheduleConfig{}, nil
	}
	return &cfg, nil
}

func (s *RequestDetailBackupService) recoverStaleRecords(ctx context.Context) {
	records, err := s.loadRecords(ctx)
	if err != nil {
		return
	}
	for i := range records {
		if records[i].Status != "running" {
			continue
		}
		records[i].Status = "failed"
		records[i].ErrorMsg = "interrupted by server restart"
		records[i].Progress = ""
		records[i].FinishedAt = requestDetailBackupNow().Format(time.RFC3339)
		_ = s.saveRecord(ctx, &records[i])
	}
}

func (s *RequestDetailBackupService) UpdateSchedule(ctx context.Context, cfg BackupScheduleConfig) (*BackupScheduleConfig, error) {
	if cfg.Enabled && cfg.CronExpr == "" {
		return nil, ErrBackupS3NotConfigured.WithCause(fmt.Errorf("cron expression is required when schedule is enabled"))
	}
	if cfg.CronExpr != "" {
		parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
		if _, err := parser.Parse(cfg.CronExpr); err != nil {
			return nil, fmt.Errorf("invalid cron expression: %w", err)
		}
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	if err := s.settingRepo.Set(ctx, settingKeyRequestDetailBackupSchedule, string(data)); err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if cfg.Enabled {
		if err := s.applyCronScheduleLocked(&cfg); err != nil {
			return nil, err
		}
	} else if s.cronSched != nil && s.cronEntryID != 0 {
		s.cronSched.Remove(s.cronEntryID)
		s.cronEntryID = 0
	}
	return &cfg, nil
}

func (s *RequestDetailBackupService) StartBackup(ctx context.Context, triggeredBy string) (*BackupRecord, error) {
	return s.startBackup(ctx, triggeredBy, RequestDetailFilters{})
}

func (s *RequestDetailBackupService) StartScheduledBackup(ctx context.Context) (*BackupRecord, error) {
	return s.startBackup(ctx, "scheduled", previousDayRequestDetailFilters(requestDetailBackupNow()))
}

func (s *RequestDetailBackupService) startBackup(ctx context.Context, triggeredBy string, filters RequestDetailFilters) (*BackupRecord, error) {
	if s == nil || s.requestDetailService == nil || s.backupService == nil {
		return nil, ErrBackupS3NotConfigured.WithCause(fmt.Errorf("request detail backup service is not initialized"))
	}
	s.backupMu.Lock()
	if s.backingUp {
		s.backupMu.Unlock()
		return nil, ErrBackupInProgress
	}
	s.backingUp = true
	s.backupMu.Unlock()

	launched := false
	defer func() {
		if !launched {
			s.backupMu.Lock()
			s.backingUp = false
			s.backupMu.Unlock()
		}
	}()

	store, cfg, err := s.backupService.NewConfiguredObjectStore(ctx)
	if err != nil {
		return nil, err
	}

	now := requestDetailBackupNow()
	dateFolder := now.Format("20060102")
	firstFileName := buildRequestDetailBackupFileName(now, 1)
	record := &BackupRecord{
		ID:          uuid.New().String()[:8],
		Status:      "running",
		BackupType:  "request_details",
		FileName:    firstFileName,
		TriggeredBy: triggeredBy,
		StartedAt:   now.Format(time.RFC3339),
	}
	record.S3Key = buildRequestDetailBackupKey(cfg, dateFolder, record.FileName)
	if err := s.saveRecord(ctx, record); err != nil {
		return nil, err
	}

	launched = true
	result := *record
	go s.executeBackup(record, store, cfg, now, filters)
	return &result, nil
}

func (s *RequestDetailBackupService) executeBackup(record *BackupRecord, store BackupObjectStore, cfg *BackupS3Config, startedAt time.Time, filters RequestDetailFilters) {
	defer func() {
		s.backupMu.Lock()
		s.backingUp = false
		s.backupMu.Unlock()
	}()
	defer func() {
		if r := recover(); r != nil {
			record.Status = "failed"
			record.ErrorMsg = fmt.Sprintf("internal panic: %v", r)
			record.Progress = ""
			record.FinishedAt = time.Now().Format(time.RFC3339)
			_ = s.saveRecord(context.Background(), record)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	record.Progress = "exporting"
	_ = s.saveRecord(ctx, record)

	uploadedParts, totalSize, err := s.writeAndUploadRequestDetailBackupParts(ctx, store, cfg, record, startedAt, filters)
	if err != nil {
		recordRequestDetailBackupFailure(record, err)
		_ = s.saveRecord(context.Background(), record)
		cleanupUploadedRequestDetailBackupParts(context.Background(), store, record.Parts)
		return
	}

	if len(uploadedParts) == 0 {
		record.Status = "failed"
		record.ErrorMsg = "request detail backup produced no parts"
		record.Progress = ""
		record.FinishedAt = time.Now().Format(time.RFC3339)
		_ = s.saveRecord(context.Background(), record)
		return
	}

	record.Status = "completed"
	record.FileName = uploadedParts[0].FileName
	record.S3Key = uploadedParts[0].S3Key
	record.SizeBytes = totalSize
	record.Parts = uploadedParts
	record.Progress = ""
	record.FinishedAt = time.Now().Format(time.RFC3339)
	_ = s.saveRecord(context.Background(), record)
}

func (s *RequestDetailBackupService) writeAndUploadRequestDetailBackupParts(ctx context.Context, store BackupObjectStore, cfg *BackupS3Config, record *BackupRecord, startedAt time.Time, filters RequestDetailFilters) ([]BackupRecordPart, int64, error) {
	if s == nil || s.requestDetailService == nil || s.requestDetailService.repo == nil {
		return nil, 0, ErrBackupS3NotConfigured.WithCause(fmt.Errorf("request detail backup service is not initialized"))
	}
	dateFolder := startedAt.Format("20060102")
	var uploadedParts []BackupRecordPart
	var totalSize int64
	var current *requestDetailBackupPartWriter

	uploadCurrent := func() error {
		if current == nil {
			return nil
		}
		part, err := current.close()
		current = nil
		if err != nil {
			return err
		}
		defer cleanupRequestDetailBackupTempParts([]requestDetailBackupTempPart{*part})
		record.Progress = fmt.Sprintf("uploading part %d", part.Index)
		_ = s.saveRecord(ctx, record)
		sizeBytes, err := uploadRequestDetailBackupPart(ctx, store, *part)
		if err != nil {
			return err
		}
		part.SizeBytes = sizeBytes
		uploadedParts = append(uploadedParts, part.BackupRecordPart)
		totalSize += sizeBytes
		record.Parts = append([]BackupRecordPart(nil), uploadedParts...)
		record.SizeBytes = totalSize
		_ = s.saveRecord(ctx, record)
		record.Progress = "exporting"
		_ = s.saveRecord(ctx, record)
		return nil
	}
	openNext := func() error {
		partIndex := len(uploadedParts) + 1
		if current != nil {
			partIndex++
		}
		fileName := buildRequestDetailBackupFileName(startedAt, partIndex)
		s3Key := buildRequestDetailBackupKey(cfg, dateFolder, fileName)
		writer, err := newRequestDetailBackupPartWriter(partIndex, fileName, s3Key)
		if err != nil {
			return err
		}
		current = writer
		return nil
	}

	err := s.requestDetailService.repo.StreamAll(ctx, filters, func(detail RequestDetail) error {
		if current == nil {
			if err := openNext(); err != nil {
				return err
			}
		}
		enc := json.NewEncoder(current)
		if err := enc.Encode(detail); err != nil {
			return err
		}
		if err := current.flush(); err != nil {
			return err
		}
		if current.sizeBytes() < requestDetailBackupPartSizeBytes {
			return nil
		}
		if err := uploadCurrent(); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		if current != nil {
			_ = current.abort()
		}
		return nil, 0, err
	}
	if err := uploadCurrent(); err != nil {
		return nil, 0, err
	}
	if len(uploadedParts) == 0 {
		if err := openNext(); err != nil {
			return nil, 0, err
		}
		if err := uploadCurrent(); err != nil {
			return nil, 0, err
		}
	}
	return uploadedParts, totalSize, nil
}

func uploadRequestDetailBackupPart(ctx context.Context, store BackupObjectStore, part requestDetailBackupTempPart) (int64, error) {
	file, err := os.Open(part.tempPath)
	if err != nil {
		return 0, err
	}
	sizeBytes, uploadErr := store.Upload(ctx, part.S3Key, file, requestDetailBackupContentType)
	closeErr := file.Close()
	if uploadErr != nil {
		return 0, uploadErr
	}
	if closeErr != nil {
		return 0, closeErr
	}
	if sizeBytes <= 0 {
		return 0, fmt.Errorf("S3 upload verification failed: object size is %d bytes key=%s content_type=%s", sizeBytes, part.S3Key, requestDetailBackupContentType)
	}
	return sizeBytes, nil
}

type requestDetailBackupPartWriter struct {
	BackupRecordPart
	file    *os.File
	counter *countingWriter
	gzip    *gzip.Writer
	closed  bool
}

func newRequestDetailBackupPartWriter(index int, fileName string, s3Key string) (*requestDetailBackupPartWriter, error) {
	file, err := os.CreateTemp("", "sub2api-request-detail-backup-*.ndjson.gz")
	if err != nil {
		return nil, fmt.Errorf("create temp request detail backup part: %w", err)
	}
	counter := &countingWriter{w: file}
	return &requestDetailBackupPartWriter{
		BackupRecordPart: BackupRecordPart{
			Index:    index,
			FileName: fileName,
			S3Key:    s3Key,
		},
		file:    file,
		counter: counter,
		gzip:    gzip.NewWriter(counter),
	}, nil
}

func (w *requestDetailBackupPartWriter) Write(p []byte) (int, error) {
	return w.gzip.Write(p)
}

func (w *requestDetailBackupPartWriter) flush() error {
	return w.gzip.Flush()
}

func (w *requestDetailBackupPartWriter) sizeBytes() int64 {
	return w.counter.n
}

func (w *requestDetailBackupPartWriter) close() (*requestDetailBackupTempPart, error) {
	if w.closed {
		return nil, fmt.Errorf("request detail backup part already closed")
	}
	w.closed = true
	if err := w.gzip.Close(); err != nil {
		_ = w.abort()
		return nil, err
	}
	w.SizeBytes = w.counter.n
	tempPath := w.file.Name()
	if err := w.file.Close(); err != nil {
		_ = os.Remove(tempPath)
		return nil, err
	}
	return &requestDetailBackupTempPart{
		BackupRecordPart: w.BackupRecordPart,
		tempPath:         tempPath,
	}, nil
}

func (w *requestDetailBackupPartWriter) abort() error {
	w.closed = true
	if w.file != nil {
		name := w.file.Name()
		_ = w.file.Close()
		_ = os.Remove(name)
	}
	return nil
}

func (s *RequestDetailBackupService) ListBackups(ctx context.Context) ([]BackupRecord, error) {
	records, err := s.loadRecords(ctx)
	if err != nil {
		return nil, err
	}
	sort.Slice(records, func(i, j int) bool {
		return records[i].StartedAt > records[j].StartedAt
	})
	return records, nil
}

func (s *RequestDetailBackupService) GetBackupRecord(ctx context.Context, id string) (*BackupRecord, error) {
	records, err := s.loadRecords(ctx)
	if err != nil {
		return nil, err
	}
	for i := range records {
		if records[i].ID == id {
			record := records[i]
			return &record, nil
		}
	}
	return nil, ErrBackupNotFound
}

func (s *RequestDetailBackupService) GetBackupDownloadURLs(ctx context.Context, id string) (*RequestDetailBackupDownloadURLs, error) {
	record, err := s.GetBackupRecord(ctx, id)
	if err != nil {
		return nil, err
	}
	if record.Status != "completed" {
		return nil, fmt.Errorf("backup is not completed")
	}
	store, _, err := s.backupService.NewConfiguredObjectStore(ctx)
	if err != nil {
		return nil, err
	}

	recordParts := normalizeRequestDetailBackupRecordParts(record)
	result := &RequestDetailBackupDownloadURLs{
		URLs:  make([]string, 0, len(recordParts)),
		Parts: make([]RequestDetailBackupDownloadPart, 0, len(recordParts)),
	}
	for _, part := range recordParts {
		url, err := store.PresignURL(ctx, part.S3Key, time.Hour)
		if err != nil {
			return nil, err
		}
		if result.URL == "" {
			result.URL = url
		}
		result.URLs = append(result.URLs, url)
		result.Parts = append(result.Parts, RequestDetailBackupDownloadPart{
			BackupRecordPart: part,
			URL:              url,
		})
	}
	return result, nil
}

func (s *RequestDetailBackupService) GetBackupDownloadURL(ctx context.Context, id string) (string, error) {
	result, err := s.GetBackupDownloadURLs(ctx, id)
	if err != nil {
		return "", err
	}
	return result.URL, nil
}

func (s *RequestDetailBackupService) applyCronScheduleLocked(cfg *BackupScheduleConfig) error {
	if s.cronSched == nil {
		return nil
	}
	if s.cronEntryID != 0 {
		s.cronSched.Remove(s.cronEntryID)
		s.cronEntryID = 0
	}
	entryID, err := s.cronSched.AddFunc(cfg.CronExpr, func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		_, _ = s.StartScheduledBackup(ctx)
	})
	if err != nil {
		return err
	}
	s.cronEntryID = entryID
	return nil
}

func (s *RequestDetailBackupService) loadRecords(ctx context.Context) ([]BackupRecord, error) {
	raw, err := s.settingRepo.GetValue(ctx, settingKeyRequestDetailBackupRecords)
	if err != nil || raw == "" {
		return nil, nil
	}
	var records []BackupRecord
	if err := json.Unmarshal([]byte(raw), &records); err != nil {
		return nil, ErrBackupRecordsCorrupt
	}
	return records, nil
}

func (s *RequestDetailBackupService) saveRecord(ctx context.Context, record *BackupRecord) error {
	records, err := s.loadRecords(ctx)
	if err != nil {
		return err
	}
	found := false
	for i := range records {
		if records[i].ID == record.ID {
			records[i] = *record
			found = true
			break
		}
	}
	if !found {
		records = append(records, *record)
	}
	if len(records) > maxBackupRecords {
		records = records[len(records)-maxBackupRecords:]
	}
	data, err := json.Marshal(records)
	if err != nil {
		return err
	}
	return s.settingRepo.Set(ctx, settingKeyRequestDetailBackupRecords, string(data))
}

func buildRequestDetailBackupFileName(t time.Time, partIndex int) string {
	return fmt.Sprintf("request_details_%s_part%03d.ndjson.gz", t.Format("20060102_150405"), partIndex)
}

func buildRequestDetailBackupKey(cfg *BackupS3Config, dateFolder string, fileName string) string {
	prefix := strings.TrimRight(cfg.Prefix, "/")
	if prefix == "" {
		prefix = "backups"
	}
	return fmt.Sprintf("%s/request-details/%s/%s", prefix, dateFolder, fileName)
}

func normalizeRequestDetailBackupRecordParts(record *BackupRecord) []BackupRecordPart {
	if record == nil {
		return nil
	}
	if len(record.Parts) > 0 {
		parts := make([]BackupRecordPart, len(record.Parts))
		copy(parts, record.Parts)
		return parts
	}
	if strings.TrimSpace(record.S3Key) == "" {
		return nil
	}
	return []BackupRecordPart{{
		Index:     1,
		FileName:  record.FileName,
		S3Key:     record.S3Key,
		SizeBytes: record.SizeBytes,
	}}
}

func previousDayRequestDetailFilters(now time.Time) RequestDetailFilters {
	end := timezone.StartOfDay(now)
	start := end.AddDate(0, 0, -1)
	return RequestDetailFilters{
		StartTime: &start,
		EndTime:   &end,
	}
}
