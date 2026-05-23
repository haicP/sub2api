package service

import (
	"encoding/json"
	"io"
)

const RequestDetailBackupArchiveFormatVersion = 3

type RequestDetailBackupArchiveWriter struct {
	enc *json.Encoder
}

type requestDetailBackupDetailRecord struct {
	Type          string        `json:"type"`
	FormatVersion int           `json:"format_version"`
	Detail        RequestDetail `json:"detail"`
}

func NewRequestDetailBackupArchiveWriter(w io.Writer) *RequestDetailBackupArchiveWriter {
	return &RequestDetailBackupArchiveWriter{
		enc: json.NewEncoder(w),
	}
}

func (w *RequestDetailBackupArchiveWriter) Reset(out io.Writer) {
	if w == nil {
		return
	}
	w.enc = json.NewEncoder(out)
}

func (w *RequestDetailBackupArchiveWriter) WriteDetail(detail RequestDetail) error {
	out := detail
	out.RequestBodyRef = RequestDetailBodyRef{}
	out.UpstreamRequestRef = RequestDetailBodyRef{}
	out.ResponseContentRef = RequestDetailBodyRef{}
	out.ResponseBodyRef = RequestDetailBodyRef{}
	return w.enc.Encode(requestDetailBackupDetailRecord{
		Type:          "request_detail",
		FormatVersion: RequestDetailBackupArchiveFormatVersion,
		Detail:        out,
	})
}
