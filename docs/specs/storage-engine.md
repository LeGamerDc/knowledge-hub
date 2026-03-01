# Storage Engine 核心存储库设计

> **包路径**: `pkg/corestore` (独立内库)

本模块作为 Knowledge Hub 的基石，完全独立于外部协议，仅通过 Go Interface 暴露能力。

## 1. 核心接口定义

```go
// Store 是 Knowledge Hub 存储引擎的顶层接口。
// 所有方法均为并发安全（SQLite WAL + Go sync）。
type Store interface {
    KnowledgeStore
    CommentStore
    TagStore
    CurationStore
    ConflictStore
    SystemStore
}

// KnowledgeStore 管理知识条目的完整生命周期。
type KnowledgeStore interface {
    // ---- Read ----
    GetByID(ctx context.Context, id string) (*KnowledgeEntry, error)
    Search(ctx context.Context, query SearchQuery) ([]*KnowledgeEntry, error)
    BrowseFacets(ctx context.Context, selectedTags []string) (*FacetResult, error)
    FindSimilar(ctx context.Context) ([]*SimilarPair, error) // Tag overlap > 80%

    // ---- Write ----
    Create(ctx context.Context, entry *KnowledgeEntry) (string, error)
    Append(ctx context.Context, id string, appendType string, content string) error // Incremental append
    Update(ctx context.Context, id string, fields UpdateFields) error               // Full rewrite (admin only)

    // ---- Lifecycle ----
    Archive(ctx context.Context, id string) error
    Restore(ctx context.Context, id string) error
    HardDelete(ctx context.Context, id string) error

    // ---- Query ----
    ListFlagged(ctx context.Context) ([]*FlaggedEntry, error) // Entries needing review
}

// CommentStore 管理评论的读写与状态标记。
type CommentStore interface {
    AddComment(ctx context.Context, comment *Comment) (string, error)
    GetByKnowledgeID(ctx context.Context, knowledgeID string) ([]*Comment, error)
    GetUnprocessed(ctx context.Context, knowledgeID string) ([]*Comment, error)
    MarkProcessed(ctx context.Context, commentIDs []string) error
}

// TagStore 管理 Tag 的归一化与健康检查。
type TagStore interface {
    GetTagHealth(ctx context.Context) (*TagHealthReport, error) // Synonym pairs, low/high freq
    MergeTags(ctx context.Context, target string, sources []string) (int, error) // Returns affected count
}

// CurationStore 管理整理操作日志。
type CurationStore interface {
    LogCuration(ctx context.Context, log *CurationLog) (string, error)
    ListCurationLogs(ctx context.Context, limit int) ([]*CurationLog, error)
}

// ConflictStore 管理冲突报告。
type ConflictStore interface {
    CreateConflict(ctx context.Context, report *ConflictReport) (string, error)
    ListConflicts(ctx context.Context, status string) ([]*ConflictReport, error)
    ResolveConflict(ctx context.Context, id string, resolution string) error
}

// SystemStore 管理全局系统操作。
type SystemStore interface {
    RecalculateWeights(ctx context.Context) (int, error) // Returns updated count
    GetStatus(ctx context.Context) (*SystemStatus, error)
}
```

## 2. 软硬删除机制 (Soft vs Hard Deletion)

设计哲学：**Agent 只能"藏"数据（软删除），只有人类可以"删"数据（硬删除）。**

### 2.1 状态位枚举
数据库 `knowledge_entries` 表包含 `status` 字段：
- `ACTIVE` (1): 正常可见，Agent 检索的默认范围。
- `ARCHIVED` (2): 软删除状态。在普通 Agent 检索 (`BrowseFacets`, `Search`) 中**默认不可见**。

### 2.2 操作权限分配
1. **工作 Agent**：只能对条目添加评论（success/failure/supplement/correction），无法直接删除。
2. **管理/整理 Agent**：当发现某条目负面权重过高或需要合并时，调用 `Archive()` 将其**软删除**。
3. **人类 (通过 `kh` CLI)**：
   - 通过 `kh list --status archived` 浏览所有已归档条目。
   - 通过 `kh delete <id>` 执行硬删除，彻底从 SQLite 物理移除。
   - 通过 `kh restore <id>` 恢复误归档的条目。

## 3. 动态分面检索算法 (Faceted Browsing)

为了让目录可以随标签动态组合，数据库查询引擎在内部执行以下逻辑：

**当输入已选 Tag `["go", "k8s"]` 时：**
1. **交集过滤**：在 `knowledge_tags` 关联表中，查找同时拥有 "go" 和 "k8s" 且 `status=ACTIVE` 的知识 `entry_id` 集合 (假设有 12 篇)。
2. **频次聚合**：对这 12 篇文章拥有的**其他所有 Tag** 进行 Count 聚合。
3. **返回**：
   - 如果篇数 <= 10，直接返回这些文章的 Meta 信息。
   - 如果篇数 > 10，返回频次最高的 Top N Tags，作为下一层"虚拟目录"供 Agent 继续下钻。

## 4. SQLite Schema

### 4.1 knowledge_entries

```sql
CREATE TABLE knowledge_entries (
    id TEXT PRIMARY KEY,
    title TEXT NOT NULL,
    summary TEXT,
    body TEXT,
    author TEXT,                              -- Contributor identifier
    weight REAL DEFAULT 1.0,
    status INTEGER DEFAULT 1,                 -- 1=ACTIVE, 2=ARCHIVED
    access_count INTEGER DEFAULT 0,           -- Read count (weight factor)
    append_count INTEGER DEFAULT 0,           -- Append count (rewrite threshold trigger)
    needs_rewrite BOOLEAN DEFAULT 0,          -- Flagged when append_count exceeds threshold
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    accessed_at DATETIME DEFAULT CURRENT_TIMESTAMP  -- Last read time (stale detection)
);
```

### 4.2 tags

```sql
CREATE TABLE tags (
    id TEXT PRIMARY KEY,
    name TEXT UNIQUE NOT NULL,
    aliases TEXT DEFAULT '[]',        -- JSON array of alias strings (e.g. ["log", "日志"])
    frequency INTEGER DEFAULT 0      -- Cached usage count, updated on write operations
);
```

### 4.3 knowledge_tags (junction table)

```sql
CREATE TABLE knowledge_tags (
    entry_id TEXT,
    tag_id TEXT,
    PRIMARY KEY (entry_id, tag_id),
    FOREIGN KEY (entry_id) REFERENCES knowledge_entries(id) ON DELETE CASCADE,
    FOREIGN KEY (tag_id) REFERENCES tags(id) ON DELETE CASCADE
);

-- Accelerate faceted browsing queries
CREATE INDEX idx_kt_tag ON knowledge_tags(tag_id);
CREATE INDEX idx_kt_entry ON knowledge_tags(entry_id);
```

### 4.4 comments

```sql
CREATE TABLE comments (
    id TEXT PRIMARY KEY,
    knowledge_id TEXT NOT NULL,
    type INTEGER NOT NULL,           -- 1=success, 2=failure, 3=supplement, 4=correction
    content TEXT NOT NULL,
    reasoning TEXT NOT NULL,          -- WHY this comment was given (evidence, context)
    scenario TEXT,                    -- Task context when knowledge was used
    author TEXT,                      -- Commenter identifier
    processed BOOLEAN DEFAULT 0,     -- Whether curation agent has handled this
    processed_at DATETIME,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (knowledge_id) REFERENCES knowledge_entries(id) ON DELETE CASCADE
);

CREATE INDEX idx_comments_knowledge ON comments(knowledge_id);
CREATE INDEX idx_comments_unprocessed ON comments(processed, knowledge_id);
```

### 4.5 curation_logs

```sql
CREATE TABLE curation_logs (
    id TEXT PRIMARY KEY,
    action INTEGER NOT NULL,         -- 1=merge_supplement, 2=apply_correction, 3=downgrade,
                                     -- 4=archive, 5=merge_tags, 6=merge_knowledge, 7=create_conflict
    target_id TEXT NOT NULL,          -- Knowledge or Tag ID being operated on
    source_ids TEXT DEFAULT '[]',     -- JSON array of comment/knowledge/tag IDs that triggered this
    description TEXT NOT NULL,        -- Agent's explanation of the action
    diff TEXT,                        -- Content diff for body modifications
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    agent_id TEXT                     -- Identifier of the curation agent
);

CREATE INDEX idx_curation_target ON curation_logs(target_id);
```

### 4.6 conflict_reports

```sql
CREATE TABLE conflict_reports (
    id TEXT PRIMARY KEY,
    type INTEGER NOT NULL,           -- 1=correction_conflict, 2=knowledge_conflict
    knowledge_ids TEXT DEFAULT '[]',  -- JSON array of involved knowledge entry IDs
    comment_ids TEXT DEFAULT '[]',    -- JSON array of involved comment IDs
    description TEXT NOT NULL,        -- Agent's analysis of the conflict
    status INTEGER DEFAULT 1,        -- 1=open, 2=resolved
    resolution TEXT,                  -- Resolution description (filled after resolved)
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    resolved_at DATETIME
);

CREATE INDEX idx_conflict_status ON conflict_reports(status);
```

## 5. 权重计算

```
weight = base_weight
       + SUM(decay(success_comment.created_at) * 1.0)  -- success with time decay
       - (unprocessed_failure_comments) * 2.0
       + access_count * 0.1
       + is_onboarding * 10.0

where decay(t) = exp(-lambda * days_since(t))
      lambda = 0.01 (half-life ~69 days, configurable)
```

**规则：**
- Success 评论的加分随时间指数衰减，确保持续被验证的知识保持高权重
- Failure 评论不衰减——失败信号始终保持警示作用，直到被管理员 Agent 处理
- 已处理的评论（`processed=true`）不再影响权重
- 访问量提供微弱的正向信号
- Onboarding 知识获得大幅提权

**驱逐阈值 (Failure Eviction)：**
- 滑动窗口（默认 30 天）内，Failure + Correction 评论达到绝对阈值（默认 3 条）时，由 `ListFlagged` 返回，管理员 Agent 决定处置
- 不走渐进扣分，避免高基础分知识的"马太效应"

## 6. 枚举值定义

```go
// CommentType
const (
    CommentTypeSuccess    = 1
    CommentTypeFailure    = 2
    CommentTypeSupplement = 3
    CommentTypeCorrection = 4
)

// CurationAction
const (
    CurationMergeSupplement = 1
    CurationApplyCorrection = 2
    CurationDowngrade       = 3
    CurationArchive         = 4
    CurationMergeTags       = 5
    CurationMergeKnowledge  = 6
    CurationCreateConflict  = 7
)

// KnowledgeStatus
const (
    KnowledgeStatusActive   = 1
    KnowledgeStatusArchived = 2
)

// ConflictStatus
const (
    ConflictStatusOpen     = 1
    ConflictStatusResolved = 2
)
```
