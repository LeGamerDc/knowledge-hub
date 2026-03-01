package corestore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ErrNotFound 表示记录不存在。
var ErrNotFound = errors.New("not found")

// Create 插入新知识条目，自动创建不存在的 Tag 并建立关联。
func (s *store) Create(ctx context.Context, entry *KnowledgeEntry) (string, error) {
	if entry.ID == "" {
		entry.ID = uuid.NewString()
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx, `
		INSERT INTO knowledge_entries
			(id, title, summary, body, author, weight, status, created_at, updated_at, accessed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`,
		entry.ID, entry.Title, entry.Summary, entry.Body, entry.Author,
		entry.Weight, KnowledgeStatusActive,
	)
	if err != nil {
		return "", fmt.Errorf("insert entry: %w", err)
	}

	if err := s.upsertTagsAndLink(ctx, tx, entry.ID, entry.Tags, true); err != nil {
		return "", err
	}

	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("commit: %w", err)
	}
	return entry.ID, nil
}

// GetByID 查询单条知识及其 Tags。
func (s *store) GetByID(ctx context.Context, id string) (*KnowledgeEntry, error) {
	entry := &KnowledgeEntry{}
	err := s.db.QueryRowContext(ctx, `
		SELECT id, title, summary, body, author, weight, status,
		       access_count, append_count, needs_rewrite,
		       created_at, updated_at, accessed_at
		FROM knowledge_entries WHERE id = ?`, id).Scan(
		&entry.ID, &entry.Title, &entry.Summary, &entry.Body, &entry.Author,
		&entry.Weight, &entry.Status, &entry.AccessCount, &entry.AppendCount,
		&entry.NeedsRewrite, &entry.CreatedAt, &entry.UpdatedAt, &entry.AccessedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("query entry: %w", err)
	}

	tags, err := s.loadTags(ctx, s.db, id)
	if err != nil {
		return nil, err
	}
	entry.Tags = tags

	// 更新 access_count 和 accessed_at
	_, _ = s.db.ExecContext(ctx,
		"UPDATE knowledge_entries SET access_count = access_count + 1, accessed_at = CURRENT_TIMESTAMP WHERE id = ?",
		id,
	)

	return entry, nil
}

// Update 全量更新知识条目（管理员重构用），重置 append_count 和 needs_rewrite。
func (s *store) Update(ctx context.Context, id string, fields UpdateFields) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	sets := []string{"updated_at = CURRENT_TIMESTAMP", "append_count = 0", "needs_rewrite = 0"}
	args := []any{}

	if fields.Title != nil {
		sets = append(sets, "title = ?")
		args = append(args, *fields.Title)
	}
	if fields.Summary != nil {
		sets = append(sets, "summary = ?")
		args = append(args, *fields.Summary)
	}
	if fields.Body != nil {
		sets = append(sets, "body = ?")
		args = append(args, *fields.Body)
	}

	args = append(args, id)
	q := fmt.Sprintf("UPDATE knowledge_entries SET %s WHERE id = ?", strings.Join(sets, ", "))
	res, err := tx.ExecContext(ctx, q, args...)
	if err != nil {
		return fmt.Errorf("update entry: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}

	if fields.Tags != nil {
		// 先清除旧关联（频次要还原）
		if err := s.unlinkAllTags(ctx, tx, id); err != nil {
			return err
		}
		// 建立新关联
		if err := s.upsertTagsAndLink(ctx, tx, id, fields.Tags, true); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// Append 追加内容到 body 末尾，格式化为 Blockquote，超阈值时标记 needs_rewrite。
func (s *store) Append(ctx context.Context, id string, appendType string, content string) error {
	var currentBody string
	var appendCount int
	err := s.db.QueryRowContext(ctx,
		"SELECT body, append_count FROM knowledge_entries WHERE id = ?", id,
	).Scan(&currentBody, &appendCount)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("query entry: %w", err)
	}

	// Blockquote 格式追加
	now := time.Now().UTC().Format("2006-01-02")
	appended := fmt.Sprintf("\n\n> **[%s - %s]**\n> %s",
		strings.ToUpper(appendType), now,
		strings.ReplaceAll(content, "\n", "\n> "),
	)
	newBody := currentBody + appended
	newCount := appendCount + 1
	needsRewrite := newCount >= AppendThreshold

	_, err = s.db.ExecContext(ctx, `
		UPDATE knowledge_entries
		SET body = ?, append_count = ?, needs_rewrite = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?`,
		newBody, newCount, needsRewrite, id,
	)
	return err
}

// Archive 将知识条目设为 ARCHIVED 状态。
func (s *store) Archive(ctx context.Context, id string) error {
	return s.setStatus(ctx, id, KnowledgeStatusArchived)
}

// Restore 将知识条目恢复为 ACTIVE 状态。
func (s *store) Restore(ctx context.Context, id string) error {
	return s.setStatus(ctx, id, KnowledgeStatusActive)
}

// HardDelete 物理删除知识条目（级联删除评论、tag 关联）。
func (s *store) HardDelete(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	// 先还原 tag frequency
	if err := s.unlinkAllTags(ctx, tx, id); err != nil {
		return err
	}

	res, err := tx.ExecContext(ctx, "DELETE FROM knowledge_entries WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete entry: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}

	return tx.Commit()
}

// Search 根据关键词和 Tag 过滤检索知识条目。
func (s *store) Search(ctx context.Context, q SearchQuery) ([]*KnowledgeEntry, error) {
	where := []string{"1=1"}
	var whereArgs []any

	if q.Status != 0 {
		where = append(where, "e.status = ?")
		whereArgs = append(whereArgs, q.Status)
	}

	if q.Q != "" {
		where = append(where, "(e.title LIKE ? OR e.summary LIKE ? OR e.body LIKE ?)")
		like := "%" + q.Q + "%"
		whereArgs = append(whereArgs, like, like, like)
	}

	// Tag JOIN args must be ordered before WHERE args because JOINs appear first in SQL.
	var tagArgs []any
	tagJoin := s.buildTagJoin(q.Tags, &tagArgs)

	baseQ := fmt.Sprintf(`
		SELECT DISTINCT e.id, e.title, e.summary, e.body, e.author, e.weight, e.status,
		       e.access_count, e.append_count, e.needs_rewrite,
		       e.created_at, e.updated_at, e.accessed_at
		FROM knowledge_entries e %s
		WHERE %s
		ORDER BY e.%s %s
		LIMIT ? OFFSET ?`,
		tagJoin,
		strings.Join(where, " AND "),
		s.safeOrderBy(q.OrderBy),
		s.orderDir(q.Descending),
	)

	limit := q.Limit
	if limit <= 0 {
		limit = 20
	}
	args := append(tagArgs, whereArgs...)
	args = append(args, limit, q.Offset)

	rows, err := s.db.QueryContext(ctx, baseQ, args...)
	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}
	defer rows.Close()

	return s.scanEntries(ctx, rows)
}

// BrowseFacets 分面检索：已选 Tag 取交集，返回文章列表或下钻 Tag。
func (s *store) BrowseFacets(ctx context.Context, selectedTags []string) (*FacetResult, error) {
	// 获取满足所有选中 Tag 的 entry_id 集合
	entryIDs, err := s.entryIDsForTags(ctx, selectedTags)
	if err != nil {
		return nil, err
	}

	result := &FacetResult{TotalHits: len(entryIDs)}

	if len(entryIDs) == 0 {
		return result, nil
	}

	// 聚合其他 Tag（始终返回，由 Agent 决定何时切换到 kh_search）
	placeholders := strings.Repeat("?,", len(entryIDs))
	placeholders = placeholders[:len(placeholders)-1]
	selectedPlaceholders := ""
	selectedArgs := []any{}
	for _, t := range selectedTags {
		selectedPlaceholders += "?,"
		selectedArgs = append(selectedArgs, t)
	}
	if len(selectedArgs) > 0 {
		selectedPlaceholders = selectedPlaceholders[:len(selectedPlaceholders)-1]
	}

	args := make([]any, len(entryIDs))
	for i, id := range entryIDs {
		args[i] = id
	}
	args = append(args, selectedArgs...)

	excludeClause := ""
	if len(selectedTags) > 0 {
		excludeClause = fmt.Sprintf("AND t.name NOT IN (%s)", selectedPlaceholders)
	}

	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT t.name, COUNT(*) as cnt
		FROM knowledge_tags kt
		JOIN tags t ON t.id = kt.tag_id
		WHERE kt.entry_id IN (%s) %s
		GROUP BY t.name
		ORDER BY cnt DESC
		LIMIT 10`, placeholders, excludeClause),
		args...,
	)
	if err != nil {
		return nil, fmt.Errorf("aggregate tags: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var ft FacetTag
		if err := rows.Scan(&ft.Name, &ft.Count); err != nil {
			return nil, err
		}
		result.NextTags = append(result.NextTags, ft)
	}
	return result, rows.Err()
}

// FindSimilar 返回 Tag 重叠率 > 80% 的知识对。
func (s *store) FindSimilar(ctx context.Context) ([]*SimilarPair, error) {
	// 获取所有 ACTIVE 条目的 tag 集合
	rows, err := s.db.QueryContext(ctx, `
		SELECT kt.entry_id, t.name
		FROM knowledge_tags kt
		JOIN tags t ON t.id = kt.tag_id
		JOIN knowledge_entries e ON e.id = kt.entry_id
		WHERE e.status = ?
		ORDER BY kt.entry_id`, KnowledgeStatusActive)
	if err != nil {
		return nil, fmt.Errorf("query tags: %w", err)
	}
	defer rows.Close()

	// 构建 entry -> tag 集合映射
	entryTags := map[string][]string{}
	for rows.Next() {
		var entryID, tagName string
		if err := rows.Scan(&entryID, &tagName); err != nil {
			return nil, err
		}
		entryTags[entryID] = append(entryTags[entryID], tagName)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// 获取所有 entry 元数据
	ids := make([]string, 0, len(entryTags))
	for id := range entryTags {
		ids = append(ids, id)
	}
	entries, err := s.fetchEntriesByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	entryMap := map[string]*KnowledgeEntry{}
	for _, e := range entries {
		entryMap[e.ID] = e
	}

	var pairs []*SimilarPair
	entryList := entries
	for i := 0; i < len(entryList); i++ {
		for j := i + 1; j < len(entryList); j++ {
			a, b := entryList[i], entryList[j]
			tagsA := setOf(entryTags[a.ID])
			tagsB := setOf(entryTags[b.ID])
			shared := intersection(tagsA, tagsB)
			if len(tagsA) == 0 && len(tagsB) == 0 {
				continue
			}
			union := len(tagsA) + len(tagsB) - len(shared)
			if union == 0 {
				continue
			}
			overlap := float64(len(shared)) / float64(union)
			if overlap > 0.8 {
				sharedNames := make([]string, 0, len(shared))
				for t := range shared {
					sharedNames = append(sharedNames, t)
				}
				pairs = append(pairs, &SimilarPair{
					EntryA:     a,
					EntryB:     b,
					Overlap:    overlap,
					SharedTags: sharedNames,
				})
			}
		}
	}
	return pairs, nil
}

// ListFlagged 返回满足任意 flag 条件的知识条目。
//
// flag 条件（任一满足即返回）：
//   - needs_rewrite = true（追加次数超阈值）
//   - 30+ 天未被访问（stale_access）
//   - 有未处理评论（has_unprocessed_comments）
//   - failure 评论占未处理评论比例 > 50%（high_failure_rate）
//   - 滑动窗口内 failure+correction 未处理评论 >= FlagThreshold（failure_eviction）
func (s *store) ListFlagged(ctx context.Context) ([]*FlaggedEntry, error) {
	cutoff := time.Now().UTC().AddDate(0, 0, -FlagWindowDays).Format("2006-01-02 15:04:05")
	staleAt := time.Now().UTC().AddDate(0, 0, -30)

	// 单查询获取所有 ACTIVE 条目及其评论统计（LEFT JOIN 避免 N+1）
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			e.id,
			e.needs_rewrite,
			e.accessed_at,
			COUNT(c.id) AS total_comments,
			COALESCE(SUM(CASE WHEN c.type = ? AND c.processed = 0 THEN 1 ELSE 0 END), 0) AS failure_count,
			COALESCE(SUM(CASE WHEN c.processed = 0 THEN 1 ELSE 0 END), 0) AS unprocessed_count,
			COALESCE(SUM(CASE WHEN c.type IN (?,?) AND c.created_at >= ? AND c.processed = 0 THEN 1 ELSE 0 END), 0) AS recent_fc
		FROM knowledge_entries e
		LEFT JOIN comments c ON c.knowledge_id = e.id
		WHERE e.status = ?
		GROUP BY e.id, e.needs_rewrite, e.accessed_at`,
		CommentTypeFailure,
		CommentTypeFailure, CommentTypeCorrection, cutoff,
		KnowledgeStatusActive,
	)
	if err != nil {
		return nil, fmt.Errorf("query flagged: %w", err)
	}

	type entryStats struct {
		id              string
		needsRewrite    bool
		accessedAt      time.Time
		totalComments   int
		failureCount    int
		unprocessedCount int
		recentFC        int
	}
	var stats []entryStats
	for rows.Next() {
		var st entryStats
		if err := rows.Scan(
			&st.id, &st.needsRewrite, &st.accessedAt,
			&st.totalComments, &st.failureCount, &st.unprocessedCount, &st.recentFC,
		); err != nil {
			rows.Close()
			return nil, err
		}
		stats = append(stats, st)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	type flagMeta struct {
		reasons      []string
		failureCount int
	}
	flagMap := map[string]flagMeta{}
	var flaggedIDs []string

	for _, st := range stats {
		var reasons []string
		if st.needsRewrite {
			reasons = append(reasons, "needs_rewrite")
		}
		if st.accessedAt.Before(staleAt) {
			reasons = append(reasons, "stale_access")
		}
		if st.unprocessedCount > 0 {
			reasons = append(reasons, "has_unprocessed_comments")
		}
		if st.unprocessedCount > 0 && float64(st.failureCount)/float64(st.unprocessedCount) > 0.5 {
			reasons = append(reasons, "high_failure_rate")
		}
		if st.recentFC >= FlagThreshold {
			reasons = append(reasons, "failure_eviction")
		}

		if len(reasons) > 0 {
			flaggedIDs = append(flaggedIDs, st.id)
			flagMap[st.id] = flagMeta{reasons: reasons, failureCount: st.failureCount}
		}
	}

	if len(flaggedIDs) == 0 {
		return nil, nil
	}

	// 批量加载 flagged 条目（不经过 GetByID，避免更新 access_count/accessed_at）
	entries, err := s.fetchEntriesByIDs(ctx, flaggedIDs)
	if err != nil {
		return nil, err
	}

	var result []*FlaggedEntry
	for _, entry := range entries {
		comments, err := s.GetUnprocessed(ctx, entry.ID)
		if err != nil {
			return nil, err
		}
		meta := flagMap[entry.ID]
		result = append(result, &FlaggedEntry{
			Entry:          entry,
			FailureCount:   meta.failureCount,
			RecentComments: comments,
			FlagReasons:    meta.reasons,
		})
	}
	return result, nil
}

// ---- 私有辅助方法 ----

func (s *store) setStatus(ctx context.Context, id string, status int) error {
	res, err := s.db.ExecContext(ctx,
		"UPDATE knowledge_entries SET status = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?",
		status, id,
	)
	if err != nil {
		return fmt.Errorf("set status: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// upsertTagsAndLink 创建不存在的 Tag，建立 knowledge_tags 关联，并更新 frequency。
func (s *store) upsertTagsAndLink(ctx context.Context, tx *sql.Tx, entryID string, tags []string, incr bool) error {
	for _, name := range tags {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}

		// 查找或创建 tag
		var tagID string
		err := tx.QueryRowContext(ctx, "SELECT id FROM tags WHERE name = ?", name).Scan(&tagID)
		if errors.Is(err, sql.ErrNoRows) {
			tagID = uuid.NewString()
			if _, err := tx.ExecContext(ctx,
				"INSERT INTO tags (id, name, aliases, frequency) VALUES (?, ?, '[]', 0)",
				tagID, name,
			); err != nil {
				return fmt.Errorf("insert tag %q: %w", name, err)
			}
		} else if err != nil {
			return fmt.Errorf("query tag %q: %w", name, err)
		}

		// 建立关联（忽略重复）
		if _, err := tx.ExecContext(ctx,
			"INSERT OR IGNORE INTO knowledge_tags (entry_id, tag_id) VALUES (?, ?)",
			entryID, tagID,
		); err != nil {
			return fmt.Errorf("link tag %q: %w", name, err)
		}

		// 更新 frequency
		delta := 1
		if !incr {
			delta = -1
		}
		if _, err := tx.ExecContext(ctx,
			"UPDATE tags SET frequency = MAX(0, frequency + ?) WHERE id = ?",
			delta, tagID,
		); err != nil {
			return fmt.Errorf("update frequency %q: %w", name, err)
		}
	}
	return nil
}

// unlinkAllTags 解除一个条目的所有 tag 关联，并还原 frequency。
func (s *store) unlinkAllTags(ctx context.Context, tx *sql.Tx, entryID string) error {
	rows, err := tx.QueryContext(ctx,
		"SELECT tag_id FROM knowledge_tags WHERE entry_id = ?", entryID)
	if err != nil {
		return fmt.Errorf("query tag links: %w", err)
	}
	var tagIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		tagIDs = append(tagIDs, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	for _, tagID := range tagIDs {
		if _, err := tx.ExecContext(ctx,
			"UPDATE tags SET frequency = MAX(0, frequency - 1) WHERE id = ?", tagID,
		); err != nil {
			return fmt.Errorf("decrement frequency: %w", err)
		}
	}

	if _, err := tx.ExecContext(ctx,
		"DELETE FROM knowledge_tags WHERE entry_id = ?", entryID,
	); err != nil {
		return fmt.Errorf("delete tag links: %w", err)
	}
	return nil
}

// loadTags 加载一个条目关联的所有 tag 名。
func (s *store) loadTags(ctx context.Context, q interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, entryID string) ([]string, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT t.name FROM tags t
		JOIN knowledge_tags kt ON kt.tag_id = t.id
		WHERE kt.entry_id = ?
		ORDER BY t.name`, entryID)
	if err != nil {
		return nil, fmt.Errorf("load tags: %w", err)
	}
	defer rows.Close()

	var tags []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		tags = append(tags, name)
	}
	return tags, rows.Err()
}

// scanEntries 扫描所有行后批量加载 tags，避免在 rows 未关闭时发起嵌套查询导致死锁。
func (s *store) scanEntries(ctx context.Context, rows *sql.Rows) ([]*KnowledgeEntry, error) {
	var entries []*KnowledgeEntry
	var ids []string
	for rows.Next() {
		e := &KnowledgeEntry{}
		if err := rows.Scan(
			&e.ID, &e.Title, &e.Summary, &e.Body, &e.Author,
			&e.Weight, &e.Status, &e.AccessCount, &e.AppendCount,
			&e.NeedsRewrite, &e.CreatedAt, &e.UpdatedAt, &e.AccessedAt,
		); err != nil {
			return nil, err
		}
		entries = append(entries, e)
		ids = append(ids, e.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// rows 已全部消费（连接已释放），安全发起批量 tag 查询
	if len(ids) > 0 {
		tagsMap, err := s.bulkLoadTags(ctx, ids)
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			e.Tags = tagsMap[e.ID]
		}
	}
	return entries, nil
}

// bulkLoadTags 一次查询加载多条 entry 的 tags，返回 entryID -> []tagName 映射。
func (s *store) bulkLoadTags(ctx context.Context, entryIDs []string) (map[string][]string, error) {
	placeholders := strings.Repeat("?,", len(entryIDs))
	placeholders = placeholders[:len(placeholders)-1]
	args := make([]any, len(entryIDs))
	for i, id := range entryIDs {
		args[i] = id
	}

	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT kt.entry_id, t.name
		FROM knowledge_tags kt
		JOIN tags t ON t.id = kt.tag_id
		WHERE kt.entry_id IN (%s)
		ORDER BY kt.entry_id, t.name`, placeholders), args...)
	if err != nil {
		return nil, fmt.Errorf("bulk load tags: %w", err)
	}
	defer rows.Close()

	result := make(map[string][]string)
	for rows.Next() {
		var entryID, tagName string
		if err := rows.Scan(&entryID, &tagName); err != nil {
			return nil, err
		}
		result[entryID] = append(result[entryID], tagName)
	}
	return result, rows.Err()
}

func (s *store) fetchEntriesByIDs(ctx context.Context, ids []string) ([]*KnowledgeEntry, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	placeholders := strings.Repeat("?,", len(ids))
	placeholders = placeholders[:len(placeholders)-1]
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT id, title, summary, body, author, weight, status,
		       access_count, append_count, needs_rewrite,
		       created_at, updated_at, accessed_at
		FROM knowledge_entries WHERE id IN (%s)`, placeholders), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return s.scanEntries(ctx, rows)
}

func (s *store) entryIDsForTags(ctx context.Context, tags []string) ([]string, error) {
	if len(tags) == 0 {
		// 无 tag 过滤，返回所有 ACTIVE 条目 ID
		rows, err := s.db.QueryContext(ctx,
			"SELECT id FROM knowledge_entries WHERE status = ?", KnowledgeStatusActive)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var ids []string
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				return nil, err
			}
			ids = append(ids, id)
		}
		return ids, rows.Err()
	}

	// 交集：找同时拥有所有指定 tag 的 entry
	placeholders := strings.Repeat("?,", len(tags))
	placeholders = placeholders[:len(placeholders)-1]
	args := make([]any, len(tags)+1)
	for i, t := range tags {
		args[i] = t
	}
	args[len(tags)] = len(tags)

	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT kt.entry_id
		FROM knowledge_tags kt
		JOIN tags t ON t.id = kt.tag_id
		JOIN knowledge_entries e ON e.id = kt.entry_id
		WHERE t.name IN (%s) AND e.status = %d
		GROUP BY kt.entry_id
		HAVING COUNT(DISTINCT t.name) = ?`,
		placeholders, KnowledgeStatusActive), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (s *store) buildTagJoin(tags []string, args *[]any) string {
	if len(tags) == 0 {
		return ""
	}
	joins := ""
	for i, tag := range tags {
		alias := fmt.Sprintf("kt%d", i)
		joins += fmt.Sprintf(`
		JOIN knowledge_tags %s ON %s.entry_id = e.id
		JOIN tags t%d ON t%d.id = %s.tag_id AND t%d.name = ?`, alias, alias, i, i, alias, i)
		*args = append(*args, tag)
	}
	return joins
}

func (s *store) safeOrderBy(col string) string {
	switch col {
	case "weight", "access_count", "append_count", "created_at", "updated_at":
		return col
	default:
		return "weight"
	}
}

func (s *store) orderDir(desc bool) string {
	if desc {
		return "DESC"
	}
	return "DESC" // 默认降序
}

// JSON 辅助
func toJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func fromJSON(s string, v any) error {
	if s == "" || s == "null" {
		return nil
	}
	return json.Unmarshal([]byte(s), v)
}

// 集合辅助
func setOf(items []string) map[string]struct{} {
	m := make(map[string]struct{}, len(items))
	for _, it := range items {
		m[it] = struct{}{}
	}
	return m
}

func intersection(a, b map[string]struct{}) map[string]struct{} {
	result := map[string]struct{}{}
	for k := range a {
		if _, ok := b[k]; ok {
			result[k] = struct{}{}
		}
	}
	return result
}
