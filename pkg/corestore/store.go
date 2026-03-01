package corestore

import (
	"context"
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

// Store 是 Knowledge Hub 存储引擎的顶层接口。
// 所有方法均为并发安全（SQLite WAL + database/sql 连接池）。
type Store interface {
	KnowledgeStore
	CommentStore
	TagStore
	CurationStore
	ConflictStore
	SystemStore
	Close() error
}

// KnowledgeStore 管理知识条目的完整生命周期。
type KnowledgeStore interface {
	GetByID(ctx context.Context, id string) (*KnowledgeEntry, error)
	Search(ctx context.Context, query SearchQuery) ([]*KnowledgeEntry, error)
	BrowseFacets(ctx context.Context, selectedTags []string) (*FacetResult, error)
	FindSimilar(ctx context.Context) ([]*SimilarPair, error)
	Create(ctx context.Context, entry *KnowledgeEntry) (string, error)
	Append(ctx context.Context, id string, appendType string, content string) error
	Update(ctx context.Context, id string, fields UpdateFields) error
	Archive(ctx context.Context, id string) error
	Restore(ctx context.Context, id string) error
	HardDelete(ctx context.Context, id string) error
	ListFlagged(ctx context.Context) ([]*FlaggedEntry, error)
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
	GetTagHealth(ctx context.Context) (*TagHealthReport, error)
	MergeTags(ctx context.Context, target string, sources []string) (int, error)
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
	RecalculateWeights(ctx context.Context) (int, error)
	GetStatus(ctx context.Context) (*SystemStatus, error)
}

// store 是 Store 的具体实现。
type store struct {
	db *sql.DB
}

// New 打开（或创建）SQLite 数据库，配置 WAL 模式，并执行 Schema Migration。
func New(dbPath string) (Store, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	// WAL 模式下允许并发读写
	db.SetMaxOpenConns(1)

	if err := configurePragmas(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("configure pragmas: %w", err)
	}

	s := &store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}

	return s, nil
}

// configurePragmas 设置必要的 SQLite PRAGMA。
func configurePragmas(db *sql.DB) error {
	pragmas := []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA synchronous=NORMAL",
		"PRAGMA foreign_keys=ON",
		"PRAGMA busy_timeout=5000",
		"PRAGMA cache_size=-64000", // 64MB
	}
	for _, p := range pragmas {
		if _, err := db.Exec(p); err != nil {
			return fmt.Errorf("%s: %w", p, err)
		}
	}
	return nil
}

// Close 关闭数据库连接。
func (s *store) Close() error {
	return s.db.Close()
}
