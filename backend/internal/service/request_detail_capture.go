package service

import (
	"bytes"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

const ginContextKeyRequestDetailCapture = "request_detail_capture"

type RequestDetailContext struct {
	Platform         string
	Endpoint         string
	UpstreamEndpoint string
	Model            string
	UpstreamModel    string
	Stream           bool
	UserID           int64
	APIKeyID         int64
	AccountID        int64
	GroupID          *int64
	SubscriptionID   *int64
	IPAddress        string
	UserAgent        string
}

type RequestDetailCapture struct {
	mu                  sync.Mutex
	requestID           string
	startedAt           time.Time
	requestHeaders      map[string][]string
	requestBody         string
	upstreamRequestBody string
	responseHeaders     http.Header
	responseBody        bytes.Buffer
	statusCode          int
	ctx                 RequestDetailContext
}

func NewRequestDetailCapture(c *gin.Context, requestID string) *RequestDetailCapture {
	capture := &RequestDetailCapture{
		requestID:       requestID,
		startedAt:       time.Now(),
		requestHeaders:  map[string][]string{},
		responseHeaders: http.Header{},
	}
	if c != nil && c.Request != nil {
		capture.requestHeaders = redactHeader(c.Request.Header)
		capture.ctx.Endpoint = c.Request.URL.Path
		capture.ctx.IPAddress = c.ClientIP()
		capture.ctx.UserAgent = c.Request.UserAgent()
	}
	return capture
}

func PutRequestDetailCapture(c *gin.Context, capture *RequestDetailCapture) {
	if c == nil || capture == nil {
		return
	}
	c.Set(ginContextKeyRequestDetailCapture, capture)
}

func GetRequestDetailCapture(c *gin.Context) (*RequestDetailCapture, bool) {
	if c == nil {
		return nil, false
	}
	value, ok := c.Get(ginContextKeyRequestDetailCapture)
	if !ok {
		return nil, false
	}
	capture, ok := value.(*RequestDetailCapture)
	return capture, ok
}

func (c *RequestDetailCapture) SetRequestBody(body []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.requestBody = string(body)
}

func (c *RequestDetailCapture) SetUpstreamRequestBody(body []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.upstreamRequestBody = string(body)
}

func (c *RequestDetailCapture) SetContext(ctx RequestDetailContext) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if ctx.Platform != "" {
		c.ctx.Platform = ctx.Platform
	}
	if ctx.Endpoint != "" {
		c.ctx.Endpoint = ctx.Endpoint
	}
	if ctx.UpstreamEndpoint != "" {
		c.ctx.UpstreamEndpoint = ctx.UpstreamEndpoint
	}
	if ctx.Model != "" {
		c.ctx.Model = ctx.Model
	}
	if ctx.UpstreamModel != "" {
		c.ctx.UpstreamModel = ctx.UpstreamModel
	}
	if ctx.Stream {
		c.ctx.Stream = true
	}
	if ctx.UserID != 0 {
		c.ctx.UserID = ctx.UserID
	}
	if ctx.APIKeyID != 0 {
		c.ctx.APIKeyID = ctx.APIKeyID
	}
	if ctx.AccountID != 0 {
		c.ctx.AccountID = ctx.AccountID
	}
	if ctx.GroupID != nil {
		c.ctx.GroupID = ctx.GroupID
	}
	if ctx.SubscriptionID != nil {
		c.ctx.SubscriptionID = ctx.SubscriptionID
	}
	if ctx.IPAddress != "" {
		c.ctx.IPAddress = ctx.IPAddress
	}
	if ctx.UserAgent != "" {
		c.ctx.UserAgent = ctx.UserAgent
	}
}

func (c *RequestDetailCapture) WrapWriter(w gin.ResponseWriter) gin.ResponseWriter {
	return &captureResponseWriter{
		ResponseWriter: w,
		capture:        c,
	}
}

func (c *RequestDetailCapture) Finish(errorMessage string) *RequestDetail {
	c.mu.Lock()
	defer c.mu.Unlock()

	completedAt := time.Now()
	durationMS := int(completedAt.Sub(c.startedAt).Milliseconds())
	statusCode := c.statusCode
	if statusCode == 0 {
		statusCode = http.StatusOK
	}

	return &RequestDetail{
		RequestID:           c.requestID,
		CreatedAt:           c.startedAt,
		CompletedAt:         &completedAt,
		DurationMS:          &durationMS,
		StatusCode:          statusCode,
		Success:             statusCode >= 200 && statusCode < 400 && errorMessage == "",
		Platform:            c.ctx.Platform,
		Endpoint:            c.ctx.Endpoint,
		UpstreamEndpoint:    c.ctx.UpstreamEndpoint,
		Model:               c.ctx.Model,
		UpstreamModel:       c.ctx.UpstreamModel,
		Stream:              c.ctx.Stream,
		UserID:              c.ctx.UserID,
		APIKeyID:            c.ctx.APIKeyID,
		AccountID:           c.ctx.AccountID,
		GroupID:             c.ctx.GroupID,
		SubscriptionID:      c.ctx.SubscriptionID,
		IPAddress:           c.ctx.IPAddress,
		UserAgent:           c.ctx.UserAgent,
		RequestHeaders:      cloneHeaderMap(c.requestHeaders),
		RequestBody:         c.requestBody,
		UpstreamRequestBody: c.upstreamRequestBody,
		ResponseHeaders:     redactHeader(c.responseHeaders),
		ResponseBody:        c.responseBody.String(),
		ResponseTruncated:   false,
		ErrorMessage:        errorMessage,
	}
}

type captureResponseWriter struct {
	gin.ResponseWriter
	capture *RequestDetailCapture
}

func (w *captureResponseWriter) Write(data []byte) (int, error) {
	w.capture.mu.Lock()
	w.capture.statusCode = w.Status()
	_, _ = w.capture.responseBody.Write(data)
	w.capture.responseHeaders = w.Header().Clone()
	w.capture.mu.Unlock()
	return w.ResponseWriter.Write(data)
}

func (w *captureResponseWriter) WriteString(s string) (int, error) {
	w.capture.mu.Lock()
	w.capture.statusCode = w.Status()
	_, _ = w.capture.responseBody.WriteString(s)
	w.capture.responseHeaders = w.Header().Clone()
	w.capture.mu.Unlock()
	return w.ResponseWriter.WriteString(s)
}

func (w *captureResponseWriter) WriteHeader(code int) {
	w.capture.mu.Lock()
	w.capture.statusCode = code
	w.capture.responseHeaders = w.Header().Clone()
	w.capture.mu.Unlock()
	w.ResponseWriter.WriteHeader(code)
}

func (w *captureResponseWriter) WriteHeaderNow() {
	w.capture.mu.Lock()
	w.capture.statusCode = w.Status()
	w.capture.responseHeaders = w.Header().Clone()
	w.capture.mu.Unlock()
	w.ResponseWriter.WriteHeaderNow()
}

func redactHeader(header http.Header) map[string][]string {
	if header == nil {
		return map[string][]string{}
	}
	out := make(map[string][]string, len(header))
	for key, values := range header {
		lower := strings.ToLower(key)
		if lower == "authorization" || lower == "proxy-authorization" || lower == "x-api-key" || lower == "cookie" || lower == "set-cookie" {
			out[key] = []string{"***REDACTED***"}
			continue
		}
		copied := make([]string, len(values))
		copy(copied, values)
		out[key] = copied
	}
	return out
}

func cloneHeaderMap(header map[string][]string) map[string][]string {
	if header == nil {
		return map[string][]string{}
	}
	out := make(map[string][]string, len(header))
	for key, values := range header {
		copied := make([]string, len(values))
		copy(copied, values)
		out[key] = copied
	}
	return out
}
