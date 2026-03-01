package corestore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/agnivade/levenshtein"
)

// GetTagHealth 返回 Tag 健康检查报告（同义 Tag 对、低频 Tag、高频 Tag）。
//
// 同义 Tag 检测条件（满足任一即返回）：
//   - 编辑距离 <= 2
//   - 一个 tag 名是另一个的子串（不区分大小写）
//   - Jaccard 共现率 > 80%（两个 tag 频繁同时出现在同一篇知识中）
//   - 别名映射匹配（一个 tag 的 aliases 包含另一个 tag 的名称）
func (s *store) GetTagHealth(ctx context.Context) (*TagHealthReport, error) {
	// Step 1: 加载所有 tag（关闭后再发起后续查询）
	rows, err := s.db.QueryContext(ctx,
		"SELECT id, name, aliases, frequency FROM tags ORDER BY name")
	if err != nil {
		return nil, fmt.Errorf("query tags: %w", err)
	}

	var allTags []*Tag
	for rows.Next() {
		t := &Tag{}
		var aliasesJSON string
		if err := rows.Scan(&t.ID, &t.Name, &aliasesJSON, &t.Frequency); err != nil {
			rows.Close()
			return nil, err
		}
		_ = json.Unmarshal([]byte(aliasesJSON), &t.Aliases)
		allTags = append(allTags, t)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Step 2: 查询 ACTIVE 条目总数（用于高频阈值计算）
	var totalEntries int
	if err := s.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM knowledge_entries WHERE status = ?", KnowledgeStatusActive,
	).Scan(&totalEntries); err != nil {
		return nil, fmt.Errorf("count active entries: %w", err)
	}

	report := &TagHealthReport{}
	tagByName := make(map[string]*Tag, len(allTags))

	for _, t := range allTags {
		tagByName[t.Name] = t
		if t.Frequency < 3 {
			report.LowFreqTags = append(report.LowFreqTags, t)
		}
		if totalEntries > 0 && float64(t.Frequency)/float64(totalEntries) > 0.3 {
			report.HighFreqTags = append(report.HighFreqTags, t)
		}
	}

	// Step 3: 构建 entry -> tag 名集合（用于共现率计算）
	entryTagRows, err := s.db.QueryContext(ctx, `
		SELECT kt.entry_id, t.name
		FROM knowledge_tags kt
		JOIN tags t ON t.id = kt.tag_id
		JOIN knowledge_entries e ON e.id = kt.entry_id
		WHERE e.status = ?`, KnowledgeStatusActive)
	if err != nil {
		return nil, fmt.Errorf("query entry tags: %w", err)
	}

	tagEntrySet := make(map[string]map[string]bool) // tagName -> set of entryIDs
	for entryTagRows.Next() {
		var entryID, tagName string
		if err := entryTagRows.Scan(&entryID, &tagName); err != nil {
			entryTagRows.Close()
			return nil, err
		}
		if tagEntrySet[tagName] == nil {
			tagEntrySet[tagName] = make(map[string]bool)
		}
		tagEntrySet[tagName][entryID] = true
	}
	entryTagRows.Close()
	if err := entryTagRows.Err(); err != nil {
		return nil, err
	}

	// Step 4: 同义检测（用 map 去重，确保每对只出现一次）
	type pairKey struct{ a, b string } // a < b 字典序
	seen := make(map[pairKey]bool)

	addPair := func(ta, tb *Tag, dist int) {
		ka, kb := ta.Name, tb.Name
		if ka > kb {
			ka, kb = kb, ka
		}
		k := pairKey{ka, kb}
		if !seen[k] {
			seen[k] = true
			report.SynonymPairs = append(report.SynonymPairs, SynonymPair{
				TagA:     ta,
				TagB:     tb,
				Distance: dist,
			})
		}
	}

	// 4a: 编辑距离 <= 2
	for i := 0; i < len(allTags); i++ {
		for j := i + 1; j < len(allTags); j++ {
			dist := levenshtein.ComputeDistance(allTags[i].Name, allTags[j].Name)
			if dist >= 1 && dist <= 2 {
				addPair(allTags[i], allTags[j], dist)
			}
		}
	}

	// 4b: 子串检测（一个名称包含另一个，不区分大小写）
	for i := 0; i < len(allTags); i++ {
		for j := i + 1; j < len(allTags); j++ {
			a, b := allTags[i], allTags[j]
			la, lb := strings.ToLower(a.Name), strings.ToLower(b.Name)
			if la != lb && (strings.Contains(lb, la) || strings.Contains(la, lb)) {
				addPair(a, b, 0)
			}
		}
	}

	// 4c: 别名映射匹配
	for i := 0; i < len(allTags); i++ {
		for j := i + 1; j < len(allTags); j++ {
			a, b := allTags[i], allTags[j]
			for _, alias := range a.Aliases {
				if strings.EqualFold(alias, b.Name) {
					addPair(a, b, 0)
					break
				}
			}
			for _, alias := range b.Aliases {
				if strings.EqualFold(alias, a.Name) {
					addPair(a, b, 0)
					break
				}
			}
		}
	}

	// 4d: Jaccard 共现率 > 80%
	tagNames := make([]string, 0, len(tagEntrySet))
	for name := range tagEntrySet {
		tagNames = append(tagNames, name)
	}
	sort.Strings(tagNames)

	for i := 0; i < len(tagNames); i++ {
		for j := i + 1; j < len(tagNames); j++ {
			ta, tb := tagNames[i], tagNames[j]
			setA, setB := tagEntrySet[ta], tagEntrySet[tb]

			inter := 0
			for entryID := range setA {
				if setB[entryID] {
					inter++
				}
			}
			if inter == 0 {
				continue
			}

			union := len(setA) + len(setB) - inter
			if union > 0 && float64(inter)/float64(union) > 0.8 {
				tagA, tagB := tagByName[ta], tagByName[tb]
				if tagA != nil && tagB != nil {
					addPair(tagA, tagB, 0)
				}
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
