package service

import (
	"net/http"

	"github.com/legamerdc/knowledge-hub/internal/server/handlers"
	"github.com/legamerdc/knowledge-hub/pkg/corestore"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

// SystemRecalculateWeights batch recalculates all knowledge weights.
// POST /api/v1/system/recalculate-weights
func (s *KHService) SystemRecalculateWeights(w http.ResponseWriter, r *http.Request) {
	count, err := s.store.RecalculateWeights(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, handlers.RecalculateResponse{UpdatedCount: count})
}

// SystemGetStatus returns the system status overview.
// GET /api/v1/system/status
func (s *KHService) SystemGetStatus(w http.ResponseWriter, r *http.Request) {
	status, err := s.store.GetStatus(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	active := status.ActiveCount
	archived := status.ArchivedCount
	total := active + archived
	tags := status.TagCount
	unprocessed := status.UnprocessedCount
	conflicts := status.OpenConflicts

	writeJSON(w, http.StatusOK, handlers.SystemStatus{
		ActiveKnowledge:     &active,
		ArchivedKnowledge:   &archived,
		TotalKnowledge:      &total,
		TotalTags:           &tags,
		UnprocessedComments: &unprocessed,
		OpenConflicts:       &conflicts,
	})
}

// SystemListKnowledge lists knowledge entries with filters.
// GET /api/v1/system/knowledge
func (s *KHService) SystemListKnowledge(w http.ResponseWriter, r *http.Request, params handlers.SystemListKnowledgeParams) {
	q := corestore.SearchQuery{
		Status:     0, // 0 = no filter (all statuses)
		Limit:      50,
		OrderBy:    "created_at",
		Descending: true,
	}
	if params.Status != nil {
		q.Status = knowledgeStatusToCS(*params.Status)
	}
	if params.Tag != nil {
		q.Tags = []string{*params.Tag}
	}
	if params.Limit != nil {
		q.Limit = *params.Limit
	}
	if params.Offset != nil {
		q.Offset = *params.Offset
	}

	entries, err := s.store.Search(r.Context(), q)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	results := make([]handlers.KnowledgeEntry, 0, len(entries))
	for _, e := range entries {
		results = append(results, toHandlerEntry(e))
	}
	writeJSON(w, http.StatusOK, results)
}

// SystemGetKnowledge reads a knowledge entry without updating access stats.
// GET /api/v1/system/knowledge/{id}
// Note: The current store.GetByID updates access stats as a side effect (MVP trade-off).
func (s *KHService) SystemGetKnowledge(w http.ResponseWriter, r *http.Request, id handlers.KnowledgeID) {
	entry, err := s.store.GetByID(r.Context(), id.String())
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "knowledge not found")
		} else {
			writeError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	writeJSON(w, http.StatusOK, toHandlerEntry(entry))
}

// SystemDeleteKnowledge hard deletes a knowledge entry.
// DELETE /api/v1/system/knowledge/{id}
func (s *KHService) SystemDeleteKnowledge(w http.ResponseWriter, r *http.Request, id handlers.KnowledgeID) {
	if err := s.store.HardDelete(r.Context(), id.String()); err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "knowledge not found")
		} else {
			writeError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	writeJSON(w, http.StatusOK, struct{}{})
}

// SystemRestoreKnowledge restores an archived knowledge entry.
// POST /api/v1/system/knowledge/{id}/restore
func (s *KHService) SystemRestoreKnowledge(w http.ResponseWriter, r *http.Request, id handlers.KnowledgeID) {
	if err := s.store.Restore(r.Context(), id.String()); err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "knowledge not found")
		} else {
			writeError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	writeJSON(w, http.StatusOK, struct{}{})
}

// SystemListConflicts lists conflict reports with optional status filter.
// GET /api/v1/system/conflicts
func (s *KHService) SystemListConflicts(w http.ResponseWriter, r *http.Request, params handlers.SystemListConflictsParams) {
	statusFilter := ""
	if params.Status != nil {
		statusFilter = string(*params.Status)
	}

	reports, err := s.store.ListConflicts(r.Context(), statusFilter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	results := make([]handlers.ConflictReport, 0, len(reports))
	for _, rep := range reports {
		results = append(results, toHandlerConflict(rep))
	}
	writeJSON(w, http.StatusOK, results)
}

// SystemResolveConflict resolves a conflict report.
// POST /api/v1/system/conflicts/{id}/resolve
func (s *KHService) SystemResolveConflict(w http.ResponseWriter, r *http.Request, id openapi_types.UUID) {
	var req handlers.ResolveConflictRequest
	if !decode(w, r, &req) {
		return
	}

	if err := s.store.ResolveConflict(r.Context(), id.String(), req.Resolution); err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "conflict not found")
		} else {
			writeError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	writeJSON(w, http.StatusOK, struct{}{})
}

// SystemListCurationLogs lists curation logs.
// GET /api/v1/system/curation-logs
func (s *KHService) SystemListCurationLogs(w http.ResponseWriter, r *http.Request, params handlers.SystemListCurationLogsParams) {
	limit := 50
	if params.Limit != nil {
		limit = *params.Limit
	}

	logs, err := s.store.ListCurationLogs(r.Context(), limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	results := make([]handlers.CurationLog, 0, len(logs))
	for _, l := range logs {
		results = append(results, toHandlerCurationLog(l))
	}
	writeJSON(w, http.StatusOK, results)
}
