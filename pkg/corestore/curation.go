package corestore

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

// LogCuration 记录一条整理操作日志。
func (s *store) LogCuration(ctx context.Context, log *CurationLog) (string, error) {
	if log.ID == "" {
		log.ID = uuid.NewString()
	}

	sourceIDsJSON := toJSON(log.SourceIDs)
	if sourceIDsJSON == "null" {
		sourceIDsJSON = "[]"
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO curation_logs
			(id, action, target_id, source_ids, description, diff, created_at, agent_id)
		VALUES (?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP, ?)`,
		log.ID, log.Action, log.TargetID, sourceIDsJSON,
		log.Description, log.Diff, log.AgentID,
	)
	if err != nil {
		return "", fmt.Errorf("insert curation log: %w", err)
	}
	return log.ID, nil
}

// ListCurationLogs 返回最近的整理日志（按时间倒序）。
func (s *store) ListCurationLogs(ctx context.Context, limit int) ([]*CurationLog, error) {
	if limit <= 0 {
		limit = 50
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, action, target_id, source_ids, description, diff, created_at, agent_id
		FROM curation_logs
		ORDER BY created_at DESC
		LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("query curation logs: %w", err)
	}
	defer rows.Close()

	var logs []*CurationLog
	for rows.Next() {
		l := &CurationLog{}
		var sourceIDsJSON string
		if err := rows.Scan(
			&l.ID, &l.Action, &l.TargetID, &sourceIDsJSON,
			&l.Description, &l.Diff, &l.CreatedAt, &l.AgentID,
		); err != nil {
			return nil, err
		}
		_ = fromJSON(sourceIDsJSON, &l.SourceIDs)
		logs = append(logs, l)
	}
	return logs, rows.Err()
}
