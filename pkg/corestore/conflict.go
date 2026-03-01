package corestore

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/google/uuid"
)

// CreateConflict 创建一条冲突报告。
func (s *store) CreateConflict(ctx context.Context, report *ConflictReport) (string, error) {
	if report.ID == "" {
		report.ID = uuid.NewString()
	}

	knowledgeIDsJSON := toJSON(report.KnowledgeIDs)
	if knowledgeIDsJSON == "null" {
		knowledgeIDsJSON = "[]"
	}
	commentIDsJSON := toJSON(report.CommentIDs)
	if commentIDsJSON == "null" {
		commentIDsJSON = "[]"
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO conflict_reports
			(id, type, knowledge_ids, comment_ids, description, status, created_at)
		VALUES (?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)`,
		report.ID, report.Type, knowledgeIDsJSON, commentIDsJSON,
		report.Description, ConflictStatusOpen,
	)
	if err != nil {
		return "", fmt.Errorf("insert conflict report: %w", err)
	}
	return report.ID, nil
}

// ListConflicts 返回冲突报告列表，status 为空时返回全部。
func (s *store) ListConflicts(ctx context.Context, status string) ([]*ConflictReport, error) {
	var (
		q    string
		args []any
	)

	switch status {
	case "open":
		q = `SELECT id, type, knowledge_ids, comment_ids, description, status, resolution, created_at, resolved_at
		     FROM conflict_reports WHERE status = ? ORDER BY created_at DESC`
		args = []any{ConflictStatusOpen}
	case "resolved":
		q = `SELECT id, type, knowledge_ids, comment_ids, description, status, resolution, created_at, resolved_at
		     FROM conflict_reports WHERE status = ? ORDER BY created_at DESC`
		args = []any{ConflictStatusResolved}
	default:
		q = `SELECT id, type, knowledge_ids, comment_ids, description, status, resolution, created_at, resolved_at
		     FROM conflict_reports ORDER BY created_at DESC`
	}

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("query conflicts: %w", err)
	}
	defer rows.Close()

	var reports []*ConflictReport
	for rows.Next() {
		r := &ConflictReport{}
		var kidJSON, cidJSON string
		var resolution sql.NullString
		var resolvedAt sql.NullTime
		if err := rows.Scan(
			&r.ID, &r.Type, &kidJSON, &cidJSON,
			&r.Description, &r.Status, &resolution,
			&r.CreatedAt, &resolvedAt,
		); err != nil {
			return nil, err
		}
		_ = fromJSON(kidJSON, &r.KnowledgeIDs)
		_ = fromJSON(cidJSON, &r.CommentIDs)
		if resolution.Valid {
			r.Resolution = resolution.String
		}
		if resolvedAt.Valid {
			r.ResolvedAt = &resolvedAt.Time
		}
		reports = append(reports, r)
	}
	return reports, rows.Err()
}

// ResolveConflict 标记冲突为已解决并记录解决说明。
func (s *store) ResolveConflict(ctx context.Context, id string, resolution string) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE conflict_reports
		SET status = ?, resolution = ?, resolved_at = CURRENT_TIMESTAMP
		WHERE id = ?`,
		ConflictStatusResolved, resolution, id,
	)
	if err != nil {
		return fmt.Errorf("resolve conflict: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
