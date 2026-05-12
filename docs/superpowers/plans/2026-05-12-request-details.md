# 请求详情功能 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 新增管理员可用的“请求详情”功能，永久记录大模型调用完整输入输出，支持查询、详情查看、Excel 导出和独立 S3 备份。

**Architecture:** 使用独立 `request_details` 表承载审计数据，网关热路径通过 `RequestDetailCapture` 捕获入站请求、上游请求和下游响应，并异步写库。管理端新增请求详情服务、API、页面和备份服务；S3 连接复用现有 `BackupService` 配置和 `BackupObjectStoreFactory`。

**Tech Stack:** Go、Gin、PostgreSQL、Ent、Wire、Vue 3、TypeScript、Vite、Tailwind、excelize。

---

## File Structure

后端新增或修改：

- Create: `backend/ent/schema/request_detail.go` - Ent schema，生成 `request_details` 表定义。
- Create: `backend/migrations/136_add_request_details.sql` - 幂等 SQL 迁移，创建表和索引。
- Modify generated Ent files under `backend/ent/` after `go generate ./ent` - 生成 request detail entity、query、mutation 和迁移 schema。
- Create: `backend/internal/service/request_detail.go` - 请求详情领域模型、筛选条件、仓储接口、服务。
- Create: `backend/internal/service/request_detail_capture.go` - 捕获器、响应 writer、上下文 helper、脱敏逻辑。
- Create: `backend/internal/service/request_detail_backup.go` - 请求详情 NDJSON gzip 备份、记录和定时调度。
- Modify: `backend/internal/service/backup_service.go` - 增加可复用的 S3 store/config 获取方法，供请求详情备份复用。
- Modify: `backend/internal/service/gateway_service.go` - 在 `setOpsUpstreamRequestBody` 同步记录上游请求体，并在上游请求构建/转发结果处补充 endpoint、account、model 上下文。
- Modify: `backend/internal/service/openai_gateway_service.go` - 同步 OpenAI 路径的上游请求体、endpoint、account、model 上下文。
- Create: `backend/internal/repository/request_detail_repo.go` - 原生 SQL 仓储，负责 create/list/get/export/backup stream。
- Modify: `backend/internal/repository/wire.go` - 注入新仓储。
- Create: `backend/internal/handler/admin/request_detail_handler.go` - 管理员 API handler。
- Modify: `backend/internal/handler/handler.go` - `AdminHandlers` 增加 `RequestDetail`。
- Modify: `backend/internal/handler/wire.go` - 注入请求详情 handler。
- Modify: `backend/internal/server/routes/admin.go` - 注册 `/api/v1/admin/request-details` 路由。
- Modify: `backend/internal/server/routes/gateway.go` - 在大模型网关路由组加入捕获 middleware。
- Modify: `backend/internal/server/router.go` - 将 `RequestDetailService` 传入 gateway route 注册。
- Modify: `backend/cmd/server/wire.go` and `backend/cmd/server/wire_gen.go` - Wire 注入和清理请求详情后台服务。
- Modify: `backend/go.mod`, `backend/go.sum` - 新增 `github.com/xuri/excelize/v2`。

前端新增或修改：

- Create: `frontend/src/api/admin/requestDetails.ts` - 请求详情管理 API client。
- Modify: `frontend/src/api/admin/index.ts` - 导出 requestDetails API。
- Create: `frontend/src/views/admin/RequestDetailsView.vue` - 管理端页面。
- Modify: `frontend/src/router/index.ts` - 新增 `/admin/request-details` 路由。
- Modify: `frontend/src/components/layout/AppSidebar.vue` - 侧边栏菜单新增“请求详情”。
- Modify: `frontend/src/i18n/locales/zh.ts`, `frontend/src/i18n/locales/en.ts` - 菜单和页面文案。
- Create: `frontend/src/views/admin/__tests__/RequestDetailsView.spec.ts` - 页面基础交互测试。

---

### Task 1: 数据表、Ent schema 和迁移

**Files:**
- Create: `backend/ent/schema/request_detail.go`
- Create: `backend/migrations/136_add_request_details.sql`
- Modify generated: `backend/ent/*`

- [ ] **Step 1: 新增 Ent schema**

Create `backend/ent/schema/request_detail.go`:

```go
package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type RequestDetail struct {
	ent.Schema
}

func (RequestDetail) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "request_details"},
	}
}

func (RequestDetail) Fields() []ent.Field {
	return []ent.Field{
		field.String("request_id").MaxLen(64).NotEmpty().Unique(),
		field.Time("created_at").Default(time.Now).Immutable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("completed_at").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Int("duration_ms").Optional().Nillable(),
		field.Int("status_code").Default(0),
		field.Bool("success").Default(false),
		field.String("platform").MaxLen(32).Optional().Default(""),
		field.String("endpoint").MaxLen(255).Optional().Default(""),
		field.String("upstream_endpoint").MaxLen(1024).Optional().Default(""),
		field.String("model").MaxLen(255).Optional().Default(""),
		field.String("upstream_model").MaxLen(255).Optional().Default(""),
		field.Bool("stream").Default(false),
		field.Int64("user_id").Optional().Nillable(),
		field.Int64("api_key_id").Optional().Nillable(),
		field.Int64("account_id").Optional().Nillable(),
		field.Int64("group_id").Optional().Nillable(),
		field.Int64("subscription_id").Optional().Nillable(),
		field.String("ip_address").MaxLen(45).Optional().Default(""),
		field.String("user_agent").MaxLen(512).Optional().Default(""),
		field.JSON("request_headers", map[string][]string{}).Optional(),
		field.String("request_body").Optional().Default("").SchemaType(map[string]string{dialect.Postgres: "text"}),
		field.String("upstream_request_body").Optional().Default("").SchemaType(map[string]string{dialect.Postgres: "text"}),
		field.JSON("response_headers", map[string][]string{}).Optional(),
		field.String("response_body").Optional().Default("").SchemaType(map[string]string{dialect.Postgres: "text"}),
		field.Bool("response_truncated").Default(false),
		field.String("error_message").Optional().Default("").SchemaType(map[string]string{dialect.Postgres: "text"}),
	}
}

func (RequestDetail) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("created_at"),
		index.Fields("user_id", "created_at"),
		index.Fields("api_key_id", "created_at"),
		index.Fields("account_id", "created_at"),
		index.Fields("model", "created_at"),
		index.Fields("platform", "created_at"),
		index.Fields("status_code", "created_at"),
	}
}
```

- [ ] **Step 2: 新增 SQL 迁移**

Create `backend/migrations/136_add_request_details.sql`:

```sql
CREATE TABLE IF NOT EXISTS request_details (
    id BIGSERIAL PRIMARY KEY,
    request_id VARCHAR(64) NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMPTZ,
    duration_ms INTEGER,
    status_code INTEGER NOT NULL DEFAULT 0,
    success BOOLEAN NOT NULL DEFAULT FALSE,
    platform VARCHAR(32) NOT NULL DEFAULT '',
    endpoint VARCHAR(255) NOT NULL DEFAULT '',
    upstream_endpoint VARCHAR(1024) NOT NULL DEFAULT '',
    model VARCHAR(255) NOT NULL DEFAULT '',
    upstream_model VARCHAR(255) NOT NULL DEFAULT '',
    stream BOOLEAN NOT NULL DEFAULT FALSE,
    user_id BIGINT,
    api_key_id BIGINT,
    account_id BIGINT,
    group_id BIGINT,
    subscription_id BIGINT,
    ip_address VARCHAR(45) NOT NULL DEFAULT '',
    user_agent VARCHAR(512) NOT NULL DEFAULT '',
    request_headers JSONB NOT NULL DEFAULT '{}'::jsonb,
    request_body TEXT NOT NULL DEFAULT '',
    upstream_request_body TEXT NOT NULL DEFAULT '',
    response_headers JSONB NOT NULL DEFAULT '{}'::jsonb,
    response_body TEXT NOT NULL DEFAULT '',
    response_truncated BOOLEAN NOT NULL DEFAULT FALSE,
    error_message TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_request_details_created_at ON request_details (created_at DESC);
CREATE INDEX IF NOT EXISTS idx_request_details_user_created_at ON request_details (user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_request_details_api_key_created_at ON request_details (api_key_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_request_details_account_created_at ON request_details (account_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_request_details_model_created_at ON request_details (model, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_request_details_platform_created_at ON request_details (platform, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_request_details_status_created_at ON request_details (status_code, created_at DESC);
```

- [ ] **Step 3: 生成 Ent 代码**

Run:

```bash
cd backend && rtk go generate ./ent
```

Expected: PASS，生成 `backend/ent/requestdetail*` 和更新 `backend/ent/migrate/schema.go`。

- [ ] **Step 4: 提交数据层骨架**

Run:

```bash
rtk git add backend/ent backend/migrations/136_add_request_details.sql
rtk git commit -m "feat: 添加请求详情数据表"
```

Expected: commit succeeds.

---

### Task 2: 请求详情模型和仓储

**Files:**
- Create: `backend/internal/service/request_detail.go`
- Create: `backend/internal/repository/request_detail_repo.go`
- Modify: `backend/internal/repository/wire.go`
- Test: `backend/internal/repository/request_detail_repo_test.go`

- [ ] **Step 1: 编写服务模型和仓储接口**

Create `backend/internal/service/request_detail.go`:

```go
package service

import (
	"context"
	"encoding/json"
	"io"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
)

var ErrRequestDetailNotFound = infraerrors.NotFound("REQUEST_DETAIL_NOT_FOUND", "request detail not found")

type RequestDetail struct {
	ID                  int64               `json:"id"`
	RequestID           string              `json:"request_id"`
	CreatedAt           time.Time           `json:"created_at"`
	CompletedAt         *time.Time          `json:"completed_at,omitempty"`
	DurationMS          *int                `json:"duration_ms,omitempty"`
	StatusCode          int                 `json:"status_code"`
	Success             bool                `json:"success"`
	Platform            string              `json:"platform"`
	Endpoint            string              `json:"endpoint"`
	UpstreamEndpoint    string              `json:"upstream_endpoint"`
	Model               string              `json:"model"`
	UpstreamModel       string              `json:"upstream_model"`
	Stream              bool                `json:"stream"`
	UserID              int64               `json:"user_id,omitempty"`
	APIKeyID            int64               `json:"api_key_id,omitempty"`
	AccountID           int64               `json:"account_id,omitempty"`
	GroupID             *int64              `json:"group_id,omitempty"`
	SubscriptionID      *int64              `json:"subscription_id,omitempty"`
	IPAddress           string              `json:"ip_address"`
	UserAgent           string              `json:"user_agent"`
	RequestHeaders      map[string][]string `json:"request_headers,omitempty"`
	RequestBody         string              `json:"request_body,omitempty"`
	UpstreamRequestBody string              `json:"upstream_request_body,omitempty"`
	ResponseHeaders     map[string][]string `json:"response_headers,omitempty"`
	ResponseBody        string              `json:"response_body,omitempty"`
	ResponseTruncated   bool                `json:"response_truncated"`
	ErrorMessage        string              `json:"error_message,omitempty"`
	RequestBodyBytes    int                 `json:"request_body_bytes,omitempty"`
	ResponseBodyBytes   int                 `json:"response_body_bytes,omitempty"`
}

type RequestDetailFilters struct {
	StartTime *time.Time
	EndTime   *time.Time

	RequestID string
	UserID    *int64
	APIKeyID  *int64
	AccountID *int64
	GroupID   *int64

	Platform   string
	Model      string
	Endpoint   string
	StatusCode *int
	Success    *bool
	Stream     *bool
}

type RequestDetailRepository interface {
	Create(ctx context.Context, detail *RequestDetail) error
	List(ctx context.Context, params pagination.PaginationParams, filters RequestDetailFilters) ([]RequestDetail, *pagination.PaginationResult, error)
	GetByID(ctx context.Context, id int64) (*RequestDetail, error)
	StreamAll(ctx context.Context, filters RequestDetailFilters, write func(RequestDetail) error) error
}

type RequestDetailService struct {
	repo RequestDetailRepository
}

func NewRequestDetailService(repo RequestDetailRepository) *RequestDetailService {
	return &RequestDetailService{repo: repo}
}

func (s *RequestDetailService) Create(ctx context.Context, detail *RequestDetail) error {
	if s == nil || s.repo == nil || detail == nil {
		return nil
	}
	return s.repo.Create(ctx, detail)
}

func (s *RequestDetailService) List(ctx context.Context, params pagination.PaginationParams, filters RequestDetailFilters) ([]RequestDetail, *pagination.PaginationResult, error) {
	return s.repo.List(ctx, params, filters)
}

func (s *RequestDetailService) GetByID(ctx context.Context, id int64) (*RequestDetail, error) {
	return s.repo.GetByID(ctx, id)
}

func (s *RequestDetailService) WriteBackupNDJSON(ctx context.Context, filters RequestDetailFilters, w io.Writer) error {
	enc := json.NewEncoder(w)
	return s.repo.StreamAll(ctx, filters, func(detail RequestDetail) error {
		return enc.Encode(detail)
	})
}
```

- [ ] **Step 2: 实现仓储**

Create `backend/internal/repository/request_detail_repo.go` with:

```go
package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

type requestDetailRepository struct {
	client *dbent.Client
	db     *sql.DB
}

func NewRequestDetailRepository(client *dbent.Client, db *sql.DB) service.RequestDetailRepository {
	return &requestDetailRepository{client: client, db: db}
}

func (r *requestDetailRepository) Create(ctx context.Context, detail *service.RequestDetail) error {
	if detail == nil {
		return nil
	}
	requestHeaders, err := json.Marshal(nonNilHeaderMap(detail.RequestHeaders))
	if err != nil {
		return err
	}
	responseHeaders, err := json.Marshal(nonNilHeaderMap(detail.ResponseHeaders))
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx, `
		INSERT INTO request_details (
			request_id, created_at, completed_at, duration_ms, status_code, success,
			platform, endpoint, upstream_endpoint, model, upstream_model, stream,
			user_id, api_key_id, account_id, group_id, subscription_id,
			ip_address, user_agent, request_headers, request_body, upstream_request_body,
			response_headers, response_body, response_truncated, error_message
		) VALUES (
			$1, $2, $3, $4, $5, $6,
			$7, $8, $9, $10, $11, $12,
			$13, $14, $15, $16, $17,
			$18, $19, $20, $21, $22,
			$23, $24, $25, $26
		)
		ON CONFLICT (request_id) DO UPDATE SET
			completed_at = EXCLUDED.completed_at,
			duration_ms = EXCLUDED.duration_ms,
			status_code = EXCLUDED.status_code,
			success = EXCLUDED.success,
			response_headers = EXCLUDED.response_headers,
			response_body = EXCLUDED.response_body,
			response_truncated = EXCLUDED.response_truncated,
			error_message = EXCLUDED.error_message
	`, detail.RequestID, detail.CreatedAt, detail.CompletedAt, detail.DurationMS, detail.StatusCode, detail.Success,
		detail.Platform, detail.Endpoint, detail.UpstreamEndpoint, detail.Model, detail.UpstreamModel, detail.Stream,
		nullableInt64(detail.UserID), nullableInt64(detail.APIKeyID), nullableInt64(detail.AccountID), detail.GroupID, detail.SubscriptionID,
		detail.IPAddress, detail.UserAgent, requestHeaders, detail.RequestBody, detail.UpstreamRequestBody,
		responseHeaders, detail.ResponseBody, detail.ResponseTruncated, detail.ErrorMessage)
	return err
}

func (r *requestDetailRepository) List(ctx context.Context, params pagination.PaginationParams, filters service.RequestDetailFilters) ([]service.RequestDetail, *pagination.PaginationResult, error) {
	where, args := buildRequestDetailWhere(filters)
	total, err := countRequestDetails(ctx, r.db, where, args)
	if err != nil {
		return nil, nil, err
	}
	sortBy := normalizeRequestDetailSort(params.SortBy)
	sortOrder := normalizeSortOrder(params.SortOrder)
	offset := (params.Page - 1) * params.PageSize
	listArgs := append(append([]any{}, args...), params.PageSize, offset)
	query := fmt.Sprintf(`
		SELECT id, request_id, created_at, completed_at, duration_ms, status_code, success,
			platform, endpoint, upstream_endpoint, model, upstream_model, stream,
			user_id, api_key_id, account_id, group_id, subscription_id, ip_address, user_agent,
			'' AS request_body, '' AS upstream_request_body, '' AS response_body,
			response_truncated, error_message,
			octet_length(request_body), octet_length(response_body)
		FROM request_details %s
		ORDER BY %s %s
		LIMIT $%d OFFSET $%d
	`, where, sortBy, sortOrder, len(args)+1, len(args)+2)
	rows, err := r.db.QueryContext(ctx, query, listArgs...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	items, err := scanRequestDetailRows(rows, false)
	if err != nil {
		return nil, nil, err
	}
	return items, paginationResultFromTotal(total, params), nil
}
```

Add the remaining functions to `backend/internal/repository/request_detail_repo.go`:

```go
func (r *requestDetailRepository) GetByID(ctx context.Context, id int64) (*service.RequestDetail, error) {
	query := `
		SELECT id, request_id, created_at, completed_at, duration_ms, status_code, success,
			platform, endpoint, upstream_endpoint, model, upstream_model, stream,
			user_id, api_key_id, account_id, group_id, subscription_id, ip_address, user_agent,
			request_headers, request_body, upstream_request_body, response_headers, response_body,
			response_truncated, error_message,
			octet_length(request_body), octet_length(response_body)
		FROM request_details WHERE id = $1
	`
	row := r.db.QueryRowContext(ctx, query, id)
	detail, err := scanRequestDetail(row, true)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, service.ErrRequestDetailNotFound
		}
		return nil, err
	}
	return detail, nil
}

func (r *requestDetailRepository) StreamAll(ctx context.Context, filters service.RequestDetailFilters, write func(service.RequestDetail) error) error {
	where, args := buildRequestDetailWhere(filters)
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, request_id, created_at, completed_at, duration_ms, status_code, success,
			platform, endpoint, upstream_endpoint, model, upstream_model, stream,
			user_id, api_key_id, account_id, group_id, subscription_id, ip_address, user_agent,
			request_headers, request_body, upstream_request_body, response_headers, response_body,
			response_truncated, error_message,
			octet_length(request_body), octet_length(response_body)
		FROM request_details `+where+`
		ORDER BY created_at ASC, id ASC
	`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		detail, err := scanRequestDetail(rows, true)
		if err != nil {
			return err
		}
		if err := write(*detail); err != nil {
			return err
		}
	}
	return rows.Err()
}
```

Add helpers with these exact responsibilities:

```go
type requestDetailScanner interface{ Scan(...any) error }

func buildRequestDetailWhere(filters service.RequestDetailFilters) (string, []any) {
	conditions := make([]string, 0, 12)
	args := make([]any, 0, 12)
	add := func(condition string, value any) {
		args = append(args, value)
		conditions = append(conditions, fmt.Sprintf(condition, len(args)))
	}
	if filters.StartTime != nil { add("created_at >= $%d", *filters.StartTime) }
	if filters.EndTime != nil { add("created_at < $%d", *filters.EndTime) }
	if strings.TrimSpace(filters.RequestID) != "" { add("request_id = $%d", strings.TrimSpace(filters.RequestID)) }
	if filters.UserID != nil { add("user_id = $%d", *filters.UserID) }
	if filters.APIKeyID != nil { add("api_key_id = $%d", *filters.APIKeyID) }
	if filters.AccountID != nil { add("account_id = $%d", *filters.AccountID) }
	if filters.GroupID != nil { add("group_id = $%d", *filters.GroupID) }
	if strings.TrimSpace(filters.Platform) != "" { add("platform = $%d", strings.TrimSpace(filters.Platform)) }
	if strings.TrimSpace(filters.Model) != "" { add("model ILIKE '%' || $%d || '%'", strings.TrimSpace(filters.Model)) }
	if strings.TrimSpace(filters.Endpoint) != "" { add("endpoint ILIKE '%' || $%d || '%'", strings.TrimSpace(filters.Endpoint)) }
	if filters.StatusCode != nil { add("status_code = $%d", *filters.StatusCode) }
	if filters.Success != nil { add("success = $%d", *filters.Success) }
	if filters.Stream != nil { add("stream = $%d", *filters.Stream) }
	if len(conditions) == 0 {
		return "", args
	}
	return "WHERE " + strings.Join(conditions, " AND "), args
}
```

Implement `scanRequestDetail`, `scanRequestDetailRows`, `countRequestDetails`, `nullableInt64`, `nonNilHeaderMap`, `normalizeRequestDetailSort`, and `normalizeSortOrder` in the same file. `normalizeRequestDetailSort` must whitelist only `created_at`, `status_code`, `duration_ms`, `model`, `platform`, and `id`; default to `created_at`.

- [ ] **Step 3: 写失败的仓储集成测试**

Create `backend/internal/repository/request_detail_repo_test.go`:

```go
package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestRequestDetailRepositoryCreateListAndGet(t *testing.T) {
	client, db := setupTestDB(t)
	repo := NewRequestDetailRepository(client, db)

	now := time.Now().UTC().Truncate(time.Millisecond)
	detail := &service.RequestDetail{
		RequestID:           "req-detail-test-1",
		CreatedAt:           now,
		CompletedAt:         ptrTime(now.Add(150 * time.Millisecond)),
		DurationMS:          ptrInt(150),
		StatusCode:          200,
		Success:             true,
		Platform:            "anthropic",
		Endpoint:            "/v1/messages",
		UpstreamEndpoint:    "https://api.anthropic.com/v1/messages",
		Model:               "claude-test",
		UpstreamModel:       "claude-upstream",
		Stream:              false,
		UserID:              11,
		APIKeyID:            22,
		AccountID:           33,
		GroupID:             ptrInt64(44),
		SubscriptionID:      ptrInt64(55),
		IPAddress:           "127.0.0.1",
		UserAgent:           "test-agent",
		RequestHeaders:      map[string][]string{"Authorization": {"***REDACTED***"}},
		RequestBody:         `{"messages":[{"role":"user","content":"hi"}]}`,
		UpstreamRequestBody: `{"model":"claude-upstream"}`,
		ResponseHeaders:     map[string][]string{"Content-Type": {"application/json"}},
		ResponseBody:        `{"content":[{"text":"hello"}]}`,
		ResponseTruncated:   false,
	}

	require.NoError(t, repo.Create(context.Background(), detail))

	items, page, err := repo.List(context.Background(), pagination.PaginationParams{
		Page:      1,
		PageSize:  20,
		SortBy:    "created_at",
		SortOrder: "desc",
	}, service.RequestDetailFilters{
		RequestID: "req-detail-test-1",
		Platform:  "anthropic",
		UserID:    ptrInt64(11),
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), page.Total)
	require.Len(t, items, 1)
	require.Equal(t, "req-detail-test-1", items[0].RequestID)
	require.Empty(t, items[0].RequestBody, "list response must not hydrate full bodies")

	got, err := repo.GetByID(context.Background(), items[0].ID)
	require.NoError(t, err)
	require.Equal(t, detail.RequestBody, got.RequestBody)
	require.Equal(t, detail.ResponseBody, got.ResponseBody)
	require.Equal(t, detail.UpstreamRequestBody, got.UpstreamRequestBody)
}

func ptrInt(v int) *int { return &v }
func ptrInt64(v int64) *int64 { return &v }
func ptrTime(v time.Time) *time.Time { return &v }
```

- [ ] **Step 4: 注册仓储 provider**

Modify `backend/internal/repository/wire.go`:

```go
var ProviderSet = wire.NewSet(
	NewUsageLogRepository,
	NewUsageLogRepository,
	NewRequestDetailRepository,
	NewUsageBillingRepository,
	NewUsageBillingRepository,
)
```

- [ ] **Step 5: 运行仓储测试**

Run:

```bash
rtk go test ./backend/internal/repository -run TestRequestDetailRepositoryCreateListAndGet -count=1
```

Expected: PASS.

- [ ] **Step 6: 提交仓储实现**

Run:

```bash
rtk git add backend/internal/service/request_detail.go backend/internal/repository/request_detail_repo.go backend/internal/repository/request_detail_repo_test.go backend/internal/repository/wire.go
rtk git commit -m "feat: 实现请求详情仓储"
```

Expected: commit succeeds.

---

### Task 3: 捕获器和响应 writer

**Files:**
- Create: `backend/internal/service/request_detail_capture.go`
- Test: `backend/internal/service/request_detail_capture_test.go`

- [ ] **Step 1: 写失败的捕获器测试**

Create `backend/internal/service/request_detail_capture_test.go`:

```go
package service

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestRequestDetailCaptureRedactsHeadersAndCapturesResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages?x=1", nil)
	c.Request.Header.Set("Authorization", "Bearer secret")
	c.Request.Header.Set("X-API-Key", "sk-secret")
	c.Request.Header.Set("User-Agent", "agent")

	capture := NewRequestDetailCapture(c, "req-1")
	capture.SetRequestBody([]byte(`{"input":"hello"}`))
	capture.SetContext(RequestDetailContext{
		Platform: "anthropic",
		Model:    "claude-test",
		Stream:   true,
		UserID:   1,
		APIKeyID: 2,
	})
	c.Writer = capture.WrapWriter(c.Writer)

	c.Status(http.StatusOK)
	_, _ = c.Writer.Write([]byte("event: message_start\n\n"))
	_, _ = c.Writer.Write([]byte("data: {\"text\":\"hi\"}\n\n"))

	detail := capture.Finish("")
	require.Equal(t, "req-1", detail.RequestID)
	require.Equal(t, "***REDACTED***", detail.RequestHeaders["Authorization"][0])
	require.Equal(t, "***REDACTED***", detail.RequestHeaders["X-API-Key"][0])
	require.Equal(t, `{"input":"hello"}`, detail.RequestBody)
	require.Equal(t, "event: message_start\n\ndata: {\"text\":\"hi\"}\n\n", detail.ResponseBody)
	require.True(t, detail.Success)
	require.False(t, detail.ResponseTruncated)
}
```

- [ ] **Step 2: 运行测试确认失败**

Run:

```bash
rtk go test ./backend/internal/service -run TestRequestDetailCaptureRedactsHeadersAndCapturesResponse -count=1
```

Expected: FAIL，原因是捕获器尚不存在。

- [ ] **Step 3: 实现捕获器**

Create `backend/internal/service/request_detail_capture.go`:

```go
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
	method              string
	path                string
	query               string
	requestHeaders      map[string][]string
	requestBody         string
	upstreamRequestBody string
	responseBody        bytes.Buffer
	responseHeaders     http.Header
	statusCode          int
	ctx                 RequestDetailContext
}

func NewRequestDetailCapture(c *gin.Context, requestID string) *RequestDetailCapture {
	cap := &RequestDetailCapture{
		requestID:      requestID,
		startedAt:      time.Now(),
		requestHeaders: redactHeader(c.Request.Header),
		responseHeaders: http.Header{},
	}
	if c != nil && c.Request != nil {
		cap.method = c.Request.Method
		cap.path = c.Request.URL.Path
		cap.query = c.Request.URL.RawQuery
		cap.ctx.Endpoint = c.Request.URL.Path
		cap.ctx.IPAddress = c.ClientIP()
		cap.ctx.UserAgent = c.Request.UserAgent()
	}
	return cap
}

func PutRequestDetailCapture(c *gin.Context, cap *RequestDetailCapture) {
	if c != nil && cap != nil {
		c.Set(ginContextKeyRequestDetailCapture, cap)
	}
}

func GetRequestDetailCapture(c *gin.Context) (*RequestDetailCapture, bool) {
	if c == nil {
		return nil, false
	}
	v, ok := c.Get(ginContextKeyRequestDetailCapture)
	if !ok {
		return nil, false
	}
	cap, ok := v.(*RequestDetailCapture)
	return cap, ok
}
```

Add the remaining capture methods to `backend/internal/service/request_detail_capture.go`:

```go
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
	if ctx.Platform != "" { c.ctx.Platform = ctx.Platform }
	if ctx.Endpoint != "" { c.ctx.Endpoint = ctx.Endpoint }
	if ctx.UpstreamEndpoint != "" { c.ctx.UpstreamEndpoint = ctx.UpstreamEndpoint }
	if ctx.Model != "" { c.ctx.Model = ctx.Model }
	if ctx.UpstreamModel != "" { c.ctx.UpstreamModel = ctx.UpstreamModel }
	c.ctx.Stream = c.ctx.Stream || ctx.Stream
	if ctx.UserID != 0 { c.ctx.UserID = ctx.UserID }
	if ctx.APIKeyID != 0 { c.ctx.APIKeyID = ctx.APIKeyID }
	if ctx.AccountID != 0 { c.ctx.AccountID = ctx.AccountID }
	if ctx.GroupID != nil { c.ctx.GroupID = ctx.GroupID }
	if ctx.SubscriptionID != nil { c.ctx.SubscriptionID = ctx.SubscriptionID }
	if ctx.IPAddress != "" { c.ctx.IPAddress = ctx.IPAddress }
	if ctx.UserAgent != "" { c.ctx.UserAgent = ctx.UserAgent }
}

func (c *RequestDetailCapture) WrapWriter(w gin.ResponseWriter) gin.ResponseWriter {
	return &captureResponseWriter{ResponseWriter: w, capture: c}
}

func (c *RequestDetailCapture) Finish(errorMessage string) *RequestDetail {
	c.mu.Lock()
	defer c.mu.Unlock()
	completedAt := time.Now()
	duration := int(completedAt.Sub(c.startedAt).Milliseconds())
	status := c.statusCode
	if status == 0 {
		status = http.StatusOK
	}
	success := status >= 200 && status < 400 && errorMessage == ""
	if c.responseHeaders == nil {
		c.responseHeaders = http.Header{}
	}
	detail := &RequestDetail{
		RequestID:           c.requestID,
		CreatedAt:           c.startedAt,
		CompletedAt:         &completedAt,
		DurationMS:          &duration,
		StatusCode:          status,
		Success:             success,
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
		RequestHeaders:      c.requestHeaders,
		RequestBody:         c.requestBody,
		UpstreamRequestBody: c.upstreamRequestBody,
		ResponseHeaders:     map[string][]string(c.responseHeaders),
		ResponseBody:        c.responseBody.String(),
		ResponseTruncated:   false,
		ErrorMessage:        errorMessage,
	}
	return detail
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
```

`redactHeader` must clone input headers and replace values for `authorization`, `proxy-authorization`, `x-api-key`, `cookie`, and `set-cookie` with `***REDACTED***`.

- [ ] **Step 4: 运行捕获器测试**

Run:

```bash
rtk go test ./backend/internal/service -run TestRequestDetailCaptureRedactsHeadersAndCapturesResponse -count=1
```

Expected: PASS.

- [ ] **Step 5: 提交捕获器**

Run:

```bash
rtk git add backend/internal/service/request_detail_capture.go backend/internal/service/request_detail_capture_test.go
rtk git commit -m "feat: 添加请求详情捕获器"
```

Expected: commit succeeds.

---

### Task 4: 异步写入服务和网关 middleware

**Files:**
- Modify: `backend/internal/service/request_detail.go`
- Modify: `backend/internal/server/routes/gateway.go`
- Modify: `backend/internal/server/router.go`
- Modify: `backend/internal/server/http.go`
- Modify: `backend/internal/service/wire.go`
- Modify: `backend/cmd/server/wire.go`, `backend/cmd/server/wire_gen.go`
- Test: `backend/internal/service/request_detail_service_test.go`, `backend/internal/server/routes/gateway_test.go`

- [ ] **Step 1: 写失败的异步写入测试**

Create `backend/internal/service/request_detail_service_test.go`:

```go
package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/stretchr/testify/require"
)

type requestDetailRepoStub struct {
	mu      sync.Mutex
	created []RequestDetail
}

func (s *requestDetailRepoStub) Create(_ context.Context, detail *RequestDetail) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.created = append(s.created, *detail)
	return nil
}
func (s *requestDetailRepoStub) List(context.Context, pagination.PaginationParams, RequestDetailFilters) ([]RequestDetail, *pagination.PaginationResult, error) {
	return nil, nil, nil
}
func (s *requestDetailRepoStub) GetByID(context.Context, int64) (*RequestDetail, error) { return nil, nil }
func (s *requestDetailRepoStub) StreamAll(context.Context, RequestDetailFilters, func(RequestDetail) error) error { return nil }

func TestRequestDetailServiceCreateAsyncFlushesOnStop(t *testing.T) {
	repo := &requestDetailRepoStub{}
	svc := NewRequestDetailService(repo)
	svc.Start()
	require.True(t, svc.Enqueue(&RequestDetail{RequestID: "req-async", CreatedAt: time.Now()}))
	svc.Stop()

	require.Len(t, repo.created, 1)
	require.Equal(t, "req-async", repo.created[0].RequestID)
}
```

- [ ] **Step 2: 运行测试确认失败**

Run:

```bash
rtk go test ./backend/internal/service -run TestRequestDetailServiceCreateAsyncFlushesOnStop -count=1
```

Expected: FAIL，原因是 `Start`、`Stop`、`Enqueue` 尚不存在。

- [ ] **Step 3: 实现队列式异步写入**

Modify `backend/internal/service/request_detail.go`:

```go
type RequestDetailService struct {
	repo RequestDetailRepository
	queue chan *RequestDetail
	stop  chan struct{}
	done  chan struct{}
	started atomic.Bool
}

func NewRequestDetailService(repo RequestDetailRepository) *RequestDetailService {
	return &RequestDetailService{
		repo:  repo,
		queue: make(chan *RequestDetail, 1024),
		stop:  make(chan struct{}),
		done:  make(chan struct{}),
	}
}

func (s *RequestDetailService) Start() {
	if s == nil || !s.started.CompareAndSwap(false, true) {
		return
	}
	go s.run()
}

func (s *RequestDetailService) Stop() {
	if s == nil || !s.started.Load() {
		return
	}
	close(s.stop)
	<-s.done
}

func (s *RequestDetailService) Enqueue(detail *RequestDetail) bool {
	if s == nil || detail == nil {
		return false
	}
	select {
	case s.queue <- detail:
		return true
	default:
		return false
	}
}
```

Add `run` loop to drain queue on stop and call `repo.Create(context.Background(), detail)`, logging failures with existing logger.

- [ ] **Step 4: 新增 middleware**

Add to `backend/internal/service/request_detail_capture.go`:

```go
func RequestDetailMiddleware(svc *RequestDetailService) gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := c.GetString("request_id")
		if requestID == "" {
			requestID = c.Writer.Header().Get("X-Request-ID")
		}
		cap := NewRequestDetailCapture(c, requestID)
		PutRequestDetailCapture(c, cap)
		c.Writer = cap.WrapWriter(c.Writer)

		c.Next()

		detail := cap.Finish(strings.Join(c.Errors.Errors(), "; "))
		if svc != nil && !svc.Enqueue(detail) {
			logger.LegacyPrintf("service.request_detail", "request detail queue full request_id=%s", detail.RequestID)
		}
	}
}
```

- [ ] **Step 5: 接入 gateway routes**

Modify `backend/internal/server/routes/gateway.go` signature:

```go
func RegisterGatewayRoutes(
	r *gin.Engine,
	h *handler.Handlers,
	apiKeyAuth middleware.APIKeyAuthMiddleware,
	apiKeyService *service.APIKeyService,
	subscriptionService *service.SubscriptionService,
	opsService *service.OpsService,
	settingService *service.SettingService,
	requestDetailService *service.RequestDetailService,
	cfg *config.Config,
)
```

Add:

```go
requestDetailCapture := service.RequestDetailMiddleware(requestDetailService)
```

Use `requestDetailCapture` after `clientRequestID` and before handlers for all LLM POST routes and route groups: `/v1`, `/v1beta`, root `/responses`, root `/chat/completions`, `/backend-api/codex`, images routes, `/antigravity/v1`, `/antigravity/v1beta`. Do not add it to model-list GET endpoints unless they call upstream LLM; the request detail feature is for model calls.

- [ ] **Step 6: 更新 router 和 Wire**

Modify `backend/internal/server/router.go` and `backend/internal/server/http.go` to accept and pass `requestDetailService *service.RequestDetailService`.

Modify `backend/internal/service/wire.go`:

```go
func ProvideRequestDetailService(repo RequestDetailRepository) *RequestDetailService {
	svc := NewRequestDetailService(repo)
	svc.Start()
	return svc
}
```

Add `ProvideRequestDetailService` to `service.ProviderSet`.

Modify `backend/cmd/server/wire.go` cleanup signature to include `requestDetail *service.RequestDetailService`, and stop it in `parallelSteps`.

Run Wire:

```bash
cd backend && rtk go run github.com/google/wire/cmd/wire ./cmd/server
```

Expected: PASS and `backend/cmd/server/wire_gen.go` updated.

- [ ] **Step 7: 运行服务测试**

Run:

```bash
rtk go test ./backend/internal/service -run 'TestRequestDetail(Service|Capture)' -count=1
rtk go test ./backend/internal/server/routes -run TestRegisterGatewayRoutes -count=1
```

Expected: PASS. If route test constructor fails, update it to pass a nil `RequestDetailService`.

- [ ] **Step 8: 提交 middleware 接入**

Run:

```bash
rtk git add backend/internal/service/request_detail.go backend/internal/service/request_detail_capture.go backend/internal/service/request_detail_service_test.go backend/internal/server/routes/gateway.go backend/internal/server/router.go backend/internal/server/http.go backend/internal/service/wire.go backend/cmd/server/wire.go backend/cmd/server/wire_gen.go
rtk git commit -m "feat: 接入请求详情捕获写入"
```

Expected: commit succeeds.

---

### Task 5: 网关上下文和上游请求记录

**Files:**
- Modify: `backend/internal/handler/gateway_handler.go`
- Modify: `backend/internal/handler/gateway_handler_responses.go`
- Modify: `backend/internal/handler/gateway_handler_chat_completions.go`
- Modify: `backend/internal/handler/openai_gateway_handler.go`
- Modify: `backend/internal/handler/openai_chat_completions.go`
- Modify: `backend/internal/handler/openai_images.go`
- Modify: `backend/internal/handler/gemini_v1beta_handler.go`
- Modify: `backend/internal/service/gateway_service.go`
- Modify: `backend/internal/service/openai_gateway_service.go`
- Test: `backend/internal/handler/request_detail_context_test.go`

- [ ] **Step 1: 写失败的 context helper 测试**

Create `backend/internal/handler/request_detail_context_test.go`:

```go
package handler

import (
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
	cap := service.NewRequestDetailCapture(c, "req-context")
	service.PutRequestDetailCapture(c, cap)

	setRequestDetailContext(c, service.RequestDetailContext{
		Platform: "openai",
		Model:    "gpt-test",
		Stream:   true,
		UserID:   10,
		APIKeyID: 20,
	})
	setRequestDetailUpstreamRequestBody(c, []byte(`{"model":"gpt-upstream"}`))

	detail := cap.Finish("")
	require.Equal(t, "openai", detail.Platform)
	require.Equal(t, "gpt-test", detail.Model)
	require.True(t, detail.Stream)
	require.Equal(t, int64(10), detail.UserID)
	require.Equal(t, `{"model":"gpt-upstream"}`, detail.UpstreamRequestBody)
}
```

- [ ] **Step 2: 实现 handler helper**

Add to a small new file `backend/internal/handler/request_detail_capture.go`:

```go
package handler

import (
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func setRequestDetailContext(c *gin.Context, ctx service.RequestDetailContext) {
	if cap, ok := service.GetRequestDetailCapture(c); ok {
		cap.SetContext(ctx)
	}
}

func setRequestDetailRequestBody(c *gin.Context, body []byte) {
	if cap, ok := service.GetRequestDetailCapture(c); ok {
		cap.SetRequestBody(body)
	}
}

func setRequestDetailUpstreamRequestBody(c *gin.Context, body []byte) {
	if cap, ok := service.GetRequestDetailCapture(c); ok {
		cap.SetUpstreamRequestBody(body)
	}
}
```

- [ ] **Step 3: 在请求 body 读取后记录入站请求体**

In gateway handlers after `ReadRequestBodyWithPrealloc` succeeds, add:

```go
setRequestDetailRequestBody(c, body)
```

Apply to messages, responses, chat completions, OpenAI responses/chat/images, Gemini native body paths.

- [ ] **Step 4: 在 handler 已解析用户上下文后记录元数据**

After `apiKey` and `subject` are available and `reqModel` / `reqStream` are parsed:

```go
setRequestDetailContext(c, service.RequestDetailContext{
	Platform:       apiKey.Group.Platform,
	Endpoint:       c.FullPath(),
	Model:          reqModel,
	Stream:         reqStream,
	UserID:         subject.UserID,
	APIKeyID:       apiKey.ID,
	GroupID:        apiKey.GroupID,
	SubscriptionID: subject.SubscriptionID,
	IPAddress:      c.ClientIP(),
	UserAgent:      c.Request.UserAgent(),
})
```

Use nil-safe platform handling when `apiKey.Group` is nil.

- [ ] **Step 5: 在上游 body 设置点同步记录**

Wherever `setOpsUpstreamRequestBody(c, body)` is called in `backend/internal/service/gateway_service.go` and `backend/internal/service/openai_gateway_service.go`, add:

```go
if cap, ok := GetRequestDetailCapture(c); ok {
	cap.SetUpstreamRequestBody(body)
}
```

Use service package helper directly if call site is in service package; avoid import cycles.

- [ ] **Step 6: 记录 account、upstream endpoint、upstream model**

In Gateway/OpenAI service forward paths, when account and mapped model are known:

```go
if cap, ok := GetRequestDetailCapture(c); ok {
	cap.SetContext(RequestDetailContext{
		AccountID:         account.ID,
		UpstreamEndpoint:  resolvedURL,
		UpstreamModel:     reqModel,
	})
}
```

Do this in the same functions that build upstream requests. Preserve existing context fields by implementing `SetContext` as merge-with-non-zero/non-empty.

- [ ] **Step 7: 运行 targeted 测试**

Run:

```bash
rtk go test ./backend/internal/handler -run TestSetRequestDetailContextUpdatesCapture -count=1
rtk go test ./backend/internal/service -run 'TestRequestDetailCapture|TestGateway' -count=1
```

Expected: PASS.

- [ ] **Step 8: 提交网关上下文**

Run:

```bash
rtk git add backend/internal/handler backend/internal/service/gateway_service.go backend/internal/service/openai_gateway_service.go
rtk git commit -m "feat: 记录请求详情网关上下文"
```

Expected: commit succeeds.

---

### Task 6: 管理员查询、详情和 Excel 导出 API

**Files:**
- Create: `backend/internal/handler/admin/request_detail_handler.go`
- Modify: `backend/internal/handler/handler.go`
- Modify: `backend/internal/handler/wire.go`
- Modify: `backend/internal/server/routes/admin.go`
- Modify: `backend/go.mod`, `backend/go.sum`
- Test: `backend/internal/handler/admin/request_detail_handler_test.go`

- [ ] **Step 1: 添加 excelize 依赖**

Run:

```bash
cd backend && rtk go get github.com/xuri/excelize/v2@latest
```

Expected: `backend/go.mod` and `backend/go.sum` updated.

- [ ] **Step 2: 写失败的 handler 测试**

Create `backend/internal/handler/admin/request_detail_handler_test.go`:

```go
package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type requestDetailServiceStub struct {
	listFilters service.RequestDetailFilters
}

func (s *requestDetailServiceStub) List(ctx context.Context, params pagination.PaginationParams, filters service.RequestDetailFilters) ([]service.RequestDetail, *pagination.PaginationResult, error) {
	s.listFilters = filters
	return []service.RequestDetail{{ID: 1, RequestID: "req-1", CreatedAt: time.Now(), Platform: "openai", Model: "gpt-test"}}, &pagination.PaginationResult{Total: 1, Page: 1, PageSize: 20, Pages: 1}, nil
}
func (s *requestDetailServiceStub) GetByID(context.Context, int64) (*service.RequestDetail, error) {
	return &service.RequestDetail{ID: 1, RequestID: "req-1", RequestBody: `{"input":"hello"}`, ResponseBody: `{"output":"hi"}`}, nil
}
func (s *requestDetailServiceStub) StreamAll(context.Context, service.RequestDetailFilters, func(service.RequestDetail) error) error {
	return nil
}

func TestRequestDetailHandlerListParsesFilters(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stub := &requestDetailServiceStub{}
	h := NewRequestDetailHandler(stub, nil)
	r := gin.New()
	r.GET("/admin/request-details", h.List)

	req := httptest.NewRequest(http.MethodGet, "/admin/request-details?platform=openai&model=gpt-test&success=true&stream=false", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "openai", stub.listFilters.Platform)
	require.Equal(t, "gpt-test", stub.listFilters.Model)
	require.NotNil(t, stub.listFilters.Success)
	require.True(t, *stub.listFilters.Success)
	require.NotNil(t, stub.listFilters.Stream)
	require.False(t, *stub.listFilters.Stream)
}
```

- [ ] **Step 3: 实现 handler**

Create `backend/internal/handler/admin/request_detail_handler.go` with:

```go
package admin

import (
	"context"
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
	service requestDetailService
	backup  requestDetailBackupService
}

type requestDetailService interface {
	List(ctx context.Context, params pagination.PaginationParams, filters service.RequestDetailFilters) ([]service.RequestDetail, *pagination.PaginationResult, error)
	GetByID(ctx context.Context, id int64) (*service.RequestDetail, error)
	StreamAll(ctx context.Context, filters service.RequestDetailFilters, write func(service.RequestDetail) error) error
}

func NewRequestDetailHandler(svc requestDetailService, backup requestDetailBackupService) *RequestDetailHandler {
	return &RequestDetailHandler{service: svc, backup: backup}
}
```

Create `List`, `Get`, and `Export` methods in `backend/internal/handler/admin/request_detail_handler.go` with these signatures and behavior:

```go
func (h *RequestDetailHandler) List(c *gin.Context)
func (h *RequestDetailHandler) Get(c *gin.Context)
func (h *RequestDetailHandler) Export(c *gin.Context)
```

- `List`: parse `page/page_size`, `sort_by/sort_order`, `start_date/end_date/timezone`, numeric IDs, `status_code`, `success`, `stream`, and string filters; call `service.List`; return `response.Paginated(c, items, page.Total, page.Page, page.PageSize)`.
- `Get`: parse numeric `:id`; call `service.GetByID`; return `response.Success`.
- `Export`: parse the same filters as `List`, fetch up to 10,000 full records with `StreamAll`, and write an `.xlsx` response.

The export response headers must be:

```go
c.Header("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
```

The Excel sheet must contain these columns in order: `ID`, `Request ID`, `Created At`, `Completed At`, `Duration MS`, `Status Code`, `Success`, `Platform`, `Endpoint`, `Upstream Endpoint`, `Model`, `Upstream Model`, `Stream`, `User ID`, `API Key ID`, `Account ID`, `Group ID`, `Subscription ID`, `IP Address`, `User Agent`, `Request Headers`, `Request Body`, `Upstream Request Body`, `Response Headers`, `Response Body`, `Error Message`.

- [ ] **Step 4: 注册 admin handler 和路由**

Modify `backend/internal/handler/handler.go`:

```go
RequestDetail *admin.RequestDetailHandler
```

Modify `backend/internal/handler/wire.go`:

```go
requestDetailHandler *admin.RequestDetailHandler,
RequestDetail: requestDetailHandler,
admin.NewRequestDetailHandler,
```

Place the parameter beside the other admin handler parameters in `ProvideAdminHandlers`, assign it in the returned `AdminHandlers` struct, and add `admin.NewRequestDetailHandler` beside the other admin handler constructors in `ProviderSet`.

Modify `backend/internal/server/routes/admin.go`:

```go
// 请求详情
registerRequestDetailRoutes(admin, h)
```

Add:

```go
func registerRequestDetailRoutes(admin *gin.RouterGroup, h *handler.Handlers) {
	requestDetails := admin.Group("/request-details")
	{
		requestDetails.GET("", h.Admin.RequestDetail.List)
		requestDetails.GET("/export", h.Admin.RequestDetail.Export)
		requestDetails.GET("/:id", h.Admin.RequestDetail.Get)
	}
}
```

- [ ] **Step 5: 重新生成 Wire**

Run:

```bash
cd backend && rtk go run github.com/google/wire/cmd/wire ./cmd/server
```

Expected: PASS.

- [ ] **Step 6: 运行 handler 测试**

Run:

```bash
rtk go test ./backend/internal/handler/admin -run TestRequestDetailHandler -count=1
```

Expected: PASS.

- [ ] **Step 7: 提交管理 API**

Run:

```bash
rtk git add backend/go.mod backend/go.sum backend/internal/handler/admin/request_detail_handler.go backend/internal/handler/admin/request_detail_handler_test.go backend/internal/handler/handler.go backend/internal/handler/wire.go backend/internal/server/routes/admin.go backend/cmd/server/wire_gen.go
rtk git commit -m "feat: 添加请求详情管理接口"
```

Expected: commit succeeds.

---

### Task 7: 请求详情 S3 备份服务和 API

**Files:**
- Create: `backend/internal/service/request_detail_backup.go`
- Modify: `backend/internal/service/backup_service.go`
- Modify: `backend/internal/handler/admin/request_detail_handler.go`
- Modify: `backend/internal/server/routes/admin.go`
- Modify: `backend/internal/service/wire.go`
- Modify: `backend/cmd/server/wire.go`, `backend/cmd/server/wire_gen.go`
- Test: `backend/internal/service/request_detail_backup_test.go`

- [ ] **Step 1: 写失败的备份服务测试**

Create `backend/internal/service/request_detail_backup_test.go`:

```go
package service

import (
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type requestDetailBackupRepoStub struct {
	RequestDetailRepository
	items []RequestDetail
}

func (s *requestDetailBackupRepoStub) StreamAll(_ context.Context, _ RequestDetailFilters, write func(RequestDetail) error) error {
	for _, item := range s.items {
		if err := write(item); err != nil {
			return err
		}
	}
	return nil
}

func TestRequestDetailBackupWritesGzipNDJSON(t *testing.T) {
	repo := &requestDetailBackupRepoStub{items: []RequestDetail{{RequestID: "req-backup", CreatedAt: time.Now(), RequestBody: `{"a":1}`, ResponseBody: `{"b":2}`}}}
	svc := NewRequestDetailService(repo)
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	require.NoError(t, svc.WriteBackupNDJSON(context.Background(), RequestDetailFilters{}, gz))
	require.NoError(t, gz.Close())

	reader, err := gzip.NewReader(bytes.NewReader(buf.Bytes()))
	require.NoError(t, err)
	out, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.Contains(t, string(out), `"request_id":"req-backup"`)
	require.True(t, strings.HasSuffix(string(out), "\n"))
}
```

- [ ] **Step 2: 运行测试确认失败或编译失败**

Run:

```bash
rtk go test ./backend/internal/service -run TestRequestDetailBackupWritesGzipNDJSON -count=1
```

Expected: FAIL until `WriteBackupNDJSON` import and implementation are complete.

- [ ] **Step 3: 在 BackupService 暴露复用方法**

Modify `backend/internal/service/backup_service.go`:

```go
func (s *BackupService) NewConfiguredObjectStore(ctx context.Context) (BackupObjectStore, *BackupS3Config, error) {
	cfg, err := s.loadS3Config(ctx)
	if err != nil {
		return nil, nil, err
	}
	if cfg == nil || !cfg.IsConfigured() {
		return nil, nil, ErrBackupS3NotConfigured
	}
	store, err := s.storeFactory(ctx, cfg)
	if err != nil {
		return nil, nil, err
	}
	return store, cfg, nil
}
```

This keeps S3 decrypt/config ownership in existing backup service.

- [ ] **Step 4: 实现 RequestDetailBackupService**

Create `backend/internal/service/request_detail_backup.go`:

```go
package service

import (
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/robfig/cron/v3"
)

const (
	settingKeyRequestDetailBackupSchedule = "request_detail_backup_schedule"
	settingKeyRequestDetailBackupRecords  = "request_detail_backup_records"
)

type RequestDetailBackupService struct {
	requestDetailService *RequestDetailService
	backupService       *BackupService
	settingRepo         SettingRepository
	mu                  sync.Mutex
	cronSched           *cron.Cron
	cronEntryID         cron.EntryID
}
```

Create these exported methods on `RequestDetailBackupService`:

```go
func NewRequestDetailBackupService(requestDetailService *RequestDetailService, backupService *BackupService, settingRepo SettingRepository) *RequestDetailBackupService
func (s *RequestDetailBackupService) Start()
func (s *RequestDetailBackupService) Stop()
func (s *RequestDetailBackupService) GetSchedule(ctx context.Context) (*BackupScheduleConfig, error)
func (s *RequestDetailBackupService) UpdateSchedule(ctx context.Context, cfg BackupScheduleConfig) (*BackupScheduleConfig, error)
func (s *RequestDetailBackupService) StartBackup(ctx context.Context, triggeredBy string) (*BackupRecord, error)
func (s *RequestDetailBackupService) ListBackups(ctx context.Context) ([]BackupRecord, error)
func (s *RequestDetailBackupService) GetBackupRecord(ctx context.Context, id string) (*BackupRecord, error)
func (s *RequestDetailBackupService) GetBackupDownloadURL(ctx context.Context, id string) (string, error)
```

Use existing `BackupScheduleConfig` and `BackupRecord`; set `BackupRecord.BackupType` to `request_details`. Store schedule JSON under `request_detail_backup_schedule` and record JSON array under `request_detail_backup_records`.

For backup body, use `io.Pipe`, `gzip.NewWriter`, and `requestDetailService.WriteBackupNDJSON` in a goroutine, upload to key:

```go
key := strings.TrimRight(cfg.Prefix, "/") + "/request-details/" + fileName
```

- [ ] **Step 5: 注册备份 API**

Modify `backend/internal/handler/admin/request_detail_handler.go` adding:

- `CreateBackup`
- `ListBackups`
- `GetBackup`
- `GetBackupDownloadURL`
- `GetBackupSchedule`
- `UpdateBackupSchedule`

Modify routes:

```go
requestDetails.POST("/backups", h.Admin.RequestDetail.CreateBackup)
requestDetails.GET("/backups", h.Admin.RequestDetail.ListBackups)
requestDetails.GET("/backups/:id", h.Admin.RequestDetail.GetBackup)
requestDetails.GET("/backups/:id/download-url", h.Admin.RequestDetail.GetBackupDownloadURL)
requestDetails.GET("/backup-schedule", h.Admin.RequestDetail.GetBackupSchedule)
requestDetails.PUT("/backup-schedule", h.Admin.RequestDetail.UpdateBackupSchedule)
```

Put `/backup-schedule` and `/backups` before `/:id`.

- [ ] **Step 6: Wire 启停**

Add provider:

```go
func ProvideRequestDetailBackupService(requestDetailService *RequestDetailService, backupService *BackupService, settingRepo SettingRepository) *RequestDetailBackupService {
	svc := NewRequestDetailBackupService(requestDetailService, backupService, settingRepo)
	svc.Start()
	return svc
}
```

Add to `service.ProviderSet`, update `provideCleanup` to stop it, regenerate Wire:

```bash
cd backend && rtk go run github.com/google/wire/cmd/wire ./cmd/server
```

- [ ] **Step 7: 运行备份测试**

Run:

```bash
rtk go test ./backend/internal/service -run 'TestRequestDetailBackup|TestBackupService' -count=1
```

Expected: PASS.

- [ ] **Step 8: 提交备份功能**

Run:

```bash
rtk git add backend/internal/service/request_detail_backup.go backend/internal/service/request_detail_backup_test.go backend/internal/service/backup_service.go backend/internal/service/wire.go backend/internal/handler/admin/request_detail_handler.go backend/internal/server/routes/admin.go backend/cmd/server/wire.go backend/cmd/server/wire_gen.go
rtk git commit -m "feat: 添加请求详情独立备份"
```

Expected: commit succeeds.

---

### Task 8: 前端 API、路由和菜单

**Files:**
- Create: `frontend/src/api/admin/requestDetails.ts`
- Modify: `frontend/src/api/admin/index.ts`
- Modify: `frontend/src/router/index.ts`
- Modify: `frontend/src/components/layout/AppSidebar.vue`
- Modify: `frontend/src/i18n/locales/zh.ts`
- Modify: `frontend/src/i18n/locales/en.ts`
- Test: `frontend/src/api/__tests__/requestDetails.spec.ts`

- [ ] **Step 1: 写失败的 API client 测试**

Create `frontend/src/api/__tests__/requestDetails.spec.ts`:

```ts
import { describe, expect, it, vi } from 'vitest'
import { requestDetailsAPI } from '@/api/admin/requestDetails'
import { apiClient } from '@/api/client'

vi.mock('@/api/client', () => ({
  apiClient: {
    get: vi.fn(),
    post: vi.fn(),
    put: vi.fn()
  }
}))

describe('requestDetailsAPI', () => {
  it('passes list filters to admin request details endpoint', async () => {
    vi.mocked(apiClient.get).mockResolvedValueOnce({ data: { items: [], total: 0, page: 1, page_size: 20, pages: 1 } })
    await requestDetailsAPI.list({ page: 1, page_size: 20, platform: 'openai', stream: true })
    expect(apiClient.get).toHaveBeenCalledWith('/admin/request-details', {
      params: { page: 1, page_size: 20, platform: 'openai', stream: true },
      signal: undefined
    })
  })
})
```

- [ ] **Step 2: 实现 API client**

Create `frontend/src/api/admin/requestDetails.ts`:

```ts
import { apiClient } from '../client'
import type { PaginatedResponse } from '@/types'

export interface RequestDetailListParams {
  page?: number
  page_size?: number
  start_date?: string
  end_date?: string
  timezone?: string
  request_id?: string
  user_id?: number
  api_key_id?: number
  account_id?: number
  group_id?: number
  platform?: string
  model?: string
  endpoint?: string
  status_code?: number
  success?: boolean
  stream?: boolean
  sort_by?: string
  sort_order?: 'asc' | 'desc'
}

export interface RequestDetailSummary {
  id: number
  request_id: string
  created_at: string
  completed_at?: string
  duration_ms?: number
  status_code: number
  success: boolean
  platform: string
  endpoint: string
  upstream_endpoint: string
  model: string
  upstream_model: string
  stream: boolean
  user_id?: number
  api_key_id?: number
  account_id?: number
  group_id?: number
  subscription_id?: number
  ip_address: string
  user_agent: string
  request_body_bytes?: number
  response_body_bytes?: number
  error_message?: string
}

export interface RequestDetail extends RequestDetailSummary {
  request_headers?: Record<string, string[]>
  request_body?: string
  upstream_request_body?: string
  response_headers?: Record<string, string[]>
  response_body?: string
  response_truncated: boolean
}

export interface RequestDetailBackupRecord {
  id: string
  status: 'pending' | 'running' | 'completed' | 'failed'
  backup_type: string
  file_name: string
  s3_key: string
  size_bytes: number
  triggered_by: string
  error_message?: string
  started_at: string
  finished_at?: string
}

export interface RequestDetailBackupSchedule {
  enabled: boolean
  cron_expr: string
  retain_days: number
  retain_count: number
}

export async function list(params: RequestDetailListParams, options?: { signal?: AbortSignal }): Promise<PaginatedResponse<RequestDetailSummary>> {
  const { data } = await apiClient.get<PaginatedResponse<RequestDetailSummary>>('/admin/request-details', {
    params,
    signal: options?.signal
  })
  return data
}
```

Add the remaining API functions to `frontend/src/api/admin/requestDetails.ts`:

```ts
export async function get(id: number): Promise<RequestDetail> {
  const { data } = await apiClient.get<RequestDetail>(`/admin/request-details/${id}`)
  return data
}

export async function exportExcel(params: RequestDetailListParams): Promise<Blob> {
  const { data } = await apiClient.get<Blob>('/admin/request-details/export', {
    params,
    responseType: 'blob'
  })
  return data
}

export async function createBackup(): Promise<RequestDetailBackupRecord> {
  const { data } = await apiClient.post<RequestDetailBackupRecord>('/admin/request-details/backups', {})
  return data
}

export async function listBackups(): Promise<{ items: RequestDetailBackupRecord[] }> {
  const { data } = await apiClient.get<{ items: RequestDetailBackupRecord[] }>('/admin/request-details/backups')
  return data
}

export async function getBackup(id: string): Promise<RequestDetailBackupRecord> {
  const { data } = await apiClient.get<RequestDetailBackupRecord>(`/admin/request-details/backups/${id}`)
  return data
}

export async function getDownloadURL(id: string): Promise<{ url: string }> {
  const { data } = await apiClient.get<{ url: string }>(`/admin/request-details/backups/${id}/download-url`)
  return data
}

export async function getBackupSchedule(): Promise<RequestDetailBackupSchedule> {
  const { data } = await apiClient.get<RequestDetailBackupSchedule>('/admin/request-details/backup-schedule')
  return data
}

export async function updateBackupSchedule(config: RequestDetailBackupSchedule): Promise<RequestDetailBackupSchedule> {
  const { data } = await apiClient.put<RequestDetailBackupSchedule>('/admin/request-details/backup-schedule', config)
  return data
}

export const requestDetailsAPI = {
  list,
  get,
  exportExcel,
  createBackup,
  listBackups,
  getBackup,
  getDownloadURL,
  getBackupSchedule,
  updateBackupSchedule
}

export default requestDetailsAPI
```

- [ ] **Step 3: 导出 API**

Modify `frontend/src/api/admin/index.ts`:

```ts
export { default as requestDetailsAPI } from './requestDetails'
export * from './requestDetails'
```

- [ ] **Step 4: 添加路由**

Modify `frontend/src/router/index.ts` near `/admin/usage`:

```ts
{
  path: '/admin/request-details',
  name: 'AdminRequestDetails',
  component: () => import('@/views/admin/RequestDetailsView.vue'),
  meta: {
    requiresAuth: true,
    requiresAdmin: true,
    title: 'Request Details',
    titleKey: 'admin.requestDetails.title',
    descriptionKey: 'admin.requestDetails.description'
  }
},
```

- [ ] **Step 5: 添加菜单**

Modify `frontend/src/components/layout/AppSidebar.vue`:

```ts
{ path: '/admin/usage', label: t('nav.usage'), icon: ChartIcon },
{ path: '/admin/request-details', label: t('nav.requestDetails'), icon: DocumentIcon }
```

If `DocumentIcon` is not already imported, use `ClipboardDocumentListIcon` from `@heroicons/vue/24/outline` and add it to the existing icon import list in `AppSidebar.vue`.

- [ ] **Step 6: 添加 i18n**

Modify `frontend/src/i18n/locales/zh.ts`:

```ts
nav: {
  requestDetails: '请求详情',
}
admin: {
  requestDetails: {
    title: '请求详情',
    description: '查看和备份大模型调用的完整输入输出参数',
  }
}
```

Modify `frontend/src/i18n/locales/en.ts` similarly:

```ts
requestDetails: 'Request Details'
```

- [ ] **Step 7: 运行前端 API 测试**

Run:

```bash
rtk npm --prefix frontend run test:run -- src/api/__tests__/requestDetails.spec.ts
```

Expected: PASS.

- [ ] **Step 8: 提交前端骨架**

Run:

```bash
rtk git add frontend/src/api/admin/requestDetails.ts frontend/src/api/admin/index.ts frontend/src/api/__tests__/requestDetails.spec.ts frontend/src/router/index.ts frontend/src/components/layout/AppSidebar.vue frontend/src/i18n/locales/zh.ts frontend/src/i18n/locales/en.ts
rtk git commit -m "feat: 添加请求详情前端入口"
```

Expected: commit succeeds.

---

### Task 9: 请求详情管理页面

**Files:**
- Create: `frontend/src/views/admin/RequestDetailsView.vue`
- Test: `frontend/src/views/admin/__tests__/RequestDetailsView.spec.ts`

- [ ] **Step 1: 写失败的页面测试**

Create `frontend/src/views/admin/__tests__/RequestDetailsView.spec.ts`:

```ts
import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import RequestDetailsView from '../RequestDetailsView.vue'

vi.mock('@/api/admin/requestDetails', () => ({
  requestDetailsAPI: {
    list: vi.fn().mockResolvedValue({ items: [], total: 0, page: 1, page_size: 20, pages: 1 }),
    listBackups: vi.fn().mockResolvedValue({ items: [] }),
    getBackupSchedule: vi.fn().mockResolvedValue({ enabled: false, cron_expr: '0 2 * * *', retain_days: 0, retain_count: 0 })
  }
}))

describe('RequestDetailsView', () => {
  it('renders request details filters and backup section', async () => {
    const wrapper = mount(RequestDetailsView, {
      global: {
        stubs: {
          DataTable: true,
          Pagination: true,
          Modal: true
        }
      }
    })
    expect(wrapper.text()).toContain('请求详情')
    expect(wrapper.text()).toContain('导出 Excel')
    expect(wrapper.text()).toContain('S3 备份')
  })
})
```

- [ ] **Step 2: 实现页面**

Create `frontend/src/views/admin/RequestDetailsView.vue` with:

```vue
<template>
  <div class="space-y-6">
    <div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
      <div>
        <h1 class="text-2xl font-semibold text-gray-900 dark:text-white">请求详情</h1>
        <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">查看大模型调用的完整输入输出参数</p>
      </div>
      <div class="flex flex-wrap gap-2">
        <button class="btn btn-secondary" :disabled="loading" @click="handleExport">导出 Excel</button>
        <button class="btn btn-primary" :disabled="backupRunning" @click="handleCreateBackup">备份请求详情</button>
      </div>
    </div>

    <section class="card p-4">
      <div class="grid gap-3 md:grid-cols-4">
        <input v-model="filters.request_id" class="input" placeholder="Request ID" />
        <input v-model="filters.user_id" class="input" placeholder="用户 ID" />
        <input v-model="filters.api_key_id" class="input" placeholder="API Key ID" />
        <input v-model="filters.model" class="input" placeholder="模型" />
        <input v-model="filters.endpoint" class="input" placeholder="Endpoint" />
        <select v-model="filters.platform" class="input">
          <option value="">全部平台</option>
          <option value="anthropic">Anthropic</option>
          <option value="openai">OpenAI</option>
          <option value="gemini">Gemini</option>
          <option value="antigravity">Antigravity</option>
        </select>
        <select v-model="filters.success" class="input">
          <option value="">全部状态</option>
          <option value="true">成功</option>
          <option value="false">失败</option>
        </select>
        <select v-model="filters.stream" class="input">
          <option value="">全部模式</option>
          <option value="true">流式</option>
          <option value="false">非流式</option>
        </select>
      </div>
      <div class="mt-4 flex gap-2">
        <button class="btn btn-primary" @click="loadData">查询</button>
        <button class="btn btn-secondary" @click="resetFilters">重置</button>
      </div>
    </section>
  </div>
</template>
```

Add a `<script setup lang="ts">` section that defines:

```ts
import { computed, onMounted, reactive, ref } from 'vue'
import { saveAs } from 'file-saver'
import { requestDetailsAPI, type RequestDetail, type RequestDetailBackupRecord, type RequestDetailBackupSchedule, type RequestDetailListParams, type RequestDetailSummary } from '@/api/admin/requestDetails'
import { useAppStore } from '@/stores'

const appStore = useAppStore()
const loading = ref(false)
const backupRunning = ref(false)
const rows = ref<RequestDetailSummary[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(20)
const selectedDetail = ref<RequestDetail | null>(null)
const backups = ref<RequestDetailBackupRecord[]>([])
const schedule = reactive<RequestDetailBackupSchedule>({ enabled: false, cron_expr: '0 2 * * *', retain_days: 0, retain_count: 0 })
const filters = reactive({
  request_id: '',
  user_id: '',
  api_key_id: '',
  account_id: '',
  group_id: '',
  platform: '',
  model: '',
  endpoint: '',
  status_code: '',
  success: '',
  stream: ''
})

const buildQueryParams = (): RequestDetailListParams => ({
  page: page.value,
  page_size: pageSize.value,
  request_id: filters.request_id || undefined,
  user_id: filters.user_id ? Number(filters.user_id) : undefined,
  api_key_id: filters.api_key_id ? Number(filters.api_key_id) : undefined,
  account_id: filters.account_id ? Number(filters.account_id) : undefined,
  group_id: filters.group_id ? Number(filters.group_id) : undefined,
  platform: filters.platform || undefined,
  model: filters.model || undefined,
  endpoint: filters.endpoint || undefined,
  status_code: filters.status_code ? Number(filters.status_code) : undefined,
  success: filters.success === '' ? undefined : filters.success === 'true',
  stream: filters.stream === '' ? undefined : filters.stream === 'true',
  sort_by: 'created_at',
  sort_order: 'desc'
})

const loadData = async () => {
  loading.value = true
  try {
    const result = await requestDetailsAPI.list(buildQueryParams())
    rows.value = result.items
    total.value = result.total
  } finally {
    loading.value = false
  }
}
```

Also implement `resetFilters`, `openDetail`, `handleExport`, `handleCreateBackup`, `loadBackups`, `saveSchedule`, `downloadBackup`, and `copyText`. Use `appStore.showSuccess/showError` for user feedback.

- [ ] **Step 3: 添加详情弹窗**

In the same component, add a modal section showing:

- 基本信息
- 请求头 JSON
- 入站请求体
- 上游请求体
- 响应头 JSON
- 响应体

Use `<pre class="max-h-96 overflow-auto rounded bg-gray-50 p-3 text-xs dark:bg-dark-800">`.

- [ ] **Step 4: 添加备份区**

Add one `section.card` for backup controls:

- manual backup button already in top actions
- schedule enabled checkbox
- cron input
- retain days/count inputs
- save schedule button
- backup records list with download action

- [ ] **Step 5: 导出 Excel**

Implement:

```ts
const handleExport = async () => {
  const blob = await requestDetailsAPI.exportExcel(buildQueryParams())
  saveAs(blob, `request_details_${new Date().toISOString().slice(0, 19).replace(/[:T]/g, '')}.xlsx`)
}
```

Use `file-saver` already installed.

- [ ] **Step 6: 运行页面测试**

Run:

```bash
rtk npm --prefix frontend run test:run -- src/views/admin/__tests__/RequestDetailsView.spec.ts
```

Expected: PASS.

- [ ] **Step 7: 提交页面**

Run:

```bash
rtk git add frontend/src/views/admin/RequestDetailsView.vue frontend/src/views/admin/__tests__/RequestDetailsView.spec.ts
rtk git commit -m "feat: 添加请求详情管理页面"
```

Expected: commit succeeds.

---

### Task 10: 集成验证和修复

**Files:**
- Modify only the files touched by earlier tasks when verification reveals compile, type, lint, or behavior failures.

- [ ] **Step 1: 后端完整测试**

Run:

```bash
rtk go test ./backend/...
```

Expected: PASS.

- [ ] **Step 2: 前端类型检查**

Run:

```bash
rtk npm --prefix frontend run typecheck
```

Expected: PASS. If script name mismatch occurs, use:

```bash
rtk npm --prefix frontend run type-check
```

only if package scripts contain `type-check`; current project uses `typecheck`.

- [ ] **Step 3: 前端构建**

Run:

```bash
rtk npm --prefix frontend run build
```

Expected: PASS.

- [ ] **Step 4: 检查工作树**

Run:

```bash
rtk git status --short
```

Expected: only intentional tracked changes remain; `.superpowers/` may remain untracked from brainstorming and should not be staged.

- [ ] **Step 5: 提交验证修复**

If verification required fixes, first inspect the worktree:

```bash
rtk git status --short
```

Then stage only the files changed by the verification fixes. Example for common compile/type fixes:

```bash
rtk git add backend/internal/service/request_detail.go backend/internal/handler/admin/request_detail_handler.go frontend/src/views/admin/RequestDetailsView.vue
rtk git commit -m "fix: 修复请求详情验证问题"
```

Expected: commit succeeds after staging the actual fixed files; no commit is needed if all tests passed without changes.

---

## Self-Review

- Spec coverage: 数据库永久保存、管理员页面、完整输入输出、Excel 导出、独立手动备份、独立定时备份、复用现有 S3 配置均有对应任务。
- Scope: 单一功能纵向切片，虽然跨后端和前端，但边界清晰，不需要拆成多个 spec。
- TDD: 每个主要后端/前端模块都有先写失败测试、再实现、再运行测试的步骤。
- Risk controls: 网关热路径先实现捕获器和异步写入，再接入路由；列表默认不返回正文；备份用 NDJSON gzip，不依赖 Excel 承载长期备份。
