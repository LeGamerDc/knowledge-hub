# API 协议规范 (OpenAPI 驱动)

本文档定义 HTTP REST API 设计。`api/openapi.yaml` 为单一事实来源（Single Source of Truth），通过 `oapi-codegen` 生成 Server Handler 和强类型 Client，确保接口绝对一致。

## 1. 路由域划分

API 划分为三大域，实现权限与职责隔离：

| 域 | 路径前缀 | 使用者 | 数据可见范围 |
|----|---------|-------|------------|
| Agent | `/api/v1/agent/` | 工作 Agent (MCP) | 仅 `status=ACTIVE` |
| Admin | `/api/v1/admin/` | 管理员 Agent (MCP) | 全部状态 + 批量操作 |
| System | `/api/v1/system/` | `kh` CLI / 人类 | 最高权限（含硬删除） |

## 2. 命名规范

- **MCP Tool 名**：统一使用 `kh_` 前缀，无角色前缀。角色隔离由 MCP 配置层处理（工作 Agent 和管理员 Agent 配置不同的 tool set）。
- **HTTP 端点**：RESTful 风格，与 MCP Tool 1:1 映射。

## 3. 工作 Agent API (`/api/v1/agent/`) — 6 endpoints

### 3.1 `POST /api/v1/agent/browse` → `kh_browse`

Faceted 浏览：无参数返回顶层高频 Tag；传入已选 Tag 返回交叉过滤后的下一层 Tag。

- **Request**: `{ "selected_tags": ["go"] }`
- **Response**: `{ "tags": [{"name": "concurrency", "count": 12}], "total_matches": 45 }`

### 3.2 `POST /api/v1/agent/search` → `kh_search`

搜索知识，按权重排序，返回标题 + 摘要（最多 20 条）。

- **Request**: `{ "tags": ["go", "concurrency"], "keyword": "goroutine leak" }`
- **Response**: `[{ "id": "uuid", "title": "...", "summary": "...", "tags": [...], "weight": 4.5 }]`

### 3.3 `GET /api/v1/agent/knowledge/{id}` → `kh_read_full`

读取知识全文。同时更新 `accessed_at` 和 `access_count`。

- **Response**: `{ "id": "...", "title": "...", "summary": "...", "body": "...", "tags": [...], "weight": 4.5, "author": "...", "comments_summary": { "success": 3, "failure": 1, "supplement": 2 }, "created_at": "...", "updated_at": "..." }`

### 3.4 `POST /api/v1/agent/knowledge` → `kh_contribute`

贡献新知识。

- **Request**: `{ "title": "...", "summary": "...", "body": "...", "tags": ["go", "concurrency"], "author": "agent-session-xxx" }`
- **Response**: `{ "id": "uuid" }`

### 3.5 `POST /api/v1/agent/knowledge/{id}/append` → `kh_append_knowledge`

追加内容到知识末尾（Blockquote 格式带时间戳），避免全文替换。`append_count` +1，超过阈值（默认 5）时自动标记 `needs_rewrite = true`。

- **Request**: `{ "type": "supplement|correction", "content": "supplementary text..." }`
- **Response**: `{ "id": "...", "append_count": 3 }`

### 3.6 `POST /api/v1/agent/knowledge/{id}/comments` → `kh_comment`

添加评论。`reasoning` 必填，说明 WHY。

- **Request**: `{ "type": "success|failure|supplement|correction", "content": "...", "reasoning": "...", "scenario": "...", "author": "agent-session-xxx" }`
- **Response**: `{ "id": "uuid" }`

## 4. 管理 Agent API (`/api/v1/admin/`) — 11 endpoints

### 4.1 Discovery（发现问题，API Server 算法驱动）

#### `GET /api/v1/admin/flagged` → `kh_list_flagged`

列出需要审查的知识条目。Server 端根据以下规则自动 flag：
- 有未处理评论（`processed=false`）
- Failure 评论占比 > 50%
- 30+ 天未被访问
- `needs_rewrite = true`（append_count 超阈值）
- 触发 failure eviction threshold（30 天内 >= 3 条 failure/correction）

- **Response**: `[{ "id": "...", "title": "...", "flag_reasons": ["unprocessed_comments", "high_failure_ratio"], "comment_stats": { "total": 5, "unprocessed": 3, "failure": 2 }, "weight": 1.2, "needs_rewrite": true }]`

#### `GET /api/v1/admin/tags/health` → `kh_tag_health`

Tag 健康度报告。Server 端执行算法检测：
- 疑似同义 Tag 对（编辑距离 <= 2、子串关系、共现率 > 80%、已有别名匹配）
- 低频 Tag（< 3 次使用）
- 异常高频 Tag（> 30% 占比，粒度可能过粗）

- **Response**: `{ "similar_pairs": [{ "tags": ["logging", "log"], "reason": "substring" }], "low_freq": [{ "name": "misc", "frequency": 1 }], "high_freq": [{ "name": "go", "frequency": 45, "share": 0.38 }] }`

#### `GET /api/v1/admin/knowledge/similar` → `kh_find_similar`

疑似重复知识对（Tag 重叠率 > 80%）。

- **Response**: `[{ "id_a": "...", "title_a": "...", "id_b": "...", "title_b": "...", "overlap_tags": ["go", "http", "timeout"], "overlap_ratio": 0.85 }]`

### 4.2 Review（审查详情）

#### `GET /api/v1/admin/knowledge/{id}/review` → `kh_get_review`

获取知识审查详情：全文 + 所有未处理评论。

- **Response**: `{ "knowledge": { /* full entry fields */ }, "unprocessed_comments": [{ "id": "...", "type": "failure", "content": "...", "reasoning": "...", "scenario": "...", "created_at": "..." }] }`

### 4.3 Execute（执行操作）

#### `PUT /api/v1/admin/knowledge/{id}` → `kh_update_knowledge`

全量更新知识内容。**仅用于管理员 Agent 全文重构**（将追加的 blockquote 融合为连贯正文）。重置 `append_count = 0`、`needs_rewrite = false`。

- **Request**: `{ "title": "...", "summary": "...", "body": "...", "tags": ["go", "http"] }`
- **Response**: `{ "updated_at": "..." }`

#### `POST /api/v1/admin/knowledge/{id}/archive` → `kh_archive`

归档知识条目（软删除）。

- **Response**: `{}`

#### `POST /api/v1/admin/comments/processed` → `kh_mark_processed`

批量标记评论为已处理。

- **Request**: `{ "comment_ids": ["id1", "id2"] }`
- **Response**: `{}`

#### `POST /api/v1/admin/tags/merge` → `kh_merge_tags`

合并 Tag：将所有 sources 的知识重新打标为 target，sources 作为 target 的 aliases。

- **Request**: `{ "target": "logging", "sources": ["log", "日志"] }`
- **Response**: `{ "affected_knowledge_count": 13 }`

#### `POST /api/v1/admin/knowledge/merge` → `kh_merge_knowledge`

合并知识条目：source 归档，target 更新为合并后的内容。

- **Request**: `{ "target_id": "...", "source_ids": ["..."], "merged_body": "...", "merged_summary": "..." }`
- **Response**: `{ "id": "..." }`

#### `POST /api/v1/admin/conflicts` → `kh_create_conflict`

创建冲突报告（Agent 无法判定矛盾时兜底给人工）。

- **Request**: `{ "type": "correction_conflict|knowledge_conflict", "knowledge_ids": ["..."], "comment_ids": ["..."], "description": "..." }`
- **Response**: `{ "id": "uuid" }`

#### `POST /api/v1/admin/curation-logs` → `kh_log_curation`

写入整理日志。

- **Request**: `{ "action": "merge_supplement|apply_correction|downgrade|archive|merge_tags|merge_knowledge|create_conflict", "target_id": "...", "source_ids": ["..."], "description": "...", "diff": "..." }`
- **Response**: `{ "id": "uuid" }`

## 5. 系统 API (`/api/v1/system/`) — 8 endpoints

供 `kh` CLI 和系统操作使用。

| HTTP | 说明 | CLI 命令 |
|------|------|---------|
| `POST /system/recalculate-weights` | 批量重算所有知识权重 | `kh_recalculate_weights` MCP Tool |
| `GET /system/status` | 系统状态概览 | `kh status` |
| `GET /system/knowledge` | 列出知识条目（支持 status/tag 过滤） | `kh list` |
| `GET /system/knowledge/{id}` | 读取知识全文 | `kh read` |
| `DELETE /system/knowledge/{id}` | 硬删除（物理移除） | `kh delete` |
| `POST /system/knowledge/{id}/restore` | 恢复归档条目 | `kh restore` |
| `GET /system/conflicts` | 列出冲突报告 | `kh conflicts` |
| `POST /system/conflicts/{id}/resolve` | 解决冲突报告 | `kh resolve` |
| `GET /system/curation-logs` | 查看整理日志 | `kh logs` |

## 6. OpenAPI 工作流 (代码生成)

1. 在 `api/openapi.yaml` 中编写上述全部接口定义。
2. 使用 `github.com/oapi-codegen/oapi-codegen`：
   - **Server 端**：生成 chi-server handler interface + types → `internal/server/handlers/`
   - **Client 端**：生成强类型 HTTP client → `pkg/khclient/`
3. MCP Shim 和 `kh` CLI 直接引入 `pkg/khclient`，享受强类型方法调用（如 `client.Browse(ctx, req)`）。

## 7. 渐进式检索 Token 控制

| 层级 | 返回内容 | 预估 Token |
|------|---------|-----------:|
| 目录浏览 (`kh_browse`) | Tag 列表 + 每个 Tag 文章数 | ~200-500 |
| 搜索结果 (`kh_search`) | 标题 + 摘要 + Tags + 权重（最多 20 条） | ~500-1500 |
| 全文 (`kh_read_full`) | 完整 Markdown + 评论概要 | ~500-2000 |
