package service

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/robfig/cron/v3"
)

const (
	settingKeyRequestDetailBackupSchedule = "request_detail_backup_schedule"
	settingKeyRequestDetailBackupRecords  = "request_detail_backup_records"
)

type RequestDetailBackupService struct {
	requestDetailService *RequestDetailService
	backupService        *BackupService
	settingRepo          SettingRepository

	mu          sync.Mutex
	cronSched   *cron.Cron
	cronEntryID cron.EntryID
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
	store, cfg, err := s.backupService.NewConfiguredObjectStore(ctx)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	record := &BackupRecord{
		ID:          uuid.New().String()[:8],
		Status:      "running",
		BackupType:  "request_details",
		FileName:    fmt.Sprintf("request_details_%s.ndjson.gz", now.Format("20060102_150405")),
		TriggeredBy: triggeredBy,
		StartedAt:   now.Format(time.RFC3339),
	}
	record.S3Key = buildRequestDetailBackupKey(cfg, record.FileName)
	if err := s.saveRecord(ctx, record); err != nil {
		return nil, err
	}

	pr, pw := io.Pipe()
	go func() {
		gz := gzip.NewWriter(pw)
		writeErr := s.requestDetailService.WriteBackupNDJSON(ctx, RequestDetailFilters{}, gz)
		if closeErr := gz.Close(); writeErr == nil {
			writeErr = closeErr
		}
		if writeErr != nil {
			_ = pw.CloseWithError(writeErr)
			return
		}
		_ = pw.Close()
	}()

	sizeBytes, err := store.Upload(ctx, record.S3Key, pr, "application/gzip")
	if err != nil {
		record.Status = "failed"
		record.ErrorMsg = err.Error()
		record.FinishedAt = time.Now().Format(time.RFC3339)
		_ = s.saveRecord(context.Background(), record)
		return record, err
	}

	record.Status = "completed"
	record.SizeBytes = sizeBytes
	record.FinishedAt = time.Now().Format(time.RFC3339)
	if err := s.saveRecord(context.Background(), record); err != nil {
		return nil, err
	}
	result := *record
	return &result, nil
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

func (s *RequestDetailBackupService) GetBackupDownloadURL(ctx context.Context, id string) (string, error) {
	record, err := s.GetBackupRecord(ctx, id)
	if err != nil {
		return "", err
	}
	if record.Status != "completed" {
		return "", fmt.Errorf("backup is not completed")
	}
	store, _, err := s.backupService.NewConfiguredObjectStore(ctx)
	if err != nil {
		return "", err
	}
	return store.PresignURL(ctx, record.S3Key, time.Hour)
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
		_, _ = s.StartBackup(ctx, "scheduled")
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

func buildRequestDetailBackupKey(cfg *BackupS3Config, fileName string) string {
	prefix := strings.TrimRight(cfg.Prefix, "/")
	if prefix == "" {
		prefix = "backups"
	}
	return fmt.Sprintf("%s/request-details/%s", prefix, fileName)
}
