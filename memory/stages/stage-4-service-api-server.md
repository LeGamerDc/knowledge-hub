# Stage 4: Service 层 + API Server

> 目标：实现业务逻辑编排层，绑定 oapi-codegen 生成的 Server Interface，启动可运行的 HTTP API Server。

## 前置条件

- Stage 1 完成（生成的 Server Interface + Types 可用）
- Stage 3 完成（存储引擎全部实现）

## 任务清单

### 4.1 Service 层接口定义

- [ ] 创建 `internal/server/service/service.go`
- [ ] 定义 Service struct（持有 corestore.Store 引用）
- [ ] DTO ↔ Model 转换函数（oapi-codegen 生成的类型 ↔ corestore models）

### 4.2 Agent 域 Service 实现

- [ ] Browse：转发到 BrowseFacets
- [ ] Search：转发到 Search
- [ ] ReadFull：GetByID + 更新 accessed_at/access_count + 附加评论概要（success/failure/supplement 各多少条）
- [ ] Contribute：创建知识（含 tag 处理）
- [ ] AppendKnowledge：追加内容
- [ ] AddComment：添加评论

### 4.3 Admin 域 Service 实现

- [ ] ListFlagged：转发到 ListFlagged
- [ ] TagHealth：转发到 GetTagHealth
- [ ] FindSimilar：转发到 FindSimilar
- [ ] GetReview：GetByID + GetUnprocessed 组合
- [ ] UpdateKnowledge：全量更新（重置 append_count）
- [ ] Archive：归档
- [ ] MarkProcessed：批量标记
- [ ] MergeTags：合并 tag
- [ ] MergeKnowledge：合并知识（target 更新 + sources 归档 + 评论迁移）
- [ ] CreateConflict：创建冲突报告
- [ ] LogCuration：写入整理日志

### 4.4 System 域 Service 实现

- [ ] RecalculateWeights：触发权重重算
- [ ] GetStatus：系统状态概览
- [ ] ListKnowledge：列出知识条目（支持 status/tag 过滤）
- [ ] ReadKnowledge：读取全文（不更新 access 计数，区别于 Agent 域）
- [ ] HardDelete：硬删除（仅允许 ARCHIVED 状态）
- [ ] Restore：恢复归档
- [ ] ListConflicts：列出冲突报告
- [ ] ResolveConflict：解决冲突
- [ ] ListCurationLogs：查看整理日志

### 4.5 实现生成的 Server Interface

- [ ] 创建 `internal/server/handlers/impl.go`（或在 handlers 包内实现）
- [ ] 实现 oapi-codegen 生成的 ServerInterface 的所有方法
- [ ] 每个 handler：解析请求参数 → 调用 Service → 序列化响应 / 错误处理
- [ ] 统一错误响应格式：`{ "code": "NOT_FOUND", "message": "..." }`

### 4.6 API Server main.go

- [ ] 创建 `cmd/kh-server/main.go`
- [ ] 初始化 SQLite 存储（数据目录配置，默认 `~/.knowledge-hub/data.db`）
- [ ] 初始化 Service 层
- [ ] 创建 chi Router + 绑定生成的 Handler
- [ ] 端口配置（默认 `:19820`，可通过环境变量或 flag 配置）
- [ ] Graceful shutdown（signal handling）
- [ ] 启动日志

### 4.7 API 集成测试

- [ ] 使用 `httptest.NewServer` 测试完整 HTTP 链路
- [ ] Agent 域：browse → search → read_full → contribute → append → comment
- [ ] Admin 域：list_flagged → get_review → update → archive → merge_tags
- [ ] System 域：status → list → delete → restore
- [ ] 错误场景：404, 400 参数校验, 删除不存在的条目

## 交付物

- `internal/server/service/` 完整 Service 层
- `internal/server/handlers/impl.go` Server Interface 实现
- `cmd/kh-server/main.go` 可启动的 API Server
- API 集成测试通过
- `curl` 可正常调用所有端点

## 参考文档

- `docs/specs/api-protocol.md` — 端点详细定义（Request/Response 格式）
- `docs/specs/architecture-overview.md` §2.3 — Server 内部架构
