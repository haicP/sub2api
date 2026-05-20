# Git 上游同步流程

## 分支约定
- `main` 分支只用于同步 GitHub 上游代码，不在 `main` 上做本地开发修改。
- `codex/` 前缀分支用于保存本地修改和 Codex 变更。

## 默认同步步骤
当 GitHub 上游代码更新后，按以下流程将更新安全合并进本地 `codex/` 分支：

```bash
# 1. 确认当前修改已提交或暂存
git status

# 2. 更新本地 main
git switch main
git fetch origin
git pull --ff-only origin main

# 3. 切回本地修改分支
git switch codex/你的分支名

# 4. 合并最新 main
git merge main
```

## 冲突处理
如果合并时出现冲突：

```bash
git status
# 手动修改冲突文件
git add 冲突文件
git commit
```

如果发现合并方向不对，或冲突复杂到不适合继续处理，可以中止合并：

```bash
git merge --abort
```

## 合并前备份
长期维护的本地分支在合并前建议创建备份分支：

```bash
git switch codex/你的分支名
git branch backup/codex-你的分支名-$(date +%Y%m%d-%H%M)
git merge main
```

## 合并后验证
合并完成后根据仓库实际技术栈运行验证命令，例如：

```bash
npm test
pnpm test
pytest
go test ./...
```

## OpenAI Responses SSE 合并注意事项
- 合并上游涉及 OpenAI Responses、Chat Completions 兼容、Messages 兼容、WebSocket fallback、SSE 转发或 usage drain 代码时，必须专项检查流式终止事件处理。
- 不要只依赖 `data:` JSON 内的 `type` 字段判断终止事件；部分上游会通过 SSE `event: response.completed` / `event: response.done` 表达事件类型，而 `data:` payload 可能不带 `type`。这种情况下应复用或保持 `openAICompatSSEFrameParser` / `openAICompatPayloadWithEventType` 等兼容逻辑。
- 在为了避免上游连接不关闭而读到 terminal event 后快速返回时，必须先把完整 SSE frame 写给下游客户端。`data: {...}\n\n` 中最后的空行是事件分帧结束标记；如果只写出 `data: {...}\n` 就关闭连接，OpenAI SDK / Codex 类客户端可能报 `stream disconnected before completion: stream closed before response.completed`。
- 针对此类合并，至少运行相关回归测试：

```bash
cd backend
go test ./internal/service -run 'TestOpenAIStreaming(EventLineTerminalWithoutPayloadTypeSucceeds|PassthroughEventLineTerminalWithoutPayloadTypeSucceeds|TerminalEventWithoutUpstreamCloseReturns|PassthroughTerminalEventWithoutUpstreamCloseReturns)'
go test ./internal/service
```

- 如果新增或改动相关测试，断言不应只检查响应体包含 `response.completed`，还应覆盖终止帧已用空行闭合，例如响应体以 `\n\n` 结束。

## merge 与 rebase 选择
- 默认使用 `git merge main`，因为它不会重写 `codex/` 分支历史，适合长期本地修改。
- 只有当 `codex/` 分支仅自己使用、没有被别人基于开发，并且明确需要线性历史时，才使用 `git rebase main`。

# GitHub 使用规则

## 远端与分支
- 默认远端为 `origin`，GitHub 仓库为本仓库唯一远端来源；执行远端操作前先用 `git remote -v` 或 `git status --short --branch` 确认目标。
- `main` 只同步 `origin/main`，不在本地 `main` 上提交业务修改，也不从本地 `main` 主动推送到 GitHub，除非用户明确要求。
- 本地开发与 Codex 变更统一保存在 `codex/` 前缀分支；当前请求详情分支为 `codex/request-details`。
- 推送 Codex 变更时默认推送当前 `codex/` 分支，例如 `git push -u origin codex/request-details`；不要把 Codex 工作分支强推到 `main`。

## GitHub CLI
- 需要查看 GitHub 状态、Actions 日志、PR 或 Release 时，优先使用 `gh` CLI；首次使用前执行 `gh auth status` 确认登录状态。
- 检查工作流时优先按当前分支过滤，例如 `gh run list --branch codex/request-details --limit 10`。
- 排查失败任务时使用 `gh run view <run-id> --log-failed` 获取失败日志，再基于日志修复代码或配置。
- 需要等待 GitHub Actions 完成时使用 `gh run watch <run-id> --exit-status`，并在结束后复查最新 run 状态。

## Push 与 PR
- push 前必须确认本地工作区状态，避免把无关文件或未确认改动带入提交。
- commit 信息遵循仓库规范：`<type>(scope): <summary>`，summary 使用中文动词开头，长度不超过 50 字，不加句号。
- 创建 PR 时默认从当前 `codex/` 分支发往 `main`，标题沿用主要 commit 语义，正文包含变更摘要、验证结果与风险说明。
- 如果远端已有同一分支 PR，优先更新现有 PR，不重复创建。

## Actions 与凭据
- 修改 `.github/workflows/`、Release、镜像构建或安全扫描规则属于 CI 变更，应在用户明确要求后执行，并同步更新本文件中的相关规则。
- GitHub Actions 中使用 `GITHUB_TOKEN`、仓库 Secrets 或 Variables 注入凭据；不得把 token、密码、私钥、API Key 写入源码、workflow 或文档示例。
- 推送 `codex/request-details` 后需关注 `CI`、`Security Scan` 与 `Codex Docker Image` 相关 workflow；失败时先定位失败 job 和日志，再做最小修复。
- GHCR 镜像发布规则以 `# 仓库打包规则` 中的 codex 分支镜像说明为准；临时 codex 镜像不要复用正式 release workflow。

# 仓库打包规则

## 本地 Docker 构建
- 本地开发容器默认使用 `deploy/docker-compose.dev.yml`，从 `deploy` 目录执行：

```bash
docker compose -p sub2api-dev -f docker-compose.dev.yml up -d --build
```

- 该配置使用仓库根目录 `Dockerfile`，构建上下文为仓库根目录，应用镜像名为 `sub2api-dev-sub2api:latest`。
- 本地开发容器使用 `deploy/data`、`deploy/postgres_data`、`deploy/redis_data` 保存数据，重建应用容器时不要删除这些目录，除非明确需要清空数据。

## Dockerfile 打包约定
- 前端构建阶段使用 `node:24-alpine`，通过 Corepack 固定 `pnpm@9`，执行 `pnpm install --frozen-lockfile` 与 `pnpm run build`。
- 前端产物写入 `backend/internal/web/dist`，后端构建阶段再使用 `go build -tags embed` 将前端静态资源嵌入服务端二进制。
- 后端构建阶段使用 `golang:1.26.3-alpine`，运行时镜像使用 `alpine:3.21`，并从 `postgres:18-alpine` 复制 `pg_dump` 与 `psql`，保证备份工具与数据库大版本一致。

## codex 分支镜像
- `codex/request-details` 分支推送后会触发 `.github/workflows/codex-docker.yml`。
- 该 workflow 构建并推送 linux/amd64 镜像到 GHCR，固定输出以下标签：
  - `ghcr.io/<owner>/sub2api:self`：始终指向最后一次成功构建的 codex 镜像。
  - `ghcr.io/<owner>/sub2api:codex-request-details`：指向当前 codex 分支最新成功构建。
  - `ghcr.io/<owner>/sub2api:codex-request-details-<短 SHA>`：用于追溯具体提交。

## 正式发布镜像
- 正式发布由 `.github/workflows/release.yml` 触发，仅在 `v*` tag push 或手动 `workflow_dispatch` 指定 tag 时运行。
- release workflow 先构建前端并上传产物，再通过 GoReleaser 使用 `Dockerfile.goreleaser` 构建发布镜像。
- 正式发布镜像标签由 `.goreleaser.yaml` 或 `.goreleaser.simple.yaml` 控制，通常包括版本号、`latest` 以及架构后缀标签；不要用正式 release workflow 来构建临时 codex 镜像。
