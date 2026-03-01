# Stage 5: MCP Shim

> 目标：实现 MCP stdio 进程，将 Agent 的 MCP Tool Call 转为 HTTP 请求发往 API Server，完成 Agent ↔ Server 的完整链路。

## 前置条件

- Stage 1 完成（生成的 HTTP Client 可用）
- Stage 4 完成（API Server 可运行）

## 任务清单

### 5.1 MCP Server 初始化

- [ ] 创建 `cmd/mcp-shim/main.go`
- [ ] 使用 `github.com/modelcontextprotocol/go-sdk` 初始化 MCP Server
- [ ] stdio transport（Claude Code spawn 此进程）
- [ ] API Server 地址配置（默认 `http://localhost:19820`，可通过环境变量配置）
- [ ] 初始化 `pkg/khclient` 生成的 HTTP Client

### 5.2 工作 Agent Tools 注册（6 个）

- [ ] `kh_browse`：
  - 参数：selected_tags []string (可选)
  - 调用 khclient.PostAgentBrowse
  - 返回 tags 列表 + total_matches
- [ ] `kh_search`：
  - 参数：tags []string, keyword string
  - 调用 khclient.PostAgentSearch
  - 返回知识摘要列表
- [ ] `kh_read_full`：
  - 参数：id string
  - 调用 khclient.GetAgentKnowledgeId
  - 返回知识全文 + 评论概要
- [ ] `kh_contribute`：
  - 参数：title, summary, body, tags, author
  - 调用 khclient.PostAgentKnowledge
  - 返回 id
- [ ] `kh_append_knowledge`：
  - 参数：id, type (supplement/correction), content
  - 调用 khclient.PostAgentKnowledgeIdAppend
  - 返回 id + append_count
- [ ] `kh_comment`：
  - 参数：knowledge_id, type, content, reasoning, scenario, author
  - 调用 khclient.PostAgentKnowledgeIdComments
  - 返回 id

### 5.3 管理 Agent Tools 注册（12 个）

- [ ] `kh_list_flagged`：GET /admin/flagged
- [ ] `kh_tag_health`：GET /admin/tags/health
- [ ] `kh_find_similar`：GET /admin/knowledge/similar
- [ ] `kh_get_review`：GET /admin/knowledge/{id}/review
- [ ] `kh_update_knowledge`：PUT /admin/knowledge/{id}
- [ ] `kh_archive`：POST /admin/knowledge/{id}/archive
- [ ] `kh_mark_processed`：POST /admin/comments/processed
- [ ] `kh_merge_tags`：POST /admin/tags/merge
- [ ] `kh_merge_knowledge`：POST /admin/knowledge/merge
- [ ] `kh_create_conflict`：POST /admin/conflicts
- [ ] `kh_log_curation`：POST /admin/curation-logs
- [ ] `kh_recalculate_weights`：POST /system/recalculate-weights

### 5.4 Tool 描述优化

- [ ] 每个 Tool 的 description 需清晰描述用途和参数含义
- [ ] 参数 schema 使用 JSON Schema 定义必填/选填
- [ ] 返回值格式说明

### 5.5 错误处理

- [ ] HTTP 请求失败 → 转为 MCP Tool 错误返回
- [ ] API Server 不可达 → 友好错误提示
- [ ] 超时处理

### 5.6 端到端测试

- [ ] 启动 API Server + MCP Shim
- [ ] 通过 MCP 协议发送 Tool Call → 验证完整链路
- [ ] 验证所有 18 个 Tool 可正常调用
- [ ] 配置 Claude Code MCP 设置，手动验证连通性

## 交付物

- `cmd/mcp-shim/main.go` 完整 MCP Shim 进程
- 18 个 MCP Tool 全部注册并可调用
- 端到端测试通过（MCP → HTTP → SQLite → 响应）

## 参考文档

- `docs/specs/agent-workflows.md` §3 — MCP Tool 完整清单
- `docs/engineering-design.md` §5.2-5.3 — Tool 参数与返回值定义
- MCP Go SDK 文档
