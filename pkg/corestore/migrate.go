package corestore

import "fmt"

const currentSchemaVersion = 1

const schemaSQL = `
CREATE TABLE IF NOT EXISTS knowledge_entries (
    id TEXT PRIMARY KEY,
    title TEXT NOT NULL,
    summary TEXT,
    body TEXT,
    author TEXT,
    weight REAL DEFAULT 1.0,
    status INTEGER DEFAULT 1,
    access_count INTEGER DEFAULT 0,
    append_count INTEGER DEFAULT 0,
    needs_rewrite BOOLEAN DEFAULT 0,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    accessed_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS tags (
    id TEXT PRIMARY KEY,
    name TEXT UNIQUE NOT NULL,
    aliases TEXT DEFAULT '[]',
    frequency INTEGER DEFAULT 0
);

CREATE TABLE IF NOT EXISTS knowledge_tags (
    entry_id TEXT,
    tag_id TEXT,
    PRIMARY KEY (entry_id, tag_id),
    FOREIGN KEY (entry_id) REFERENCES knowledge_entries(id) ON DELETE CASCADE,
    FOREIGN KEY (tag_id) REFERENCES tags(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_kt_tag ON knowledge_tags(tag_id);
CREATE INDEX IF NOT EXISTS idx_kt_entry ON knowledge_tags(entry_id);

CREATE TABLE IF NOT EXISTS comments (
    id TEXT PRIMARY KEY,
    knowledge_id TEXT NOT NULL,
    type INTEGER NOT NULL,
    content TEXT NOT NULL,
    reasoning TEXT NOT NULL,
    scenario TEXT,
    author TEXT,
    processed BOOLEAN DEFAULT 0,
    processed_at DATETIME,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (knowledge_id) REFERENCES knowledge_entries(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_comments_knowledge ON comments(knowledge_id);
CREATE INDEX IF NOT EXISTS idx_comments_unprocessed ON comments(processed, knowledge_id);

CREATE TABLE IF NOT EXISTS curation_logs (
    id TEXT PRIMARY KEY,
    action INTEGER NOT NULL,
    target_id TEXT NOT NULL,
    source_ids TEXT DEFAULT '[]',
    description TEXT NOT NULL,
    diff TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    agent_id TEXT
);

CREATE INDEX IF NOT EXISTS idx_curation_target ON curation_logs(target_id);

CREATE TABLE IF NOT EXISTS conflict_reports (
    id TEXT PRIMARY KEY,
    type INTEGER NOT NULL,
    knowledge_ids TEXT DEFAULT '[]',
    comment_ids TEXT DEFAULT '[]',
    description TEXT NOT NULL,
    status INTEGER DEFAULT 1,
    resolution TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    resolved_at DATETIME
);

CREATE INDEX IF NOT EXISTS idx_conflict_status ON conflict_reports(status);
`

// migrate 创建表结构并更新 schema 版本。
func (s *store) migrate() error {
	var version int
	row := s.db.QueryRow("PRAGMA user_version")
	if err := row.Scan(&version); err != nil {
		return fmt.Errorf("read user_version: %w", err)
	}

	if version >= currentSchemaVersion {
		return nil
	}

	if _, err := s.db.Exec(schemaSQL); err != nil {
		return fmt.Errorf("create schema: %w", err)
	}

	if _, err := s.db.Exec(fmt.Sprintf("PRAGMA user_version = %d", currentSchemaVersion)); err != nil {
		return fmt.Errorf("set user_version: %w", err)
	}

	return nil
}
