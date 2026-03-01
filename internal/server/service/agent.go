package service

import (
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/legamerdc/knowledge-hub/internal/server/handlers"
	"github.com/legamerdc/knowledge-hub/pkg/corestore"
)

// AgentBrowse implements faceted browsing.
// POST /api/v1/agent/browse
func (s *KHService) AgentBrowse(w http.ResponseWriter, r *http.Request) {
	var req handlers.BrowseRequest
	if !decode(w, r, &req) {
		return
	}

	var selectedTags []string
	if req.SelectedTags != nil {
		selectedTags = *req.SelectedTags
	}

	result, err := s.store.BrowseFacets(r.Context(), selectedTags)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	resp := handlers.BrowseResponse{
		TotalMatches: result.TotalHits,
		Tags:         []handlers.Tag{},
	}
	for _, ft := range result.NextTags {
		count := ft.Count
		resp.Tags = append(resp.Tags, handlers.Tag{Name: ft.Name, Count: &count})
	}

	writeJSON(w, http.StatusOK, resp)
}

// AgentSearch implements knowledge search.
// POST /api/v1/agent/search
func (s *KHService) AgentSearch(w http.ResponseWriter, r *http.Request) {
	var req handlers.SearchRequest
	if !decode(w, r, &req) {
		return
	}

	q := corestore.SearchQuery{
		Status:     corestore.KnowledgeStatusActive,
		Limit:      20,
		OrderBy:    "weight",
		Descending: true,
	}
	if req.Keyword != nil {
		q.Q = *req.Keyword
	}
	if req.Tags != nil {
		q.Tags = *req.Tags
	}

	entries, err := s.store.Search(r.Context(), q)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	results := make([]handlers.SearchResult, 0, len(entries))
	for _, e := range entries {
		results = append(results, toSearchResult(e))
	}
	writeJSON(w, http.StatusOK, results)
}

// AgentReadKnowledge reads a knowledge entry (updates access stats).
// GET /api/v1/agent/knowledge/{id}
func (s *KHService) AgentReadKnowledge(w http.ResponseWriter, r *http.Request, id handlers.KnowledgeID) {
	entry, err := s.store.GetByID(r.Context(), id.String())
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "knowledge not found")
		} else {
			writeError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	if entry.Status != corestore.KnowledgeStatusActive {
		writeError(w, http.StatusNotFound, "knowledge not found")
		return
	}

	// Build comment summary
	comments, err := s.store.GetByKnowledgeID(r.Context(), id.String())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, toHandlerDetail(entry, buildCommentsSummary(comments)))
}

// AgentContribute creates a new knowledge entry.
// POST /api/v1/agent/knowledge
func (s *KHService) AgentContribute(w http.ResponseWriter, r *http.Request) {
	var req handlers.ContributeRequest
	if !decode(w, r, &req) {
		return
	}

	entry := &corestore.KnowledgeEntry{
		Title:   req.Title,
		Summary: req.Summary,
		Body:    req.Body,
		Tags:    req.Tags,
		Weight:  1.0,
	}
	if req.Author != nil {
		entry.Author = *req.Author
	}

	id, err := s.store.Create(r.Context(), entry)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, handlers.ContributeResponse{
		Id: uuid.MustParse(id),
	})
}

// AgentAppendKnowledge appends content to a knowledge entry.
// POST /api/v1/agent/knowledge/{id}/append
func (s *KHService) AgentAppendKnowledge(w http.ResponseWriter, r *http.Request, id handlers.KnowledgeID) {
	var req handlers.AppendRequest
	if !decode(w, r, &req) {
		return
	}
	if !req.Type.Valid() {
		writeError(w, http.StatusBadRequest, "invalid append type")
		return
	}

	appendType := strings.ToLower(string(req.Type))
	if err := s.store.Append(r.Context(), id.String(), appendType, req.Content); err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "knowledge not found")
		} else {
			writeError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}

	// Fetch updated entry to get new append_count.
	// Note: GetByID also increments access_count; this is an acceptable MVP trade-off.
	entry, err := s.store.GetByID(r.Context(), id.String())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, handlers.AppendResponse{
		Id:          id,
		AppendCount: entry.AppendCount,
	})
}

// AgentComment adds a comment to a knowledge entry.
// POST /api/v1/agent/knowledge/{id}/comments
func (s *KHService) AgentComment(w http.ResponseWriter, r *http.Request, id handlers.KnowledgeID) {
	var req handlers.CommentRequest
	if !decode(w, r, &req) {
		return
	}
	if !req.Type.Valid() {
		writeError(w, http.StatusBadRequest, "invalid comment type")
		return
	}

	// Verify the knowledge exists and is active
	entry, err := s.store.GetByID(r.Context(), id.String())
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "knowledge not found")
		} else {
			writeError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	if entry.Status != corestore.KnowledgeStatusActive {
		writeError(w, http.StatusNotFound, "knowledge not found")
		return
	}

	comment := &corestore.Comment{
		KnowledgeID: id.String(),
		Type:        commentTypeToCS(req.Type),
		Content:     req.Content,
		Reasoning:   req.Reasoning,
		CreatedAt:   time.Now().UTC(),
	}
	if req.Author != nil {
		comment.Author = *req.Author
	}
	if req.Scenario != nil {
		comment.Scenario = *req.Scenario
	}

	cid, err := s.store.AddComment(r.Context(), comment)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, handlers.CommentResponse{
		Id: uuid.MustParse(cid),
	})
}
