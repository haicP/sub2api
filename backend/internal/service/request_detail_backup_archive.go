package service

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
)

const RequestDetailBackupArchiveFormatVersion = 2

type RequestDetailBackupArchiveWriter struct {
	enc       *json.Encoder
	seenBlobs map[string]struct{}
}

type requestDetailBackupBodyBlobRecord struct {
	Type                string `json:"type"`
	FormatVersion       int    `json:"format_version"`
	SHA256              string `json:"sha256"`
	Codec               string `json:"codec"`
	RawSizeBytes        int    `json:"raw_size_bytes"`
	CompressedSizeBytes int    `json:"compressed_size_bytes"`
	ContentBase64       string `json:"content_base64"`
}

type requestDetailBackupBodyRefRecord struct {
	SHA256              string `json:"sha256,omitempty"`
	Codec               string `json:"codec,omitempty"`
	RawSizeBytes        int    `json:"raw_size_bytes,omitempty"`
	CompressedSizeBytes int    `json:"compressed_size_bytes,omitempty"`
	Inline              string `json:"inline,omitempty"`
}

type requestDetailBackupDetailRecord struct {
	Type          string        `json:"type"`
	FormatVersion int           `json:"format_version"`
	Detail        RequestDetail `json:"detail"`

	RequestBody         requestDetailBackupBodyRefRecord `json:"request_body_ref,omitempty"`
	UpstreamRequestBody requestDetailBackupBodyRefRecord `json:"upstream_request_body_ref,omitempty"`
	ResponseContent     requestDetailBackupBodyRefRecord `json:"response_content_ref,omitempty"`
	ResponseBody        requestDetailBackupBodyRefRecord `json:"response_body_ref,omitempty"`
}

func NewRequestDetailBackupArchiveWriter(w io.Writer) *RequestDetailBackupArchiveWriter {
	return &RequestDetailBackupArchiveWriter{
		enc:       json.NewEncoder(w),
		seenBlobs: map[string]struct{}{},
	}
}

func (w *RequestDetailBackupArchiveWriter) Reset(out io.Writer) {
	if w == nil {
		return
	}
	w.enc = json.NewEncoder(out)
}

func (w *RequestDetailBackupArchiveWriter) WriteDetail(detail RequestDetail) error {
	requestRef, err := w.prepareBody(detail.RequestBody, detail.RequestBodyRef)
	if err != nil {
		return err
	}
	upstreamRef, err := w.prepareBody(detail.UpstreamRequestBody, detail.UpstreamRequestRef)
	if err != nil {
		return err
	}
	responseContentRef, err := w.prepareBody(detail.ResponseContent, detail.ResponseContentRef)
	if err != nil {
		return err
	}
	responseRef, err := w.prepareBody(detail.ResponseBody, detail.ResponseBodyRef)
	if err != nil {
		return err
	}

	out := detail
	out.RequestBody = ""
	out.UpstreamRequestBody = ""
	out.ResponseContent = ""
	out.ResponseBody = ""
	out.RequestBodyRef = RequestDetailBodyRef{}
	out.UpstreamRequestRef = RequestDetailBodyRef{}
	out.ResponseContentRef = RequestDetailBodyRef{}
	out.ResponseBodyRef = RequestDetailBodyRef{}
	return w.enc.Encode(requestDetailBackupDetailRecord{
		Type:                "request_detail",
		FormatVersion:       RequestDetailBackupArchiveFormatVersion,
		Detail:              out,
		RequestBody:         requestRef,
		UpstreamRequestBody: upstreamRef,
		ResponseContent:     responseContentRef,
		ResponseBody:        responseRef,
	})
}

func (w *RequestDetailBackupArchiveWriter) prepareBody(raw string, ref RequestDetailBodyRef) (requestDetailBackupBodyRefRecord, error) {
	if raw == "" && ref.RawSizeBytes == 0 && ref.SHA256 == "" {
		return requestDetailBackupBodyRefRecord{}, nil
	}
	if ref.SHA256 == "" || len(ref.Content) == 0 {
		if raw == "" && ref.SHA256 != "" && ref.RawSizeBytes > 0 {
			return requestDetailBackupBodyRefRecord{}, fmt.Errorf("request detail body blob content missing sha256=%s raw_size=%d", ref.SHA256, ref.RawSizeBytes)
		}
		if len([]byte(raw)) >= RequestDetailBodyCompressionMinSize {
			built, err := BuildRequestDetailBodyBlob(raw)
			if err != nil {
				return requestDetailBackupBodyRefRecord{}, err
			}
			ref = *built
		} else {
			return requestDetailBackupBodyRefRecord{Inline: raw, RawSizeBytes: len([]byte(raw))}, nil
		}
	}
	if ref.SHA256 == "" || len(ref.Content) == 0 {
		return requestDetailBackupBodyRefRecord{Inline: raw, RawSizeBytes: len([]byte(raw))}, nil
	}
	key := ref.SHA256 + ":" + strconv.Itoa(ref.RawSizeBytes) + ":" + ref.Codec
	if _, ok := w.seenBlobs[key]; !ok {
		w.seenBlobs[key] = struct{}{}
		if err := w.enc.Encode(requestDetailBackupBodyBlobRecord{
			Type:                "body_blob",
			FormatVersion:       RequestDetailBackupArchiveFormatVersion,
			SHA256:              ref.SHA256,
			Codec:               ref.Codec,
			RawSizeBytes:        ref.RawSizeBytes,
			CompressedSizeBytes: ref.CompressedSizeBytes,
			ContentBase64:       base64.StdEncoding.EncodeToString(ref.Content),
		}); err != nil {
			return requestDetailBackupBodyRefRecord{}, err
		}
	}
	return requestDetailBackupBodyRefRecord{
		SHA256:              ref.SHA256,
		Codec:               ref.Codec,
		RawSizeBytes:        ref.RawSizeBytes,
		CompressedSizeBytes: ref.CompressedSizeBytes,
	}, nil
}
