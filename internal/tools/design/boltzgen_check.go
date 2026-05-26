package design

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/alvarogonjim/fova/internal/backends"
	"github.com/alvarogonjim/fova/internal/domain"
	"github.com/alvarogonjim/fova/internal/tools"
)

// boltzGenCheckInput is the design.boltzgen_check tool input: just the
// workspace-relative path to the spec YAML to validate.
type boltzGenCheckInput struct {
	SpecPath string `json:"spec_path"` // required: workspace-relative spec YAML
}

// boltzGenCheckResult is the structured output of design.boltzgen_check. It is
// the pinned contract the plan integration (Task 7) codes against:
// {valid, errors, visualization_path}.
type boltzGenCheckResult struct {
	Valid             bool     `json:"valid"`
	Errors            []string `json:"errors"`
	VisualizationPath string   `json:"visualization_path"`
}

// boltzGenCheckTool is the bespoke design.boltzgen_check tool. It validates an
// agent-authored BoltzGen specification YAML by running `boltzgen check` inside
// the container — cheap (no GPU, no weights), so it runs synchronously and
// requires no user confirmation. The agent calls it while iterating on a spec,
// and the plan flow runs it as a gate before a real run.
type boltzGenCheckTool struct {
	backend       backends.Backend
	workspaceRoot string
}

// NewBoltzGenCheckTool builds the design.boltzgen_check tool. workspaceRoot
// scopes the relative spec_path input; backend dispatches the in-container
// `boltzgen check`.
func NewBoltzGenCheckTool(workspaceRoot string, backend backends.Backend) *boltzGenCheckTool {
	return &boltzGenCheckTool{
		backend:       backend,
		workspaceRoot: workspaceRoot,
	}
}

func (*boltzGenCheckTool) Name() string { return "design.boltzgen_check" }

func (*boltzGenCheckTool) Description() string {
	return "Validate a BoltzGen specification YAML by running `boltzgen check` " +
		"in the container. Cheap (no GPU, no weights) and synchronous — call it " +
		"while authoring a spec (see the boltzgen-spec skill) to catch errors " +
		"before a design.boltzgen run. Returns {valid, errors, " +
		"visualization_path}; visualization_path is the mmCIF BoltzGen renders " +
		"of the parsed spec."
}

// InputSchema advertises the single required spec_path field.
func (*boltzGenCheckTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"spec_path": map[string]any{
				"type":        "string",
				"description": "Workspace-relative path to the BoltzGen specification YAML to validate",
			},
		},
		"required": []string{"spec_path"},
	}
}

// boltzgen check is cheap — no GPU, no weights — so it never needs approval.
func (*boltzGenCheckTool) RequiresConfirmation(json.RawMessage) bool       { return false }
func (*boltzGenCheckTool) EstimatedCostUSD(json.RawMessage) float64        { return 0 }
func (*boltzGenCheckTool) EstimatedDuration(json.RawMessage) time.Duration { return 10 * time.Second }

// Execute validates the request, resolves spec_path against the workspace, and
// runs `boltzgen check` synchronously through the backend. Unlike
// design.boltzgen it does NOT submit a background job — it blocks until the
// check finishes (a few seconds) and returns the {valid, errors,
// visualization_path} result directly in tools.Result.Output.
func (t *boltzGenCheckTool) Execute(ctx context.Context, input json.RawMessage) (tools.Result, error) {
	var in boltzGenCheckInput
	if err := json.Unmarshal(input, &in); err != nil {
		return tools.Result{}, fmt.Errorf("invalid design.boltzgen_check request: %w", err)
	}
	if strings.TrimSpace(in.SpecPath) == "" {
		return tools.Result{}, fmt.Errorf(
			"design.boltzgen_check: spec_path is required — pass the workspace " +
				"path of the BoltzGen specification YAML to validate")
	}
	resolvedSpec, err := tools.ResolveWorkspacePath(t.workspaceRoot, in.SpecPath)
	if err != nil {
		return tools.Result{}, fmt.Errorf("design.boltzgen_check: spec_path: %w", err)
	}
	in.SpecPath = resolvedSpec
	resolved, err := json.Marshal(in)
	if err != nil {
		return tools.Result{}, fmt.Errorf("design.boltzgen_check: %w", err)
	}

	out, err := t.backend.Run(ctx, "design.boltzgen_check", resolved, io.Discard, nil)
	if err != nil {
		return tools.Result{}, fmt.Errorf("design.boltzgen_check: %w", err)
	}

	var result boltzGenCheckResult
	if err := json.Unmarshal(out, &result); err != nil {
		return tools.Result{}, fmt.Errorf("design.boltzgen_check: backend output is not valid JSON: %w", err)
	}
	if result.Errors == nil {
		result.Errors = []string{}
	}

	return tools.Result{
		Output:     out,
		Display:    boltzGenCheckDisplay(result),
		Provenance: domain.NewToolCallRef("design.boltzgen_check", input),
	}, nil
}

// boltzGenCheckDisplay renders a one-line human/LLM summary of a check result.
func boltzGenCheckDisplay(r boltzGenCheckResult) string {
	if r.Valid {
		if r.VisualizationPath != "" {
			return "boltzgen check: spec is valid — visualization written to " + r.VisualizationPath
		}
		return "boltzgen check: spec is valid"
	}
	if len(r.Errors) == 0 {
		return "boltzgen check: spec is invalid"
	}
	return "boltzgen check: spec is invalid — " + strings.Join(r.Errors, "; ")
}
