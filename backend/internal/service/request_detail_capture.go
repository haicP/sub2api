package service

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tidwall/gjson"
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
		ResponseContent:     extractResponseContent(c.ctx, c.responseBody.String()),
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

func extractResponseContent(ctx RequestDetailContext, responseBody string) string {
	body := strings.TrimSpace(responseBody)
	if body == "" {
		return ""
	}

	if text := extractOpenAIResponseContent(body); text != "" {
		return text
	}

	if text := extractAnthropicResponseContent(body); text != "" {
		return text
	}

	if text := extractGeminiResponseContent(body); text != "" {
		return text
	}

	return ""
}

func ExtractRequestDetailResponseContent(ctx RequestDetailContext, responseBody string) string {
	return extractResponseContent(ctx, responseBody)
}

func extractOpenAIResponseContent(body string) string {
	if strings.Contains(body, `"direction":"upstream_to_client"`) || strings.Contains(body, `"payload"`) {
		if text := extractOpenAIResponseContentFromWSNDJSON(body); text != "" {
			return text
		}
	}

	if strings.Contains(body, "data:") {
		if text := extractChatCompletionsContentFromSSE(body); text != "" {
			return text
		}
		if outputJSON, ok := reconstructResponseOutputFromSSE(body); ok {
			return extractTextFromOpenAIOutputJSON(outputJSON)
		}
		if text := extractOpenAIFunctionCallsFromSSE(body); text != "" {
			return text
		}
	}

	trimmed := strings.TrimSpace(body)
	if !gjson.Valid(trimmed) {
		return ""
	}

	if output := gjson.Get(trimmed, "output"); output.Exists() {
		return extractTextFromOpenAIOutputJSON([]byte(output.Raw))
	}
	if choices := gjson.Get(trimmed, "choices"); choices.Exists() {
		return extractTextFromChatChoicesJSON([]byte(choices.Raw))
	}
	return ""
}

func extractOpenAIResponseContentFromWSNDJSON(body string) string {
	var deltas []string
	var fallback string
	lines := strings.Split(body, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || !gjson.Valid(trimmed) {
			continue
		}
		payload := gjson.Get(trimmed, "payload")
		if !payload.Exists() || !payload.IsObject() {
			continue
		}
		payloadRaw := []byte(payload.Raw)
		switch payload.Get("type").String() {
		case "response.output_text.delta":
			if text := payload.Get("delta").String(); text != "" {
				deltas = append(deltas, text)
			}
		case "response.output_item.done":
			item := payload.Get("item")
			if !item.Exists() || !item.IsObject() || item.Get("type").String() != "function_call" {
				continue
			}
			var raw map[string]any
			if err := json.Unmarshal([]byte(item.Raw), &raw); err != nil {
				continue
			}
			if text := extractTextFromOpenAIFunctionCall(raw); text != "" {
				deltas = append(deltas, text)
			}
		case "response.completed":
			response := payload.Get("response")
			if !response.Exists() || !response.IsObject() {
				continue
			}
			if text := strings.TrimSpace(response.Get("output_text").String()); text != "" {
				fallback = text
				continue
			}
			if output := response.Get("output"); output.Exists() {
				if text := extractTextFromOpenAIOutputJSON([]byte(output.Raw)); text != "" {
					fallback = text
				}
			}
		}
		if len(deltas) == 0 && fallback == "" {
			if text := extractOpenAIResponseContentFromJSONPayload(payloadRaw); text != "" {
				fallback = text
			}
		}
	}
	if len(deltas) > 0 {
		return strings.Join(deltas, "")
	}
	return fallback
}

func extractOpenAIResponseContentFromJSONPayload(payload []byte) string {
	if len(payload) == 0 || !gjson.ValidBytes(payload) {
		return ""
	}
	if output := gjson.GetBytes(payload, "output"); output.Exists() {
		return extractTextFromOpenAIOutputJSON([]byte(output.Raw))
	}
	if choices := gjson.GetBytes(payload, "choices"); choices.Exists() {
		return extractTextFromChatChoicesJSON([]byte(choices.Raw))
	}
	return ""
}

func extractChatCompletionsContentFromSSE(body string) string {
	var texts []string
	forEachOpenAISSEDataPayload(body, func(data []byte) {
		if len(data) == 0 || !gjson.ValidBytes(data) {
			return
		}
		choices := gjson.GetBytes(data, "choices")
		if !choices.Exists() || !choices.IsArray() {
			return
		}
		choices.ForEach(func(_, choice gjson.Result) bool {
			if text := choice.Get("delta.content").String(); text != "" {
				texts = append(texts, text)
			}
			return true
		})
	})
	return strings.Join(texts, "")
}

func extractOpenAIFunctionCallsFromSSE(body string) string {
	var texts []string
	forEachOpenAISSEDataPayload(body, func(data []byte) {
		if len(data) == 0 || !gjson.ValidBytes(data) {
			return
		}
		if gjson.GetBytes(data, "type").String() != "response.output_item.done" {
			return
		}
		item := gjson.GetBytes(data, "item")
		if !item.Exists() || !item.IsObject() || item.Get("type").String() != "function_call" {
			return
		}
		var raw map[string]any
		if err := json.Unmarshal([]byte(item.Raw), &raw); err != nil {
			return
		}
		if text := extractTextFromOpenAIFunctionCall(raw); text != "" {
			texts = append(texts, text)
		}
	})
	return strings.Join(texts, "\n\n")
}

func extractAnthropicResponseContent(body string) string {
	if strings.Contains(body, "data:") || strings.Contains(body, "event:") {
		var texts []string
		lines := strings.Split(body, "\n")
		for _, line := range lines {
			payload, ok := extractAnthropicSSEDataLine(line)
			if !ok {
				continue
			}
			trimmed := strings.TrimSpace(payload)
			if trimmed == "" || trimmed == "[DONE]" || !gjson.Valid(trimmed) {
				continue
			}
			eventType := gjson.Get(trimmed, "type").String()
			switch eventType {
			case "content_block_start":
				if text := gjson.Get(trimmed, "content_block.text").String(); text != "" {
					texts = append(texts, text)
				}
			case "content_block_delta":
				if text := gjson.Get(trimmed, "delta.text").String(); text != "" {
					texts = append(texts, text)
				}
			}
		}
		return strings.Join(texts, "")
	}

	trimmed := strings.TrimSpace(body)
	if !gjson.Valid(trimmed) {
		return ""
	}
	content := gjson.Get(trimmed, "content")
	if !content.Exists() {
		return ""
	}
	var raw any
	if err := json.Unmarshal([]byte(content.Raw), &raw); err != nil {
		return ""
	}
	return extractTextFromMixedContent(raw)
}

func extractGeminiResponseContent(body string) string {
	trimmed := strings.TrimSpace(body)
	if !gjson.Valid(trimmed) {
		return ""
	}

	candidates := gjson.Get(trimmed, "candidates")
	if !candidates.Exists() || !candidates.IsArray() {
		return ""
	}
	var texts []string
	candidates.ForEach(func(_, candidate gjson.Result) bool {
		parts := candidate.Get("content.parts")
		if !parts.Exists() || !parts.IsArray() {
			return true
		}
		parts.ForEach(func(_, part gjson.Result) bool {
			if text := part.Get("text").String(); text != "" {
				texts = append(texts, text)
			}
			return true
		})
		return true
	})
	return strings.Join(texts, "")
}

func extractTextFromOpenAIOutputJSON(outputJSON []byte) string {
	if len(outputJSON) == 0 || !gjson.ValidBytes(outputJSON) {
		return ""
	}
	var output []map[string]any
	if err := json.Unmarshal(outputJSON, &output); err != nil {
		return ""
	}
	var texts []string
	for _, item := range output {
		if text := extractTextFromOpenAIFunctionCall(item); text != "" {
			texts = append(texts, text)
		}
		content, ok := item["content"]
		if !ok {
			continue
		}
		if text := extractTextFromMixedContent(content); text != "" {
			texts = append(texts, text)
		}
	}
	return strings.Join(texts, "")
}

func extractTextFromOpenAIFunctionCall(item map[string]any) string {
	if item == nil {
		return ""
	}
	itemType, _ := item["type"].(string)
	if itemType != "function_call" {
		return ""
	}

	name, _ := item["name"].(string)
	arguments, _ := item["arguments"].(string)
	if strings.TrimSpace(name) == "" && strings.TrimSpace(arguments) == "" {
		return ""
	}
	if strings.TrimSpace(arguments) != "" && json.Valid([]byte(arguments)) {
		var out bytes.Buffer
		if err := json.Indent(&out, []byte(arguments), "", "  "); err == nil {
			arguments = out.String()
		}
	}
	if strings.TrimSpace(name) == "" {
		return strings.TrimSpace(arguments)
	}
	if strings.TrimSpace(arguments) == "" {
		return "function_call: " + strings.TrimSpace(name)
	}
	return "function_call: " + strings.TrimSpace(name) + "\narguments: " + strings.TrimSpace(arguments)
}

func extractTextFromChatChoicesJSON(choicesJSON []byte) string {
	if len(choicesJSON) == 0 || !gjson.ValidBytes(choicesJSON) {
		return ""
	}
	var choices []map[string]any
	if err := json.Unmarshal(choicesJSON, &choices); err != nil {
		return ""
	}
	var texts []string
	for _, choice := range choices {
		message, ok := choice["message"].(map[string]any)
		if !ok {
			continue
		}
		if text := extractTextFromMixedContent(message["content"]); text != "" {
			texts = append(texts, text)
		}
	}
	return strings.Join(texts, "")
}

func extractTextFromMixedContent(content any) string {
	switch v := content.(type) {
	case string:
		return v
	case []any:
		var texts []string
		for _, part := range v {
			m, ok := part.(map[string]any)
			if !ok {
				continue
			}
			switch t, _ := m["type"].(string); t {
			case "text", "input_text", "output_text":
				if text, ok := m["text"].(string); ok {
					texts = append(texts, text)
				}
			}
		}
		return strings.Join(texts, "")
	case []map[string]any:
		raw := make([]any, 0, len(v))
		for _, item := range v {
			raw = append(raw, item)
		}
		return extractTextFromMixedContent(raw)
	default:
		return ""
	}
}

func RequestDetailMiddleware(svc *RequestDetailService) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c != nil && c.Request != nil && strings.EqualFold(strings.TrimSpace(c.Request.Header.Get("Upgrade")), "websocket") {
			c.Next()
			return
		}
		requestID, _ := c.Request.Context().Value(ctxkey.RequestID).(string)
		requestID = strings.TrimSpace(requestID)
		if requestID == "" {
			requestID = uuid.NewString()
		}
		capture := NewRequestDetailCapture(c, requestID)
		PutRequestDetailCapture(c, capture)
		originalWriter := c.Writer
		captureWriter := capture.WrapWriter(c.Writer)
		c.Writer = captureWriter

		c.Next()
		if c.Writer == captureWriter {
			c.Writer = originalWriter
		}

		detail := capture.Finish(strings.Join(c.Errors.Errors(), "; "))
		if svc != nil && !svc.Enqueue(detail) {
			logger.LegacyPrintf("service.request_detail", "request detail queue full request_id=%s", detail.RequestID)
		}
	}
}
