package repository

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/aws/smithy-go"
	smithyhttp "github.com/aws/smithy-go/transport/http"
	"github.com/stretchr/testify/require"
)

func TestParseS3ContentRangeSize(t *testing.T) {
	size, ok := parseS3ContentRangeSize("bytes 0-0/12345")
	require.True(t, ok)
	require.Equal(t, int64(12345), size)

	_, ok = parseS3ContentRangeSize("bytes 0-0/*")
	require.False(t, ok)

	_, ok = parseS3ContentRangeSize("invalid")
	require.False(t, ok)
}

func TestIsS3Forbidden(t *testing.T) {
	require.True(t, isS3Forbidden(&smithyhttp.ResponseError{
		Response: &smithyhttp.Response{Response: &http.Response{StatusCode: http.StatusForbidden}},
		Err:      fmt.Errorf("forbidden"),
	}))
	require.True(t, isS3Forbidden(&smithy.GenericAPIError{Code: "AccessDenied", Message: "denied"}))
	require.False(t, isS3Forbidden(fmt.Errorf("plain error")))
}

func TestSpoolBackupUploadBodyWritesReplayableTempFile(t *testing.T) {
	payload := strings.Repeat("backup-data-", 1024)
	tmp, sizeBytes, err := spoolBackupUploadBody(strings.NewReader(payload))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
	})

	require.Equal(t, int64(len(payload)), sizeBytes)
	out, err := io.ReadAll(tmp)
	require.NoError(t, err)
	require.Equal(t, payload, string(out))
}
