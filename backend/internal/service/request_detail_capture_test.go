package service

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestRequestDetailCaptureRedactsHeadersAndCapturesResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req.Header.Set("Authorization", "Bearer secret-token")
	req.Header.Set("X-API-Key", "secret-api-key")
	req.Header.Set("Proxy-Authorization", "Bearer proxy-secret")
	req.Header.Set("Cookie", "session=secret")
	req.Header.Set("User-Agent", "capture-test")
	req.Header.Set("X-Trace-ID", "trace-123")
	c.Request = req

	capture := NewRequestDetailCapture(c, "req-1")
	PutRequestDetailCapture(c, capture)
	gotCapture, ok := GetRequestDetailCapture(c)
	require.True(t, ok)
	require.Same(t, capture, gotCapture)

	capture.SetContext(RequestDetailContext{
		Platform:         "openai",
		Endpoint:         "/v1/chat/completions",
		UpstreamEndpoint: "https://api.openai.com/v1/chat/completions",
		Model:            "gpt-5",
		UpstreamModel:    "gpt-5",
		UserID:           42,
		AccountID:        7,
		IPAddress:        "203.0.113.9",
		UserAgent:        "capture-test",
	})
	capture.SetContext(RequestDetailContext{
		Platform: "anthropic",
	})
	capture.SetRequestBody([]byte(`{"model":"gpt-5","messages":[]}`))
	capture.SetUpstreamRequestBody([]byte(`{"model":"gpt-5","stream":false}`))

	c.Writer.Header().Set("Content-Type", "application/json")
	c.Writer.Header().Set("Set-Cookie", "upstream=secret")
	c.Writer = capture.WrapWriter(c.Writer)
	c.Status(http.StatusAccepted)
	_, err := c.Writer.Write([]byte(`{"id":"resp-1"`))
	require.NoError(t, err)
	_, err = c.Writer.WriteString(`,"ok":true}`)
	require.NoError(t, err)

	detail := capture.Finish("")

	require.Equal(t, http.StatusAccepted, detail.StatusCode)
	require.True(t, detail.Success)
	require.Equal(t, "", detail.ResponseContent)
	require.Equal(t, `{"id":"resp-1","ok":true}`, detail.ResponseBody)
	require.Equal(t, `{"model":"gpt-5","messages":[]}`, detail.RequestBody)
	require.Equal(t, `{"model":"gpt-5","stream":false}`, detail.UpstreamRequestBody)

	require.Equal(t, "req-1", detail.RequestID)
	require.Equal(t, "anthropic", detail.Platform)
	require.Equal(t, "/v1/chat/completions", detail.Endpoint)
	require.Equal(t, int64(42), detail.UserID)
	require.Equal(t, int64(7), detail.AccountID)

	require.Equal(t, []string{"***REDACTED***"}, detail.RequestHeaders["Authorization"])
	require.Equal(t, []string{"***REDACTED***"}, detail.RequestHeaders["X-Api-Key"])
	require.Equal(t, []string{"***REDACTED***"}, detail.RequestHeaders["Proxy-Authorization"])
	require.Equal(t, []string{"***REDACTED***"}, detail.RequestHeaders["Cookie"])
	require.Equal(t, []string{"trace-123"}, detail.RequestHeaders["X-Trace-Id"])
	require.Equal(t, "Bearer secret-token", req.Header.Get("Authorization"))
	require.Equal(t, "secret-api-key", req.Header.Get("X-API-Key"))

	require.Equal(t, []string{"application/json"}, detail.ResponseHeaders["Content-Type"])
	require.Equal(t, []string{"***REDACTED***"}, detail.ResponseHeaders["Set-Cookie"])
}

func TestRequestDetailImageArtifactsSanitizeResponseB64WithoutS3(t *testing.T) {
	detail := &RequestDetail{
		RequestID:    "req-img-1",
		Endpoint:     "/v1/images/generations",
		Platform:     PlatformOpenAI,
		ResponseBody: `{"created":1,"data":[{"b64_json":"iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJ","revised_prompt":"cat"}]}`,
	}

	svc := NewRequestDetailService(&requestDetailRepoStub{})
	svc.prepareImageArtifacts(detail)

	require.NotContains(t, detail.ResponseBody, "iVBORw0KGgoAAAANS")
	require.Contains(t, detail.ResponseBody, "artifact_ref")
	require.Len(t, detail.ImageArtifacts, 1)
	require.Equal(t, requestDetailArtifactStatusSkipped, detail.ImageArtifacts[0].Status)
	require.Equal(t, "response", detail.ImageArtifacts[0].Direction)
	require.Equal(t, "$.data.0.b64_json", detail.ImageArtifacts[0].Source)
	require.NotZero(t, detail.ImageArtifacts[0].SizeBytes)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(detail.ResponseBody), &parsed))
	require.NotNil(t, parsed)
}

func TestRequestDetailImageArtifactsDeduplicatesEquivalentResponseFields(t *testing.T) {
	pngB64 := "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAA=="
	detail := &RequestDetail{
		RequestID:    "req-img-dedupe",
		Endpoint:     "/v1/images/generations",
		Platform:     PlatformOpenAI,
		ResponseBody: `{"created":1,"data":[{"b64_json":"` + pngB64 + `","url":"data:image/png;base64,` + pngB64 + `","revised_prompt":"cat"}]}`,
	}

	svc := NewRequestDetailService(&requestDetailRepoStub{})
	svc.prepareImageArtifacts(detail)

	require.Len(t, detail.ImageArtifacts, 1)
	require.Equal(t, "$.data.0.b64_json, $.data.0.url", detail.ImageArtifacts[0].Source)
	require.Equal(t, []string{"$.data.0.b64_json", "$.data.0.url"}, detail.ImageArtifacts[0].Metadata["sources"])

	b64Ref := gjson.Get(detail.ResponseBody, "data.0.b64_json.artifact_ref").String()
	urlRef := gjson.Get(detail.ResponseBody, "data.0.url.artifact_ref").String()
	require.NotEmpty(t, b64Ref)
	require.Equal(t, b64Ref, urlRef)
	require.NotContains(t, gjson.Get(detail.ResponseBody, "data.0.b64_json").Raw, pngB64)
	require.NotContains(t, gjson.Get(detail.ResponseBody, "data.0.url").Raw, pngB64)
}

func TestRequestDetailImageArtifactsSanitizeMultipartInputWithoutS3(t *testing.T) {
	var body strings.Builder
	_, _ = body.WriteString("--boundary\r\n")
	_, _ = body.WriteString("Content-Disposition: form-data; name=\"prompt\"\r\n\r\n")
	_, _ = body.WriteString("edit it\r\n")
	_, _ = body.WriteString("--boundary\r\n")
	_, _ = body.WriteString("Content-Disposition: form-data; name=\"image\"; filename=\"input.png\"\r\n")
	_, _ = body.WriteString("Content-Type: image/png\r\n\r\n")
	_, _ = body.WriteString("\x89PNG\r\n\x1a\nimage-bytes\r\n")
	_, _ = body.WriteString("--boundary--\r\n")

	detail := &RequestDetail{
		RequestID: "req-img-2",
		Endpoint:  "/v1/images/edits",
		Platform:  PlatformOpenAI,
		RequestHeaders: map[string][]string{
			"Content-Type": {"multipart/form-data; boundary=boundary"},
		},
		RequestBody: body.String(),
	}

	svc := NewRequestDetailService(&requestDetailRepoStub{})
	svc.prepareImageArtifacts(detail)

	require.NotContains(t, detail.RequestBody, "image-bytes")
	require.Contains(t, detail.RequestBody, "multipart/form-data")
	require.Len(t, detail.ImageArtifacts, 1)
	require.Equal(t, requestDetailArtifactStatusSkipped, detail.ImageArtifacts[0].Status)
	require.Equal(t, "request", detail.ImageArtifacts[0].Direction)
	require.Equal(t, "multipart.image", detail.ImageArtifacts[0].Source)
}

func TestGetRequestDetailCaptureMissing(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)

	capture, ok := GetRequestDetailCapture(nil)
	require.False(t, ok)
	require.Nil(t, capture)

	capture, ok = GetRequestDetailCapture(c)
	require.False(t, ok)
	require.Nil(t, capture)
}

func TestRequestDetailCaptureCapturesWriteHeaderWithoutBody(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/messages", nil)

	capture := NewRequestDetailCapture(c, "req-status-only")
	c.Writer = capture.WrapWriter(c.Writer)
	c.Writer.Header().Set("Set-Cookie", "server-only-secret")
	c.Writer.WriteHeader(http.StatusNoContent)

	detail := capture.Finish("")

	require.Equal(t, http.StatusNoContent, detail.StatusCode)
	require.True(t, detail.Success)
	require.Equal(t, []string{"***REDACTED***"}, detail.ResponseHeaders["Set-Cookie"])
	require.Empty(t, detail.ResponseBody)
}

func TestExtractResponseContent(t *testing.T) {
	t.Run("openai responses sse", func(t *testing.T) {
		body := "data: {\"type\":\"response.output_text.delta\",\"delta\":\"Hello\"}\n\n" +
			"data: {\"type\":\"response.output_text.delta\",\"delta\":\" world\"}\n\n" +
			"data: {\"type\":\"response.completed\",\"response\":{\"output\":[]}}\n\n" +
			"data: [DONE]\n\n"
		got := extractResponseContent(RequestDetailContext{Platform: PlatformOpenAI}, body)
		require.Equal(t, "Hello world", got)
	})

	t.Run("openai chat completions sse", func(t *testing.T) {
		body := "data: {\"id\":\"chatcmpl_1\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\"},\"finish_reason\":null}]}\n\n" +
			"data: {\"id\":\"chatcmpl_1\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"Hello\"},\"finish_reason\":null}]}\n\n" +
			"data: {\"id\":\"chatcmpl_1\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\" world\"},\"finish_reason\":null}]}\n\n" +
			"data: {\"id\":\"chatcmpl_1\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"\"},\"finish_reason\":\"stop\"}]}\n\n" +
			"data: [DONE]\n\n"
		got := extractResponseContent(RequestDetailContext{Platform: PlatformOpenAI}, body)
		require.Equal(t, "Hello world", got)
	})

	t.Run("anthropic sse", func(t *testing.T) {
		body := "event: content_block_start\n" +
			"data: {\"type\":\"content_block_start\",\"content_block\":{\"type\":\"text\",\"text\":\"Hello\"}}\n\n" +
			"event: content_block_delta\n" +
			"data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\" world\"}}\n\n" +
			"event: message_stop\n" +
			"data: {\"type\":\"message_stop\"}\n\n"
		got := extractResponseContent(RequestDetailContext{Platform: PlatformAnthropic}, body)
		require.Equal(t, "Hello world", got)
	})

	t.Run("anthropic sse on openai messages endpoint", func(t *testing.T) {
		body := "event: content_block_start\n" +
			"data: {\"type\":\"content_block_start\",\"content_block\":{\"type\":\"text\",\"text\":\"Hello\"}}\n\n" +
			"event: content_block_delta\n" +
			"data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\" world\"}}\n\n" +
			"event: message_stop\n" +
			"data: {\"type\":\"message_stop\"}\n\n"
		got := extractResponseContent(RequestDetailContext{Platform: PlatformOpenAI, Endpoint: "/v1/messages"}, body)
		require.Equal(t, "Hello world", got)
	})

	t.Run("gemini json", func(t *testing.T) {
		body := `{"candidates":[{"content":{"parts":[{"text":"Hello"},{"text":" world"}]}}]}`
		got := extractResponseContent(RequestDetailContext{Platform: PlatformGemini}, body)
		require.Equal(t, "Hello world", got)
	})
}
