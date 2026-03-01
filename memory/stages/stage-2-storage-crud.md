# Stage 2: 存储引擎 — Schema + 基础 CRUD

> 目标：实现 `pkg/corestore` 的全部基础读写操作，覆盖 6 张表的完整 CRUD，含单元测试。

## 前置条件

- Stage 1 完成（Go 项目可编译，类型定义已生成）

## 任务清单

### 2.1 SQLite 初始化

- [ ] 创建 `pkg/corestore/store.go`：连接管理、WAL 模式、PRAGMA 配置
- [ ] `New(dbPath string) (Store, error)` 构造函数
- [ ] `Close()` 关闭连接
- [ ] 使用 `modernc.org/sqlite` 纯 Go 驱动

### 2.2 Schema Migration

- [ ] 创建 `pkg/corestore/migrate.go`：建表 + 索引
- [ ] 6 张表（参照 `docs/specs/storage-engine.md` §4）：
  - knowledge_entries
  - tags
  - knowledge_tags (junction)
  - comments
  - curation_logs
  - conflict_reports
- [ ] 所有索引创建
- [ ] 版本管理（简单 user_version PRAGMA 即可）

### 2.3 Model 类型定义

- [ ] 创建 `pkg/corestore/models.go`：Go 结构体 + 枚举常量
- [ ] KnowledgeEntry, Comment, Tag, CurationLog, ConflictReport, SystemStatus
- [ ] FlaggedEntry, SimilarPair, FacetResult, SearchQuery, UpdateFields
- [ ] 枚举常量：CommentType (1-4), CurationAction (1-7), KnowledgeStatus (1-2), ConflictStatus (1-2)

### 2.4 KnowledgeStore 实现

- [ ] `Create(ctx, entry) → (id, error)`：插入知识 + 处理 tags（创建不存在的 tag + 关联 + 更新 frequency）
- [ ] `GetByID(ctx, id) → (*KnowledgeEntry, error)`：查询含 tags
- [ ] `Update(ctx, id, fields) → error`：全量更新（管理员重构用），重置 append_count/needs_rewrite
- [ ] `Append(ctx, id, appendType, content) → error`：追加 Blockquote 格式内容到 body 末尾，更新 append_count，超阈值标记 needs_rewrite
- [ ] `Archive(ctx, id) → error`：status 改为 ARCHIVED
- [ ] `Restore(ctx, id) → error`：status 改为 ACTIVE
- [ ] `HardDelete(ctx, id) → error`：物理删除（级联删除评论、tag 关联等）

### 2.5 CommentStore 实现

- [ ] `AddComment(ctx, comment) → (id, error)`
- [ ] `GetByKnowledgeID(ctx, knowledgeID) → ([]*Comment, error)`
- [ ] `GetUnprocessed(ctx, knowledgeID) → ([]*Comment, error)`
- [ ] `MarkProcessed(ctx, commentIDs) → error`：批量标记 processed=true + processed_at

### 2.6 TagStore 实现

- [ ] Tag 自动创建：贡献知识时，不存在的 tag 自动创建
- [ ] Tag frequency 维护：知识创建/归档/删除时更新引用计数
- [ ] `MergeTags(ctx, target, sources) → (affectedCount, error)`：合并 tag（更新 knowledge_tags 关联 + 别名 + 删除 source tag）

### 2.7 CurationStore + ConflictStore + SystemStore

- [ ] `LogCuration(ctx, log) → (id, error)`
- [ ] `ListCurationLogs(ctx, limit) → ([]*CurationLog, error)`
- [ ] `CreateConflict(ctx, report) → (id, error)`
- [ ] `ListConflicts(ctx, status) → ([]*ConflictReport, error)`
- [ ] `ResolveConflict(ctx, id, resolution) → error`
- [ ] `GetStatus(ctx) → (*SystemStatus, error)`：统计 active/archived 条目数、tag 数、未处理评论数、open 冲突数

### 2.8 单元测试

- [ ] 每个 Store 接口的基础 CRUD 测试
- [ ] Tag 自动创建 + frequency 维护测试
- [ ] MergeTags 级联更新测试
- [ ] Append 阈值触发 needs_rewrite 测试
- [ ] Archive/Restore/HardDelete 生命周期测试
- [ ] 使用内存 SQLite (`:memory:`) 加速测试

## 交付物

- `pkg/corestore/` 完整包：store.go, migrate.go, models.go, knowledge.go, comment.go, tag.go, curation.go, conflict.go, system.go
- 全部单元测试通过
- `go build ./...` 和 `go test ./pkg/corestore/...` 通过

## 参考文档

- `docs/specs/storage-engine.md` — Schema、索引、枚举值定义
- `docs/engineering-design.md` §3 — 数据模型
