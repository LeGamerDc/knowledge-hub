package service

import (
	"net/http"

	"github.com/google/uuid"
	"github.com/legamerdc/knowledge-hub/internal/server/handlers"
	"github.com/legamerdc/knowledge-hub/pkg/corestore"
)

// AdminListFlagged lists knowledge entries needing review.
// GET /api/v1/admin/flagged
func (s *KHService) AdminListFlagged(w http.ResponseWriter, r *http.Request) {
	flagged, err := s.store.ListFlagged(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	results := make([]handlers.FlaggedEntry, 0, len(flagged))
	for _, f := range flagged {
		results = append(results, toHandlerFlagged(f))
	}
	writeJSON(w, http.StatusOK, results)
}

// AdminTagHealth returns a tag health report.
// GET /api/v1/admin/tags/health
func (s *KHService) AdminTagHealth(w http.ResponseWriter, r *http.Request) {
	report, err := s.store.GetTagHealth(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	resp := handlers.TagHealthReport{}

	if len(report.SynonymPairs) > 0 {
		pairs := make([]handlers.SimilarTagPair, 0, len(report.SynonymPairs))
		for _, p := range report.SynonymPairs {
			reason := "edit_distance"
			if p.Distance == 0 {
				reason = "alias"
			}
			pairs = append(pairs, handlers.SimilarTagPair{
				Tags:   []string{p.TagA.Name, p.TagB.Name},
				Reason: reason,
			})
		}
		resp.SimilarPairs = &pairs
	}

	if len(report.LowFreqTags) > 0 {
		lf := make([]handlers.LowFreqTag, 0, len(report.LowFreqTags))
		for _, t := range report.LowFreqTags {
			lf = append(lf, handlers.LowFreqTag{Name: t.Name, Frequency: t.Frequency})
		}
		resp.LowFreq = &lf
	}

	if len(report.HighFreqTags) > 0 {
		hf := make([]handlers.HighFreqTag, 0, len(report.HighFreqTags))
		for _, t := range report.HighFreqTags {
			hf = append(hf, handlers.HighFreqTag{
				Name:      t.Name,
				Frequency: t.Frequency,
				Share:     0, // computed below if needed
			})
		}
		resp.HighFreq = &hf
	}

	writeJSON(w, http.StatusOK, resp)
}

// AdminFindSimilar finds similar knowledge pairs by tag overlap.
// GET /api/v1/admin/knowledge/similar
func (s *KHService) AdminFindSimilar(w http.ResponseWriter, r *http.Request) {
	pairs, err := s.store.FindSimilar(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	results := make([]handlers.SimilarPair, 0, len(pairs))
	for _, p := range pairs {
		results = append(results, handlers.SimilarPair{
			IdA:          uuid.MustParse(p.EntryA.ID),
			TitleA:       p.EntryA.Title,
			IdB:          uuid.MustParse(p.EntryB.ID),
			TitleB:       p.EntryB.Title,
			OverlapTags:  p.SharedTags,
			OverlapRatio: p.Overlap,
		})
	}
	writeJSON(w, http.StatusOK, results)
}

// AdminGetReview returns review details for a knowledge entry.
// GET /api/v1/admin/knowledge/{id}/review
func (s *KHService) AdminGetReview(w http.ResponseWriter, r *http.Request, id handlers.KnowledgeID) {
	entry, err := s.store.GetByID(r.Context(), id.String())
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "knowledge not found")
		} else {
			writeError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}

	unprocessed, err := s.store.GetUnprocessed(r.Context(), id.String())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	hComments := make([]handlers.Comment, 0, len(unprocessed))
	for _, c := range unprocessed {
		hComments = append(hComments, toHandlerComment(c))
	}

	writeJSON(w, http.StatusOK, handlers.ReviewData{
		Knowledge:           toHandlerEntry(entry),
		UnprocessedComments: hComments,
	})
}

// AdminUpdateKnowledge performs a full update of a knowledge entry.
// PUT /api/v1/admin/knowledge/{id}
func (s *KHService) AdminUpdateKnowledge(w http.ResponseWriter, r *http.Request, id handlers.KnowledgeID) {
	var req handlers.UpdateKnowledgeRequest
	if !decode(w, r, &req) {
		return
	}

	fields := corestore.UpdateFields{
		Title:   &req.Title,
		Summary: &req.Summary,
		Body:    &req.Body,
		Tags:    req.Tags,
	}

	if err := s.store.Update(r.Context(), id.String(), fields); err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "knowledge not found")
		} else {
			writeError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}

	// Fetch updated entry to get updated_at timestamp.
	entry, err := s.store.GetByID(r.Context(), id.String())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, handlers.UpdateResponse{UpdatedAt: entry.UpdatedAt})
}

// AdminArchive archives a knowledge entry (soft delete).
// POST /api/v1/admin/knowledge/{id}/archive
func (s *KHService) AdminArchive(w http.ResponseWriter, r *http.Request, id handlers.KnowledgeID) {
	if err := s.store.Archive(r.Context(), id.String()); err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "knowledge not found")
		} else {
			writeError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	writeJSON(w, http.StatusOK, struct{}{})
}

// AdminMarkProcessed batch marks comments as processed.
// POST /api/v1/admin/comments/processed
func (s *KHService) AdminMarkProcessed(w http.ResponseWriter, r *http.Request) {
	var req handlers.MarkProcessedRequest
	if !decode(w, r, &req) {
		return
	}

	ids := make([]string, len(req.CommentIds))
	for i, cid := range req.CommentIds {
		ids[i] = cid.String()
	}

	if err := s.store.MarkProcessed(r.Context(), ids); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, struct{}{})
}

// AdminMergeTags merges source tags into a target tag.
// POST /api/v1/admin/tags/merge
func (s *KHService) AdminMergeTags(w http.ResponseWriter, r *http.Request) {
	var req handlers.MergeTagsRequest
	if !decode(w, r, &req) {
		return
	}

	count, err := s.store.MergeTags(r.Context(), req.Target, req.Sources)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, handlers.MergeTagsResponse{AffectedKnowledgeCount: count})
}

// AdminMergeKnowledge merges knowledge entries: target updated, sources archived.
// POST /api/v1/admin/knowledge/merge
func (s *KHService) AdminMergeKnowledge(w http.ResponseWriter, r *http.Request) {
	var req handlers.MergeKnowledgeRequest
	if !decode(w, r, &req) {
		return
	}

	targetID := req.TargetId.String()

	// Verify target exists
	if _, err := s.store.GetByID(r.Context(), targetID); err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "target knowledge not found")
		} else {
			writeError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}

	// Update target with merged content
	fields := corestore.UpdateFields{
		Summary: &req.MergedSummary,
		Body:    &req.MergedBody,
	}
	if err := s.store.Update(r.Context(), targetID, fields); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Archive all source entries
	for _, srcID := range req.SourceIds {
		if err := s.store.Archive(r.Context(), srcID.String()); err != nil {
			if !isNotFound(err) {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
		}
	}

	writeJSON(w, http.StatusOK, handlers.MergeKnowledgeResponse{Id: req.TargetId})
}

// AdminCreateConflict creates a conflict report.
// POST /api/v1/admin/conflicts
func (s *KHService) AdminCreateConflict(w http.ResponseWriter, r *http.Request) {
	var req handlers.CreateConflictRequest
	if !decode(w, r, &req) {
		return
	}

	kidStrs := make([]string, len(req.KnowledgeIds))
	for i, kid := range req.KnowledgeIds {
		kidStrs[i] = kid.String()
	}
	var cidStrs []string
	if req.CommentIds != nil {
		for _, cid := range *req.CommentIds {
			cidStrs = append(cidStrs, cid.String())
		}
	}

	report := &corestore.ConflictReport{
		Type:         conflictTypeToCS(req.Type),
		KnowledgeIDs: kidStrs,
		CommentIDs:   cidStrs,
		Description:  req.Description,
	}

	id, err := s.store.CreateConflict(r.Context(), report)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, handlers.CreateConflictResponse{Id: uuid.MustParse(id)})
}

// AdminLogCuration writes a curation log entry.
// POST /api/v1/admin/curation-logs
func (s *KHService) AdminLogCuration(w http.ResponseWriter, r *http.Request) {
	var req handlers.CurationLogRequest
	if !decode(w, r, &req) {
		return
	}

	log := &corestore.CurationLog{
		Action:      curationActionToCS(req.Action),
		TargetID:    req.TargetId,
		Description: req.Description,
	}
	if req.Diff != nil {
		log.Diff = *req.Diff
	}
	if req.SourceIds != nil {
		log.SourceIDs = *req.SourceIds
	}

	id, err := s.store.LogCuration(r.Context(), log)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, handlers.CurationLogResponse{Id: uuid.MustParse(id)})
}
