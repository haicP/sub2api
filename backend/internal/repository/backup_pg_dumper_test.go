package repository

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

func TestBuildPgDumpArgsExcludesRequestDetailTables(t *testing.T) {
	args := buildPgDumpArgs(&config.DatabaseConfig{
		Host:   "db",
		Port:   5432,
		User:   "postgres",
		DBName: "sub2api",
	})

	require.Contains(t, args, "--exclude-table=request_details")
	require.Contains(t, args, "--exclude-table-data=request_details")
	require.Contains(t, args, "--exclude-table=request_detail_body_blobs")
	require.Contains(t, args, "--exclude-table-data=request_detail_body_blobs")
	require.Contains(t, args, "--exclude-table=request_detail_image_artifacts")
	require.Contains(t, args, "--exclude-table-data=request_detail_image_artifacts")
}
