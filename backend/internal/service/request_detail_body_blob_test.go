package service

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRequestDetailBodyBlobRoundTrip(t *testing.T) {
	raw := strings.Repeat("large-body-", 2048)
	ref, err := BuildRequestDetailBodyBlob(raw)
	require.NoError(t, err)
	require.Equal(t, RequestDetailBodyBlobCodecGzip, ref.Codec)
	require.Equal(t, len(raw), ref.RawSizeBytes)
	require.NotEmpty(t, ref.SHA256)
	require.NotEmpty(t, ref.Content)

	got, err := DecodeRequestDetailBodyBlob(*ref)
	require.NoError(t, err)
	require.Equal(t, raw, got)
}

func TestRequestDetailBodyBlobDetectsCorruption(t *testing.T) {
	ref, err := BuildRequestDetailBodyBlob("hello")
	require.NoError(t, err)
	ref.Content = append([]byte(nil), ref.Content...)
	ref.Content[len(ref.Content)-1] ^= 0xff

	_, err = DecodeRequestDetailBodyBlob(*ref)
	require.Error(t, err)
}
