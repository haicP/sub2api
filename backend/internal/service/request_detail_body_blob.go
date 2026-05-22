package service

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"time"
)

const (
	RequestDetailBodyBlobCodecGzip      = "gzip"
	RequestDetailBodyCompressionMinSize = 8 * 1024
)

type RequestDetailBodyBlob struct {
	ID                  int64     `json:"id"`
	SHA256              string    `json:"sha256"`
	Codec               string    `json:"codec"`
	RawSizeBytes        int       `json:"raw_size_bytes"`
	CompressedSizeBytes int       `json:"compressed_size_bytes"`
	Content             []byte    `json:"content,omitempty"`
	CreatedAt           time.Time `json:"created_at"`
}

type RequestDetailBodyRef struct {
	BlobID              *int64 `json:"blob_id,omitempty"`
	SHA256              string `json:"sha256,omitempty"`
	RawSizeBytes        int    `json:"raw_size_bytes,omitempty"`
	CompressedSizeBytes int    `json:"compressed_size_bytes,omitempty"`
	Codec               string `json:"codec,omitempty"`
	Content             []byte `json:"-"`
}

func BuildRequestDetailBodyBlob(raw string) (*RequestDetailBodyRef, error) {
	if raw == "" {
		return &RequestDetailBodyRef{}, nil
	}
	rawBytes := []byte(raw)
	sum := sha256.Sum256(rawBytes)
	compressed, err := gzipCompress(rawBytes)
	if err != nil {
		return nil, err
	}
	return &RequestDetailBodyRef{
		SHA256:              hex.EncodeToString(sum[:]),
		RawSizeBytes:        len(rawBytes),
		CompressedSizeBytes: len(compressed),
		Codec:               RequestDetailBodyBlobCodecGzip,
		Content:             compressed,
	}, nil
}

func DecodeRequestDetailBodyBlob(ref RequestDetailBodyRef) (string, error) {
	if ref.Codec != "" && ref.Codec != RequestDetailBodyBlobCodecGzip {
		return "", fmt.Errorf("unsupported request detail body codec: %s", ref.Codec)
	}
	if len(ref.Content) == 0 {
		return "", nil
	}
	raw, err := gzipDecompress(ref.Content)
	if err != nil {
		return "", err
	}
	if ref.RawSizeBytes > 0 && len(raw) != ref.RawSizeBytes {
		return "", fmt.Errorf("request detail body raw size mismatch: got=%d want=%d", len(raw), ref.RawSizeBytes)
	}
	if ref.SHA256 != "" {
		sum := sha256.Sum256(raw)
		if got := hex.EncodeToString(sum[:]); got != ref.SHA256 {
			return "", fmt.Errorf("request detail body sha256 mismatch: got=%s want=%s", got, ref.SHA256)
		}
	}
	return string(raw), nil
}

func gzipCompress(raw []byte) ([]byte, error) {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(raw); err != nil {
		_ = zw.Close()
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func gzipDecompress(compressed []byte) ([]byte, error) {
	zr, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		return nil, err
	}
	defer func() { _ = zr.Close() }()
	out, err := io.ReadAll(zr)
	if err != nil {
		return nil, err
	}
	return out, nil
}
