package local

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// parseRFdiffusionOutput collects the backbone PDBs RFdiffusion wrote under
// outDir (out_0.pdb, out_1.pdb, ...) into designs with the structure file set.
// RFdiffusion emits backbones only — sequence and scores are left empty.
func parseRFdiffusionOutput(outDir string) ([]designOut, error) {
	files, err := filepath.Glob(filepath.Join(outDir, "out_*.pdb"))
	if err != nil {
		return nil, err
	}
	var designs []designOut
	for _, f := range files {
		designs = append(designs, designOut{
			Sequence:      map[string]string{},
			StructureFile: f,
			Scores:        map[string]float64{},
		})
	}
	if len(designs) == 0 {
		return nil, fmt.Errorf("design.rfdiffusion: no backbone PDBs found in %s", outDir)
	}
	return designs, nil
}

// init registers the RFdiffusion adapter with the local backend.
func init() { registerAdapter(rfdiffusionAdapter{}) }

// rfdiffusionAdapter wires design.rfdiffusion to the installed RFdiffusion tool.
type rfdiffusionAdapter struct{}

func (rfdiffusionAdapter) AgentTool() string { return "design.rfdiffusion" }
func (rfdiffusionAdapter) Recipe() string    { return "rfdiffusion" }

// rfdiffusionRequest is the subset of the design.rfdiffusion input the adapter uses.
type rfdiffusionRequest struct {
	Contigs    string `json:"contigs"`
	Target     string `json:"target"`
	Hotspots   string `json:"hotspots"`
	NumDesigns int    `json:"num_designs"`
}

// Invoke runs RFdiffusion for one contig map and collects the generated
// backbone PDBs into the {"designs":[...]} schema.
func (rfdiffusionAdapter) Invoke(ctx context.Context, env AdapterEnv, request []byte) ([]byte, error) {
	var req rfdiffusionRequest
	if err := json.Unmarshal(request, &req); err != nil {
		return nil, fmt.Errorf("design.rfdiffusion: invalid request: %w", err)
	}
	contigs := strings.TrimSpace(req.Contigs)
	if contigs == "" {
		return nil, fmt.Errorf("design.rfdiffusion: contigs is required (the RFdiffusion contig map)")
	}
	target := strings.TrimSpace(req.Target)
	if target != "" {
		if !strings.HasSuffix(target, ".pdb") {
			return nil, fmt.Errorf("design.rfdiffusion: target %q must be a .pdb file", target)
		}
		if info, err := os.Stat(target); err != nil || info.IsDir() {
			return nil, fmt.Errorf("design.rfdiffusion: target %q does not exist", target)
		}
	}
	if info, err := os.Stat(env.Recipe.InstallDir); err != nil || !info.IsDir() {
		return nil, fmt.Errorf("design.rfdiffusion: rfdiffusion is not installed (run /install rfdiffusion)")
	}
	if info, err := os.Stat(env.Recipe.VenvDir); err != nil || !info.IsDir() {
		return nil, fmt.Errorf("design.rfdiffusion: rfdiffusion is not installed (run /install rfdiffusion)")
	}
	if env.Registry == nil {
		return nil, fmt.Errorf("design.rfdiffusion: adapter registry unavailable")
	}
	asset, ok := env.Registry.DataAsset("rfdiffusion_weights")
	if !ok {
		return nil, fmt.Errorf("design.rfdiffusion: the rfdiffusion_weights data asset is not registered")
	}
	if info, err := os.Stat(asset.TargetDir); err != nil || !info.IsDir() {
		return nil, fmt.Errorf("design.rfdiffusion: RFdiffusion weights missing — install the rfdiffusion_weights data asset")
	}
	ckpt := filepath.Join(asset.TargetDir, "Base_ckpt.pt")
	if target != "" {
		ckpt = filepath.Join(asset.TargetDir, "Complex_base_ckpt.pt")
	}
	numDesigns := req.NumDesigns
	if numDesigns < 1 {
		numDesigns = 1
	}

	outDir := filepath.Join(env.Registry.Home(), "designs",
		fmt.Sprintf("rfdiffusion-%d", time.Now().UnixNano()))
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, err
	}

	cmd := fmt.Sprintf(
		"%s %s inference.output_prefix=%s inference.num_designs=%d inference.ckpt_override_path=%s 'contigmap.contigs=[%s]'",
		filepath.Join(env.Recipe.VenvDir, "bin", "python"),
		filepath.Join(env.Recipe.InstallDir, "scripts", "run_inference.py"),
		filepath.Join(outDir, "out"), numDesigns, ckpt, contigs)
	if target != "" {
		cmd += " inference.input_pdb=" + target
	}
	if h := strings.TrimSpace(req.Hotspots); h != "" {
		cmd += fmt.Sprintf(" 'ppi.hotspot_res=[%s]'", h)
	}
	if out, err := env.Run(ctx, env.Recipe.InstallDir, cmd); err != nil {
		return nil, fmt.Errorf("design.rfdiffusion: run failed: %w\n%s", err, out)
	}

	designs, err := parseRFdiffusionOutput(outDir)
	if err != nil {
		return nil, err
	}
	return json.Marshal(designsEnvelope{Designs: designs})
}
