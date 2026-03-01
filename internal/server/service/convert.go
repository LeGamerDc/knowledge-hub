package service

import (
	"github.com/google/uuid"
	"github.com/legamerdc/knowledge-hub/internal/server/handlers"
	"github.com/legamerdc/knowledge-hub/pkg/corestore"
)

// — Comment type conversion —

func commentTypeToCS(t handlers.CommentType) int {
	switch t {
	case handlers.CommentTypeSuccess:
		return corestore.CommentTypeSuccess
	case handlers.CommentTypeFailure:
		return corestore.CommentTypeFailure
	case handlers.CommentTypeSupplement:
		return corestore.CommentTypeSupplement
	case handlers.CommentTypeCorrection:
		return corestore.CommentTypeCorrection
	default:
		return corestore.CommentTypeSuccess
	}
}

func commentTypeToHandler(t int) handlers.CommentType {
	switch t {
	case corestore.CommentTypeSuccess:
		return handlers.CommentTypeSuccess
	case corestore.CommentTypeFailure:
		return handlers.CommentTypeFailure
	case corestore.CommentTypeSupplement:
		return handlers.CommentTypeSupplement
	case corestore.CommentTypeCorrection:
		return handlers.CommentTypeCorrection
	default:
		return handlers.CommentTypeSuccess
	}
}

// — KnowledgeStatus conversion —

func knowledgeStatusToCS(s handlers.KnowledgeStatus) int {
	if s == handlers.Active {
		return corestore.KnowledgeStatusActive
	}
	return corestore.KnowledgeStatusArchived
}

func knowledgeStatusToHandler(s int) handlers.KnowledgeStatus {
	if s == corestore.KnowledgeStatusActive {
		return handlers.Active
	}
	return handlers.Archived
}

// — ConflictType conversion — (corestore has no constants; 1=correction, 2=knowledge)

const (
	csConflictTypeCorrection = 1
	csConflictTypeKnowledge  = 2
)

func conflictTypeToCS(t handlers.ConflictType) int {
	if t == handlers.CorrectionConflict {
		return csConflictTypeCorrection
	}
	return csConflictTypeKnowledge
}

func conflictTypeToHandler(t int) handlers.ConflictType {
	if t == csConflictTypeCorrection {
		return handlers.CorrectionConflict
	}
	return handlers.KnowledgeConflict
}

// — ConflictStatus conversion —

func conflictStatusToHandler(s int) handlers.ConflictStatus {
	if s == corestore.ConflictStatusOpen {
		return handlers.Open
	}
	return handlers.Resolved
}

// — CurationAction conversion —

func curationActionToCS(a handlers.CurationAction) int {
	switch a {
	case handlers.MergeSupplement:
		return corestore.CurationMergeSupplement
	case handlers.ApplyCorrection:
		return corestore.CurationApplyCorrection
	case handlers.Downgrade:
		return corestore.CurationDowngrade
	case handlers.Archive:
		return corestore.CurationArchive
	case handlers.MergeTags:
		return corestore.CurationMergeTags
	case handlers.MergeKnowledge:
		return corestore.CurationMergeKnowledge
	case handlers.CreateConflict:
		return corestore.CurationCreateConflict
	default:
		return corestore.CurationMergeSupplement
	}
}

func curationActionToHandler(a int) handlers.CurationAction {
	switch a {
	case corestore.CurationMergeSupplement:
		return handlers.MergeSupplement
	case corestore.CurationApplyCorrection:
		return handlers.ApplyCorrection
	case corestore.CurationDowngrade:
		return handlers.Downgrade
	case corestore.CurationArchive:
		return handlers.Archive
	case corestore.CurationMergeTags:
		return handlers.MergeTags
	case corestore.CurationMergeKnowledge:
		return handlers.MergeKnowledge
	case corestore.CurationCreateConflict:
		return handlers.CreateConflict
	default:
		return handlers.MergeSupplement
	}
}

// — KnowledgeEntry → handlers types —

func toHandlerEntry(e *corestore.KnowledgeEntry) handlers.KnowledgeEntry {
	id := uuid.MustParse(e.ID)
	w := e.Weight
	ac := e.AccessCount
	appc := e.AppendCount
	nr := e.NeedsRewrite
	s := e.Summary
	b := e.Body
	a := e.Author
	aa := e.AccessedAt
	tags := append([]string(nil), e.Tags...)
	status := knowledgeStatusToHandler(e.Status)
	return handlers.KnowledgeEntry{
		Id:           id,
		Title:        e.Title,
		Summary:      &s,
		Body:         &b,
		Author:       &a,
		Weight:       &w,
		Status:       status,
		AccessCount:  &ac,
		AppendCount:  &appc,
		NeedsRewrite: &nr,
		CreatedAt:    e.CreatedAt,
		UpdatedAt:    e.UpdatedAt,
		AccessedAt:   &aa,
		Tags:         &tags,
	}
}

func toHandlerDetail(e *corestore.KnowledgeEntry, summary *handlers.CommentsSummary) handlers.KnowledgeDetail {
	id := uuid.MustParse(e.ID)
	w := e.Weight
	s := e.Summary
	b := e.Body
	a := e.Author
	tags := append([]string(nil), e.Tags...)
	return handlers.KnowledgeDetail{
		Id:              id,
		Title:           e.Title,
		Summary:         &s,
		Body:            &b,
		Author:          &a,
		Weight:          &w,
		CommentsSummary: summary,
		CreatedAt:       e.CreatedAt,
		UpdatedAt:       e.UpdatedAt,
		Tags:            &tags,
	}
}

func toSearchResult(e *corestore.KnowledgeEntry) handlers.SearchResult {
	id := uuid.MustParse(e.ID)
	w := e.Weight
	s := e.Summary
	tags := append([]string(nil), e.Tags...)
	return handlers.SearchResult{
		Id:      id,
		Title:   e.Title,
		Summary: &s,
		Weight:  &w,
		Tags:    &tags,
	}
}

// — Comment → handlers.Comment —

func toHandlerComment(c *corestore.Comment) handlers.Comment {
	id := uuid.MustParse(c.ID)
	kid := uuid.MustParse(c.KnowledgeID)
	processed := c.Processed
	hc := handlers.Comment{
		Id:          id,
		KnowledgeId: kid,
		Type:        commentTypeToHandler(c.Type),
		Content:     c.Content,
		Reasoning:   c.Reasoning,
		Processed:   &processed,
		ProcessedAt: c.ProcessedAt,
		CreatedAt:   c.CreatedAt,
	}
	if c.Author != "" {
		a := c.Author
		hc.Author = &a
	}
	if c.Scenario != "" {
		sc := c.Scenario
		hc.Scenario = &sc
	}
	return hc
}

// — ConflictReport → handlers.ConflictReport —

func toHandlerConflict(r *corestore.ConflictReport) handlers.ConflictReport {
	id := uuid.MustParse(r.ID)
	hc := handlers.ConflictReport{
		Id:          id,
		Type:        conflictTypeToHandler(r.Type),
		Description: r.Description,
		Status:      conflictStatusToHandler(r.Status),
		CreatedAt:   r.CreatedAt,
		ResolvedAt:  r.ResolvedAt,
	}
	if r.Resolution != "" {
		res := r.Resolution
		hc.Resolution = &res
	}
	if len(r.KnowledgeIDs) > 0 {
		kids := append([]string(nil), r.KnowledgeIDs...)
		hc.KnowledgeIds = &kids
	}
	if len(r.CommentIDs) > 0 {
		cids := append([]string(nil), r.CommentIDs...)
		hc.CommentIds = &cids
	}
	return hc
}

// — CurationLog → handlers.CurationLog —

func toHandlerCurationLog(l *corestore.CurationLog) handlers.CurationLog {
	id := uuid.MustParse(l.ID)
	hl := handlers.CurationLog{
		Id:          id,
		Action:      curationActionToHandler(l.Action),
		TargetId:    l.TargetID,
		Description: l.Description,
		CreatedAt:   l.CreatedAt,
	}
	if l.AgentID != "" {
		a := l.AgentID
		hl.AgentId = &a
	}
	if l.Diff != "" {
		d := l.Diff
		hl.Diff = &d
	}
	if len(l.SourceIDs) > 0 {
		sids := append([]string(nil), l.SourceIDs...)
		hl.SourceIds = &sids
	}
	return hl
}

// — FlaggedEntry → handlers.FlaggedEntry —

func toHandlerFlagged(f *corestore.FlaggedEntry) handlers.FlaggedEntry {
	id := uuid.MustParse(f.Entry.ID)
	w := f.Entry.Weight
	nr := f.Entry.NeedsRewrite

	unprocessed := 0
	failure := f.FailureCount
	total := len(f.RecentComments)
	for _, c := range f.RecentComments {
		if !c.Processed {
			unprocessed++
		}
	}

	hf := handlers.FlaggedEntry{
		Id:           id,
		Title:        f.Entry.Title,
		FlagReasons:  f.FlagReasons,
		Weight:       &w,
		NeedsRewrite: &nr,
		CommentStats: &handlers.CommentStats{
			Total:       &total,
			Unprocessed: &unprocessed,
			Failure:     &failure,
		},
	}
	return hf
}

// — buildCommentsSummary counts comments by type —

func buildCommentsSummary(comments []*corestore.Comment) *handlers.CommentsSummary {
	s := 0
	f := 0
	sup := 0
	cor := 0
	for _, c := range comments {
		switch c.Type {
		case corestore.CommentTypeSuccess:
			s++
		case corestore.CommentTypeFailure:
			f++
		case corestore.CommentTypeSupplement:
			sup++
		case corestore.CommentTypeCorrection:
			cor++
		}
	}
	return &handlers.CommentsSummary{
		Success:    &s,
		Failure:    &f,
		Supplement: &sup,
		Correction: &cor,
	}
}
