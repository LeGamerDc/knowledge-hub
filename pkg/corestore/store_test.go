package corestore_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/legamerdc/knowledge-hub/pkg/corestore"
)

func newTestStore(t *testing.T) corestore.Store {
	t.Helper()
	s, err := corestore.New(":memory:")
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func ptr[T any](v T) *T { return &v }

// ---- KnowledgeStore ----

func TestCreate_GetByID(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	entry := &corestore.KnowledgeEntry{
		Title:   "Go Concurrency",
		Summary: "Goroutines and channels",
		Body:    "Use goroutines for concurrent tasks.",
		Author:  "agent-1",
		Tags:    []string{"go", "concurrency"},
	}
	id, err := s.Create(ctx, entry)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if id == "" {
		t.Fatal("expected non-empty ID")
	}

	got, err := s.GetByID(ctx, id)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Title != entry.Title {
		t.Errorf("title: got %q want %q", got.Title, entry.Title)
	}
	if len(got.Tags) != 2 {
		t.Errorf("tags count: got %d want 2", len(got.Tags))
	}
	if got.Status != corestore.KnowledgeStatusActive {
		t.Errorf("status: got %d want %d", got.Status, corestore.KnowledgeStatusActive)
	}
}

func TestGetByID_NotFound(t *testing.T) {
	s := newTestStore(t)
	_, err := s.GetByID(context.Background(), "nonexistent")
	if err != corestore.ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestTagAutoCreate_Frequency(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// 创建两篇共享 "go" tag 的知识
	id1, _ := s.Create(ctx, &corestore.KnowledgeEntry{
		Title: "A", Tags: []string{"go", "test"},
	})
	id2, _ := s.Create(ctx, &corestore.KnowledgeEntry{
		Title: "B", Tags: []string{"go", "bench"},
	})
	_ = id1
	_ = id2

	// GetTagHealth 检查 frequency
	report, err := s.GetTagHealth(ctx)
	if err != nil {
		t.Fatalf("GetTagHealth: %v", err)
	}

	// "go" 的 frequency 应该是 2
	// low freq tags 应包含 "test" 和 "bench"（frequency=1）
	lowNames := map[string]bool{}
	for _, t := range report.LowFreqTags {
		lowNames[t.Name] = true
	}
	if !lowNames["test"] {
		t.Error("expected 'test' in low freq tags")
	}
	if !lowNames["bench"] {
		t.Error("expected 'bench' in low freq tags")
	}
}

func TestArchive_Restore(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	id, _ := s.Create(ctx, &corestore.KnowledgeEntry{Title: "X"})

	if err := s.Archive(ctx, id); err != nil {
		t.Fatalf("Archive: %v", err)
	}
	got, _ := s.GetByID(ctx, id)
	if got.Status != corestore.KnowledgeStatusArchived {
		t.Errorf("after archive: status=%d", got.Status)
	}

	if err := s.Restore(ctx, id); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	got, _ = s.GetByID(ctx, id)
	if got.Status != corestore.KnowledgeStatusActive {
		t.Errorf("after restore: status=%d", got.Status)
	}
}

func TestHardDelete(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	id, _ := s.Create(ctx, &corestore.KnowledgeEntry{
		Title: "Delete me", Tags: []string{"tmp"},
	})

	// 添加评论验证级联删除
	_, err := s.AddComment(ctx, &corestore.Comment{
		KnowledgeID: id,
		Type:        corestore.CommentTypeSuccess,
		Content:     "ok",
		Reasoning:   "worked",
	})
	if err != nil {
		t.Fatalf("AddComment: %v", err)
	}

	if err := s.HardDelete(ctx, id); err != nil {
		t.Fatalf("HardDelete: %v", err)
	}
	if _, err := s.GetByID(ctx, id); err != corestore.ErrNotFound {
		t.Errorf("after hard delete: expected ErrNotFound, got %v", err)
	}
}

func TestHardDelete_NotFound(t *testing.T) {
	s := newTestStore(t)
	err := s.HardDelete(context.Background(), "bad-id")
	if err != corestore.ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestAppend_NeedsRewrite(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	id, _ := s.Create(ctx, &corestore.KnowledgeEntry{Title: "AppendMe", Body: "Initial"})

	for i := 0; i < corestore.AppendThreshold; i++ {
		if err := s.Append(ctx, id, "supplement", "extra info"); err != nil {
			t.Fatalf("Append #%d: %v", i, err)
		}
	}

	got, _ := s.GetByID(ctx, id)
	if !got.NeedsRewrite {
		t.Error("expected NeedsRewrite=true after threshold appends")
	}
	if got.AppendCount != corestore.AppendThreshold {
		t.Errorf("AppendCount: got %d want %d", got.AppendCount, corestore.AppendThreshold)
	}
}

func TestUpdate_ResetsAppendCount(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	id, _ := s.Create(ctx, &corestore.KnowledgeEntry{Title: "U", Body: "old"})
	for i := 0; i < corestore.AppendThreshold; i++ {
		_ = s.Append(ctx, id, "supplement", "x")
	}

	newTitle := "Updated Title"
	newBody := "rewritten body"
	if err := s.Update(ctx, id, corestore.UpdateFields{
		Title: &newTitle,
		Body:  &newBody,
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	got, _ := s.GetByID(ctx, id)
	if got.Title != newTitle {
		t.Errorf("title: got %q want %q", got.Title, newTitle)
	}
	if got.AppendCount != 0 {
		t.Errorf("AppendCount not reset: %d", got.AppendCount)
	}
	if got.NeedsRewrite {
		t.Error("NeedsRewrite should be false after update")
	}
}

func TestUpdate_Tags(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	id, _ := s.Create(ctx, &corestore.KnowledgeEntry{
		Title: "T", Tags: []string{"old-tag"},
	})
	if err := s.Update(ctx, id, corestore.UpdateFields{
		Tags: []string{"new-tag-1", "new-tag-2"},
	}); err != nil {
		t.Fatalf("Update tags: %v", err)
	}

	got, _ := s.GetByID(ctx, id)
	if len(got.Tags) != 2 {
		t.Errorf("tags count: got %d want 2, tags=%v", len(got.Tags), got.Tags)
	}
}

// ---- CommentStore ----

func TestAddComment_GetByKnowledgeID(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	id, _ := s.Create(ctx, &corestore.KnowledgeEntry{Title: "Z"})

	c := &corestore.Comment{
		KnowledgeID: id,
		Type:        corestore.CommentTypeSuccess,
		Content:     "worked great",
		Reasoning:   "completed task successfully",
		Scenario:    "deploy to prod",
		Author:      "agent-a",
	}
	cid, err := s.AddComment(ctx, c)
	if err != nil {
		t.Fatalf("AddComment: %v", err)
	}

	comments, err := s.GetByKnowledgeID(ctx, id)
	if err != nil {
		t.Fatalf("GetByKnowledgeID: %v", err)
	}
	if len(comments) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(comments))
	}
	if comments[0].ID != cid {
		t.Errorf("comment ID mismatch")
	}
}

func TestMarkProcessed(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	id, _ := s.Create(ctx, &corestore.KnowledgeEntry{Title: "MP"})
	cid, _ := s.AddComment(ctx, &corestore.Comment{
		KnowledgeID: id,
		Type:        corestore.CommentTypeFailure,
		Content:     "failed",
		Reasoning:   "broken",
	})

	unprocessed, _ := s.GetUnprocessed(ctx, id)
	if len(unprocessed) != 1 {
		t.Fatalf("expected 1 unprocessed, got %d", len(unprocessed))
	}

	if err := s.MarkProcessed(ctx, []string{cid}); err != nil {
		t.Fatalf("MarkProcessed: %v", err)
	}

	unprocessed, _ = s.GetUnprocessed(ctx, id)
	if len(unprocessed) != 0 {
		t.Errorf("expected 0 unprocessed after mark, got %d", len(unprocessed))
	}
}

// ---- TagStore ----

func TestMergeTags(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// 创建两条知识，分别用 "golang" 和 "go" tag
	id1, _ := s.Create(ctx, &corestore.KnowledgeEntry{Title: "A", Tags: []string{"golang"}})
	id2, _ := s.Create(ctx, &corestore.KnowledgeEntry{Title: "B", Tags: []string{"go"}})

	// 将 "golang" 合并到 "go"
	affected, err := s.MergeTags(ctx, "go", []string{"golang"})
	if err != nil {
		t.Fatalf("MergeTags: %v", err)
	}
	if affected != 1 {
		t.Errorf("affected: got %d want 1", affected)
	}

	// id1 现在应该有 "go" tag
	e1, _ := s.GetByID(ctx, id1)
	hasGo := false
	for _, tag := range e1.Tags {
		if tag == "go" {
			hasGo = true
		}
	}
	if !hasGo {
		t.Errorf("id1 should have 'go' tag after merge, tags=%v", e1.Tags)
	}

	// id2 也有 "go" tag
	e2, _ := s.GetByID(ctx, id2)
	hasGo = false
	for _, tag := range e2.Tags {
		if tag == "go" {
			hasGo = true
		}
	}
	if !hasGo {
		t.Errorf("id2 should still have 'go' tag, tags=%v", e2.Tags)
	}
}

func TestMergeTags_BothHaveSameTag(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// 两条知识都有 "go" 和 "golang"
	id1, _ := s.Create(ctx, &corestore.KnowledgeEntry{Title: "A", Tags: []string{"go", "golang"}})
	id2, _ := s.Create(ctx, &corestore.KnowledgeEntry{Title: "B", Tags: []string{"go"}})
	_ = id2

	// 合并 golang -> go，id1 不应该有重复 tag
	_, err := s.MergeTags(ctx, "go", []string{"golang"})
	if err != nil {
		t.Fatalf("MergeTags: %v", err)
	}

	e1, _ := s.GetByID(ctx, id1)
	count := 0
	for _, tag := range e1.Tags {
		if tag == "go" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected exactly 1 'go' tag, got %d, tags=%v", count, e1.Tags)
	}
}

// ---- CurationStore ----

func TestLogCuration_List(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	id, _ := s.Create(ctx, &corestore.KnowledgeEntry{Title: "C"})

	logID, err := s.LogCuration(ctx, &corestore.CurationLog{
		Action:      corestore.CurationArchive,
		TargetID:    id,
		SourceIDs:   []string{"comment-1"},
		Description: "archived due to failures",
		AgentID:     "curator-agent",
	})
	if err != nil {
		t.Fatalf("LogCuration: %v", err)
	}

	logs, err := s.ListCurationLogs(ctx, 10)
	if err != nil {
		t.Fatalf("ListCurationLogs: %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("expected 1 log, got %d", len(logs))
	}
	if logs[0].ID != logID {
		t.Error("log ID mismatch")
	}
	if len(logs[0].SourceIDs) != 1 {
		t.Errorf("source IDs: got %v", logs[0].SourceIDs)
	}
}

// ---- ConflictStore ----

func TestConflict_CreateListResolve(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	cid, err := s.CreateConflict(ctx, &corestore.ConflictReport{
		Type:        1,
		Description: "Two entries contradict each other",
		KnowledgeIDs: []string{"id-a", "id-b"},
	})
	if err != nil {
		t.Fatalf("CreateConflict: %v", err)
	}

	open, err := s.ListConflicts(ctx, "open")
	if err != nil {
		t.Fatalf("ListConflicts open: %v", err)
	}
	if len(open) != 1 {
		t.Fatalf("expected 1 open conflict, got %d", len(open))
	}
	if open[0].ID != cid {
		t.Error("conflict ID mismatch")
	}

	if err := s.ResolveConflict(ctx, cid, "merged into one entry"); err != nil {
		t.Fatalf("ResolveConflict: %v", err)
	}

	open, _ = s.ListConflicts(ctx, "open")
	if len(open) != 0 {
		t.Errorf("expected 0 open after resolve, got %d", len(open))
	}

	resolved, _ := s.ListConflicts(ctx, "resolved")
	if len(resolved) != 1 {
		t.Errorf("expected 1 resolved, got %d", len(resolved))
	}
	if resolved[0].Resolution != "merged into one entry" {
		t.Errorf("resolution: %q", resolved[0].Resolution)
	}
}

// ---- SystemStore ----

func TestGetStatus(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	id, _ := s.Create(ctx, &corestore.KnowledgeEntry{Title: "S", Tags: []string{"t1"}})
	_ = s.Archive(ctx, id)

	id2, _ := s.Create(ctx, &corestore.KnowledgeEntry{Title: "S2", Tags: []string{"t2"}})
	_, _ = s.AddComment(ctx, &corestore.Comment{
		KnowledgeID: id2, Type: corestore.CommentTypeFailure,
		Content: "x", Reasoning: "y",
	})
	_, _ = s.CreateConflict(ctx, &corestore.ConflictReport{
		Type: 1, Description: "conflict",
	})

	status, err := s.GetStatus(ctx)
	if err != nil {
		t.Fatalf("GetStatus: %v", err)
	}
	if status.ActiveCount != 1 {
		t.Errorf("active: got %d want 1", status.ActiveCount)
	}
	if status.ArchivedCount != 1 {
		t.Errorf("archived: got %d want 1", status.ArchivedCount)
	}
	if status.TagCount < 2 {
		t.Errorf("tags: got %d want >=2", status.TagCount)
	}
	if status.UnprocessedCount != 1 {
		t.Errorf("unprocessed: got %d want 1", status.UnprocessedCount)
	}
	if status.OpenConflicts != 1 {
		t.Errorf("open conflicts: got %d want 1", status.OpenConflicts)
	}
}

func TestRecalculateWeights(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	id, _ := s.Create(ctx, &corestore.KnowledgeEntry{Title: "W"})
	_, _ = s.AddComment(ctx, &corestore.Comment{
		KnowledgeID: id, Type: corestore.CommentTypeSuccess,
		Content: "worked", Reasoning: "test",
	})
	_, _ = s.AddComment(ctx, &corestore.Comment{
		KnowledgeID: id, Type: corestore.CommentTypeFailure,
		Content: "failed", Reasoning: "bug",
	})

	n, err := s.RecalculateWeights(ctx)
	if err != nil {
		t.Fatalf("RecalculateWeights: %v", err)
	}
	if n != 1 {
		t.Errorf("updated count: got %d want 1", n)
	}

	got, _ := s.GetByID(ctx, id)
	// weight = 1.0 + decay*1.0 (success) - 1*2.0 (failure) + access_count*0.1
	// 应该小于 1.0（有 failure 扣分）
	if got.Weight >= 1.0 {
		t.Errorf("weight should be < 1.0 due to failure penalty, got %f", got.Weight)
	}
}

// ---- Search ----

func TestSearch(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	s.Create(ctx, &corestore.KnowledgeEntry{Title: "Go Routines", Tags: []string{"go"}})
	s.Create(ctx, &corestore.KnowledgeEntry{Title: "Python Async", Tags: []string{"python"}})

	results, err := s.Search(ctx, corestore.SearchQuery{Q: "Go", Limit: 10})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result, got %d", len(results))
	}
	if results[0].Title != "Go Routines" {
		t.Errorf("unexpected title: %q", results[0].Title)
	}
}

func TestBrowseFacets_SmallResult(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	s.Create(ctx, &corestore.KnowledgeEntry{Title: "A", Tags: []string{"go", "test"}})
	s.Create(ctx, &corestore.KnowledgeEntry{Title: "B", Tags: []string{"go", "bench"}})
	s.Create(ctx, &corestore.KnowledgeEntry{Title: "C", Tags: []string{"python"}})

	result, err := s.BrowseFacets(ctx, []string{"go"})
	if err != nil {
		t.Fatalf("BrowseFacets: %v", err)
	}
	if result.TotalHits != 2 {
		t.Errorf("total hits: got %d want 2", result.TotalHits)
	}
	// 始终返回 NextTags（由 Agent 决定何时切换到 kh_search）
	if len(result.NextTags) == 0 {
		t.Errorf("expected NextTags to be non-empty for 2 matching entries")
	}
}

func TestListFlagged_FailureEviction(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	id, _ := s.Create(ctx, &corestore.KnowledgeEntry{Title: "Flagged"})

	for i := 0; i < corestore.FlagThreshold; i++ {
		s.AddComment(ctx, &corestore.Comment{
			KnowledgeID: id, Type: corestore.CommentTypeFailure,
			Content: "fail", Reasoning: "broken",
		})
	}

	flagged, err := s.ListFlagged(ctx)
	if err != nil {
		t.Fatalf("ListFlagged: %v", err)
	}
	if len(flagged) != 1 {
		t.Errorf("expected 1 flagged, got %d", len(flagged))
	}
	if flagged[0].Entry.ID != id {
		t.Error("flagged entry ID mismatch")
	}
	hasReason := false
	for _, r := range flagged[0].FlagReasons {
		if r == "failure_eviction" {
			hasReason = true
		}
	}
	if !hasReason {
		t.Errorf("expected failure_eviction reason, got %v", flagged[0].FlagReasons)
	}
}

func TestListFlagged_NeedsRewrite(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	id, _ := s.Create(ctx, &corestore.KnowledgeEntry{Title: "Rewrite Me"})

	// 追加超过阈值次数触发 needs_rewrite
	for i := 0; i < corestore.AppendThreshold; i++ {
		if err := s.Append(ctx, id, "supplement", "extra"); err != nil {
			t.Fatalf("Append #%d: %v", i, err)
		}
	}

	flagged, err := s.ListFlagged(ctx)
	if err != nil {
		t.Fatalf("ListFlagged: %v", err)
	}

	var found *corestore.FlaggedEntry
	for _, f := range flagged {
		if f.Entry.ID == id {
			found = f
			break
		}
	}
	if found == nil {
		t.Fatal("expected flagged entry for needs_rewrite")
	}
	hasReason := false
	for _, r := range found.FlagReasons {
		if r == "needs_rewrite" {
			hasReason = true
		}
	}
	if !hasReason {
		t.Errorf("expected needs_rewrite reason, got %v", found.FlagReasons)
	}
}

func TestListFlagged_HighFailureRate(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	id, _ := s.Create(ctx, &corestore.KnowledgeEntry{Title: "High Failure"})

	// 1 success + 3 failure → failure rate = 75% > 50%
	s.AddComment(ctx, &corestore.Comment{
		KnowledgeID: id, Type: corestore.CommentTypeSuccess,
		Content: "worked", Reasoning: "ok",
	})
	for i := 0; i < 3; i++ {
		s.AddComment(ctx, &corestore.Comment{
			KnowledgeID: id, Type: corestore.CommentTypeFailure,
			Content: "fail", Reasoning: "broken",
		})
	}

	flagged, err := s.ListFlagged(ctx)
	if err != nil {
		t.Fatalf("ListFlagged: %v", err)
	}

	var found *corestore.FlaggedEntry
	for _, f := range flagged {
		if f.Entry.ID == id {
			found = f
			break
		}
	}
	if found == nil {
		t.Fatal("expected flagged entry for high_failure_rate")
	}
	hasReason := false
	for _, r := range found.FlagReasons {
		if r == "high_failure_rate" {
			hasReason = true
		}
	}
	if !hasReason {
		t.Errorf("expected high_failure_rate reason, got %v", found.FlagReasons)
	}
}

// ---- FindSimilar ----

func TestFindSimilar(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// A 和 B 共享 3/4 的 tag → Jaccard = 3/(4+4-3) = 3/5 = 60%，不超过 80%
	s.Create(ctx, &corestore.KnowledgeEntry{
		Title: "A", Tags: []string{"go", "concurrency", "goroutine", "channel"},
	})
	s.Create(ctx, &corestore.KnowledgeEntry{
		Title: "B", Tags: []string{"go", "concurrency", "goroutine", "mutex"},
	})
	// C 和 D 高度重叠：3/3 = 100% > 80%
	idC, _ := s.Create(ctx, &corestore.KnowledgeEntry{
		Title: "C", Tags: []string{"python", "async", "await"},
	})
	idD, _ := s.Create(ctx, &corestore.KnowledgeEntry{
		Title: "D", Tags: []string{"python", "async", "await"},
	})

	pairs, err := s.FindSimilar(ctx)
	if err != nil {
		t.Fatalf("FindSimilar: %v", err)
	}

	// 应该找到 C-D 对（100% overlap）
	found := false
	for _, p := range pairs {
		aID, bID := p.EntryA.ID, p.EntryB.ID
		if (aID == idC && bID == idD) || (aID == idD && bID == idC) {
			found = true
			if p.Overlap <= 0.8 {
				t.Errorf("C-D overlap should be > 0.8, got %f", p.Overlap)
			}
		}
	}
	if !found {
		t.Errorf("expected C-D pair in similar results, got %d pairs", len(pairs))
	}
}

// ---- GetTagHealth enhanced ----

func TestGetTagHealth_Substring(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// "go" 是 "golang" 的子串
	s.Create(ctx, &corestore.KnowledgeEntry{Title: "A", Tags: []string{"go"}})
	s.Create(ctx, &corestore.KnowledgeEntry{Title: "B", Tags: []string{"golang"}})

	report, err := s.GetTagHealth(ctx)
	if err != nil {
		t.Fatalf("GetTagHealth: %v", err)
	}

	found := false
	for _, p := range report.SynonymPairs {
		names := []string{p.TagA.Name, p.TagB.Name}
		hasGo, hasGolang := false, false
		for _, n := range names {
			if n == "go" {
				hasGo = true
			}
			if n == "golang" {
				hasGolang = true
			}
		}
		if hasGo && hasGolang {
			found = true
		}
	}
	if !found {
		t.Error("expected 'go'/'golang' synonym pair via substring detection")
	}
}

func TestGetTagHealth_CoOccurrence(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// "typescript" 和 "ts" 总是同时出现 → Jaccard = 1.0 > 0.8
	for i := 0; i < 3; i++ {
		s.Create(ctx, &corestore.KnowledgeEntry{
			Title: fmt.Sprintf("Entry%d", i),
			Tags:  []string{"typescript", "ts"},
		})
	}

	report, err := s.GetTagHealth(ctx)
	if err != nil {
		t.Fatalf("GetTagHealth: %v", err)
	}

	found := false
	for _, p := range report.SynonymPairs {
		names := []string{p.TagA.Name, p.TagB.Name}
		hasTS, hasFull := false, false
		for _, n := range names {
			if n == "ts" {
				hasTS = true
			}
			if n == "typescript" {
				hasFull = true
			}
		}
		if hasTS && hasFull {
			found = true
		}
	}
	if !found {
		t.Error("expected 'typescript'/'ts' synonym pair via co-occurrence detection")
	}
}

// ---- BrowseFacets large result ----

func TestBrowseFacets_LargeResult(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// 创建 12 条共享 "go" tag 的知识 → > 10 条，返回 NextTags 而非 Entries
	for i := 0; i < 12; i++ {
		s.Create(ctx, &corestore.KnowledgeEntry{
			Title: fmt.Sprintf("Go Entry %d", i),
			Tags:  []string{"go", fmt.Sprintf("sub%d", i)},
		})
	}

	result, err := s.BrowseFacets(ctx, []string{"go"})
	if err != nil {
		t.Fatalf("BrowseFacets: %v", err)
	}
	if result.TotalHits != 12 {
		t.Errorf("total hits: got %d want 12", result.TotalHits)
	}
	if len(result.Entries) != 0 {
		t.Errorf("expected no entries (>10 results), got %d", len(result.Entries))
	}
	if len(result.NextTags) == 0 {
		t.Error("expected NextTags for large result set")
	}
}
