package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPendingInputRoundTrip locks in the spec §3.4 round-trip: write the
// tool's proposed JSON to the pending-input file with a `// fova:` header,
// then read it back and verify the comment lines are stripped and the body
// bytes survive intact.
func TestPendingInputRoundTrip(t *testing.T) {
	workspace := t.TempDir()
	path := pendingInputPath(workspace, "fold.boltz2")
	if !strings.Contains(path, ".fova"+string(filepath.Separator)+"pending") {
		t.Errorf("pending path should live under .fova/pending: %q", path)
	}
	if !strings.HasPrefix(filepath.Base(path), "fold.boltz2-") {
		t.Errorf("pending file name should be tool-prefixed: %q", filepath.Base(path))
	}

	input := json.RawMessage(`{"sequence":"MAQ"}`)
	if err := writePendingInput(path, "fold.boltz2", input, ""); err != nil {
		t.Fatalf("writePendingInput: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read raw file: %v", err)
	}
	rawStr := string(raw)
	if !strings.Contains(rawStr, "// fova:") {
		t.Errorf("seed file missing the // fova: header:\n%s", rawStr)
	}
	if !strings.Contains(rawStr, "// Tool: fold.boltz2") {
		t.Errorf("seed file missing the tool line:\n%s", rawStr)
	}

	body, err := readPendingInput(path)
	if err != nil {
		t.Fatalf("readPendingInput: %v", err)
	}
	// Body must be valid JSON that decodes to the original payload.
	if !json.Valid(body) {
		t.Fatalf("readPendingInput returned non-JSON bytes: %q", body)
	}
	var got map[string]string
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("decode body: %v (%q)", err, body)
	}
	if got["sequence"] != "MAQ" {
		t.Errorf("round-trip lost the sequence: %+v", got)
	}
}

// TestPendingInputErrorHeader confirms a validator-failure rewrite pins the
// error message at the top of the comment block (spec §3.4 retry path).
func TestPendingInputErrorHeader(t *testing.T) {
	workspace := t.TempDir()
	path := pendingInputPath(workspace, "design.bindcraft")
	input := json.RawMessage(`{"target":"6vxx"}`)
	if err := writePendingInput(path, "design.bindcraft", input, "missing chain"); err != nil {
		t.Fatalf("writePendingInput: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read raw file: %v", err)
	}
	if !strings.Contains(string(raw), "// ERROR: missing chain") {
		t.Errorf("seed with errMsg missing the ERROR line:\n%s", raw)
	}
	// The body must still strip cleanly — the error line is metadata, not data.
	body, err := readPendingInput(path)
	if err != nil {
		t.Fatalf("readPendingInput: %v", err)
	}
	if !json.Valid(body) || !strings.Contains(string(body), `"target"`) {
		t.Errorf("body still includes the comments after read-back: %q", body)
	}
}

// TestEnsureGitignore covers spec §3.4 hygiene: the workspace .fova/.gitignore
// gets a `pending/` line, idempotently. Two calls must leave the file stable.
func TestEnsureGitignore(t *testing.T) {
	workspace := t.TempDir()
	if err := ensureGitignore(workspace); err != nil {
		t.Fatalf("ensureGitignore first call: %v", err)
	}
	path := filepath.Join(workspace, ".fova", ".gitignore")
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read first: %v", err)
	}
	if !strings.Contains(string(first), "pending/") {
		t.Errorf("gitignore missing pending/ entry:\n%s", first)
	}
	if err := ensureGitignore(workspace); err != nil {
		t.Fatalf("ensureGitignore second call: %v", err)
	}
	second, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read second: %v", err)
	}
	if string(first) != string(second) {
		t.Errorf("idempotency broken: first=%q second=%q", first, second)
	}
	// Pre-existing content (e.g. user-added line) must be preserved.
	if err := os.WriteFile(path, []byte("custom/\npending/\n"), 0o644); err != nil {
		t.Fatalf("seed custom: %v", err)
	}
	if err := ensureGitignore(workspace); err != nil {
		t.Fatalf("ensureGitignore after custom seed: %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read after: %v", err)
	}
	if !strings.Contains(string(after), "custom/") {
		t.Errorf("ensureGitignore stripped user lines:\n%s", after)
	}
}
