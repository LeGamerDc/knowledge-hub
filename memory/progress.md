# 开发进度

## 当前阶段

Stage 6: CLI 工具（待开始）

## 阶段规划

共 7 个阶段，每个阶段设计为一次独立开发 session 可完成的工作量。

| Stage | 名称 | 状态 | 核心交付物 |
|-------|------|------|-----------|
| 1 | 项目初始化 + OpenAPI + 代码生成 | `done` | go.mod, openapi.yaml, 生成代码, Makefile |
| 2 | 存储引擎 — Schema + 基础 CRUD | `done` | pkg/corestore 全部 CRUD + 单元测试 |
| 3 | 存储引擎 — 检索与算法层 | `done` | Search, BrowseFacets, Weight, Flagging, Similar, TagHealth |
| 4 | Service 层 + API Server | `done` | internal/server/, cmd/kh-server, API 集成测试 |
| 5 | MCP Shim | `done` | cmd/mcp-shim, 18 个 MCP Tool, 端到端链路 |
| 6 | CLI 工具 | `pending` | cmd/kh, 8 个命令 |
| 7 | Agent 集成 + 端到端验收 | `pending` | Rules, Skills, MCP 配置, 5 个验收场景 |

## 依赖关系

```
Stage 1 (项目骨架)
  ├──> Stage 2 (存储 CRUD)
  │      └──> Stage 3 (存储算法)
  │             └──> Stage 4 (Service + API Server)
  │                    ├──> Stage 5 (MCP Shim)
  │                    └──> Stage 6 (CLI)
  │                           └──> Stage 7 (Agent 集成 + 验收)
  └──────────────────────────────────────────┘
           (Stage 1 的生成代码被 4/5/6 直接使用)
```

## 阶段详情

详见 `memory/stages/stage-{N}-*.md`

## 变更日志

- [2026-03-01] Stage 1 完成：项目骨架 + OpenAPI 26 端点 + 代码生成
  - go.mod + 目录结构 + 关键依赖
  - api/openapi.yaml: 3 域 26 端点（Agent 6 + Admin 11 + System 9）+ 30+ Schema
  - oapi-codegen 生成 ServerInterface (26 methods) + 强类型 Client
  - StubService (501) + cmd/kh-server/main.go + Makefile
  - go build / go vet 全通过
- [2026-03-01] Stage 2 完成：pkg/corestore 存储引擎 CRUD
  - store.go + migrate.go：SQLite WAL 连接管理 + 6 张表 Schema
  - models.go：全部 Go 结构体 + 枚举常量
  - knowledge.go：KnowledgeStore（Create/GetByID/Update/Append/Archive/Restore/HardDelete/Search/BrowseFacets/FindSimilar/ListFlagged）
  - comment.go：CommentStore（AddComment/GetByKnowledgeID/GetUnprocessed/MarkProcessed）
  - tag.go：TagStore（GetTagHealth/MergeTags），Levenshtein 同义 Tag 检测
  - curation.go：CurationStore（LogCuration/ListCurationLogs）
  - conflict.go：ConflictStore（CreateConflict/ListConflicts/ResolveConflict）
  - system.go：SystemStore（GetStatus/RecalculateWeights 指数衰减权重计算）
  - store_test.go：21 个单元测试全部通过（:memory: SQLite）
  - 关键 Bug 修复：scanEntries 嵌套查询死锁（先收集行再批量加载 tags）
- [2026-03-01] Stage 3 完成：存储引擎算法层
  - ListFlagged 重构为多条件检测（needs_rewrite / stale_access / has_unprocessed_comments / high_failure_rate / failure_eviction）
  - FlaggedEntry 增加 FlagReasons []string 字段
  - GetTagHealth 增加子串检测、Jaccard 共现率 >80% 检测、别名映射匹配，并更新频率阈值（LowFreq: <3, HighFreq: >30%）
  - 新增 6 个集成测试：TestListFlagged_{FailureEviction,NeedsRewrite,HighFailureRate}、TestFindSimilar、TestGetTagHealth_{Substring,CoOccurrence}、TestBrowseFacets_LargeResult
  - 全量 26 个测试通过
- [2026-03-01] Stage 4 完成：Service 层 + API Server
  - internal/server/service/: KHService 实现全部 26 个 ServerInterface 方法
    - service.go: 结构体 + writeJSON/writeError/decode 辅助
    - convert.go: corestore ↔ handlers 全量 DTO 转换
    - agent.go: 6 个 Agent 域接口（Browse/Search/ReadFull/Contribute/Append/Comment）
    - admin.go: 11 个 Admin 域接口（Flagged/TagHealth/Similar/Review/Update/Archive/MarkProcessed/MergeTags/MergeKnowledge/Conflict/CurationLog）
    - system.go: 9 个 System 域接口（Status/List/Get/Delete/Restore/Conflicts/Resolve/CurationLogs/Recalculate）
  - cmd/kh-server/main.go: 真实 SQLite 初始化 + 默认端口 :19820 + graceful shutdown
  - 修复 corestore.Search 的 tag 参数顺序 bug（JOIN args 必须在 WHERE args 之前）
  - 修复 corestore.Search status=0 不过滤 bug
  - service_test.go: 8 个集成测试全部通过（httptest + :memory: SQLite）
- [2026-03-01] Stage 5 完成：MCP Shim
  - cmd/mcp-shim/main.go: 完整 MCP stdio 进程，18 个 Tool 全部注册
  - 6 个工作 Agent Tool（kh_browse/search/read_full/contribute/append_knowledge/comment）
  - 12 个管理 Agent Tool（list_flagged/tag_health/find_similar/get_review/update_knowledge/archive/mark_processed/merge_tags/merge_knowledge/create_conflict/log_curation/recalculate_weights）
  - 依赖：github.com/modelcontextprotocol/go-sdk v1.4.0，StdioTransport
  - 端到端测试通过：MCP Tool Call → HTTP → SQLite → 响应 完整链路验证
- [2026-03-01] 初始化开发计划，拆分为 7 个阶段
  - 将原 engineering-design.md 的 5 Phase 细化为 7 Stage
  - 主要变化：原 Phase 1（基础框架 + HTTP API）拆为 Stage 1-4，按关注点分离
  - 原 Phase 4（知识整理）的存储层算法前置到 Stage 3，与基础存储紧邻
  - 原 Phase 5（Faceted 浏览 + 权重）同样前置到 Stage 3，因为 API Server 依赖这些实现
