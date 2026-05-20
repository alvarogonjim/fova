package viz

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/alvarogonjim/fova/internal/domain"
	"github.com/alvarogonjim/fova/internal/tools"
)

// lookPath is a test seam — overridden in tests to fake the presence/absence
// of the pymol binary. Defaults to exec.LookPath.
var lookPath = exec.LookPath

// pymolTimeout caps any single render so a hung pymol cannot wedge the agent.
const pymolTimeout = 30 * time.Second

// PyMolRender implements viz.pymol_render: a ray-traced PNG of a structure
// via the PyMOL CLI. PyMOL must be on $PATH; absence is reported at call
// time so the rest of the registry still loads when it is missing.
type PyMolRender struct {
	noopMeta
	workspace string
}

// NewPyMolRender builds the viz.pymol_render tool.
func NewPyMolRender(workspace string) *PyMolRender {
	return &PyMolRender{workspace: workspace}
}

func (*PyMolRender) Name() string { return "viz.pymol_render" }
func (*PyMolRender) Description() string {
	return "Render a ray-traced PNG of a PDB structure via the PyMOL CLI (must be on PATH)."
}
func (*PyMolRender) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"pdb": map[string]any{"type": "string", "description": "Path to a PDB file."},
		},
		"required": []string{"pdb"},
	}
}

// EstimatedDuration overrides the package default — a PyMOL ray is slow.
func (*PyMolRender) EstimatedDuration(json.RawMessage) time.Duration { return 15 * time.Second }

func (t *PyMolRender) Execute(ctx context.Context, input json.RawMessage) (tools.Result, error) {
	var in struct {
		PDB string `json:"pdb"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return tools.Result{}, fmt.Errorf("viz.pymol_render: parse input: %w", err)
	}
	if in.PDB == "" {
		return tools.Result{}, fmt.Errorf("viz.pymol_render: pdb is required")
	}
	if _, err := os.Stat(in.PDB); err != nil {
		return tools.Result{}, fmt.Errorf("viz.pymol_render: stat pdb: %w", err)
	}
	bin, err := lookPath("pymol")
	if err != nil {
		return tools.Result{}, fmt.Errorf("viz.pymol_render: pymol not found on PATH; install PyMOL or use viz.ascii_structure as a fallback")
	}
	outPath, err := OutputPath(t.workspace, "pymol_render", "png")
	if err != nil {
		return tools.Result{}, fmt.Errorf("viz.pymol_render: %w", err)
	}

	script := fmt.Sprintf("load %s; bg_color white; orient; ray 600,600; png %s", in.PDB, outPath)
	cctx, cancel := context.WithTimeout(ctx, pymolTimeout)
	defer cancel()
	cmd := exec.CommandContext(cctx, bin, "-c", "-q", "-d", script)
	combined, runErr := cmd.CombinedOutput()
	if runErr != nil {
		if cctx.Err() == context.DeadlineExceeded {
			return tools.Result{}, fmt.Errorf("viz.pymol_render: timed out after %s", pymolTimeout)
		}
		return tools.Result{}, fmt.Errorf("viz.pymol_render: pymol exited with %w: %s", runErr, string(combined))
	}
	if st, err := os.Stat(outPath); err != nil || st.Size() == 0 {
		return tools.Result{}, fmt.Errorf("viz.pymol_render: pymol produced no PNG at %s", outPath)
	}
	body, _ := json.Marshal(map[string]any{"path": outPath})
	return tools.Result{
		Output:     body,
		Display:    fmt.Sprintf("pymol_render: %s → %s", in.PDB, outPath),
		Provenance: domain.NewToolCallRef("viz.pymol_render", input),
	}, nil
}
