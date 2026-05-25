package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestSetRequestDetailContextUpdatesCapture(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	capture := service.NewRequestDetailCapture(c, "req-context")
	service.PutRequestDetailCapture(c, capture)

	setRequestDetailContext(c, service.RequestDetailContext{
		Platform: "anthropic",
		Model:    "claude-sonnet-4-5",
		Stream:   true,
		UserID:   42,
		APIKeyID: 7,
	})

	detail := capture.Finish("")
	require.Equal(t, "anthropic", detail.Platform)
	require.Equal(t, "claude-sonnet-4-5", detail.Model)
	require.True(t, detail.Stream)
	require.Equal(t, int64(42), detail.UserID)
	require.Equal(t, int64(7), detail.APIKeyID)
}

func TestSetRequestDetailUpstreamRequestBodyUpdatesCapture(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	capture := service.NewRequestDetailCapture(c, "req-upstream-body")
	service.PutRequestDetailCapture(c, capture)

	setRequestDetailUpstreamRequestBody(c, []byte(`{"model":"mapped-model","stream":true}`))

	detail := capture.Finish("")
	require.Equal(t, `{"model":"mapped-model","stream":true}`, detail.UpstreamRequestBody)
}

type wsRequestDetailRepoStub struct {
	service.RequestDetailRepository
	created []*service.RequestDetail
}

func (s *wsRequestDetailRepoStub) Create(_ context.Context, detail *service.RequestDetail) error {
	s.created = append(s.created, detail)
	return nil
}

func TestOpenAIWSTurnRecordersCreatePerTurnRequestDetail(t *testing.T) {
	repo := &wsRequestDetailRepoStub{}
	svc := service.NewRequestDetailService(repo)
	svc.Start()
	defer svc.Stop()

	recorders := newOpenAIWSTurnRecorders(svc, openAIWSTurnRequestDetailMeta{
		ConnectionRequestID: "conn-1",
		Endpoint:            "/v1/responses",
		UpstreamEndpoint:    "/v1/responses",
		UserID:              10,
		APIKeyID:            20,
		AccountID:           30,
		IPAddress:           "127.0.0.1",
		UserAgent:           "codex_cli_rs/0.1.0",
		RequestHeaders:      map[string][]string{"User-Agent": {"codex_cli_rs/0.1.0"}},
	})
	recorders.recordRequest(1, coderws.MessageText, []byte(`{"type":"response.create","model":"gpt-5.4"}`), "gpt-5.4")
	recorders.recordUpstreamRequest(1, coderws.MessageText, []byte(`{"type":"response.create","model":"gpt-5.4-upstream"}`), "gpt-5.4-upstream")
	recorders.recordDownstream(1, coderws.MessageText, []byte(`{"type":"response.completed","response":{"id":"resp_1","output_text":"ok"}}`))
	recorders.finish(1, &service.OpenAIForwardResult{
		RequestID:     "resp_1",
		Model:         "gpt-5.4",
		UpstreamModel: "gpt-5.4-upstream",
		OpenAIWSMode:  true,
		Stream:        true,
	}, nil)
	require.Eventually(t, func() bool {
		return len(repo.created) == 1
	}, time.Second, 10*time.Millisecond)

	require.Len(t, repo.created, 1)
	detail := repo.created[0]
	require.Equal(t, "conn-1:turn:1", detail.RequestID)
	require.Equal(t, service.PlatformOpenAI, detail.Platform)
	require.True(t, detail.Stream)
	require.True(t, detail.Success)
	require.Equal(t, "gpt-5.4", detail.Model)
	require.Equal(t, "gpt-5.4-upstream", detail.UpstreamModel)
	require.Contains(t, detail.RequestBody, `"direction":"client_to_gateway"`)
	require.Contains(t, detail.UpstreamRequestBody, `"direction":"gateway_to_upstream"`)
	require.Contains(t, detail.ResponseBody, `"direction":"upstream_to_client"`)
	require.Contains(t, detail.ResponseBody, `"response_id":"resp_1"`)
	require.Equal(t, "ok", detail.ResponseContent)
}
