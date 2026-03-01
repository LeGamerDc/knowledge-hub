package corestore

import (
	"context"
	"fmt"
	"math"
	"time"
)

// weightLambda 是成功评论时间衰减系数（半衰期约 69 天）。
const weightLambda = 0.01

// GetStatus 返回系统状态统计快照。
func (s *store) GetStatus(ctx context.Context) (*SystemStatus, error) {
	status := &SystemStatus{}

	row := s.db.QueryRowContext(ctx, `
		SELECT
			COALESCE(SUM(CASE WHEN status = ? THEN 1 ELSE 0 END), 0) AS active,
			COALESCE(SUM(CASE WHEN status = ? THEN 1 ELSE 0 END), 0) AS archived
		FROM knowledge_entries`,
		KnowledgeStatusActive, KnowledgeStatusArchived,
	)
	if err := row.Scan(&status.ActiveCount, &status.ArchivedCount); err != nil {
		return nil, fmt.Errorf("scan entry counts: %w", err)
	}

	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM tags").
		Scan(&status.TagCount); err != nil {
		return nil, fmt.Errorf("scan tag count: %w", err)
	}

	if err := s.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM comments WHERE processed = 0").
		Scan(&status.UnprocessedCount); err != nil {
		return nil, fmt.Errorf("scan unprocessed count: %w", err)
	}

	if err := s.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM conflict_reports WHERE status = ?", ConflictStatusOpen).
		Scan(&status.OpenConflicts); err != nil {
		return nil, fmt.Errorf("scan open conflicts: %w", err)
	}

	return status, nil
}

// RecalculateWeights 重新计算所有 ACTIVE 知识条目的权重，返回更新条目数。
//
// 权重公式：
//
//	weight = 1.0
//	       + SUM(decay(success.created_at) * 1.0)   -- success 时间衰减加分
//	       - unprocessed_failure_count * 2.0         -- failure 固定扣分
//	       + access_count * 0.1                      -- 访问量微弱加分
//
// 注：Onboarding 提权留给 Service 层通过 tag 判断，此处不实现。
func (s *store) RecalculateWeights(ctx context.Context) (int, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT id, access_count FROM knowledge_entries WHERE status = ?",
		KnowledgeStatusActive,
	)
	if err != nil {
		return 0, fmt.Errorf("query entries: %w", err)
	}
	defer rows.Close()

	type entryInfo struct {
		id          string
		accessCount int
	}
	var entries []entryInfo
	for rows.Next() {
		var e entryInfo
		if err := rows.Scan(&e.id, &e.accessCount); err != nil {
			return 0, err
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}

	now := time.Now().UTC()
	updated := 0

	for _, e := range entries {
		weight, err := s.calcWeight(ctx, e.id, e.accessCount, now)
		if err != nil {
			return updated, err
		}

		if _, err := s.db.ExecContext(ctx,
			"UPDATE knowledge_entries SET weight = ? WHERE id = ?", weight, e.id,
		); err != nil {
			return updated, fmt.Errorf("update weight for %s: %w", e.id, err)
		}
		updated++
	}
	return updated, nil
}

// calcWeight 计算单条知识的权重。
func (s *store) calcWeight(ctx context.Context, entryID string, accessCount int, now time.Time) (float64, error) {
	weight := 1.0
	weight += float64(accessCount) * 0.1

	// 成功评论加分（时间衰减）
	successRows, err := s.db.QueryContext(ctx, `
		SELECT created_at FROM comments
		WHERE knowledge_id = ? AND type = ? AND processed = 0`,
		entryID, CommentTypeSuccess,
	)
	if err != nil {
		return 0, fmt.Errorf("query success comments: %w", err)
	}
	defer successRows.Close()

	for successRows.Next() {
		var createdAt time.Time
		if err := successRows.Scan(&createdAt); err != nil {
			return 0, err
		}
		days := now.Sub(createdAt).Hours() / 24
		decay := math.Exp(-weightLambda * days)
		weight += decay * 1.0
	}
	if err := successRows.Err(); err != nil {
		return 0, err
	}

	// Failure 评论扣分（不衰减）
	var failureCount int
	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM comments
		WHERE knowledge_id = ? AND type = ? AND processed = 0`,
		entryID, CommentTypeFailure,
	).Scan(&failureCount); err != nil {
		return 0, fmt.Errorf("count failure comments: %w", err)
	}
	weight -= float64(failureCount) * 2.0

	return weight, nil
}
