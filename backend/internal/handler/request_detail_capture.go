package handler

import (
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func setRequestDetailContext(c *gin.Context, ctx service.RequestDetailContext) {
	capture, ok := service.GetRequestDetailCapture(c)
	if !ok {
		return
	}
	capture.SetContext(ctx)
}

func setRequestDetailRequestBody(c *gin.Context, body []byte) {
	capture, ok := service.GetRequestDetailCapture(c)
	if !ok {
		return
	}
	capture.SetRequestBody(body)
}

func setRequestDetailUpstreamRequestBody(c *gin.Context, body []byte) {
	capture, ok := service.GetRequestDetailCapture(c)
	if !ok {
		return
	}
	capture.SetUpstreamRequestBody(body)
}
