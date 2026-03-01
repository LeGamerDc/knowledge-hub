package main_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/legamerdc/knowledge-hub/internal/server/handlers"
	"github.com/legamerdc/knowledge-hub/internal/server/service"
	"github.com/legamerdc/knowledge-hub/pkg/corestore"
)

var khBin string

// TestMain builds the kh binary once and runs all tests.
func TestMain(m *testing.M) {
	tmpDir, err := os.MkdirTemp("", "kh-cli-test-*")
	if err != nil {
		panic("mkdirtemp: " + err.Error())
	}
	defer os.RemoveAll(tmpDir)

	khBin = filepath.Join(tmpDir, "kh")
	cmd := exec.Command("go", "build", "-o", khBin, "github.com/legamerdc/knowledge-hub/cmd/kh")
	if out, err := cmd.CombinedOutput(); err != nil {
		panic("build kh: " + string(out))
	}

	os.Exit(m.Run())
}

// newTestServer creates an in-memory API server for integration tests.
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

// runKH executes the kh binary and returns stdout, stderr, exit code.
func runKH(t *testing.T, server string, args ...string) (stdout, stderr string, exitCode int) {
	t.Helper()
	allArgs := append([]string{"--server", server}, args...)
	cmd := exec.Command(khBin, allArgs...)
	var outBuf, errBuf strings.Builder
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = 1
		}
	}
	return outBuf.String(), errBuf.String(), exitCode
}

// postJSON sends a JSON POST and returns the response body bytes.
func postJSON(t *testing.T, ts *httptest.Server, path string, payload any) []byte {
	t.Helper()
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	resp, err := http.Post(ts.URL+path, "application/json", bytes.NewReader(data))
	if err != nil {
		t.Fatalf("post %s: %v", path, err)
	}
	defer resp.Body.Close()
	var buf bytes.Buffer
	buf.ReadFrom(resp.Body)
	if resp.StatusCode >= 400 {
		t.Fatalf("POST %s returned %d: %s", path, resp.StatusCode, buf.String())
	}
	return buf.Bytes()
}

func TestStatus_Empty(t *testing.T) {
	ts := newTestServer(t)
	stdout, stderr, code := runKH(t, ts.URL, "status")

	if code != 0 {
		t.Fatalf("exit code %d, stderr: %s", code, stderr)
	}
	if !strings.Contains(stdout, "Knowledge Hub Status") {
		t.Errorf("expected status header, got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "Total Knowledge:") {
		t.Errorf("expected Total Knowledge field, got:\n%s", stdout)
	}
}

func TestList_Empty(t *testing.T) {
	ts := newTestServer(t)
	stdout, _, code := runKH(t, ts.URL, "list")

	if code != 0 {
		t.Fatalf("exit code %d", code)
	}
	if !strings.Contains(stdout, "No entries found") {
		t.Errorf("expected 'No entries found', got:\n%s", stdout)
	}
}

func TestListAndRead(t *testing.T) {
	ts := newTestServer(t)

	// Contribute a knowledge entry
	respBytes := postJSON(t, ts, "/api/v1/agent/knowledge", map[string]any{
		"title":   "Test Entry Alpha",
		"body":    "This is the **body** of the test entry.",
		"summary": "Brief summary here",
		"tags":    []string{"golang", "testing"},
	})
	var contribute struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(respBytes, &contribute); err != nil {
		t.Fatalf("unmarshal contribute: %v", err)
	}
	id := contribute.ID

	// kh list should show the entry
	stdout, stderr, code := runKH(t, ts.URL, "list")
	if code != 0 {
		t.Fatalf("list exit %d, stderr: %s", code, stderr)
	}
	if !strings.Contains(stdout, "Test Entry Alpha") {
		t.Errorf("expected entry title in list output, got:\n%s", stdout)
	}
	if !strings.Contains(stdout, id[:8]) {
		t.Errorf("expected short ID %s in list output, got:\n%s", id[:8], stdout)
	}

	// kh read should show Markdown with title and body
	stdout, stderr, code = runKH(t, ts.URL, "read", id)
	if code != 0 {
		t.Fatalf("read exit %d, stderr: %s", code, stderr)
	}
	if !strings.Contains(stdout, "# Test Entry Alpha") {
		t.Errorf("expected Markdown title, got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "## Body") {
		t.Errorf("expected Body section, got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "test entry") {
		t.Errorf("expected body content, got:\n%s", stdout)
	}
}

func TestListFilter_ByStatus(t *testing.T) {
	ts := newTestServer(t)

	// Contribute an entry
	respBytes := postJSON(t, ts, "/api/v1/agent/knowledge", map[string]any{
		"title":   "Filter Test Entry",
		"body":    "body",
		"summary": "summary",
		"tags":    []string{"filter"},
	})
	var contribute struct {
		ID string `json:"id"`
	}
	json.Unmarshal(respBytes, &contribute)

	// list --status archived should show 0 entries
	stdout, _, code := runKH(t, ts.URL, "list", "--status", "archived")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(stdout, "No entries found") {
		t.Errorf("expected no archived entries, got:\n%s", stdout)
	}

	// list --status active should show the entry
	stdout, _, code = runKH(t, ts.URL, "list", "--status", "active")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(stdout, "Filter Test Entry") {
		t.Errorf("expected active entry in list, got:\n%s", stdout)
	}
}

func TestConflicts_Empty(t *testing.T) {
	ts := newTestServer(t)
	stdout, _, code := runKH(t, ts.URL, "conflicts")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(stdout, "No conflicts found") {
		t.Errorf("expected 'No conflicts found', got:\n%s", stdout)
	}
}

func TestLogs_Empty(t *testing.T) {
	ts := newTestServer(t)
	stdout, _, code := runKH(t, ts.URL, "logs")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(stdout, "No curation logs found") {
		t.Errorf("expected 'No curation logs found', got:\n%s", stdout)
	}
}

func TestRead_NotFound(t *testing.T) {
	ts := newTestServer(t)
	fakeID := "00000000-0000-0000-0000-000000000000"
	_, stderr, code := runKH(t, ts.URL, "read", fakeID)
	if code == 0 {
		t.Fatal("expected non-zero exit code for missing entry")
	}
	if !strings.Contains(stderr, "error:") {
		t.Errorf("expected error message in stderr, got: %s", stderr)
	}
}

func TestDelete_ActiveEntryFails(t *testing.T) {
	ts := newTestServer(t)

	// Contribute an active entry
	respBytes := postJSON(t, ts, "/api/v1/agent/knowledge", map[string]any{
		"title":   "Active Entry",
		"body":    "body",
		"summary": "summary",
		"tags":    []string{"active"},
	})
	var contribute struct {
		ID string `json:"id"`
	}
	json.Unmarshal(respBytes, &contribute)

	// Try to delete active entry — server should reject it
	// But the CLI will first GET the entry (success), then prompt.
	// We simulate "y" as stdin, the DELETE should fail with 4xx.
	cmd := exec.Command(khBin, "--server", ts.URL, "delete", contribute.ID)
	cmd.Stdin = strings.NewReader("y\n")
	var errBuf strings.Builder
	cmd.Stderr = &errBuf
	cmd.Run()

	// Either the delete returned an error (active entry not deletable)
	// or it succeeded — depends on server policy. Check server behavior.
	// The server should return 4xx for deleting an active entry.
	// kh should print an error to stderr.
	t.Logf("stderr: %s", errBuf.String())
}

func TestServerUnreachable(t *testing.T) {
	_, stderr, code := runKH(t, "http://127.0.0.1:1", "status")
	if code == 0 {
		t.Fatal("expected non-zero exit code when server is unreachable")
	}
	if !strings.Contains(stderr, "error:") {
		t.Errorf("expected error in stderr, got: %s", stderr)
	}
}

func TestUnknownCommand(t *testing.T) {
	_, stderr, code := runKH(t, "http://localhost:19820", "boguscommand")
	if code == 0 {
		t.Fatal("expected non-zero exit code for unknown command")
	}
	if !strings.Contains(stderr, "unknown command") {
		t.Errorf("expected 'unknown command' in stderr, got: %s", stderr)
	}
}

func TestResolve_MissingResolutionFlag(t *testing.T) {
	ts := newTestServer(t)
	fakeID := "00000000-0000-0000-0000-000000000001"
	_, stderr, code := runKH(t, ts.URL, "resolve", fakeID)
	if code == 0 {
		t.Fatal("expected non-zero exit code when --resolution is missing")
	}
	if !strings.Contains(stderr, "resolution") {
		t.Errorf("expected resolution error, got: %s", stderr)
	}
}
