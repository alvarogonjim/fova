package tui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

// Pending-input file helpers backing the editable confirmation gate
// (spec §3.4). The file lives in the active workspace so the user's editor
// opens a real path — sibling files navigable, LSP / JSON-schema highlighting
// active, no /tmp indirection — and is cleaned up on every modal exit. A
// `pending/` line in .fova/.gitignore guards against accidental commits if
// a crash leaves a stale file behind.

// pendingHeaderPrefix is the leading comment block written to every pending
// file. Read by readPendingInput, which strips `// ...` lines before
// validating / submitting the body.
const pendingHeaderPrefix = "// fova: edit the JSON below, save and quit. " +
	"Comments (lines starting with //) are stripped before validation and submission."

// pendingInputPath returns a unique workspace path for the pending-input
// file of a tool. The 8-hex-char uuid prefix keeps two concurrent confirm
// overlays from clobbering each other (the TUI is single-modal today, but
// the path stays collision-safe under future replay / async confirm work).
func pendingInputPath(workspace, tool string) string {
	short := uuid.NewString()
	if len(short) > 8 {
		short = short[:8]
	}
	name := fmt.Sprintf("%s-%s.json", tool, short)
	return filepath.Join(workspace, ".fova", "pending", name)
}

// writePendingInput writes the pending-input file at path, seeding it with a
// `// fova:` header, the tool name, an optional `// ERROR:` line (when errMsg
// is non-empty), and the pretty-printed JSON body. Parent directories are
// created on demand and the workspace .fova/.gitignore is ensured to contain
// the `pending/` line idempotently.
//
// On retry (validator rejection) the same path is rewritten in place, so the
// user keeps editing the same buffer in the same editor session.
func writePendingInput(path, tool string, input json.RawMessage, errMsg string) error {
	workspace := filepath.Dir(filepath.Dir(path)) // strip /pending/<file>
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := ensureGitignore(workspace); err != nil {
		return err
	}

	var pretty bytes.Buffer
	if len(input) > 0 {
		if err := json.Indent(&pretty, input, "", "  "); err != nil {
			// Fall back to the raw bytes — the user can still fix invalid JSON
			// in the editor; the header line carries the same hint.
			pretty.Reset()
			pretty.Write(input)
		}
	}

	var buf bytes.Buffer
	buf.WriteString(pendingHeaderPrefix)
	buf.WriteByte('\n')
	fmt.Fprintf(&buf, "// Tool: %s\n", tool)
	if errMsg != "" {
		fmt.Fprintf(&buf, "// ERROR: %s\n", errMsg)
	}
	buf.WriteString("\n")
	buf.Write(pretty.Bytes())
	if pretty.Len() > 0 && pretty.Bytes()[pretty.Len()-1] != '\n' {
		buf.WriteByte('\n')
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}

// readPendingInput reads the pending-input file at path, strips every
// `// ...` comment line (line starts with `//` after optional leading
// whitespace), trims surrounding whitespace, and returns the body bytes.
// The body is what the validator / agent loop sees — it never inherits the
// header noise.
func readPendingInput(path string) ([]byte, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var body bytes.Buffer
	for _, line := range strings.Split(string(raw), "\n") {
		trimmed := strings.TrimLeft(line, " \t")
		if strings.HasPrefix(trimmed, "//") {
			continue
		}
		body.WriteString(line)
		body.WriteByte('\n')
	}
	return bytes.TrimSpace(body.Bytes()), nil
}

// removePendingInput is the best-effort cleanup invoked on every modal-exit
// path (accept, decline, cancel-turn). Failure to delete is intentionally
// swallowed — the workspace gitignore catches abandoned files.
func removePendingInput(path string) {
	if path == "" {
		return
	}
	_ = os.Remove(path)
}

// ensureGitignore ensures <workspace>/.fova/.gitignore exists and contains
// the `pending/` line. Idempotent: re-running it on an already-correct file
// leaves the contents untouched; running it on a file without the line
// appends; running it without the file creates the file with just that line.
func ensureGitignore(workspace string) error {
	if workspace == "" {
		return nil
	}
	dir := filepath.Join(workspace, ".fova")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(dir, ".gitignore")
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	for _, line := range strings.Split(string(existing), "\n") {
		if strings.TrimSpace(line) == "pending/" {
			return nil // already present, no-op
		}
	}
	var buf bytes.Buffer
	buf.Write(existing)
	if len(existing) > 0 && existing[len(existing)-1] != '\n' {
		buf.WriteByte('\n')
	}
	buf.WriteString("pending/\n")
	return os.WriteFile(path, buf.Bytes(), 0o644)
}
