# Stage 1: 项目初始化 + OpenAPI 定义 + 代码生成

> 目标：搭建项目骨架，定义 API 协议（单一事实来源），生成全部 Server/Client 代码，确保编译通过。

## 前置条件

- 无（首个阶段）

## 任务清单

### 1.1 Go Module 初始化

- [ ] `go mod init` 创建模块
- [ ] 添加所有关键依赖到 go.mod：
  - `github.com/oapi-codegen/oapi-codegen/v2`
  - `github.com/go-chi/chi/v5`
  - `modernc.org/sqlite`
  - `github.com/google/uuid`
  - `github.com/agnivade/levenshtein`
  - `github.com/modelcontextprotocol/go-sdk`（MCP Go SDK，暂不使用但先引入）
- [ ] 创建目标目录结构骨架（cmd/, pkg/, internal/ 等空目录 + .gitkeep）

### 1.2 编写 OpenAPI 3.0 定义

- [ ] 创建 `api/openapi.yaml`，定义完整 API 协议
- [ ] **Agent 域** `/api/v1/agent/`（6 端点）：
  - POST /agent/browse (kh_browse)
  - POST /agent/search (kh_search)
  - GET /agent/knowledge/{id} (kh_read_full)
  - POST /agent/knowledge (kh_contribute)
  - POST /agent/knowledge/{id}/append (kh_append_knowledge)
  - POST /agent/knowledge/{id}/comments (kh_comment)
- [ ] **Admin 域** `/api/v1/admin/`（11 端点）：
  - GET /admin/flagged (kh_list_flagged)
  - GET /admin/tags/health (kh_tag_health)
  - GET /admin/knowledge/similar (kh_find_similar)
  - GET /admin/knowledge/{id}/review (kh_get_review)
  - PUT /admin/knowledge/{id} (kh_update_knowledge)
  - POST /admin/knowledge/{id}/archive (kh_archive)
  - POST /admin/comments/processed (kh_mark_processed)
  - POST /admin/tags/merge (kh_merge_tags)
  - POST /admin/knowledge/merge (kh_merge_knowledge)
  - POST /admin/conflicts (kh_create_conflict)
  - POST /admin/curation-logs (kh_log_curation)
- [ ] **System 域** `/api/v1/system/`（8 端点）：
  - POST /system/recalculate-weights
  - GET /system/status
  - GET /system/knowledge
  - GET /system/knowledge/{id}
  - DELETE /system/knowledge/{id}
  - POST /system/knowledge/{id}/restore
  - GET /system/conflicts
  - POST /system/conflicts/{id}/resolve
  - GET /system/curation-logs
- [ ] 定义所有 Schema（Request/Response 类型）：
  - KnowledgeEntry, Comment, Tag, CurationLog, ConflictReport
  - BrowseRequest/Response, SearchRequest/Response
  - 各端点的 Request/Response body
  - 错误响应统一格式 (ErrorResponse)
- [ ] 定义枚举类型：CommentType, CurationAction, KnowledgeStatus, ConflictStatus, AppendType

### 1.3 oapi-codegen 配置 + 代码生成

- [ ] 创建 oapi-codegen 配置文件：
  - Server 端配置 → `internal/server/handlers/` (chi-server + types)
  - Client 端配置 → `pkg/khclient/` (client + types)
- [ ] 运行 oapi-codegen 生成代码
- [ ] 添加 `go generate` 指令到项目

### 1.4 构建验证

- [ ] 确保 `go build ./...` 编译通过（可能需要添加 stub 文件）
- [ ] 创建 Makefile（generate, build, test, clean 目标）

## 交付物

- `go.mod` / `go.sum`
- `api/openapi.yaml`（完整 25 端点定义）
- `internal/server/handlers/` 生成的 Server Interface + Types
- `pkg/khclient/` 生成的 HTTP Client + Types
- `Makefile`
- 所有代码编译通过

## 参考文档

- `docs/specs/api-protocol.md` — 端点详细定义
- `docs/engineering-design.md` §5 — API 接口设计
