package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/legamerdc/knowledge-hub/pkg/khclient"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

const defaultServer = "http://localhost:19820"

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "error: "+format+"\n", args...)
	os.Exit(1)
}

func printUsage() {
	fmt.Fprint(os.Stderr, `Usage: kh [--server ADDR] <command> [flags]

Commands:
  status                                      System status overview
  list [--status active|archived] [--tag TAG] [--limit N]
                                              List knowledge entries
  read <id>                                   Read full knowledge entry (Markdown)
  restore <id>                                Restore archived entry
  delete <id>                                 Hard-delete archived entry (requires confirmation)
  conflicts [--status open|resolved]          List conflict reports
  resolve <conflict-id> --resolution TEXT     Resolve a conflict
  logs [--limit N]                            View curation logs (default: 20)

Global flags:
  --server ADDR   API server address (default: $KH_SERVER or http://localhost:19820)
`)
}

func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n-3] + "..."
	}
	return s
}

func decodeJSON(r io.Reader, v any) error {
	return json.NewDecoder(r).Decode(v)
}

func checkHTTPError(resp *http.Response) error {
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	var e khclient.ErrorResponse
	if err := decodeJSON(resp.Body, &e); err == nil && e.Message != "" {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, e.Message)
	}
	return fmt.Errorf("HTTP %d", resp.StatusCode)
}

func newClient(server string) *khclient.Client {
	c, err := khclient.NewClient(server)
	if err != nil {
		fatalf("create client: %v", err)
	}
	return c
}

func parseUUID(s string) (openapi_types.UUID, error) {
	var id openapi_types.UUID
	return id, id.UnmarshalText([]byte(s))
}

func derefInt(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

func derefFloat(p *float64) float64 {
	if p == nil {
		return 0
	}
	return *p
}

func derefString(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	var serverAddr string
	globalFS := flag.NewFlagSet("kh", flag.ContinueOnError)
	globalFS.StringVar(&serverAddr, "server", envOrDefault("KH_SERVER", defaultServer), "API server address")

	if err := globalFS.Parse(os.Args[1:]); err != nil {
		fatalf("%v", err)
	}

	remaining := globalFS.Args()
	if len(remaining) == 0 {
		printUsage()
		os.Exit(1)
	}

	cmd := remaining[0]
	cmdArgs := remaining[1:]
	ctx := context.Background()

	switch cmd {
	case "status":
		runStatus(ctx, serverAddr)
	case "list":
		runList(ctx, serverAddr, cmdArgs)
	case "read":
		runRead(ctx, serverAddr, cmdArgs)
	case "restore":
		runRestore(ctx, serverAddr, cmdArgs)
	case "delete":
		runDelete(ctx, serverAddr, cmdArgs)
	case "conflicts":
		runConflicts(ctx, serverAddr, cmdArgs)
	case "resolve":
		runResolve(ctx, serverAddr, cmdArgs)
	case "logs":
		runLogs(ctx, serverAddr, cmdArgs)
	case "help", "-h", "--help":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", cmd)
		printUsage()
		os.Exit(1)
	}
}

// runStatus prints a system status overview.
func runStatus(ctx context.Context, server string) {
	c := newClient(server)
	resp, err := c.SystemGetStatus(ctx)
	if err != nil {
		fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if err := checkHTTPError(resp); err != nil {
		fatalf("%v", err)
	}

	var status khclient.SystemStatus
	if err := decodeJSON(resp.Body, &status); err != nil {
		fatalf("decode response: %v", err)
	}

	fmt.Println("Knowledge Hub Status")
	fmt.Println("====================")
	fmt.Printf("Total Knowledge:      %d\n", derefInt(status.TotalKnowledge))
	fmt.Printf("  Active:             %d\n", derefInt(status.ActiveKnowledge))
	fmt.Printf("  Archived:           %d\n", derefInt(status.ArchivedKnowledge))
	fmt.Printf("Total Tags:           %d\n", derefInt(status.TotalTags))
	fmt.Printf("Total Comments:       %d\n", derefInt(status.TotalComments))
	fmt.Printf("Unprocessed Comments: %d\n", derefInt(status.UnprocessedComments))
	fmt.Printf("Open Conflicts:       %d\n", derefInt(status.OpenConflicts))
}

// runList lists knowledge entries with optional filters.
func runList(ctx context.Context, server string, args []string) {
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	statusFlag := fs.String("status", "active", "Filter by status: active|archived")
	tagFlag := fs.String("tag", "", "Filter by tag")
	limitFlag := fs.Int("limit", 50, "Maximum results")
	fs.Parse(args)

	c := newClient(server)
	params := &khclient.SystemListKnowledgeParams{}

	if *statusFlag != "" {
		s := khclient.KnowledgeStatus(*statusFlag)
		params.Status = &s
	}
	if *tagFlag != "" {
		params.Tag = tagFlag
	}
	if *limitFlag > 0 {
		params.Limit = limitFlag
	}

	resp, err := c.SystemListKnowledge(ctx, params)
	if err != nil {
		fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if err := checkHTTPError(resp); err != nil {
		fatalf("%v", err)
	}

	var entries []khclient.KnowledgeEntry
	if err := decodeJSON(resp.Body, &entries); err != nil {
		fatalf("decode response: %v", err)
	}

	if len(entries) == 0 {
		fmt.Println("No entries found.")
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tSTATUS\tWEIGHT\tTAGS\tTITLE")
	fmt.Fprintln(w, "--------\t--------\t------\t----\t-----")
	for _, e := range entries {
		tags := ""
		if e.Tags != nil {
			tags = strings.Join(*e.Tags, ",")
		}
		fmt.Fprintf(w, "%s\t%s\t%.2f\t%s\t%s\n",
			shortID(e.Id.String()),
			e.Status,
			derefFloat(e.Weight),
			truncate(tags, 30),
			truncate(e.Title, 60),
		)
	}
	w.Flush()
	fmt.Printf("\n%d entries\n", len(entries))
}

// runRead prints the full text of a knowledge entry in Markdown.
func runRead(ctx context.Context, server string, args []string) {
	if len(args) < 1 {
		fatalf("usage: kh read <id>")
	}
	id := args[0]
	knowledgeID, err := parseUUID(id)
	if err != nil {
		fatalf("invalid id %q: %v", id, err)
	}

	c := newClient(server)
	resp, err := c.SystemGetKnowledge(ctx, knowledgeID)
	if err != nil {
		fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if err := checkHTTPError(resp); err != nil {
		fatalf("%v", err)
	}

	var detail khclient.KnowledgeDetail
	if err := decodeJSON(resp.Body, &detail); err != nil {
		fatalf("decode response: %v", err)
	}

	fmt.Printf("# %s\n\n", detail.Title)
	fmt.Printf("**ID:** %s  \n", detail.Id)
	if detail.Author != nil {
		fmt.Printf("**Author:** %s  \n", *detail.Author)
	}
	fmt.Printf("**Created:** %s  \n", detail.CreatedAt.Format(time.DateTime))
	fmt.Printf("**Updated:** %s  \n", detail.UpdatedAt.Format(time.DateTime))
	if detail.Weight != nil {
		fmt.Printf("**Weight:** %.4f  \n", *detail.Weight)
	}
	if detail.Tags != nil && len(*detail.Tags) > 0 {
		fmt.Printf("**Tags:** %s  \n", strings.Join(*detail.Tags, ", "))
	}
	fmt.Println()

	if detail.Summary != nil {
		fmt.Printf("## Summary\n\n%s\n\n", *detail.Summary)
	}
	if detail.Body != nil {
		fmt.Printf("## Body\n\n%s\n\n", *detail.Body)
	}
	if cs := detail.CommentsSummary; cs != nil {
		fmt.Println("## Comments")
		fmt.Printf("- Success: %d  Failure: %d  Correction: %d  Supplement: %d\n",
			derefInt(cs.Success),
			derefInt(cs.Failure),
			derefInt(cs.Correction),
			derefInt(cs.Supplement),
		)
	}
}

// runRestore restores an archived knowledge entry.
func runRestore(ctx context.Context, server string, args []string) {
	if len(args) < 1 {
		fatalf("usage: kh restore <id>")
	}
	id := args[0]
	knowledgeID, err := parseUUID(id)
	if err != nil {
		fatalf("invalid id %q: %v", id, err)
	}

	c := newClient(server)
	resp, err := c.SystemRestoreKnowledge(ctx, knowledgeID)
	if err != nil {
		fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if err := checkHTTPError(resp); err != nil {
		fatalf("%v", err)
	}
	fmt.Printf("Entry %s restored successfully.\n", shortID(id))
}

// runDelete hard-deletes an archived knowledge entry after confirmation.
func runDelete(ctx context.Context, server string, args []string) {
	if len(args) < 1 {
		fatalf("usage: kh delete <id>")
	}
	id := args[0]
	knowledgeID, err := parseUUID(id)
	if err != nil {
		fatalf("invalid id %q: %v", id, err)
	}

	// Fetch entry first to show the title for confirmation
	c := newClient(server)
	getResp, err := c.SystemGetKnowledge(ctx, knowledgeID)
	if err != nil {
		fatalf("request failed: %v", err)
	}
	defer getResp.Body.Close()
	if err := checkHTTPError(getResp); err != nil {
		fatalf("%v", err)
	}
	var detail khclient.KnowledgeDetail
	if err := decodeJSON(getResp.Body, &detail); err != nil {
		fatalf("decode response: %v", err)
	}

	fmt.Printf("About to permanently delete:\n")
	fmt.Printf("  Title: %s\n", detail.Title)
	fmt.Printf("  ID:    %s\n", detail.Id)
	fmt.Printf("This action is IRREVERSIBLE. Confirm? [y/N] ")

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Scan()
	answer := strings.TrimSpace(scanner.Text())
	if !strings.EqualFold(answer, "y") {
		fmt.Println("Cancelled.")
		return
	}

	delResp, err := c.SystemDeleteKnowledge(ctx, knowledgeID)
	if err != nil {
		fatalf("request failed: %v", err)
	}
	defer delResp.Body.Close()
	if err := checkHTTPError(delResp); err != nil {
		fatalf("%v", err)
	}
	fmt.Printf("Entry %s deleted.\n", shortID(id))
}

// runConflicts lists conflict reports.
func runConflicts(ctx context.Context, server string, args []string) {
	fs := flag.NewFlagSet("conflicts", flag.ExitOnError)
	statusFlag := fs.String("status", "open", "Filter by status: open|resolved")
	fs.Parse(args)

	c := newClient(server)
	params := &khclient.SystemListConflictsParams{}
	if *statusFlag != "" {
		s := khclient.ConflictStatus(*statusFlag)
		params.Status = &s
	}

	resp, err := c.SystemListConflicts(ctx, params)
	if err != nil {
		fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if err := checkHTTPError(resp); err != nil {
		fatalf("%v", err)
	}

	var conflicts []khclient.ConflictReport
	if err := decodeJSON(resp.Body, &conflicts); err != nil {
		fatalf("decode response: %v", err)
	}

	if len(conflicts) == 0 {
		fmt.Println("No conflicts found.")
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tTYPE\tSTATUS\tCREATED\tDESCRIPTION")
	fmt.Fprintln(w, "--------\t--------------------\t--------\t----------------\t-----------")
	for _, cf := range conflicts {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
			shortID(cf.Id.String()),
			cf.Type,
			cf.Status,
			cf.CreatedAt.Format("2006-01-02 15:04"),
			truncate(cf.Description, 60),
		)
	}
	w.Flush()
	fmt.Printf("\n%d conflicts\n", len(conflicts))
}

// runResolve resolves a conflict report.
func runResolve(ctx context.Context, server string, args []string) {
	fs := flag.NewFlagSet("resolve", flag.ExitOnError)
	resolution := fs.String("resolution", "", "Resolution description (required)")
	fs.Parse(args)

	if fs.NArg() < 1 {
		fatalf("usage: kh resolve <conflict-id> --resolution TEXT")
	}
	if *resolution == "" {
		fatalf("--resolution is required")
	}

	id := fs.Arg(0)
	conflictID, err := parseUUID(id)
	if err != nil {
		fatalf("invalid id %q: %v", id, err)
	}

	c := newClient(server)
	resp, err := c.SystemResolveConflict(ctx, conflictID, khclient.ResolveConflictRequest{
		Resolution: *resolution,
	})
	if err != nil {
		fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if err := checkHTTPError(resp); err != nil {
		fatalf("%v", err)
	}
	fmt.Printf("Conflict %s resolved.\n", shortID(id))
}

// runLogs shows curation logs.
func runLogs(ctx context.Context, server string, args []string) {
	fs := flag.NewFlagSet("logs", flag.ExitOnError)
	limitFlag := fs.Int("limit", 20, "Maximum number of logs to show")
	fs.Parse(args)

	c := newClient(server)
	params := &khclient.SystemListCurationLogsParams{
		Limit: limitFlag,
	}

	resp, err := c.SystemListCurationLogs(ctx, params)
	if err != nil {
		fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if err := checkHTTPError(resp); err != nil {
		fatalf("%v", err)
	}

	var logs []khclient.CurationLog
	if err := decodeJSON(resp.Body, &logs); err != nil {
		fatalf("decode response: %v", err)
	}

	if len(logs) == 0 {
		fmt.Println("No curation logs found.")
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "TIME\tACTION\tTARGET\tDESCRIPTION")
	fmt.Fprintln(w, "-------------------\t----------\t--------\t-----------")
	for _, l := range logs {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
			l.CreatedAt.Format("2006-01-02 15:04:05"),
			l.Action,
			shortID(l.TargetId),
			truncate(l.Description, 60),
		)
	}
	w.Flush()
	fmt.Printf("\n%d logs\n", len(logs))
}

// derefString is exported only for compiler
var _ = derefString
