package corestore

import "time"

// 枚举常量

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

// AppendThreshold 是触发 needs_rewrite 的追加次数阈值。
const AppendThreshold = 5

// FlagWindowDays 是驱逐检测的滑动窗口天数。
const FlagWindowDays = 30

// FlagThreshold 是触发 ListFlagged 的 failure+correction 评论数阈值。
const FlagThreshold = 3

// KnowledgeEntry 代表一条知识条目。
type KnowledgeEntry struct {
	ID           string    `db:"id"`
	Title        string    `db:"title"`
	Summary      string    `db:"summary"`
	Body         string    `db:"body"`
	Author       string    `db:"author"`
	Weight       float64   `db:"weight"`
	Status       int       `db:"status"`
	AccessCount  int       `db:"access_count"`
	AppendCount  int       `db:"append_count"`
	NeedsRewrite bool      `db:"needs_rewrite"`
	CreatedAt    time.Time `db:"created_at"`
	UpdatedAt    time.Time `db:"updated_at"`
	AccessedAt   time.Time `db:"accessed_at"`
	Tags         []string  // 关联 tag 名列表，查询时填充
}

// Comment 代表一条评论。
type Comment struct {
	ID          string     `db:"id"`
	KnowledgeID string     `db:"knowledge_id"`
	Type        int        `db:"type"`
	Content     string     `db:"content"`
	Reasoning   string     `db:"reasoning"`
	Scenario    string     `db:"scenario"`
	Author      string     `db:"author"`
	Processed   bool       `db:"processed"`
	ProcessedAt *time.Time `db:"processed_at"`
	CreatedAt   time.Time  `db:"created_at"`
}

// Tag 代表一个标签。
type Tag struct {
	ID        string   `db:"id"`
	Name      string   `db:"name"`
	Aliases   []string // JSON 数组，查询时解析
	Frequency int      `db:"frequency"`
}

// CurationLog 代表一条整理操作日志。
type CurationLog struct {
	ID          string    `db:"id"`
	Action      int       `db:"action"`
	TargetID    string    `db:"target_id"`
	SourceIDs   []string  // JSON 数组
	Description string    `db:"description"`
	Diff        string    `db:"diff"`
	CreatedAt   time.Time `db:"created_at"`
	AgentID     string    `db:"agent_id"`
}

// ConflictReport 代表一条冲突报告。
type ConflictReport struct {
	ID           string     `db:"id"`
	Type         int        `db:"type"`
	KnowledgeIDs []string   // JSON 数组
	CommentIDs   []string   // JSON 数组
	Description  string     `db:"description"`
	Status       int        `db:"status"`
	Resolution   string     `db:"resolution"`
	CreatedAt    time.Time  `db:"created_at"`
	ResolvedAt   *time.Time `db:"resolved_at"`
}

// SystemStatus 代表系统状态快照。
type SystemStatus struct {
	ActiveCount      int `db:"active_count"`
	ArchivedCount    int `db:"archived_count"`
	TagCount         int `db:"tag_count"`
	UnprocessedCount int `db:"unprocessed_count"`
	OpenConflicts    int `db:"open_conflicts"`
}

// FlaggedEntry 代表需要审核的知识条目。
type FlaggedEntry struct {
	Entry          *KnowledgeEntry
	FailureCount   int
	RecentComments []*Comment
}

// SimilarPair 代表两条 Tag 重叠度高的知识条目对。
type SimilarPair struct {
	EntryA   *KnowledgeEntry
	EntryB   *KnowledgeEntry
	Overlap  float64 // Tag 重叠比例 (0.0-1.0)
	SharedTags []string
}

// FacetResult 是分面检索的返回结果。
type FacetResult struct {
	Entries   []*KnowledgeEntry // 当结果 <= 10 时返回
	NextTags  []FacetTag        // 当结果 > 10 时返回下钻 Tag
	TotalHits int
}

// FacetTag 是下钻 Tag 及其命中数。
type FacetTag struct {
	Name  string
	Count int
}

// SearchQuery 是检索参数。
type SearchQuery struct {
	Q          string   // 全文关键词
	Tags       []string // Tag 过滤
	Status     int      // 0=不过滤, 1=ACTIVE, 2=ARCHIVED
	Limit      int
	Offset     int
	OrderBy    string // "weight", "created_at", "access_count"
	Descending bool
}

// UpdateFields 是管理员重构时可更新的字段集合。
type UpdateFields struct {
	Title   *string
	Summary *string
	Body    *string
	Tags    []string // 非 nil 时完全替换 tag 列表
}

// TagHealthReport 是 Tag 健康检查报告。
type TagHealthReport struct {
	SynonymPairs []SynonymPair // 编辑距离 <= 2 的 Tag 对
	LowFreqTags  []*Tag        // frequency <= 1 的 Tag
	HighFreqTags []*Tag        // frequency >= 20 的 Tag（可能需要拆分）
}

// SynonymPair 是疑似同义的 Tag 对。
type SynonymPair struct {
	TagA     *Tag
	TagB     *Tag
	Distance int // Levenshtein 距离
}
