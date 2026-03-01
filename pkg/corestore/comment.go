package corestore

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// AddComment 添加一条评论。
func (s *store) AddComment(ctx context.Context, comment *Comment) (string, error) {
	if comment.ID == "" {
		comment.ID = uuid.NewString()
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO comments
			(id, knowledge_id, type, content, reasoning, scenario, author, processed, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, 0, CURRENT_TIMESTAMP)`,
		comment.ID, comment.KnowledgeID, comment.Type,
		comment.Content, comment.Reasoning, comment.Scenario, comment.Author,
	)
	if err != nil {
		return "", fmt.Errorf("insert comment: %w", err)
	}
	return comment.ID, nil
}

// GetByKnowledgeID 获取某知识条目的所有评论。
func (s *store) GetByKnowledgeID(ctx context.Context, knowledgeID string) ([]*Comment, error) {
	return s.queryComments(ctx,
		"WHERE knowledge_id = ? ORDER BY created_at ASC", knowledgeID)
}

// GetUnprocessed 获取某知识条目的未处理评论。
func (s *store) GetUnprocessed(ctx context.Context, knowledgeID string) ([]*Comment, error) {
	return s.queryComments(ctx,
		"WHERE knowledge_id = ? AND processed = 0 ORDER BY created_at ASC", knowledgeID)
}

// MarkProcessed 批量标记评论为已处理。
func (s *store) MarkProcessed(ctx context.Context, commentIDs []string) error {
	if len(commentIDs) == 0 {
		return nil
	}
	placeholders := strings.Repeat("?,", len(commentIDs))
	placeholders = placeholders[:len(placeholders)-1]

	args := []any{time.Now().UTC().Format("2006-01-02 15:04:05")}
	for _, id := range commentIDs {
		args = append(args, id)
	}

	_, err := s.db.ExecContext(ctx, fmt.Sprintf(
		"UPDATE comments SET processed = 1, processed_at = ? WHERE id IN (%s)", placeholders,
	), args...)
	if err != nil {
		return fmt.Errorf("mark processed: %w", err)
	}
	return nil
}

// queryComments 是 comment 查询的公共辅助方法。
func (s *store) queryComments(ctx context.Context, where string, args ...any) ([]*Comment, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, knowledge_id, type, content, reasoning, scenario, author,
		       processed, processed_at, created_at
		FROM comments `+where, args...)
	if err != nil {
		return nil, fmt.Errorf("query comments: %w", err)
	}
	defer rows.Close()

	var comments []*Comment
	for rows.Next() {
		c := &Comment{}
		var processedAt sql.NullTime
		if err := rows.Scan(
			&c.ID, &c.KnowledgeID, &c.Type, &c.Content, &c.Reasoning,
			&c.Scenario, &c.Author, &c.Processed, &processedAt, &c.CreatedAt,
		); err != nil {
			return nil, err
		}
		if processedAt.Valid {
			c.ProcessedAt = &processedAt.Time
		}
		comments = append(comments, c)
	}
	return comments, rows.Err()
}

