package corestore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/agnivade/levenshtein"
)

// GetTagHealth 返回 Tag 健康检查报告（同义 Tag 对、低频 Tag、高频 Tag）。
func (s *store) GetTagHealth(ctx context.Context) (*TagHealthReport, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT id, name, aliases, frequency FROM tags ORDER BY name")
	if err != nil {
		return nil, fmt.Errorf("query tags: %w", err)
	}
	defer rows.Close()

	var allTags []*Tag
	for rows.Next() {
		t := &Tag{}
		var aliasesJSON string
		if err := rows.Scan(&t.ID, &t.Name, &aliasesJSON, &t.Frequency); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(aliasesJSON), &t.Aliases)
		allTags = append(allTags, t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	report := &TagHealthReport{}

	for _, t := range allTags {
		if t.Frequency <= 1 {
			report.LowFreqTags = append(report.LowFreqTags, t)
		}
		if t.Frequency >= 20 {
			report.HighFreqTags = append(report.HighFreqTags, t)
		}
	}

	// 检测编辑距离 <= 2 的疑似同义 Tag 对
	for i := 0; i < len(allTags); i++ {
		for j := i + 1; j < len(allTags); j++ {
			dist := levenshtein.ComputeDistance(allTags[i].Name, allTags[j].Name)
			if dist <= 2 && dist > 0 {
				report.SynonymPairs = append(report.SynonymPairs, SynonymPair{
					TagA:     allTags[i],
					TagB:     allTags[j],
					Distance: dist,
				})
			}
		}
	}

	return report, nil
}

// MergeTags 将 sources Tag 合并到 target Tag：
// 1. 将 knowledge_tags 中指向 source 的记录改为 target（处理冲突）
// 2. 将 source name 加入 target 的 aliases
// 3. 删除 source Tag
// 返回受影响的 knowledge_entry 数量。
func (s *store) MergeTags(ctx context.Context, target string, sources []string) (int, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	// 查找 target tag ID
	var targetID string
	var targetAliasesJSON string
	err = tx.QueryRowContext(ctx, "SELECT id, aliases FROM tags WHERE name = ?", target).
		Scan(&targetID, &targetAliasesJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("target tag %q not found", target)
	}
	if err != nil {
		return 0, fmt.Errorf("query target tag: %w", err)
	}

	var targetAliases []string
	_ = json.Unmarshal([]byte(targetAliasesJSON), &targetAliases)

	totalAffected := 0

	for _, sourceName := range sources {
		var sourceID string
		err = tx.QueryRowContext(ctx, "SELECT id FROM tags WHERE name = ?", sourceName).Scan(&sourceID)
		if errors.Is(err, sql.ErrNoRows) {
			continue // source 不存在，跳过
		}
		if err != nil {
			return 0, fmt.Errorf("query source tag %q: %w", sourceName, err)
		}
		if sourceID == targetID {
			continue
		}

		// 找出 source 关联的所有 entry_id
		eRows, err := tx.QueryContext(ctx,
			"SELECT entry_id FROM knowledge_tags WHERE tag_id = ?", sourceID)
		if err != nil {
			return 0, fmt.Errorf("query source entries: %w", err)
		}
		var entryIDs []string
		for eRows.Next() {
			var eid string
			if err := eRows.Scan(&eid); err != nil {
				eRows.Close()
				return 0, err
			}
			entryIDs = append(entryIDs, eid)
		}
		eRows.Close()
		if err := eRows.Err(); err != nil {
			return 0, err
		}

		for _, eid := range entryIDs {
			// 如果 target 已关联该 entry，直接删除 source 关联
			var exists int
			_ = tx.QueryRowContext(ctx,
				"SELECT COUNT(*) FROM knowledge_tags WHERE entry_id = ? AND tag_id = ?",
				eid, targetID,
			).Scan(&exists)

			if exists > 0 {
				_, err = tx.ExecContext(ctx,
					"DELETE FROM knowledge_tags WHERE entry_id = ? AND tag_id = ?",
					eid, sourceID)
			} else {
				// 将 source 关联改为 target
				_, err = tx.ExecContext(ctx,
					"UPDATE knowledge_tags SET tag_id = ? WHERE entry_id = ? AND tag_id = ?",
					targetID, eid, sourceID)
			}
			if err != nil {
				return 0, fmt.Errorf("relink entry %s: %w", eid, err)
			}
			totalAffected++
		}

		// 将 source name 加入 target aliases
		targetAliases = appendUnique(targetAliases, sourceName)

		// 删除 source tag（knowledge_tags 已被处理，frequency 置零前先清除）
		if _, err := tx.ExecContext(ctx, "DELETE FROM tags WHERE id = ?", sourceID); err != nil {
			return 0, fmt.Errorf("delete source tag %q: %w", sourceName, err)
		}
	}

	// 更新 target aliases 和 frequency
	aliasesJSON, _ := json.Marshal(targetAliases)
	if _, err := tx.ExecContext(ctx,
		`UPDATE tags SET aliases = ?,
		       frequency = (SELECT COUNT(*) FROM knowledge_tags WHERE tag_id = ?)
		 WHERE id = ?`,
		string(aliasesJSON), targetID, targetID,
	); err != nil {
		return 0, fmt.Errorf("update target tag: %w", err)
	}

	return totalAffected, tx.Commit()
}

func appendUnique(slice []string, item string) []string {
	for _, s := range slice {
		if s == item {
			return slice
		}
	}
	return append(slice, item)
}
