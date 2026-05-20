package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/alvarogonjim/proteus/internal/domain"
)

// bashDenyTokens trigger a confirmation modal before fs.bash runs.
var bashDenyTokens = []string{"rm -rf", "dd ", "mkfs", "sudo "}

// bashAllowlist is the set of external binaries fs.bash may invoke.
var bashAllowlist = []string{"ls", "cat", "grep", "sed", "awk", "jq", "python3", "git", "curl", "wget"}

// buildBashSandbox creates a temp dir populated with symlinks to the
// allowlisted binaries and returns its path. It is used as the sole PATH
// entry for fs.bash child processes. On failure it returns "" so callers
// fail closed (an empty PATH resolves no external commands).
func buildBashSandbox() string {
	dir, err := os.MkdirTemp("", "proteus-bash-*")
	if err != nil {
		return ""
	}
	for _, name := range bashAllowlist {
		resolved, err := exec.LookPath(name)
		if err != nil {
			continue
		}
		_ = os.Symlink(resolved, filepath.Join(dir, name))
	}
	return dir
}

// NewFSTools returns the four filesystem/shell tools bound to a workspace root.
func NewFSTools(root string) []Tool {
	return []Tool{
		fsRead{root: root}, fsWrite{root: root}, fsEdit{root: root},
		fsBash{root: root, binDir: buildBashSandbox()},
	}
}

// --- fs.read ---

type fsRead struct{ root string }

func (fsRead) Name() string        { return "fs.read" }
func (fsRead) Description() string { return "Read a UTF-8 text file within the project workspace." }
func (fsRead) InputSchema() map[string]any {
	return objectSchema(map[string]any{
		"path": strProp("Path within the workspace"),
	}, "path")
}
func (fsRead) RequiresConfirmation(json.RawMessage) bool       { return false }
func (fsRead) EstimatedCostUSD(json.RawMessage) float64        { return 0 }
func (fsRead) EstimatedDuration(json.RawMessage) time.Duration { return 50 * time.Millisecond }
func (t fsRead) Execute(_ context.Context, input json.RawMessage) (Result, error) {
	var in struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return Result{}, err
	}
	abs, err := SafeJoin(t.root, in.Path)
	if err != nil {
		return Result{}, err
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return Result{}, err
	}
	return Result{
		Display:    string(data),
		Provenance: domain.NewToolCallRef("fs.read", input),
	}, nil
}

// --- fs.write ---

type fsWrite struct{ root string }

func (fsWrite) Name() string { return "fs.write" }
func (fsWrite) Description() string {
	return "Create or overwrite a file within the project workspace."
}
func (fsWrite) InputSchema() map[string]any {
	return objectSchema(map[string]any{
		"path":    strProp("Path within the workspace"),
		"content": strProp("File contents"),
	}, "path", "content")
}
func (fsWrite) RequiresConfirmation(json.RawMessage) bool       { return false }
func (fsWrite) EstimatedCostUSD(json.RawMessage) float64        { return 0 }
func (fsWrite) EstimatedDuration(json.RawMessage) time.Duration { return 50 * time.Millisecond }
func (t fsWrite) Execute(_ context.Context, input json.RawMessage) (Result, error) {
	var in struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return Result{}, err
	}
	abs, err := SafeJoin(t.root, in.Path)
	if err != nil {
		return Result{}, err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return Result{}, err
	}
	if err := os.WriteFile(abs, []byte(in.Content), 0o644); err != nil {
		return Result{}, err
	}
	return Result{
		Display:    fmt.Sprintf("wrote %d bytes to %s", len(in.Content), in.Path),
		Provenance: domain.NewToolCallRef("fs.write", input),
	}, nil
}

// --- fs.edit ---

type fsEdit struct{ root string }

func (fsEdit) Name() string { return "fs.edit" }
func (fsEdit) Description() string {
	return "Replace the first occurrence of a string in a workspace file."
}
func (fsEdit) InputSchema() map[string]any {
	return objectSchema(map[string]any{
		"path": strProp("Path within the workspace"),
		"old":  strProp("Exact text to replace"),
		"new":  strProp("Replacement text"),
	}, "path", "old", "new")
}
func (fsEdit) RequiresConfirmation(json.RawMessage) bool       { return false }
func (fsEdit) EstimatedCostUSD(json.RawMessage) float64        { return 0 }
func (fsEdit) EstimatedDuration(json.RawMessage) time.Duration { return 50 * time.Millisecond }
func (t fsEdit) Execute(_ context.Context, input json.RawMessage) (Result, error) {
	var in struct {
		Path string `json:"path"`
		Old  string `json:"old"`
		New  string `json:"new"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return Result{}, err
	}
	abs, err := SafeJoin(t.root, in.Path)
	if err != nil {
		return Result{}, err
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return Result{}, err
	}
	if !strings.Contains(string(data), in.Old) {
		return Result{}, fmt.Errorf("text to replace not found in %s", in.Path)
	}
	updated := strings.Replace(string(data), in.Old, in.New, 1)
	if err := os.WriteFile(abs, []byte(updated), 0o644); err != nil {
		return Result{}, err
	}
	return Result{
		Display:    fmt.Sprintf("edited %s", in.Path),
		Provenance: domain.NewToolCallRef("fs.edit", input),
	}, nil
}

// --- fs.bash ---

type fsBash struct {
	root   string
	binDir string // sole PATH entry for child processes
}

func (fsBash) Name() string { return "fs.bash" }
func (fsBash) Description() string {
	return "Run a shell command inside the project workspace (60s default timeout)."
}
func (fsBash) InputSchema() map[string]any {
	return objectSchema(map[string]any{
		"command":   strProp("Shell command to run"),
		"timeout_s": map[string]any{"type": "integer", "description": "Timeout in seconds (default 60)"},
	}, "command")
}
func (fsBash) RequiresConfirmation(input json.RawMessage) bool {
	var in struct {
		Command string `json:"command"`
	}
	_ = json.Unmarshal(input, &in)
	cmd := strings.ToLower(in.Command)
	for _, tok := range bashDenyTokens {
		if strings.Contains(cmd, tok) {
			return true
		}
	}
	return false
}
func (fsBash) EstimatedCostUSD(json.RawMessage) float64        { return 0 }
func (fsBash) EstimatedDuration(json.RawMessage) time.Duration { return 5 * time.Second }
func (t fsBash) Execute(ctx context.Context, input json.RawMessage) (Result, error) {
	var in struct {
		Command  string `json:"command"`
		TimeoutS int    `json:"timeout_s"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return Result{}, err
	}
	timeout := 60 * time.Second
	if in.TimeoutS > 0 {
		timeout = time.Duration(in.TimeoutS) * time.Second
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, "bash", "-c", in.Command)
	cmd.Dir = t.root
	cmd.Env = []string{
		"PATH=" + t.binDir,
		"HOME=" + os.Getenv("HOME"),
	}
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	display := out.String()
	if err != nil {
		display += "\n[exit error: " + err.Error() + "]"
	}
	return Result{
		Display:    display,
		Provenance: domain.NewToolCallRef("fs.bash", input),
	}, err
}

// --- schema helpers ---

func strProp(desc string) map[string]any {
	return map[string]any{"type": "string", "description": desc}
}

func objectSchema(props map[string]any, required ...string) map[string]any {
	return map[string]any{
		"type":       "object",
		"properties": props,
		"required":   required,
	}
}
