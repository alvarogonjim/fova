// Package tools defines the tool abstraction and the registry that dispatches
// tool calls on behalf of the agent.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/alvarogonjim/fova/internal/domain"
	"github.com/alvarogonjim/fova/internal/llm"
)

// Tool is one capability the agent can invoke.
type Tool interface {
	Name() string
	Description() string
	InputSchema() map[string]any
	Execute(ctx context.Context, input json.RawMessage) (Result, error)
	RequiresConfirmation(input json.RawMessage) bool
	EstimatedCostUSD(input json.RawMessage) float64
	EstimatedDuration(input json.RawMessage) time.Duration
}

// Concurrent is an optional interface a Tool may implement to declare it is
// safe to run in parallel with other Concurrent tools in the same batched
// tool-call response. Tools that do not implement it are treated as serial.
//
// Invariant: a Tool that returns Concurrent()=true must NOT return
// RequiresConfirmation()=true. The agent loop refuses to parallelise calls
// requiring confirmation; combining the two is a bug.
type Concurrent interface {
	Concurrent() bool
}

// IsConcurrent reports whether t opts in to concurrent execution.
func IsConcurrent(t Tool) bool {
	c, ok := t.(Concurrent)
	return ok && c.Concurrent()
}

// Result is the outcome of a tool execution.
type Result struct {
	Output     json.RawMessage    // JSON-serialisable structured output
	Display    string             // human/LLM-readable summary
	JobID      domain.JobID       // set if the tool started an async job
	Cost       float64            // USD
	Provenance domain.ToolCallRef // lineage record
}

// Validator is implemented by tools that want their input revalidated after
// the user edits a proposed tool call on the editable confirmation gate,
// before Execute runs. Tools that don't implement it accept any well-formed
// JSON the user produces; Execute is still the last line of defense.
//
// The contract: return nil for a runnable input; return a non-nil error
// describing the first problem otherwise. The error message is shown to the
// user inline and pinned at the top of the pending-input file on retry, so
// it should read as a fix-it hint, not a stack trace.
type Validator interface {
	Validate(input json.RawMessage) error
}

// Registry holds and dispatches tools.
type Registry struct {
	tools map[string]Tool
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry { return &Registry{tools: map[string]Tool{}} }

// Register adds a tool, replacing any tool with the same name.
func (r *Registry) Register(t Tool) { r.tools[t.Name()] = t }

// Get returns the tool with the given name.
func (r *Registry) Get(name string) (Tool, bool) {
	t, ok := r.tools[name]
	return t, ok
}

// Specs returns the tool specs advertised to the LLM, sorted by name.
func (r *Registry) Specs() []llm.ToolSpec {
	specs := make([]llm.ToolSpec, 0, len(r.tools))
	for _, t := range r.tools {
		specs = append(specs, llm.ToolSpec{
			Name:        t.Name(),
			Description: t.Description(),
			InputSchema: t.InputSchema(),
		})
	}
	// Stable order so prompts and tests are deterministic.
	for i := 1; i < len(specs); i++ {
		for j := i; j > 0 && specs[j-1].Name > specs[j].Name; j-- {
			specs[j-1], specs[j] = specs[j], specs[j-1]
		}
	}
	return specs
}

// Execute dispatches a tool call by name.
func (r *Registry) Execute(ctx context.Context, name string, input json.RawMessage) (Result, error) {
	t, ok := r.tools[name]
	if !ok {
		return Result{}, fmt.Errorf("unknown tool %q", name)
	}
	return t.Execute(ctx, input)
}

// SafeJoin resolves rel within root and rejects any path escaping root.
// Absolute rel paths are always rejected.
func SafeJoin(root, rel string) (string, error) {
	if filepath.IsAbs(rel) {
		return "", fmt.Errorf("path %q escapes the workspace", rel)
	}
	cleanRoot := filepath.Clean(root)
	joined := filepath.Clean(filepath.Join(cleanRoot, rel))
	if joined != cleanRoot && !strings.HasPrefix(joined, cleanRoot+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q escapes the workspace", rel)
	}
	return joined, nil
}
