package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/tidwall/gjson"
)

const (
	requestDetailArtifactStatusStored  = "stored"
	requestDetailArtifactStatusSkipped = "skipped"
	requestDetailArtifactStatusFailed  = "failed"

	requestDetailMaxRemoteImageBytes = 20 << 20
)

type requestDetailArtifactProcessor struct {
	requestID           string
	createdAt           time.Time
	store               BackupObjectStore
	cfg                 *BackupS3Config
	nextIndex           int
	collectedArtifacts  []RequestDetailImageArtifact
	artifactRefsByHash  map[string]requestDetailArtifactRef
	artifactIndexByHash map[string]int
}

type requestDetailImagePayload struct {
	Direction   string
	Source      string
	ContentType string
	FileName    string
	Data        []byte
	OriginalURL string
	ImageIndex  *int
	Metadata    map[string]any
}

type requestDetailArtifactRef struct {
	ArtifactID  string `json:"artifact_ref"`
	Status      string `json:"status"`
	S3Key       string `json:"s3_key,omitempty"`
	ContentType string `json:"content_type,omitempty"`
	SizeBytes   int64  `json:"size_bytes,omitempty"`
	SHA256      string `json:"sha256,omitempty"`
	Error       string `json:"error_message,omitempty"`
}

func (s *RequestDetailService) prepareImageArtifacts(detail *RequestDetail) {
	if s == nil || detail == nil || !isRequestDetailImageEndpoint(detail.Endpoint) {
		return
	}
	processor := s.newRequestDetailArtifactProcessor(detail)

	detail.RequestBody = processor.sanitizeRequestBody(detail.RequestBody, detail.RequestHeaders, "request")
	detail.UpstreamRequestBody = processor.sanitizeRequestBody(detail.UpstreamRequestBody, nil, "upstream_request")
	detail.ResponseBody = processor.sanitizeResponseBody(detail.ResponseBody)
	detail.ResponseContent = extractResponseContent(RequestDetailContext{
		Platform: detail.Platform,
		Endpoint: detail.Endpoint,
	}, detail.ResponseBody)
	detail.ImageArtifacts = processor.artifacts()
}

func (s *RequestDetailService) newRequestDetailArtifactProcessor(detail *RequestDetail) *requestDetailArtifactProcessor {
	p := &requestDetailArtifactProcessor{
		requestID: strings.TrimSpace(detail.RequestID),
		createdAt: detail.CreatedAt,
	}
	if p.createdAt.IsZero() {
		p.createdAt = time.Now()
	}
	if s == nil || s.backupService == nil {
		return p
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	store, cfg, err := s.backupService.NewConfiguredObjectStore(ctx)
	if err != nil {
		return p
	}
	p.store = store
	p.cfg = cfg
	return p
}

func (p *requestDetailArtifactProcessor) artifacts() []RequestDetailImageArtifact {
	if p == nil {
		return nil
	}
	return p.collectedArtifacts
}

func (p *requestDetailArtifactProcessor) sanitizeRequestBody(body string, headers map[string][]string, direction string) string {
	body = strings.TrimSpace(body)
	if body == "" {
		return body
	}
	if isMultipartRequestDetailBody(headers) {
		return p.sanitizeMultipartBody(body, headers, direction)
	}
	return p.sanitizeJSONOrSSE(body, direction, false)
}

func (p *requestDetailArtifactProcessor) sanitizeResponseBody(body string) string {
	body = strings.TrimSpace(body)
	if body == "" {
		return body
	}
	return p.sanitizeJSONOrSSE(body, "response", true)
}

func (p *requestDetailArtifactProcessor) sanitizeJSONOrSSE(body string, direction string, downloadRemoteURL bool) string {
	if !gjson.Valid(body) {
		if looksLikeRequestDetailSSE(body) {
			return p.sanitizeSSEBody(body, direction, downloadRemoteURL)
		}
		return body
	}
	var value any
	if err := json.Unmarshal([]byte(body), &value); err != nil {
		return body
	}
	sanitized := p.sanitizeJSONValue(value, direction, "$", downloadRemoteURL)
	out, err := json.Marshal(sanitized)
	if err != nil {
		return body
	}
	return string(out)
}

func looksLikeRequestDetailSSE(body string) bool {
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(strings.TrimRight(line, "\r"))
		if line == "" {
			continue
		}
		return strings.HasPrefix(line, "data:") || strings.HasPrefix(line, "event:")
	}
	return false
}

func (p *requestDetailArtifactProcessor) sanitizeSSEBody(body string, direction string, downloadRemoteURL bool) string {
	var out []string
	var dataLines []string
	flushData := func() {
		if len(dataLines) == 0 {
			return
		}
		payload := strings.Join(dataLines, "\n")
		trimmed := strings.TrimSpace(payload)
		if trimmed != "" && trimmed != "[DONE]" && gjson.Valid(trimmed) {
			var value any
			if err := json.Unmarshal([]byte(trimmed), &value); err == nil {
				value = p.sanitizeJSONValue(value, direction, "sse.data", downloadRemoteURL)
				if b, err := json.Marshal(value); err == nil {
					payload = string(b)
				}
			}
		}
		out = append(out, "data: "+payload)
		dataLines = nil
	}

	lines := strings.Split(body, "\n")
	for _, line := range lines {
		trimmedLine := strings.TrimRight(line, "\r")
		if strings.HasPrefix(trimmedLine, "data:") {
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(trimmedLine, "data:")))
			continue
		}
		flushData()
		out = append(out, trimmedLine)
	}
	flushData()
	return strings.Join(out, "\n")
}

func (p *requestDetailArtifactProcessor) sanitizeJSONValue(value any, direction string, sourcePath string, downloadRemoteURL bool) any {
	switch v := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		eventType, _ := v["type"].(string)
		for key, item := range v {
			path := sourcePath + "." + key
			if isImageBase64Field(key) {
				if s, ok := item.(string); ok {
					if ref, changed := p.storeBase64String(s, direction, path, eventType); changed {
						out[key] = ref
						continue
					}
				}
			}
			if key == "url" {
				if s, ok := item.(string); ok {
					if ref, changed := p.storeURLString(s, direction, path, downloadRemoteURL); changed {
						out[key] = ref
						continue
					}
				}
			}
			out[key] = p.sanitizeJSONValue(item, direction, path, downloadRemoteURL)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = p.sanitizeJSONValue(item, direction, sourcePath+"."+strconv.Itoa(i), downloadRemoteURL)
		}
		return out
	case string:
		if ref, changed := p.storeDataURLString(v, direction, sourcePath); changed {
			return ref
		}
		return v
	default:
		return value
	}
}

func (p *requestDetailArtifactProcessor) sanitizeMultipartBody(body string, headers map[string][]string, direction string) string {
	contentType := headerFirst(headers, "Content-Type")
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil || !strings.EqualFold(mediaType, "multipart/form-data") {
		return "[multipart request body omitted]"
	}
	boundary := strings.TrimSpace(params["boundary"])
	if boundary == "" {
		return "[multipart request body omitted]"
	}
	reader := multipart.NewReader(strings.NewReader(body), boundary)
	parts := make([]map[string]any, 0)
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			break
		}
		formName := strings.TrimSpace(part.FormName())
		fileName := strings.TrimSpace(part.FileName())
		partContentType := strings.TrimSpace(part.Header.Get("Content-Type"))
		data, _ := io.ReadAll(io.LimitReader(part, requestDetailMaxRemoteImageBytes+1))
		_ = part.Close()
		item := map[string]any{"field": formName}
		if fileName != "" {
			item["file_name"] = fileName
			item["content_type"] = partContentType
			if isImageContentType(partContentType) && len(data) <= requestDetailMaxRemoteImageBytes {
				item["image"] = p.storeImagePayload(requestDetailImagePayload{
					Direction:   direction,
					Source:      "multipart." + formName,
					ContentType: partContentType,
					FileName:    fileName,
					Data:        data,
				})
			} else {
				item["omitted"] = true
				item["size_bytes"] = len(data)
			}
		} else {
			item["value"] = string(data)
		}
		parts = append(parts, item)
	}
	out, err := json.Marshal(map[string]any{
		"content_type": "multipart/form-data",
		"parts":        parts,
	})
	if err != nil {
		return "[multipart request body omitted]"
	}
	return string(out)
}

func (p *requestDetailArtifactProcessor) storeBase64String(value string, direction string, source string, eventType string) (requestDetailArtifactRef, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return requestDetailArtifactRef{}, false
	}
	data, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return requestDetailArtifactRef{}, false
	}
	contentType := http.DetectContentType(data)
	if !isImageContentType(contentType) {
		contentType = ""
	}
	return p.storeImagePayload(requestDetailImagePayload{
		Direction:   direction,
		Source:      source,
		ContentType: contentType,
		Data:        data,
		Metadata:    map[string]any{"event_type": eventType},
	}), true
}

func (p *requestDetailArtifactProcessor) storeDataURLString(value string, direction string, source string) (requestDetailArtifactRef, bool) {
	contentType, data, ok := parseDataURLImage(value)
	if !ok {
		return requestDetailArtifactRef{}, false
	}
	return p.storeImagePayload(requestDetailImagePayload{
		Direction:   direction,
		Source:      source,
		ContentType: contentType,
		Data:        data,
	}), true
}

func (p *requestDetailArtifactProcessor) storeURLString(value string, direction string, source string, downloadRemoteURL bool) (requestDetailArtifactRef, bool) {
	if ref, ok := p.storeDataURLString(value, direction, source); ok {
		return ref, true
	}
	if !downloadRemoteURL || !isHTTPURL(value) {
		return requestDetailArtifactRef{}, false
	}
	contentType, data, err := downloadRequestDetailImage(value)
	if err != nil {
		return p.failedArtifactRef(requestDetailImagePayload{Direction: direction, Source: source, OriginalURL: value}, err), true
	}
	return p.storeImagePayload(requestDetailImagePayload{
		Direction:   direction,
		Source:      source,
		ContentType: contentType,
		Data:        data,
		OriginalURL: value,
	}), true
}

func (p *requestDetailArtifactProcessor) storeImagePayload(payload requestDetailImagePayload) requestDetailArtifactRef {
	if p == nil {
		return requestDetailArtifactRef{Status: requestDetailArtifactStatusFailed, Error: "artifact processor unavailable"}
	}
	contentType := strings.TrimSpace(payload.ContentType)
	if contentType == "" && len(payload.Data) > 0 {
		contentType = http.DetectContentType(payload.Data)
	}
	sum := sha256.Sum256(payload.Data)
	sha := hex.EncodeToString(sum[:])
	dedupKey := payload.Direction + ":" + sha
	if ref, ok := p.findDuplicateArtifact(dedupKey, payload.Source); ok {
		return ref
	}
	artifactID := p.nextArtifactID()
	artifact := RequestDetailImageArtifact{
		RequestID:   p.requestID,
		Direction:   payload.Direction,
		Source:      payload.Source,
		Status:      requestDetailArtifactStatusStored,
		OriginalURL: payload.OriginalURL,
		ContentType: contentType,
		FileName:    payload.FileName,
		SizeBytes:   int64(len(payload.Data)),
		SHA256:      sha,
		ImageIndex:  payload.ImageIndex,
		Metadata:    payload.Metadata,
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}
	if artifact.Metadata == nil {
		artifact.Metadata = map[string]any{}
	}
	artifact.Metadata["artifact_ref"] = artifactID

	if p.store == nil || p.cfg == nil {
		artifact.Status = requestDetailArtifactStatusSkipped
		artifact.ErrorMessage = "backup S3 storage is not configured"
		p.appendArtifact(artifact)
		ref := requestDetailArtifactRef{
			ArtifactID:  artifactID,
			Status:      artifact.Status,
			ContentType: contentType,
			SizeBytes:   int64(len(payload.Data)),
			SHA256:      sha,
			Error:       artifact.ErrorMessage,
		}
		p.rememberArtifact(dedupKey, ref)
		return ref
	}

	key := p.buildArtifactKey(artifactID, contentType, payload.FileName)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if _, err := p.store.Upload(ctx, key, bytes.NewReader(payload.Data), contentType); err != nil {
		artifact.Status = requestDetailArtifactStatusFailed
		artifact.ErrorMessage = err.Error()
		p.appendArtifact(artifact)
		ref := requestDetailArtifactRef{
			ArtifactID:  artifactID,
			Status:      artifact.Status,
			ContentType: contentType,
			SizeBytes:   int64(len(payload.Data)),
			SHA256:      sha,
			Error:       artifact.ErrorMessage,
		}
		p.rememberArtifact(dedupKey, ref)
		return ref
	}
	artifact.S3Key = key
	p.appendArtifact(artifact)
	ref := requestDetailArtifactRef{
		ArtifactID:  artifactID,
		Status:      artifact.Status,
		S3Key:       key,
		ContentType: contentType,
		SizeBytes:   int64(len(payload.Data)),
		SHA256:      sha,
	}
	p.rememberArtifact(dedupKey, ref)
	return ref
}

func (p *requestDetailArtifactProcessor) failedArtifactRef(payload requestDetailImagePayload, err error) requestDetailArtifactRef {
	artifactID := p.nextArtifactID()
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	p.appendArtifact(RequestDetailImageArtifact{
		RequestID:    p.requestID,
		Direction:    payload.Direction,
		Source:       payload.Source,
		Status:       requestDetailArtifactStatusFailed,
		OriginalURL:  payload.OriginalURL,
		ErrorMessage: msg,
		Metadata:     map[string]any{"artifact_ref": artifactID},
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	})
	return requestDetailArtifactRef{ArtifactID: artifactID, Status: requestDetailArtifactStatusFailed, Error: msg}
}

func (p *requestDetailArtifactProcessor) nextArtifactID() string {
	if p == nil {
		return "artifact-0"
	}
	p.nextIndex++
	return fmt.Sprintf("artifact-%d", p.nextIndex)
}

func (p *requestDetailArtifactProcessor) buildArtifactKey(artifactID string, contentType string, fileName string) string {
	prefix := "backups"
	if p != nil && p.cfg != nil && strings.TrimSpace(p.cfg.Prefix) != "" {
		prefix = strings.TrimRight(strings.TrimSpace(p.cfg.Prefix), "/")
	}
	ext := strings.ToLower(filepath.Ext(fileName))
	if ext == "" {
		ext = extensionForImageContentType(contentType)
	}
	if ext == "" {
		ext = ".bin"
	}
	return fmt.Sprintf("%s/request-detail-images/%s/%s/%s%s",
		prefix,
		p.createdAt.Format("2006/01/02"),
		sanitizeS3PathPart(p.requestID),
		sanitizeS3PathPart(artifactID),
		ext,
	)
}

func (p *requestDetailArtifactProcessor) appendArtifact(artifact RequestDetailImageArtifact) {
	if p == nil {
		return
	}
	p.collectedArtifacts = append(p.collectedArtifacts, artifact)
}

func (p *requestDetailArtifactProcessor) findDuplicateArtifact(key string, source string) (requestDetailArtifactRef, bool) {
	if p == nil || key == ":" || p.artifactRefsByHash == nil {
		return requestDetailArtifactRef{}, false
	}
	ref, ok := p.artifactRefsByHash[key]
	if !ok {
		return requestDetailArtifactRef{}, false
	}
	if idx, ok := p.artifactIndexByHash[key]; ok && idx >= 0 && idx < len(p.collectedArtifacts) {
		artifact := p.collectedArtifacts[idx]
		artifact.Source = mergeRequestDetailArtifactSources(artifact.Source, source)
		if artifact.Metadata == nil {
			artifact.Metadata = map[string]any{}
		}
		artifact.Metadata["sources"] = splitRequestDetailArtifactSources(artifact.Source)
		p.collectedArtifacts[idx] = artifact
	}
	return ref, true
}

func (p *requestDetailArtifactProcessor) rememberArtifact(key string, ref requestDetailArtifactRef) {
	if p == nil || key == ":" {
		return
	}
	if p.artifactRefsByHash == nil {
		p.artifactRefsByHash = make(map[string]requestDetailArtifactRef)
	}
	if p.artifactIndexByHash == nil {
		p.artifactIndexByHash = make(map[string]int)
	}
	p.artifactRefsByHash[key] = ref
	p.artifactIndexByHash[key] = len(p.collectedArtifacts) - 1
}

func mergeRequestDetailArtifactSources(values ...string) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, splitRequestDetailArtifactSources(value)...)
	}
	if len(parts) == 0 {
		return ""
	}
	seen := make(map[string]struct{}, len(parts))
	unique := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if _, ok := seen[part]; ok {
			continue
		}
		seen[part] = struct{}{}
		unique = append(unique, part)
	}
	sort.Strings(unique)
	return strings.Join(unique, ", ")
}

func splitRequestDetailArtifactSources(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	raw := strings.Split(value, ",")
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		item = strings.TrimSpace(item)
		if item != "" {
			out = append(out, item)
		}
	}
	return out
}

func isRequestDetailImageEndpoint(endpoint string) bool {
	endpoint = strings.ToLower(strings.TrimSpace(endpoint))
	return strings.Contains(endpoint, "/images/generations") || strings.Contains(endpoint, "/images/edits")
}

func isImageBase64Field(key string) bool {
	key = strings.ToLower(strings.TrimSpace(key))
	switch key {
	case "b64_json", "partial_image_b64", "result":
		return true
	default:
		return false
	}
}

func isMultipartRequestDetailBody(headers map[string][]string) bool {
	contentType := strings.ToLower(strings.TrimSpace(headerFirst(headers, "Content-Type")))
	return strings.HasPrefix(contentType, "multipart/form-data")
}

func headerFirst(headers map[string][]string, name string) string {
	for key, values := range headers {
		if strings.EqualFold(key, name) && len(values) > 0 {
			return values[0]
		}
	}
	return ""
}

func parseDataURLImage(value string) (string, []byte, bool) {
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(value)), "data:image/") {
		return "", nil, false
	}
	prefix, encoded, ok := strings.Cut(value, ",")
	if !ok || !strings.Contains(strings.ToLower(prefix), ";base64") {
		return "", nil, false
	}
	contentType := strings.TrimPrefix(prefix, "data:")
	contentType = strings.TrimSuffix(contentType, ";base64")
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", nil, false
	}
	return contentType, data, true
}

func isImageContentType(contentType string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(contentType)), "image/")
}

func isHTTPURL(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	return strings.HasPrefix(value, "https://") || strings.HasPrefix(value, "http://")
}

func downloadRequestDetailImage(rawURL string) (string, []byte, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, rawURL, nil)
	if err != nil {
		return "", nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", nil, fmt.Errorf("download image status %d", resp.StatusCode)
	}
	contentType := strings.TrimSpace(resp.Header.Get("Content-Type"))
	if !isImageContentType(contentType) {
		return "", nil, fmt.Errorf("downloaded content is not image")
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, requestDetailMaxRemoteImageBytes+1))
	if err != nil {
		return "", nil, err
	}
	if len(data) > requestDetailMaxRemoteImageBytes {
		return "", nil, fmt.Errorf("image exceeds %d bytes", requestDetailMaxRemoteImageBytes)
	}
	return contentType, data, nil
}

func extensionForImageContentType(contentType string) string {
	switch strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0])) {
	case "image/png":
		return ".png"
	case "image/jpeg", "image/jpg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	default:
		return ""
	}
}

func sanitizeS3PathPart(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_' || r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}
