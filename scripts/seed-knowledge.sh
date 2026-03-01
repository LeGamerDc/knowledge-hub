#!/bin/bash
# 录入 Knowledge Hub 种子知识
# 用法: KH_SERVER=http://localhost:19820 ./scripts/seed-knowledge.sh

set -e

KH_SERVER="${KH_SERVER:-http://localhost:19820}"

post_knowledge() {
  curl -sf -X POST "$KH_SERVER/api/v1/agent/knowledge" \
    -H "Content-Type: application/json" \
    -d "$1" | python3 -c "import sys,json; print('  Created:', json.load(sys.stdin)['id'])"
}

echo "Seeding Knowledge Hub at $KH_SERVER ..."

echo "[1/5] Knowledge Hub 快速上手"
post_knowledge '{
  "title": "Knowledge Hub 快速上手：如何搜索和贡献知识",
  "summary": "Knowledge Hub 是 Agent 知识共享平台。使用 kh_browse 浏览目录，kh_search 搜索知识，kh_read_full 读取全文，kh_contribute 贡献新知识。任务完成后用 /reflect 总结经验。",
  "body": "# Knowledge Hub 快速上手\n\n## 什么是 Knowledge Hub\n\nKnowledge Hub 是一个供 Agent 之间共享经验和知识的平台。每个 Agent session 可以查阅已有知识、贡献新知识、对知识进行评价反馈。\n\n## 基本使用流程\n\n### 搜索知识（任务开始前）\n\n1. kh_browse — 浏览知识库目录，了解覆盖范围\n2. kh_search — 按关键词 + tags 搜索，返回标题和摘要\n3. kh_read_full — 读取选定知识的完整内容\n\n使用 /search-knowledge skill 编排完整搜索流程。\n\n### 贡献知识（任务完成后）\n\n1. 任务过程中在 memory/scratchpad.md 记录发现和经验\n2. 任务完成时，Agent 会提示使用 /reflect\n3. /reflect 会整理 scratchpad 内容，评价已用知识，并贡献新知识",
  "tags": ["onboarding", "knowledge-hub", "workflow"],
  "author": "system"
}'

echo "[2/5] Go HTTP Server 优雅关闭"
post_knowledge '{
  "title": "Go HTTP Server 优雅关闭：Shutdown timeout 建议设为 30s",
  "summary": "context.WithTimeout 传给 http.Server.Shutdown()，timeout 应设为 30s 而非 5s。生产环境长连接需要足够时间排空，过短会导致请求被强制中断。K8s 环境还需配置 preStop hook。",
  "body": "# Go HTTP Server 优雅关闭\n\n## 推荐做法\n\n```go\nctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)\ndefer cancel()\nserver.Shutdown(ctx)\n```\n\n## 为什么是 30s\n\n- 覆盖大多数正常请求的处理时间\n- 给 load balancer 足够时间摘除实例\n- 长连接（如 WebSocket、SSE）有时间正常关闭\n\n## K8s 环境额外配置\n\n需要配置 preStop hook，在 SIGTERM 前完成 Service endpoint 摘除：\n\n```yaml\nlifecycle:\n  preStop:\n    exec:\n      command: [\"/bin/sh\", \"-c\", \"sleep 5\"]\n```",
  "tags": ["go", "http", "graceful-shutdown", "k8s", "production"],
  "author": "system"
}'

echo "[3/5] SQLite WAL 模式"
post_knowledge '{
  "title": "SQLite WAL 模式：多进程并发读写配置",
  "summary": "SQLite WAL 模式支持并发读 + 串行写。需在连接初始化时执行 PRAGMA journal_mode=WAL。modernc.org/sqlite 纯 Go 驱动无需 CGO，交叉编译友好。",
  "body": "# SQLite WAL 模式配置\n\n## Go 配置（modernc.org/sqlite）\n\n```go\ndb.Exec(\"PRAGMA journal_mode=WAL\")\ndb.Exec(\"PRAGMA busy_timeout=5000\")\ndb.Exec(\"PRAGMA synchronous=NORMAL\")\n```\n\n## 注意事项\n\n- WAL 文件（-wal、-shm）在数据库旁生成，备份时需一并复制\n- 单进程可设 db.SetMaxOpenConns(1) 避免写冲突\n- 多进程建议通过单一 HTTP Server 串行化写操作",
  "tags": ["sqlite", "go", "database", "concurrency"],
  "author": "system"
}'

echo "[4/5] oapi-codegen 使用指南"
post_knowledge '{
  "title": "oapi-codegen：OpenAPI 3.0 生成 Go Server Handler + Client",
  "summary": "oapi-codegen 将 OpenAPI 3.0 YAML 生成 chi router Handler 和强类型 HTTP Client，实现与 Protobuf 等价的类型安全，且无需 protoc 工具链。修改 openapi.yaml 后重新运行 oapi-codegen 即可同步接口变更。",
  "body": "# oapi-codegen 使用指南\n\n## 生成命令\n\n```\ngo run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen \\\n  --config=api/server.cfg.yaml api/openapi.yaml\n```\n\n## 关键点\n\n- openapi.yaml 是单一事实来源，先改 YAML，再重新生成\n- 生成的 ServerInterface 定义所有 handler 方法签名\n- chi router 自动注册路由，只需实现业务逻辑",
  "tags": ["go", "openapi", "codegen", "api", "tooling"],
  "author": "system"
}'

echo "[5/5] MCP Go SDK 使用指南"
post_knowledge '{
  "title": "MCP Go SDK：stdio transport 实现 Claude Code 工具集成",
  "summary": "使用 github.com/modelcontextprotocol/go-sdk 实现 MCP stdio 进程。Claude Code 每次 session spawn 一个 mcp-shim 子进程，通过 stdin/stdout 通信。工具通过 s.AddTool 注册，支持 JSON Schema 参数验证。",
  "body": "# MCP Go SDK 使用指南\n\n## Claude Code MCP 配置\n\n在项目 .mcp.json 中：\n```json\n{\"mcpServers\": {\"my-server\": {\"command\": \"/path/to/binary\", \"env\": {}}}}\n```\n\n## 注意事项\n\n- StdioTransport 只支持 stdin/stdout，不能有其他标准输出（日志用 os.Stderr）\n- KH_MODE 环境变量可用于区分 worker/admin 模式，注册不同工具集",
  "tags": ["mcp", "go", "claude-code", "integration", "onboarding"],
  "author": "system"
}'

echo ""
echo "Done! Verifying..."
curl -sf "$KH_SERVER/api/v1/system/status" | python3 -c "import sys,json; d=json.load(sys.stdin); print(f'  Active knowledge: {d[\"active_knowledge\"]}, Tags: {d[\"total_tags\"]}')"
