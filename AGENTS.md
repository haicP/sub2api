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

## merge 与 rebase 选择
- 默认使用 `git merge main`，因为它不会重写 `codex/` 分支历史，适合长期本地修改。
- 只有当 `codex/` 分支仅自己使用、没有被别人基于开发，并且明确需要线性历史时，才使用 `git rebase main`。

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
