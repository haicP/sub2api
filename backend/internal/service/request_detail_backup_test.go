package service

import (
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"strings"
	"testing"
	"time"

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
