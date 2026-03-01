package service_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/legamerdc/knowledge-hub/internal/server/handlers"
	"github.com/legamerdc/knowledge-hub/internal/server/service"
	"github.com/legamerdc/knowledge-hub/pkg/corestore"
)

// newTestServer creates a full HTTP test server with in-memory SQLite.
func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	store, err := corestore.New(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	svc := service.New(store)
	r := chi.NewRouter()
	handlers.HandlerFromMux(svc, r)

	ts := httptest.NewServer(r)
	t.Cleanup(ts.Close)
	return ts
}

// doJSON sends a JSON request and returns the response.
func doJSON(t *testing.T, ts *httptest.Server, method, path string, body any) *http.Response {
	t.Helper()
	var bodyBytes []byte
	if body != nil {
		var err error
		bodyBytes, err = json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
	}
	req, err := http.NewRequestWithContext(
		context.Background(), method,
		ts.URL+path,
		bytes.NewReader(bodyBytes),
	)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	return resp
}

// decodeJSON decodes the response body into v.
func decodeJSON(t *testing.T, resp *http.Response, v any) {
	t.Helper()
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		t.Fatalf("decode response: %v", err)
	}
}

// mustStatus asserts the response has the expected status code.
func mustStatus(t *testing.T, resp *http.Response, want int) {
	t.Helper()
	if resp.StatusCode != want {
		t.Errorf("status = %d, want %d", resp.StatusCode, want)
	}
}

// ─── Agent Domain Tests ───────────────────────────────────────────────────────

func TestAgentFlow(t *testing.T) {
	ts := newTestServer(t)

	// 1. Browse with no tags → empty tags (empty DB)
	resp := doJSON(t, ts, "POST", "/api/v1/agent/browse", map[string]any{})
	mustStatus(t, resp, 200)
	var browseResp handlers.BrowseResponse
	decodeJSON(t, resp, &browseResp)
	if browseResp.TotalMatches != 0 {
		t.Errorf("expected 0 total matches, got %d", browseResp.TotalMatches)
	}

	// 2. Contribute a knowledge entry
	resp = doJSON(t, ts, "POST", "/api/v1/agent/knowledge", map[string]any{
		"title":   "Go Goroutines",
		"summary": "Using goroutines effectively",
		"body":    "Goroutines are lightweight threads managed by the Go runtime.",
		"tags":    []string{"go", "concurrency"},
		"author":  "agent-test",
	})
	mustStatus(t, resp, 201)
	var contribResp handlers.ContributeResponse
	decodeJSON(t, resp, &contribResp)
	kidStr := contribResp.Id.String()
	if kidStr == "" {
		t.Fatal("expected non-empty knowledge ID")
	}

	// 3. Search by keyword
	resp = doJSON(t, ts, "POST", "/api/v1/agent/search", map[string]any{
		"keyword": "goroutine",
	})
	mustStatus(t, resp, 200)
	var searchResults []handlers.SearchResult
	decodeJSON(t, resp, &searchResults)
	if len(searchResults) != 1 {
		t.Fatalf("expected 1 result, got %d", len(searchResults))
	}
	if searchResults[0].Title != "Go Goroutines" {
		t.Errorf("unexpected title: %s", searchResults[0].Title)
	}

	// 4. Search by tag
	resp = doJSON(t, ts, "POST", "/api/v1/agent/search", map[string]any{
		"tags": []string{"go"},
	})
	mustStatus(t, resp, 200)
	var tagResults []handlers.SearchResult
	decodeJSON(t, resp, &tagResults)
	if len(tagResults) != 1 {
		t.Errorf("expected 1 result by tag, got %d", len(tagResults))
	}

	// 5. Read full entry (updates access stats)
	resp = doJSON(t, ts, "GET", "/api/v1/agent/knowledge/"+kidStr, nil)
	mustStatus(t, resp, 200)
	var detail handlers.KnowledgeDetail
	decodeJSON(t, resp, &detail)
	if detail.Title != "Go Goroutines" {
		t.Errorf("unexpected title: %s", detail.Title)
	}
	if detail.Body == nil || *detail.Body == "" {
		t.Error("expected non-empty body")
	}
	if detail.CommentsSummary == nil {
		t.Error("expected comments summary")
	}

	// 6. Append to knowledge entry
	resp = doJSON(t, ts, "POST", "/api/v1/agent/knowledge/"+kidStr+"/append", map[string]any{
		"type":    "supplement",
		"content": "Also, goroutines use very little memory.",
	})
	mustStatus(t, resp, 200)
	var appendResp handlers.AppendResponse
	decodeJSON(t, resp, &appendResp)
	if appendResp.AppendCount != 1 {
		t.Errorf("expected append_count=1, got %d", appendResp.AppendCount)
	}

	// 7. Add comment (success)
	resp = doJSON(t, ts, "POST", "/api/v1/agent/knowledge/"+kidStr+"/comments", map[string]any{
		"type":      "success",
		"content":   "Applied this in production, worked great.",
		"reasoning": "The goroutine pattern solved our latency issue.",
		"author":    "agent-test",
	})
	mustStatus(t, resp, 201)
	var commentResp handlers.CommentResponse
	decodeJSON(t, resp, &commentResp)
	if commentResp.Id.String() == "" {
		t.Error("expected non-empty comment ID")
	}

	// 8. Add comment (failure)
	resp = doJSON(t, ts, "POST", "/api/v1/agent/knowledge/"+kidStr+"/comments", map[string]any{
		"type":      "failure",
		"content":   "Caused goroutine leak when context was not cancelled.",
		"reasoning": "The example lacks context cancellation.",
		"scenario":  "long-running server",
	})
	mustStatus(t, resp, 201)

	// 9. Browse with tag → should return sub-tags or entry results
	resp = doJSON(t, ts, "POST", "/api/v1/agent/browse", map[string]any{
		"selected_tags": []string{"go"},
	})
	mustStatus(t, resp, 200)
	var browseResp2 handlers.BrowseResponse
	decodeJSON(t, resp, &browseResp2)
	if browseResp2.TotalMatches != 1 {
		t.Errorf("expected 1 total match, got %d", browseResp2.TotalMatches)
	}
}

func TestAgentReadNotFound(t *testing.T) {
	ts := newTestServer(t)
	resp := doJSON(t, ts, "GET", "/api/v1/agent/knowledge/00000000-0000-0000-0000-000000000000", nil)
	mustStatus(t, resp, 404)
}

func TestAgentCommentInvalidType(t *testing.T) {
	ts := newTestServer(t)

	// Create a knowledge entry first
	resp := doJSON(t, ts, "POST", "/api/v1/agent/knowledge", map[string]any{
		"title": "Test", "summary": "test", "body": "test", "tags": []string{"test"},
	})
	mustStatus(t, resp, 201)
	var cr handlers.ContributeResponse
	decodeJSON(t, resp, &cr)

	// Try invalid comment type
	resp = doJSON(t, ts, "POST", "/api/v1/agent/knowledge/"+cr.Id.String()+"/comments", map[string]any{
		"type":      "invalid_type",
		"content":   "test",
		"reasoning": "test",
	})
	mustStatus(t, resp, 400)
}

// ─── Admin Domain Tests ───────────────────────────────────────────────────────

func TestAdminFlow(t *testing.T) {
	ts := newTestServer(t)

	// Setup: create two knowledge entries
	var ids [2]string
	for i, title := range []string{"Entry Alpha", "Entry Beta"} {
		resp := doJSON(t, ts, "POST", "/api/v1/agent/knowledge", map[string]any{
			"title":   title,
			"summary": fmt.Sprintf("Summary for %s", title),
			"body":    fmt.Sprintf("Body for %s", title),
			"tags":    []string{"go", "testing"},
		})
		mustStatus(t, resp, 201)
		var cr handlers.ContributeResponse
		decodeJSON(t, resp, &cr)
		ids[i] = cr.Id.String()
	}

	// 1. Tag health check
	resp := doJSON(t, ts, "GET", "/api/v1/admin/tags/health", nil)
	mustStatus(t, resp, 200)
	var health handlers.TagHealthReport
	decodeJSON(t, resp, &health)

	// 2. Find similar pairs (entries with same tags)
	resp = doJSON(t, ts, "GET", "/api/v1/admin/knowledge/similar", nil)
	mustStatus(t, resp, 200)
	var similar []handlers.SimilarPair
	decodeJSON(t, resp, &similar)
	if len(similar) != 1 {
		t.Errorf("expected 1 similar pair, got %d", len(similar))
	}

	// 3. Get review details for first entry
	resp = doJSON(t, ts, "GET", "/api/v1/admin/knowledge/"+ids[0]+"/review", nil)
	mustStatus(t, resp, 200)
	var review handlers.ReviewData
	decodeJSON(t, resp, &review)
	if review.Knowledge.Title != "Entry Alpha" {
		t.Errorf("unexpected title: %s", review.Knowledge.Title)
	}

	// 4. Add a comment to first entry, then list flagged
	doJSON(t, ts, "POST", "/api/v1/agent/knowledge/"+ids[0]+"/comments", map[string]any{
		"type":      "failure",
		"content":   "This is wrong.",
		"reasoning": "Wrong approach.",
	})
	doJSON(t, ts, "POST", "/api/v1/agent/knowledge/"+ids[0]+"/comments", map[string]any{
		"type":      "failure",
		"content":   "Also fails here.",
		"reasoning": "Another failure.",
	})
	doJSON(t, ts, "POST", "/api/v1/agent/knowledge/"+ids[0]+"/comments", map[string]any{
		"type":      "correction",
		"content":   "Here is the fix.",
		"reasoning": "Correction needed.",
	})

	resp = doJSON(t, ts, "GET", "/api/v1/admin/flagged", nil)
	mustStatus(t, resp, 200)
	var flagged []handlers.FlaggedEntry
	decodeJSON(t, resp, &flagged)
	// Entry with 3 failure+correction comments should be flagged
	if len(flagged) == 0 {
		t.Error("expected at least 1 flagged entry")
	}

	// 5. Update knowledge entry (admin rewrite)
	resp = doJSON(t, ts, "PUT", "/api/v1/admin/knowledge/"+ids[0], map[string]any{
		"title":   "Entry Alpha (Updated)",
		"summary": "Updated summary",
		"body":    "Updated body with all corrections applied.",
		"tags":    []string{"go", "testing", "updated"},
	})
	mustStatus(t, resp, 200)
	var updateResp handlers.UpdateResponse
	decodeJSON(t, resp, &updateResp)
	if updateResp.UpdatedAt.IsZero() {
		t.Error("expected non-zero updated_at")
	}

	// 6. Mark comments as processed (from review)
	resp = doJSON(t, ts, "GET", "/api/v1/admin/knowledge/"+ids[0]+"/review", nil)
	mustStatus(t, resp, 200)
	var review2 handlers.ReviewData
	decodeJSON(t, resp, &review2)

	if len(review2.UnprocessedComments) > 0 {
		commentIDs := make([]string, len(review2.UnprocessedComments))
		for i, c := range review2.UnprocessedComments {
			commentIDs[i] = c.Id.String()
		}
		resp = doJSON(t, ts, "POST", "/api/v1/admin/comments/processed", map[string]any{
			"comment_ids": commentIDs,
		})
		mustStatus(t, resp, 200)
	}

	// 7. Merge tags
	resp = doJSON(t, ts, "POST", "/api/v1/admin/tags/merge", map[string]any{
		"target":  "testing",
		"sources": []string{"updated"},
	})
	mustStatus(t, resp, 200)
	var mergeTagsResp handlers.MergeTagsResponse
	decodeJSON(t, resp, &mergeTagsResp)
	if mergeTagsResp.AffectedKnowledgeCount < 0 {
		t.Error("expected non-negative affected count")
	}

	// 8. Archive the second entry
	resp = doJSON(t, ts, "POST", "/api/v1/admin/knowledge/"+ids[1]+"/archive", nil)
	mustStatus(t, resp, 200)

	// 9. Create conflict report
	resp = doJSON(t, ts, "POST", "/api/v1/admin/conflicts", map[string]any{
		"type":          "knowledge_conflict",
		"knowledge_ids": []string{ids[0], ids[1]},
		"description":   "These two entries conflict on goroutine lifecycle.",
	})
	mustStatus(t, resp, 201)
	var conflictResp handlers.CreateConflictResponse
	decodeJSON(t, resp, &conflictResp)
	if conflictResp.Id.String() == "" {
		t.Error("expected non-empty conflict ID")
	}

	// 10. Write curation log
	resp = doJSON(t, ts, "POST", "/api/v1/admin/curation-logs", map[string]any{
		"action":      "apply_correction",
		"target_id":   ids[0],
		"description": "Applied community corrections to Entry Alpha.",
	})
	mustStatus(t, resp, 201)
	var logResp handlers.CurationLogResponse
	decodeJSON(t, resp, &logResp)
	if logResp.Id.String() == "" {
		t.Error("expected non-empty log ID")
	}
}

func TestAdminMergeKnowledge(t *testing.T) {
	ts := newTestServer(t)

	// Create source and target entries
	var ids [3]string
	for i, title := range []string{"Target Entry", "Source A", "Source B"} {
		resp := doJSON(t, ts, "POST", "/api/v1/agent/knowledge", map[string]any{
			"title": title, "summary": "s", "body": "b", "tags": []string{"go"},
		})
		mustStatus(t, resp, 201)
		var cr handlers.ContributeResponse
		decodeJSON(t, resp, &cr)
		ids[i] = cr.Id.String()
	}

	// Merge sources into target
	resp := doJSON(t, ts, "POST", "/api/v1/admin/knowledge/merge", map[string]any{
		"target_id":      ids[0],
		"source_ids":     []string{ids[1], ids[2]},
		"merged_body":    "Combined body from all entries.",
		"merged_summary": "Combined summary.",
	})
	mustStatus(t, resp, 200)
	var mergeResp handlers.MergeKnowledgeResponse
	decodeJSON(t, resp, &mergeResp)
	if mergeResp.Id.String() != ids[0] {
		t.Errorf("expected target ID %s, got %s", ids[0], mergeResp.Id.String())
	}

	// Sources should now be archived
	resp = doJSON(t, ts, "GET", "/api/v1/system/knowledge/"+ids[1], nil)
	mustStatus(t, resp, 200)
	var entry handlers.KnowledgeEntry
	decodeJSON(t, resp, &entry)
	if entry.Status != "archived" {
		t.Errorf("expected source to be archived, got %s", entry.Status)
	}
}

// ─── System Domain Tests ──────────────────────────────────────────────────────

func TestSystemFlow(t *testing.T) {
	ts := newTestServer(t)

	// 1. System status (empty DB)
	resp := doJSON(t, ts, "GET", "/api/v1/system/status", nil)
	mustStatus(t, resp, 200)
	var status handlers.SystemStatus
	decodeJSON(t, resp, &status)
	if status.ActiveKnowledge == nil || *status.ActiveKnowledge != 0 {
		t.Error("expected 0 active knowledge entries")
	}

	// 2. Contribute entries
	var ids [3]string
	for i, title := range []string{"Entry 1", "Entry 2", "Entry 3"} {
		resp = doJSON(t, ts, "POST", "/api/v1/agent/knowledge", map[string]any{
			"title":   title,
			"summary": "summary",
			"body":    "body",
			"tags":    []string{"go"},
		})
		mustStatus(t, resp, 201)
		var cr handlers.ContributeResponse
		decodeJSON(t, resp, &cr)
		ids[i] = cr.Id.String()
	}

	// 3. List all knowledge (no filter)
	resp = doJSON(t, ts, "GET", "/api/v1/system/knowledge", nil)
	mustStatus(t, resp, 200)
	var entries []handlers.KnowledgeEntry
	decodeJSON(t, resp, &entries)
	if len(entries) != 3 {
		t.Errorf("expected 3 entries, got %d", len(entries))
	}

	// 4. List with status filter
	resp = doJSON(t, ts, "GET", "/api/v1/system/knowledge?status=active", nil)
	mustStatus(t, resp, 200)
	var activeEntries []handlers.KnowledgeEntry
	decodeJSON(t, resp, &activeEntries)
	if len(activeEntries) != 3 {
		t.Errorf("expected 3 active entries, got %d", len(activeEntries))
	}

	// 5. Read single knowledge entry
	resp = doJSON(t, ts, "GET", "/api/v1/system/knowledge/"+ids[0], nil)
	mustStatus(t, resp, 200)
	var singleEntry handlers.KnowledgeEntry
	decodeJSON(t, resp, &singleEntry)
	if singleEntry.Title != "Entry 1" {
		t.Errorf("unexpected title: %s", singleEntry.Title)
	}

	// 6. Archive one entry
	resp = doJSON(t, ts, "POST", "/api/v1/admin/knowledge/"+ids[2]+"/archive", nil)
	mustStatus(t, resp, 200)

	// 7. Restore archived entry
	resp = doJSON(t, ts, "POST", "/api/v1/system/knowledge/"+ids[2]+"/restore", nil)
	mustStatus(t, resp, 200)

	// 8. Archive again, then hard delete
	doJSON(t, ts, "POST", "/api/v1/admin/knowledge/"+ids[2]+"/archive", nil)

	resp = doJSON(t, ts, "DELETE", "/api/v1/system/knowledge/"+ids[2], nil)
	mustStatus(t, resp, 200)

	// Confirm deletion
	resp = doJSON(t, ts, "GET", "/api/v1/system/knowledge/"+ids[2], nil)
	mustStatus(t, resp, 404)

	// 9. Recalculate weights
	resp = doJSON(t, ts, "POST", "/api/v1/system/recalculate-weights", nil)
	mustStatus(t, resp, 200)
	var recalcResp handlers.RecalculateResponse
	decodeJSON(t, resp, &recalcResp)
	if recalcResp.UpdatedCount < 0 {
		t.Error("expected non-negative updated count")
	}

	// 10. Conflict lifecycle
	resp = doJSON(t, ts, "POST", "/api/v1/admin/conflicts", map[string]any{
		"type":          "knowledge_conflict",
		"knowledge_ids": []string{ids[0]},
		"description":   "Conflicting information found.",
	})
	mustStatus(t, resp, 201)
	var conflictResp handlers.CreateConflictResponse
	decodeJSON(t, resp, &conflictResp)
	conflictID := conflictResp.Id.String()

	// List conflicts
	resp = doJSON(t, ts, "GET", "/api/v1/system/conflicts", nil)
	mustStatus(t, resp, 200)
	var conflicts []handlers.ConflictReport
	decodeJSON(t, resp, &conflicts)
	if len(conflicts) != 1 {
		t.Errorf("expected 1 conflict, got %d", len(conflicts))
	}

	// List open conflicts
	resp = doJSON(t, ts, "GET", "/api/v1/system/conflicts?status=open", nil)
	mustStatus(t, resp, 200)
	var openConflicts []handlers.ConflictReport
	decodeJSON(t, resp, &openConflicts)
	if len(openConflicts) != 1 {
		t.Errorf("expected 1 open conflict, got %d", len(openConflicts))
	}

	// Resolve conflict
	resp = doJSON(t, ts, "POST", "/api/v1/system/conflicts/"+conflictID+"/resolve", map[string]any{
		"resolution": "Reviewed and merged the conflicting entries.",
	})
	mustStatus(t, resp, 200)

	// List resolved conflicts
	resp = doJSON(t, ts, "GET", "/api/v1/system/conflicts?status=resolved", nil)
	mustStatus(t, resp, 200)
	var resolvedConflicts []handlers.ConflictReport
	decodeJSON(t, resp, &resolvedConflicts)
	if len(resolvedConflicts) != 1 {
		t.Errorf("expected 1 resolved conflict, got %d", len(resolvedConflicts))
	}

	// 11. Curation logs
	doJSON(t, ts, "POST", "/api/v1/admin/curation-logs", map[string]any{
		"action":      "merge_supplement",
		"target_id":   ids[0],
		"description": "Applied supplement.",
	})
	resp = doJSON(t, ts, "GET", "/api/v1/system/curation-logs", nil)
	mustStatus(t, resp, 200)
	var logs []handlers.CurationLog
	decodeJSON(t, resp, &logs)
	if len(logs) != 1 {
		t.Errorf("expected 1 curation log, got %d", len(logs))
	}

	// 12. System status after operations
	resp = doJSON(t, ts, "GET", "/api/v1/system/status", nil)
	mustStatus(t, resp, 200)
	var finalStatus handlers.SystemStatus
	decodeJSON(t, resp, &finalStatus)
	if finalStatus.ActiveKnowledge == nil || *finalStatus.ActiveKnowledge != 2 {
		t.Errorf("expected 2 active entries, got %v", finalStatus.ActiveKnowledge)
	}
}

func TestSystemDeleteNotFound(t *testing.T) {
	ts := newTestServer(t)
	resp := doJSON(t, ts, "DELETE", "/api/v1/system/knowledge/00000000-0000-0000-0000-000000000000", nil)
	mustStatus(t, resp, 404)
}

func TestSystemListKnowledgeByTag(t *testing.T) {
	ts := newTestServer(t)

	doJSON(t, ts, "POST", "/api/v1/agent/knowledge", map[string]any{
		"title": "Go Entry", "summary": "s", "body": "b", "tags": []string{"go"},
	})
	doJSON(t, ts, "POST", "/api/v1/agent/knowledge", map[string]any{
		"title": "Python Entry", "summary": "s", "body": "b", "tags": []string{"python"},
	})

	resp := doJSON(t, ts, "GET", "/api/v1/system/knowledge?tag=go", nil)
	mustStatus(t, resp, 200)
	var entries []handlers.KnowledgeEntry
	decodeJSON(t, resp, &entries)
	if len(entries) != 1 {
		t.Errorf("expected 1 entry with tag=go, got %d", len(entries))
	}
}
