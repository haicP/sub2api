# 请求详情功能设计

## 背景

管理员需要审计用户调用大模型时的完整输入输出参数。现有 `usage_logs` 只保存用量、成本、模型、endpoint、IP、User-Agent 等摘要数据，不能还原完整请求与响应；现有数据库备份功能支持 S3 配置、手动备份和定时备份，但备份对象是整库 dump，不适合单独管理请求详情审计数据。

本功能新增独立的“请求详情”能力：完整记录大模型调用的输入输出，管理员可查询、查看详情、导出 Excel，并可单独将请求详情备份到现有 S3 存储。

## 目标

- 仅管理员可访问请求详情页面和 API。
- 记录用户调用大模型时完整输入输出参数，永久保存在数据库。
- 新增管理端菜单“请求详情”，页面风格与现有管理页面一致。
- 支持按当前筛选条件导出 Excel。
- 支持请求详情独立手动备份到 S3。
- 支持请求详情独立定时备份到 S3，定时配置放在“请求详情”页面内。
- S3 连接复用现有数据库备份功能的 S3 配置、加密存储和对象存储实现。

## 非目标

- 不改动现有数据库整库备份/恢复语义。
- 不把完整请求响应混入 `usage_logs`。
- 不为请求详情新增独立 S3 连接配置。
- 不实现请求详情自动清理；数据按需求永久保留。
- 不向普通用户暴露请求详情。

## 推荐方案

新增独立 `request_details` 表，配套捕获器、仓储、服务、管理员 API、前端页面和独立 S3 备份服务。

此方案比扩展 `usage_logs` 更清晰：`usage_logs` 继续负责计费和统计，`request_details` 负责审计和回溯。完整请求响应体可能非常大，独立表能减少对现有使用统计查询的影响，也便于后续单独索引、导出和备份。

## 数据模型

新增 Ent schema `RequestDetail`，对应表 `request_details`。字段如下：

- `id`: 自增主键。
- `request_id`: 请求唯一 ID，用于和 `usage_logs.request_id` 关联。
- `created_at`: 接收到请求的时间。
- `completed_at`: 响应完成或失败的时间。
- `duration_ms`: 请求总耗时。
- `status_code`: 写给客户端的 HTTP 状态码。
- `success`: 是否成功。
- `platform`: 协议/平台，例如 `anthropic`、`openai`、`gemini`、`antigravity`。
- `endpoint`: 客户端访问的 endpoint。
- `upstream_endpoint`: 实际上游 endpoint。
- `model`: 客户端请求模型。
- `upstream_model`: 实际上游模型。
- `stream`: 是否流式。
- `user_id`: 用户 ID。
- `api_key_id`: API Key ID。
- `account_id`: 上游账号 ID。
- `group_id`: 分组 ID。
- `subscription_id`: 用户订阅 ID。
- `ip_address`: 客户端 IP。
- `user_agent`: User-Agent。
- `request_headers`: JSON，保存脱敏后的入站请求头。
- `request_body`: Text，保存完整入站请求 body。
- `upstream_request_body`: Text，保存实际发给上游的请求 body。
- `response_headers`: JSON，保存写给客户端的响应头。
- `response_body`: Text，保存完整写给客户端的响应体；流式请求保存原始 SSE 文本。
- `response_truncated`: bool，固定为 `false`，表示本版本不截断响应。
- `error_message`: 请求失败时的错误描述。

索引：

- `request_id` 唯一索引。
- `created_at` 降序查询索引。
- `user_id, created_at`。
- `api_key_id, created_at`。
- `account_id, created_at`。
- `model, created_at`。
- `platform, created_at`。
- `status_code, created_at`。

正文永久保留。由于请求和响应可能包含敏感内容，只允许管理员 API 返回这些字段，并且普通用量页面、运维实时页面不展示正文。

## 捕获链路

新增 `RequestDetailCapture`，在大模型网关请求入口创建并放入 `gin.Context`。

捕获内容：

- 入站请求：记录 method、path、query、脱敏 headers、原始 body、IP、User-Agent、request_id。
- 请求上下文：记录用户、API Key、分组、订阅、账号、平台、endpoint、模型、stream。
- 上游请求：复用现有 `setOpsUpstreamRequestBody(c, body)` 的调用思路，在同一位置同步写入 `upstream_request_body`；构建上游请求时记录 `upstream_endpoint` 和 `upstream_model`。
- 下游响应：用包装后的 `gin.ResponseWriter` tee 捕获写给客户端的字节；非流式保存完整 JSON，流式保存实际写出的 SSE 文本。
- 结束状态：请求完成时记录 status、headers、completed_at、duration、success、error_message。

写入策略：

- 请求处理结束后异步持久化，避免阻塞热路径。
- 后台写入队列满时返回日志错误，不影响用户请求。
- 捕获失败不影响网关转发、计费或现有 `usage_logs` 写入。
- 失败请求也保存已知上下文、请求体、错误信息和已写出的响应。

脱敏策略：

- `Authorization`、`Proxy-Authorization`、`X-API-Key`、`Cookie`、`Set-Cookie` 等头部保存为 `***REDACTED***`。
- 正文按用户要求完整保存，不做字段级脱敏。

## 后端接口

在 `/api/v1/admin` 下新增请求详情路由，继承现有 `adminAuth`。

- `GET /request-details`: 列表查询。
- `GET /request-details/:id`: 详情查询。
- `GET /request-details/export`: 按筛选条件导出 Excel。
- `POST /request-details/backups`: 创建请求详情手动备份。
- `GET /request-details/backups`: 查看请求详情备份记录。
- `GET /request-details/backups/:id`: 查看单个备份记录。
- `GET /request-details/backups/:id/download-url`: 获取 S3 临时下载链接。
- `GET /request-details/backup-schedule`: 查看请求详情定时备份配置。
- `PUT /request-details/backup-schedule`: 更新请求详情定时备份配置。

列表筛选支持：

- 时间范围。
- 用户 ID。
- API Key ID。
- 账号 ID。
- 分组 ID。
- 平台。
- 模型。
- endpoint。
- request_id。
- status_code。
- success。
- stream。

列表默认不返回完整正文，只返回正文长度、状态和摘要字段。详情接口返回完整请求体、上游请求体和响应体。

## Excel 导出

导出由后端生成 `.xlsx`，按当前筛选条件导出请求详情。为符合“完整输入输出参数”要求，Excel 包含：

- 元数据列：请求时间、完成时间、耗时、状态、平台、endpoint、模型、用户、API Key、账号、分组、IP、User-Agent。
- 请求列：请求头、入站请求体、上游请求体。
- 响应列：响应头、响应体、错误信息。

导出接口使用流式响应，文件名格式为 `request_details_YYYYMMDD_HHMMSS.xlsx`。导出量过大时由接口返回明确错误，提示缩小筛选范围；该限制只保护 Excel 生成，不影响数据库永久保存和 S3 备份完整性。

## S3 备份

新增 `RequestDetailBackupService`，复用现有：

- `BackupS3Config`。
- `BackupObjectStoreFactory`。
- `BackupService` 加载和解密 S3 配置的能力。

备份文件格式：

- 文件名：`request_details_YYYYMMDD_HHMMSS.ndjson.gz`。
- 内容：每行一条请求详情完整记录，包含正文和元数据。
- Content-Type：`application/gzip`。
- S3 key：使用现有 S3 prefix，并追加 `request-details/` 子目录。

备份记录独立保存，避免和整库备份记录混淆。记录字段包括 ID、状态、文件名、S3 key、大小、触发方式、错误、开始时间、完成时间。

定时备份配置独立保存到 settings，建议 key：

- `request_detail_backup_schedule`。
- `request_detail_backup_records`。

定时表达式语义与现有数据库备份一致，默认关闭。手动备份和定时备份都执行全量请求详情备份。

## 前端页面

新增管理端路由 `/admin/request-details`，`requiresAdmin: true`。侧边栏管理员菜单新增“请求详情”，放在“使用记录”附近。

页面结构：

- 顶部筛选区：时间、用户 ID、API Key ID、账号 ID、分组 ID、平台、模型、endpoint、request_id、状态、stream。
- 操作区：查询、重置、导出 Excel、手动备份。
- 表格区：按时间倒序展示请求摘要。
- 详情抽屉或弹窗：展示请求头、入站请求体、上游请求体、响应头、响应体，支持复制。
- 备份区：显示 S3 连接状态提示、定时备份配置、备份记录列表、下载链接。

样式复用现有管理端 `card`、`input`、`btn`、`DataTable`、`Pagination` 等组件和 Tailwind class，不新增独立视觉体系。

## 错误处理

- 非管理员访问由现有鉴权拦截。
- 请求详情写入失败只记录后端日志，不影响用户调用大模型。
- Excel 导出超出保护阈值时返回 400，提示缩小筛选范围。
- S3 未配置时，手动备份和定时备份返回现有风格的 `BACKUP_S3_NOT_CONFIGURED` 错误。
- 定时备份任务启动后如果服务重启，重启时将未完成的请求详情备份记录标记为失败。

## 测试计划

后端：

- 捕获器单元测试：脱敏 headers、捕获响应体、捕获流式 SSE、失败请求仍落库。
- 仓储测试：创建、列表筛选、详情查询、索引字段查询。
- 管理 API 测试：管理员鉴权、列表、详情、导出、备份接口。
- 备份服务测试：生成 NDJSON gzip、复用 S3 配置、手动备份记录、定时配置保存。

前端：

- API client 测试：筛选参数、导出 blob、备份接口。
- 页面组件测试：筛选、分页、详情展示、导出按钮、备份配置。

验证：

- `rtk go test ./backend/...`。
- `rtk npm --prefix frontend run type-check`。
- `rtk npm --prefix frontend run build`。

## 实施顺序

1. 新增数据模型、迁移和仓储。
2. 新增请求详情捕获器和后台写入服务。
3. 接入网关入口、上游请求记录点和响应 writer。
4. 新增管理员查询、详情、Excel 导出接口。
5. 新增请求详情 S3 手动备份和定时备份。
6. 新增前端 API client、路由、菜单和页面。
7. 补充测试并执行后端、前端验证。
