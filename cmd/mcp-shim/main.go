package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/google/uuid"
	"github.com/legamerdc/knowledge-hub/pkg/khclient"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

func main() {
	serverURL := os.Getenv("KH_SERVER_URL")
	if serverURL == "" {
		serverURL = "http://localhost:19820"
	}

	client, err := khclient.NewClient(serverURL)
	if err != nil {
		log.Fatalf("failed to create API client: %v", err)
	}

	s := mcp.NewServer(&mcp.Implementation{
		Name:    "knowledge-hub",
		Version: "1.0.0",
	}, nil)

	registerTools(s, client)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if _, err := s.Connect(ctx, &mcp.StdioTransport{}, nil); err != nil {
		log.Fatalf("MCP server error: %v", err)
	}

	<-ctx.Done()
}

// readJSON decodes HTTP response body into target struct.
func readJSON(resp *http.Response, target any) error {
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API error %d: %s", resp.StatusCode, string(body))
	}
	return json.NewDecoder(resp.Body).Decode(target)
}

// textResult returns a CallToolResult with JSON-encoded text content.
func textResult(v any) (*mcp.CallToolResult, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, err
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(b)}},
	}, nil
}

// errResult returns a tool error result.
func errResult(err error) (*mcp.CallToolResult, error) {
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
	}, nil
}

func registerTools(s *mcp.Server, c *khclient.Client) {
	// ── Working Agent Tools ──────────────────────────────────────────────────

	// kh_browse: Faceted tag browsing. Returns matching tags + total_matches.
	s.AddTool(&mcp.Tool{
		Name:        "kh_browse",
		Description: "Browse knowledge entries by tags. Pass selected_tags to filter; omit to get all top-level tags and entry count.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"selected_tags": {
					"type": "array",
					"items": {"type": "string"},
					"description": "Tags to filter by (optional)"
				}
			}
		}`),
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args struct {
			SelectedTags *[]string `json:"selected_tags"`
		}
		if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
			return errResult(fmt.Errorf("invalid arguments: %w", err))
		}
		resp, err := c.AgentBrowse(ctx, khclient.BrowseRequest{SelectedTags: args.SelectedTags})
		if err != nil {
			return errResult(fmt.Errorf("API request failed: %w", err))
		}
		var result khclient.BrowseResponse
		if err := readJSON(resp, &result); err != nil {
			return errResult(err)
		}
		return textResult(result)
	})

	// kh_search: Full-text + tag search. Returns knowledge summary list.
	s.AddTool(&mcp.Tool{
		Name:        "kh_search",
		Description: "Search knowledge entries by tags and/or keyword. Returns a ranked list of matching entries with title, summary, tags, and weight.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"tags": {
					"type": "array",
					"items": {"type": "string"},
					"description": "Filter by these tags (optional)"
				},
				"keyword": {
					"type": "string",
					"description": "Full-text keyword to search in title, summary, and body (optional)"
				}
			}
		}`),
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args struct {
			Tags    *[]string `json:"tags"`
			Keyword *string   `json:"keyword"`
		}
		if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
			return errResult(fmt.Errorf("invalid arguments: %w", err))
		}
		resp, err := c.AgentSearch(ctx, khclient.SearchRequest{
			Tags:    args.Tags,
			Keyword: args.Keyword,
		})
		if err != nil {
			return errResult(fmt.Errorf("API request failed: %w", err))
		}
		var results []khclient.SearchResult
		if err := readJSON(resp, &results); err != nil {
			return errResult(err)
		}
		return textResult(results)
	})

	// kh_read_full: Read full knowledge entry by ID.
	s.AddTool(&mcp.Tool{
		Name:        "kh_read_full",
		Description: "Read the full content of a knowledge entry by its ID. Returns title, summary, body, tags, author, and comment summary.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"required": ["id"],
			"properties": {
				"id": {
					"type": "string",
					"format": "uuid",
					"description": "Knowledge entry UUID"
				}
			}
		}`),
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
			return errResult(fmt.Errorf("invalid arguments: %w", err))
		}
		id, err := uuid.Parse(args.ID)
		if err != nil {
			return errResult(fmt.Errorf("invalid UUID: %w", err))
		}
		resp, err := c.AgentReadKnowledge(ctx, id)
		if err != nil {
			return errResult(fmt.Errorf("API request failed: %w", err))
		}
		var result khclient.KnowledgeDetail
		if err := readJSON(resp, &result); err != nil {
			return errResult(err)
		}
		return textResult(result)
	})

	// kh_contribute: Submit new knowledge entry.
	s.AddTool(&mcp.Tool{
		Name:        "kh_contribute",
		Description: "Contribute a new knowledge entry to the hub. Returns the new entry's UUID.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"required": ["title", "summary", "body", "tags"],
			"properties": {
				"title": {"type": "string", "description": "Short, descriptive title"},
				"summary": {"type": "string", "description": "One-paragraph summary of the knowledge"},
				"body": {"type": "string", "description": "Full Markdown body of the knowledge entry"},
				"tags": {
					"type": "array",
					"items": {"type": "string"},
					"description": "Categorization tags (at least one recommended)"
				},
				"author": {"type": "string", "description": "Agent or human author identifier (optional)"}
			}
		}`),
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args struct {
			Title   string    `json:"title"`
			Summary string    `json:"summary"`
			Body    string    `json:"body"`
			Tags    []string  `json:"tags"`
			Author  *string   `json:"author"`
		}
		if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
			return errResult(fmt.Errorf("invalid arguments: %w", err))
		}
		resp, err := c.AgentContribute(ctx, khclient.ContributeRequest{
			Title:   args.Title,
			Summary: args.Summary,
			Body:    args.Body,
			Tags:    args.Tags,
			Author:  args.Author,
		})
		if err != nil {
			return errResult(fmt.Errorf("API request failed: %w", err))
		}
		var result khclient.ContributeResponse
		if err := readJSON(resp, &result); err != nil {
			return errResult(err)
		}
		return textResult(result)
	})

	// kh_append_knowledge: Append supplement or correction to existing entry.
	s.AddTool(&mcp.Tool{
		Name:        "kh_append_knowledge",
		Description: "Append a supplement or correction to an existing knowledge entry. Returns the entry ID and updated append count.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"required": ["id", "type", "content"],
			"properties": {
				"id": {"type": "string", "format": "uuid", "description": "Target knowledge entry UUID"},
				"type": {
					"type": "string",
					"enum": ["supplement", "correction"],
					"description": "supplement: adds new information; correction: fixes an error"
				},
				"content": {"type": "string", "description": "Markdown content to append"}
			}
		}`),
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args struct {
			ID      string `json:"id"`
			Type    string `json:"type"`
			Content string `json:"content"`
		}
		if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
			return errResult(fmt.Errorf("invalid arguments: %w", err))
		}
		id, err := uuid.Parse(args.ID)
		if err != nil {
			return errResult(fmt.Errorf("invalid UUID: %w", err))
		}
		resp, err := c.AgentAppendKnowledge(ctx, id, khclient.AppendRequest{
			Type:    khclient.AppendType(args.Type),
			Content: args.Content,
		})
		if err != nil {
			return errResult(fmt.Errorf("API request failed: %w", err))
		}
		var result khclient.AppendResponse
		if err := readJSON(resp, &result); err != nil {
			return errResult(err)
		}
		return textResult(result)
	})

	// kh_comment: Add feedback comment to a knowledge entry.
	s.AddTool(&mcp.Tool{
		Name:        "kh_comment",
		Description: "Add a feedback comment to a knowledge entry. Use 'success' or 'failure' to report usage outcomes; 'supplement' or 'correction' to suggest improvements.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"required": ["knowledge_id", "type", "content", "reasoning"],
			"properties": {
				"knowledge_id": {"type": "string", "format": "uuid", "description": "Target knowledge entry UUID"},
				"type": {
					"type": "string",
					"enum": ["success", "failure", "supplement", "correction"],
					"description": "Type of feedback"
				},
				"content": {"type": "string", "description": "Feedback content"},
				"reasoning": {"type": "string", "description": "Why this feedback is being submitted"},
				"scenario": {"type": "string", "description": "The context or task where this knowledge was applied (optional)"},
				"author": {"type": "string", "description": "Agent or human author identifier (optional)"}
			}
		}`),
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args struct {
			KnowledgeID string  `json:"knowledge_id"`
			Type        string  `json:"type"`
			Content     string  `json:"content"`
			Reasoning   string  `json:"reasoning"`
			Scenario    *string `json:"scenario"`
			Author      *string `json:"author"`
		}
		if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
			return errResult(fmt.Errorf("invalid arguments: %w", err))
		}
		id, err := uuid.Parse(args.KnowledgeID)
		if err != nil {
			return errResult(fmt.Errorf("invalid UUID: %w", err))
		}
		resp, err := c.AgentComment(ctx, id, khclient.CommentRequest{
			Type:      khclient.CommentType(args.Type),
			Content:   args.Content,
			Reasoning: args.Reasoning,
			Scenario:  args.Scenario,
			Author:    args.Author,
		})
		if err != nil {
			return errResult(fmt.Errorf("API request failed: %w", err))
		}
		var result khclient.CommentResponse
		if err := readJSON(resp, &result); err != nil {
			return errResult(err)
		}
		return textResult(result)
	})

	// ── Curation Agent Tools ─────────────────────────────────────────────────

	// kh_list_flagged: List entries that need curation attention.
	s.AddTool(&mcp.Tool{
		Name:        "kh_list_flagged",
		Description: "List knowledge entries flagged for curation: stale access, high failure rate, unprocessed corrections, needs_rewrite flag, or eviction candidates.",
		InputSchema: json.RawMessage(`{"type": "object", "properties": {}}`),
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		resp, err := c.AdminListFlagged(ctx)
		if err != nil {
			return errResult(fmt.Errorf("API request failed: %w", err))
		}
		var results []khclient.FlaggedEntry
		if err := readJSON(resp, &results); err != nil {
			return errResult(err)
		}
		return textResult(results)
	})

	// kh_tag_health: Analyze tag quality.
	s.AddTool(&mcp.Tool{
		Name:        "kh_tag_health",
		Description: "Analyze tag health: returns low-frequency tags (candidates for merge/removal), high-frequency tags (candidates for split), and similar tag pairs (possible duplicates).",
		InputSchema: json.RawMessage(`{"type": "object", "properties": {}}`),
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		resp, err := c.AdminTagHealth(ctx)
		if err != nil {
			return errResult(fmt.Errorf("API request failed: %w", err))
		}
		var result khclient.TagHealthReport
		if err := readJSON(resp, &result); err != nil {
			return errResult(err)
		}
		return textResult(result)
	})

	// kh_find_similar: Find similar knowledge entry pairs.
	s.AddTool(&mcp.Tool{
		Name:        "kh_find_similar",
		Description: "Find pairs of knowledge entries with high tag overlap that are candidates for merging.",
		InputSchema: json.RawMessage(`{"type": "object", "properties": {}}`),
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		resp, err := c.AdminFindSimilar(ctx)
		if err != nil {
			return errResult(fmt.Errorf("API request failed: %w", err))
		}
		var results []khclient.SimilarPair
		if err := readJSON(resp, &results); err != nil {
			return errResult(err)
		}
		return textResult(results)
	})

	// kh_get_review: Get full review data for a knowledge entry.
	s.AddTool(&mcp.Tool{
		Name:        "kh_get_review",
		Description: "Get a knowledge entry along with all its unprocessed comments for curation review.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"required": ["id"],
			"properties": {
				"id": {"type": "string", "format": "uuid", "description": "Knowledge entry UUID to review"}
			}
		}`),
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
			return errResult(fmt.Errorf("invalid arguments: %w", err))
		}
		id, err := uuid.Parse(args.ID)
		if err != nil {
			return errResult(fmt.Errorf("invalid UUID: %w", err))
		}
		resp, err := c.AdminGetReview(ctx, id)
		if err != nil {
			return errResult(fmt.Errorf("API request failed: %w", err))
		}
		var result khclient.ReviewData
		if err := readJSON(resp, &result); err != nil {
			return errResult(err)
		}
		return textResult(result)
	})

	// kh_update_knowledge: Update a knowledge entry's content.
	s.AddTool(&mcp.Tool{
		Name:        "kh_update_knowledge",
		Description: "Update the title, summary, body, and tags of a knowledge entry (full replacement).",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"required": ["id", "title", "summary", "body", "tags"],
			"properties": {
				"id": {"type": "string", "format": "uuid", "description": "Knowledge entry UUID"},
				"title": {"type": "string"},
				"summary": {"type": "string"},
				"body": {"type": "string", "description": "Full Markdown body"},
				"tags": {"type": "array", "items": {"type": "string"}}
			}
		}`),
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args struct {
			ID      string   `json:"id"`
			Title   string   `json:"title"`
			Summary string   `json:"summary"`
			Body    string   `json:"body"`
			Tags    []string `json:"tags"`
		}
		if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
			return errResult(fmt.Errorf("invalid arguments: %w", err))
		}
		id, err := uuid.Parse(args.ID)
		if err != nil {
			return errResult(fmt.Errorf("invalid UUID: %w", err))
		}
		resp, err := c.AdminUpdateKnowledge(ctx, id, khclient.UpdateKnowledgeRequest{
			Title:   args.Title,
			Summary: args.Summary,
			Body:    args.Body,
			Tags:    args.Tags,
		})
		if err != nil {
			return errResult(fmt.Errorf("API request failed: %w", err))
		}
		var result khclient.UpdateResponse
		if err := readJSON(resp, &result); err != nil {
			return errResult(err)
		}
		return textResult(result)
	})

	// kh_archive: Archive a knowledge entry.
	s.AddTool(&mcp.Tool{
		Name:        "kh_archive",
		Description: "Archive a knowledge entry (soft-delete). Archived entries are hidden from Agent search but preserved for audit.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"required": ["id"],
			"properties": {
				"id": {"type": "string", "format": "uuid", "description": "Knowledge entry UUID to archive"}
			}
		}`),
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
			return errResult(fmt.Errorf("invalid arguments: %w", err))
		}
		id, err := uuid.Parse(args.ID)
		if err != nil {
			return errResult(fmt.Errorf("invalid UUID: %w", err))
		}
		resp, err := c.AdminArchive(ctx, id)
		if err != nil {
			return errResult(fmt.Errorf("API request failed: %w", err))
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 400 {
			body, _ := io.ReadAll(resp.Body)
			return errResult(fmt.Errorf("API error %d: %s", resp.StatusCode, string(body)))
		}
		return textResult(map[string]string{"status": "archived", "id": args.ID})
	})

	// kh_mark_processed: Mark comments as processed.
	s.AddTool(&mcp.Tool{
		Name:        "kh_mark_processed",
		Description: "Mark one or more comments as processed after the curation agent has reviewed and acted on them.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"required": ["comment_ids"],
			"properties": {
				"comment_ids": {
					"type": "array",
					"items": {"type": "string", "format": "uuid"},
					"description": "List of comment UUIDs to mark as processed"
				}
			}
		}`),
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args struct {
			CommentIDs []string `json:"comment_ids"`
		}
		if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
			return errResult(fmt.Errorf("invalid arguments: %w", err))
		}
		ids := make([]openapi_types.UUID, 0, len(args.CommentIDs))
		for _, s := range args.CommentIDs {
			id, err := uuid.Parse(s)
			if err != nil {
				return errResult(fmt.Errorf("invalid UUID %q: %w", s, err))
			}
			ids = append(ids, id)
		}
		resp, err := c.AdminMarkProcessed(ctx, khclient.MarkProcessedRequest{CommentIds: ids})
		if err != nil {
			return errResult(fmt.Errorf("API request failed: %w", err))
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 400 {
			body, _ := io.ReadAll(resp.Body)
			return errResult(fmt.Errorf("API error %d: %s", resp.StatusCode, string(body)))
		}
		return textResult(map[string]any{"processed_count": len(ids)})
	})

	// kh_merge_tags: Merge multiple tags into one canonical tag.
	s.AddTool(&mcp.Tool{
		Name:        "kh_merge_tags",
		Description: "Merge source tags into a target tag. All knowledge entries using source tags will be updated to use the target tag.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"required": ["sources", "target"],
			"properties": {
				"sources": {
					"type": "array",
					"items": {"type": "string"},
					"description": "Tag names to merge (will be removed)"
				},
				"target": {"type": "string", "description": "Canonical tag name to merge into"}
			}
		}`),
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args struct {
			Sources []string `json:"sources"`
			Target  string   `json:"target"`
		}
		if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
			return errResult(fmt.Errorf("invalid arguments: %w", err))
		}
		resp, err := c.AdminMergeTags(ctx, khclient.MergeTagsRequest{
			Sources: args.Sources,
			Target:  args.Target,
		})
		if err != nil {
			return errResult(fmt.Errorf("API request failed: %w", err))
		}
		var result khclient.MergeTagsResponse
		if err := readJSON(resp, &result); err != nil {
			return errResult(err)
		}
		return textResult(result)
	})

	// kh_merge_knowledge: Merge multiple knowledge entries into one.
	s.AddTool(&mcp.Tool{
		Name:        "kh_merge_knowledge",
		Description: "Merge multiple knowledge entries into a single target entry with combined content. Source entries are archived after merge.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"required": ["target_id", "source_ids", "merged_summary", "merged_body"],
			"properties": {
				"target_id": {"type": "string", "format": "uuid", "description": "UUID of the entry to keep as the merge target"},
				"source_ids": {
					"type": "array",
					"items": {"type": "string", "format": "uuid"},
					"description": "UUIDs of entries to merge into the target (will be archived)"
				},
				"merged_summary": {"type": "string", "description": "New summary for the merged entry"},
				"merged_body": {"type": "string", "description": "New Markdown body for the merged entry"}
			}
		}`),
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args struct {
			TargetID      string   `json:"target_id"`
			SourceIDs     []string `json:"source_ids"`
			MergedSummary string   `json:"merged_summary"`
			MergedBody    string   `json:"merged_body"`
		}
		if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
			return errResult(fmt.Errorf("invalid arguments: %w", err))
		}
		targetID, err := uuid.Parse(args.TargetID)
		if err != nil {
			return errResult(fmt.Errorf("invalid target_id UUID: %w", err))
		}
		sourceIDs := make([]openapi_types.UUID, 0, len(args.SourceIDs))
		for _, s := range args.SourceIDs {
			id, err := uuid.Parse(s)
			if err != nil {
				return errResult(fmt.Errorf("invalid source UUID %q: %w", s, err))
			}
			sourceIDs = append(sourceIDs, id)
		}
		resp, err := c.AdminMergeKnowledge(ctx, khclient.MergeKnowledgeRequest{
			TargetId:      targetID,
			SourceIds:     sourceIDs,
			MergedSummary: args.MergedSummary,
			MergedBody:    args.MergedBody,
		})
		if err != nil {
			return errResult(fmt.Errorf("API request failed: %w", err))
		}
		var result khclient.MergeKnowledgeResponse
		if err := readJSON(resp, &result); err != nil {
			return errResult(err)
		}
		return textResult(result)
	})

	// kh_create_conflict: Record a conflict between knowledge entries.
	s.AddTool(&mcp.Tool{
		Name:        "kh_create_conflict",
		Description: "Record a conflict between knowledge entries (e.g., contradictory information). Creates a conflict report for later resolution.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"required": ["knowledge_ids", "type", "description"],
			"properties": {
				"knowledge_ids": {
					"type": "array",
					"items": {"type": "string", "format": "uuid"},
					"description": "UUIDs of conflicting knowledge entries"
				},
				"comment_ids": {
					"type": "array",
					"items": {"type": "string", "format": "uuid"},
					"description": "Related comment UUIDs (optional)"
				},
				"type": {
					"type": "string",
					"enum": ["knowledge_conflict", "correction_conflict"],
					"description": "Type of conflict"
				},
				"description": {"type": "string", "description": "Description of the conflict"}
			}
		}`),
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args struct {
			KnowledgeIDs []string  `json:"knowledge_ids"`
			CommentIDs   []string  `json:"comment_ids"`
			Type         string    `json:"type"`
			Description  string    `json:"description"`
		}
		if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
			return errResult(fmt.Errorf("invalid arguments: %w", err))
		}
		knowledgeIDs := make([]openapi_types.UUID, 0, len(args.KnowledgeIDs))
		for _, s := range args.KnowledgeIDs {
			id, err := uuid.Parse(s)
			if err != nil {
				return errResult(fmt.Errorf("invalid knowledge UUID %q: %w", s, err))
			}
			knowledgeIDs = append(knowledgeIDs, id)
		}
		var commentIDs *[]openapi_types.UUID
		if len(args.CommentIDs) > 0 {
			ids := make([]openapi_types.UUID, 0, len(args.CommentIDs))
			for _, s := range args.CommentIDs {
				id, err := uuid.Parse(s)
				if err != nil {
					return errResult(fmt.Errorf("invalid comment UUID %q: %w", s, err))
				}
				ids = append(ids, id)
			}
			commentIDs = &ids
		}
		resp, err := c.AdminCreateConflict(ctx, khclient.CreateConflictRequest{
			KnowledgeIds: knowledgeIDs,
			CommentIds:   commentIDs,
			Type:         khclient.ConflictType(args.Type),
			Description:  args.Description,
		})
		if err != nil {
			return errResult(fmt.Errorf("API request failed: %w", err))
		}
		var result khclient.CreateConflictResponse
		if err := readJSON(resp, &result); err != nil {
			return errResult(err)
		}
		return textResult(result)
	})

	// kh_log_curation: Record a curation action in the audit log.
	s.AddTool(&mcp.Tool{
		Name:        "kh_log_curation",
		Description: "Record a curation action in the audit log (e.g., after merging, archiving, or applying corrections).",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"required": ["action", "target_id", "description"],
			"properties": {
				"action": {
					"type": "string",
					"enum": ["archive", "merge_knowledge", "merge_supplement", "merge_tags", "apply_correction", "create_conflict", "downgrade"],
					"description": "The curation action performed"
				},
				"target_id": {"type": "string", "description": "UUID or identifier of the primary target"},
				"description": {"type": "string", "description": "Human-readable description of what was done and why"},
				"source_ids": {
					"type": "array",
					"items": {"type": "string"},
					"description": "UUIDs of source entries involved (optional)"
				},
				"diff": {"type": "string", "description": "Diff or before/after summary of changes (optional)"}
			}
		}`),
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args struct {
			Action      string    `json:"action"`
			TargetID    string    `json:"target_id"`
			Description string    `json:"description"`
			SourceIDs   *[]string `json:"source_ids"`
			Diff        *string   `json:"diff"`
		}
		if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
			return errResult(fmt.Errorf("invalid arguments: %w", err))
		}
		resp, err := c.AdminLogCuration(ctx, khclient.CurationLogRequest{
			Action:      khclient.CurationAction(args.Action),
			TargetId:    args.TargetID,
			Description: args.Description,
			SourceIds:   args.SourceIDs,
			Diff:        args.Diff,
		})
		if err != nil {
			return errResult(fmt.Errorf("API request failed: %w", err))
		}
		var result khclient.CurationLogResponse
		if err := readJSON(resp, &result); err != nil {
			return errResult(err)
		}
		return textResult(result)
	})

	// kh_recalculate_weights: Trigger weight recalculation for all entries.
	s.AddTool(&mcp.Tool{
		Name:        "kh_recalculate_weights",
		Description: "Trigger recalculation of access-frequency weights for all knowledge entries using exponential decay. Returns the number of entries updated.",
		InputSchema: json.RawMessage(`{"type": "object", "properties": {}}`),
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		resp, err := c.SystemRecalculateWeights(ctx)
		if err != nil {
			return errResult(fmt.Errorf("API request failed: %w", err))
		}
		var result khclient.RecalculateResponse
		if err := readJSON(resp, &result); err != nil {
			return errResult(err)
		}
		return textResult(result)
	})
}
