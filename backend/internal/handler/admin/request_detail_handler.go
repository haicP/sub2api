package admin

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/xuri/excelize/v2"
)

type RequestDetailHandler struct {
	service *service.RequestDetailService
}

func NewRequestDetailHandler(svc *service.RequestDetailService) *RequestDetailHandler {
	return &RequestDetailHandler{service: svc}
}

func (h *RequestDetailHandler) List(c *gin.Context) {
	page, pageSize := response.ParsePagination(c)
	params := pagination.PaginationParams{
		Page:      page,
		PageSize:  pageSize,
		SortBy:    c.DefaultQuery("sort_by", "created_at"),
		SortOrder: c.DefaultQuery("sort_order", "desc"),
	}

	filters, ok := parseRequestDetailFilters(c)
	if !ok {
		return
	}

	items, result, err := h.service.List(c.Request.Context(), params, filters)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Paginated(c, items, result.Total, result.Page, result.PageSize)
}

func (h *RequestDetailHandler) Get(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "invalid request detail id")
		return
	}
	item, err := h.service.GetByID(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, item)
}

func (h *RequestDetailHandler) Export(c *gin.Context) {
	filters, ok := parseRequestDetailFilters(c)
	if !ok {
		return
	}

	rows := make([]service.RequestDetail, 0, 1024)
	if err := h.service.StreamAll(c.Request.Context(), filters, func(item service.RequestDetail) error {
		if len(rows) >= 10000 {
			return fmt.Errorf("too many records to export, please narrow filters")
		}
		rows = append(rows, item)
		return nil
	}); err != nil {
		if strings.Contains(err.Error(), "too many records") {
			response.BadRequest(c, err.Error())
			return
		}
		response.ErrorFrom(c, err)
		return
	}

	file := excelize.NewFile()
	sheet := file.GetSheetName(0)
	headers := []string{
		"ID", "Request ID", "Created At", "Completed At", "Duration MS", "Status Code", "Success",
		"Platform", "Endpoint", "Upstream Endpoint", "Model", "Upstream Model", "Stream",
		"User ID", "API Key ID", "Account ID", "Group ID", "Subscription ID", "IP Address", "User Agent",
		"Request Headers", "Request Body", "Upstream Request Body", "Response Headers", "Response Body", "Error Message",
	}
	for idx, header := range headers {
		cell, _ := excelize.CoordinatesToCellName(idx+1, 1)
		_ = file.SetCellValue(sheet, cell, header)
	}
	for rowIdx, item := range rows {
		values := []any{
			item.ID,
			item.RequestID,
			item.CreatedAt.Format(time.RFC3339),
			formatTimePtr(item.CompletedAt),
			formatIntPtr(item.DurationMS),
			item.StatusCode,
			item.Success,
			item.Platform,
			item.Endpoint,
			item.UpstreamEndpoint,
			item.Model,
			item.UpstreamModel,
			item.Stream,
			zeroToNil(item.UserID),
			zeroToNil(item.APIKeyID),
			zeroToNil(item.AccountID),
			formatInt64Ptr(item.GroupID),
			formatInt64Ptr(item.SubscriptionID),
			item.IPAddress,
			item.UserAgent,
			mustJSON(item.RequestHeaders),
			item.RequestBody,
			item.UpstreamRequestBody,
			mustJSON(item.ResponseHeaders),
			item.ResponseBody,
			item.ErrorMessage,
		}
		for colIdx, value := range values {
			cell, _ := excelize.CoordinatesToCellName(colIdx+1, rowIdx+2)
			_ = file.SetCellValue(sheet, cell, value)
		}
	}

	buffer, err := file.WriteToBuffer()
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	filename := fmt.Sprintf("request_details_%s.xlsx", time.Now().Format("20060102_150405"))
	c.Header("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	c.Data(http.StatusOK, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", buffer.Bytes())
}

func parseRequestDetailFilters(c *gin.Context) (service.RequestDetailFilters, bool) {
	filters := service.RequestDetailFilters{
		RequestID: strings.TrimSpace(c.Query("request_id")),
		Platform:  strings.TrimSpace(c.Query("platform")),
		Model:     strings.TrimSpace(c.Query("model")),
		Endpoint:  strings.TrimSpace(c.Query("endpoint")),
	}

	parseInt64Ptr := func(name string) (*int64, bool) {
		raw := strings.TrimSpace(c.Query(name))
		if raw == "" {
			return nil, true
		}
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			response.BadRequest(c, "invalid "+name)
			return nil, false
		}
		return &value, true
	}
	parseBoolPtr := func(name string) (*bool, bool) {
		raw := strings.TrimSpace(c.Query(name))
		if raw == "" {
			return nil, true
		}
		value, err := strconv.ParseBool(raw)
		if err != nil {
			response.BadRequest(c, "invalid "+name)
			return nil, false
		}
		return &value, true
	}

	var ok bool
	if filters.UserID, ok = parseInt64Ptr("user_id"); !ok {
		return service.RequestDetailFilters{}, false
	}
	if filters.APIKeyID, ok = parseInt64Ptr("api_key_id"); !ok {
		return service.RequestDetailFilters{}, false
	}
	if filters.AccountID, ok = parseInt64Ptr("account_id"); !ok {
		return service.RequestDetailFilters{}, false
	}
	if filters.GroupID, ok = parseInt64Ptr("group_id"); !ok {
		return service.RequestDetailFilters{}, false
	}
	if filters.Success, ok = parseBoolPtr("success"); !ok {
		return service.RequestDetailFilters{}, false
	}
	if filters.Stream, ok = parseBoolPtr("stream"); !ok {
		return service.RequestDetailFilters{}, false
	}

	if raw := strings.TrimSpace(c.Query("status_code")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil {
			response.BadRequest(c, "invalid status_code")
			return service.RequestDetailFilters{}, false
		}
		filters.StatusCode = &value
	}

	userTZ := c.Query("timezone")
	if raw := strings.TrimSpace(c.Query("start_date")); raw != "" {
		value, err := timezone.ParseInUserLocation("2006-01-02", raw, userTZ)
		if err != nil {
			response.BadRequest(c, "invalid start_date format, use YYYY-MM-DD")
			return service.RequestDetailFilters{}, false
		}
		filters.StartTime = &value
	}
	if raw := strings.TrimSpace(c.Query("end_date")); raw != "" {
		value, err := timezone.ParseInUserLocation("2006-01-02", raw, userTZ)
		if err != nil {
			response.BadRequest(c, "invalid end_date format, use YYYY-MM-DD")
			return service.RequestDetailFilters{}, false
		}
		value = value.AddDate(0, 0, 1)
		filters.EndTime = &value
	}

	return filters, true
}

func formatTimePtr(value *time.Time) string {
	if value == nil {
		return ""
	}
	return value.Format(time.RFC3339)
}

func formatIntPtr(value *int) any {
	if value == nil {
		return ""
	}
	return *value
}

func formatInt64Ptr(value *int64) any {
	if value == nil {
		return ""
	}
	return *value
}

func zeroToNil(value int64) any {
	if value == 0 {
		return ""
	}
	return value
}

func mustJSON(value any) string {
	if value == nil {
		return ""
	}
	bytes, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return string(bytes)
}
