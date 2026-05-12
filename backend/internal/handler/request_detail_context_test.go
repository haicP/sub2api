package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
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
