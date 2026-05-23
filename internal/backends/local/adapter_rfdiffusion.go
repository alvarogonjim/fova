package local

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/alvarogonjim/fova/internal/domain"
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

// rfdiffusionRequest is the typed adapter request — an alias of the canonical
// domain.RFdiffusionParams so the schema is owned in one place and the adapter
// covers every Hydra override fova advertises.
type rfdiffusionRequest = domain.RFdiffusionParams

// rfdiffusionArgs compiles the full set of Hydra `key=value` overrides for one
// run of `python /opt/rfdiffusion/scripts/run_inference.py`. Each typed field
// in p maps onto its dotted Hydra key per THE CONTRACT — and is emitted only
// when explicitly set (non-zero / non-nil), so RFdiffusion's own defaults
// govern everything the agent didn't ask about.
//
// The caller layers in `inference.output_prefix=...` separately (fova owns
// the output path).
func rfdiffusionArgs(p domain.RFdiffusionParams, ckpt string) []string {
	var args []string
	if p.Target != "" {
		args = append(args, "inference.input_pdb="+p.Target)
	}
	if p.Contigs != "" {
		args = append(args, fmt.Sprintf("'contigmap.contigs=[%s]'", p.Contigs))
	}
	if p.NumDesigns > 0 {
		args = append(args, "inference.num_designs="+strconv.Itoa(p.NumDesigns))
	}
	if ckpt != "" {
		args = append(args, "inference.ckpt_override_path="+ckpt)
	}
	if p.Deterministic != nil {
		args = append(args, "inference.deterministic="+strconv.FormatBool(*p.Deterministic))
	}
	if p.Symmetric != nil {
		args = append(args, "inference.symmetric="+strconv.FormatBool(*p.Symmetric))
	}
	if p.SymmetryKind != "" {
		args = append(args, "symmetry.symmetry_kind="+p.SymmetryKind)
	}
	if p.NChains > 0 {
		args = append(args, "symmetry.n_chains="+strconv.Itoa(p.NChains))
	}
	if p.PartialT > 0 {
		args = append(args, "diffuser.partial_T="+strconv.Itoa(p.PartialT))
	}
	if p.NoiseScaleCA != nil {
		args = append(args, "diffuser.noise_scale_ca="+strconv.FormatFloat(*p.NoiseScaleCA, 'g', -1, 64))
	}
	if p.NoiseScaleFrame != nil {
		args = append(args, "diffuser.noise_scale_frame="+strconv.FormatFloat(*p.NoiseScaleFrame, 'g', -1, 64))
	}
	if h := strings.TrimSpace(p.Hotspots); h != "" {
		args = append(args, fmt.Sprintf("'ppi.hotspot_res=[%s]'", h))
	}
	if len(p.GuidingPotentials) > 0 {
		args = append(args, fmt.Sprintf("'potentials.guiding_potentials=[%s]'",
			strings.Join(p.GuidingPotentials, ",")))
	}
	if p.GuideScale != nil {
		args = append(args, "potentials.guide_scale="+strconv.FormatFloat(*p.GuideScale, 'g', -1, 64))
	}
	return args
}

// Invoke runs RFdiffusion for one typed RFdiffusionParams request and collects
// the generated backbone PDBs into the {"designs":[...]} schema. Preserves
// the container-mode + venv-mode dual path and the Base-vs-Complex_base ckpt
// auto-pick.
func (rfdiffusionAdapter) Invoke(ctx context.Context, env AdapterEnv, request []byte) ([]byte, error) {
	var req rfdiffusionRequest
	if err := json.Unmarshal(request, &req); err != nil {
		return nil, fmt.Errorf("design.rfdiffusion: invalid request: %w", err)
	}
	// Defensive backstop — the bespoke tool's Validate already rejects this,
	// but the adapter cannot trust its caller.
	contigs := strings.TrimSpace(req.Contigs)
	if contigs == "" {
		return nil, fmt.Errorf("design.rfdiffusion: contigs is required (the RFdiffusion contig map)")
	}
	req.Contigs = contigs
	target := strings.TrimSpace(req.Target)
	if target != "" {
		if !strings.HasSuffix(target, ".pdb") {
			return nil, fmt.Errorf("design.rfdiffusion: target %q must be a .pdb file", target)
		}
		if info, err := os.Stat(target); err != nil || info.IsDir() {
			return nil, fmt.Errorf(
				"design.rfdiffusion: target %q not found (workspace root). "+
					"Use fs.read_structure or fs.bash to confirm the file exists, "+
					"or pass an absolute path.",
				target)
		}
	}
	req.Target = target
	if env.Registry == nil {
		return nil, fmt.Errorf("design.rfdiffusion: adapter registry unavailable")
	}
	outDir := filepath.Join(env.Registry.Home(), "designs",
		fmt.Sprintf("rfdiffusion-%d", time.Now().UnixNano()))
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, err
	}
	env.Tick(0.05) // output dir staged

	if env.Recipe.ImageTag != "" {
		// Container-mode: weights are bind-mounted from
		// ~/.fova/models/rfdiffusion/ at /models inside the container;
		// the target PDB and outDir are bind-mounted via env.WorkDir at /work.
		rt := Detect()
		if !rt.Available() {
			return nil, fmt.Errorf("design.rfdiffusion: no container runtime — install podman or docker")
		}
		if ok, _ := rt.ImageExists(env.Recipe.ImageTag); !ok {
			return nil, fmt.Errorf(
				"design.rfdiffusion: image %s is missing — run /install rfdiffusion",
				env.Recipe.ImageTag)
		}
		// Container weights live at /models (bind-mounted via WeightsPaths in
		// tools.toml). Pick the binder-vs-unconditional ckpt name; same logic
		// as the legacy adapter.
		ckpt := "/models/Base_ckpt.pt"
		// Stage the target (if any) into the workdir so the container sees it
		// at a fixed /work-relative path, then rewrite the param for the
		// shared rfdiffusionArgs builder.
		if target != "" {
			ckpt = "/models/Complex_base_ckpt.pt"
			containerTarget := filepath.Join(env.WorkDir, filepath.Base(target))
			if err := copyFile(target, containerTarget); err != nil {
				return nil, fmt.Errorf("design.rfdiffusion: stage target: %w", err)
			}
			req.Target = "/work/" + filepath.Base(target)
		}
		args := append(rfdiffusionArgs(req, ckpt), "inference.output_prefix=/work/out")

		mounts := []Mount{{HostPath: env.WorkDir, ContainerPath: "/work"}}
		modelsCache := ModelsRoot(env.Registry.Home(), "rfdiffusion")
		if info, err := os.Stat(modelsCache); err == nil && info.IsDir() {
			mounts = append(mounts, Mount{HostPath: modelsCache, ContainerPath: "/models"})
		} else {
			return nil, fmt.Errorf(
				"design.rfdiffusion: weights cache %s missing — run /install rfdiffusion",
				modelsCache)
		}
		// The Containerfile's ENTRYPOINT is `python /opt/rfdiffusion/scripts/run_inference.py`.
		if _, err := rt.RunContainer(ctx, ContainerRunArgs{
			Image:   env.Recipe.ImageTag,
			Cmd:     args,
			GPU:     env.Recipe.GPU,
			Mounts:  mounts,
			Workdir: "/work",
			Log:     env.LogWriter(),
		}); err != nil {
			return nil, fmt.Errorf("design.rfdiffusion: container run failed: %w", err)
		}
		// Collect outputs from /work/out_*.pdb on the host (since /work was
		// bind-mounted from env.WorkDir).
		if _, err := os.Stat(filepath.Join(env.WorkDir, "out_0.pdb")); err == nil {
			outDir = env.WorkDir
		}
	} else {
		// Legacy venv-mode (pre-v0.7 install).
		if info, err := os.Stat(env.Recipe.InstallDir); err != nil || !info.IsDir() {
			return nil, fmt.Errorf("design.rfdiffusion: rfdiffusion is not installed (run /install rfdiffusion)")
		}
		if info, err := os.Stat(env.Recipe.VenvDir); err != nil || !info.IsDir() {
			return nil, fmt.Errorf("design.rfdiffusion: rfdiffusion is not installed (run /install rfdiffusion)")
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
		// Venv-mode: prepend python + run_inference.py, append the host
		// output prefix; the rest of the overrides come from rfdiffusionArgs.
		cmd := strings.Join(append(
			[]string{
				filepath.Join(env.Recipe.VenvDir, "bin", "python"),
				filepath.Join(env.Recipe.InstallDir, "scripts", "run_inference.py"),
				"inference.output_prefix=" + filepath.Join(outDir, "out"),
			},
			rfdiffusionArgs(req, ckpt)...,
		), " ")
		if out, err := env.Run(ctx, env.Recipe.InstallDir, cmd, env.LogWriter()); err != nil {
			return nil, fmt.Errorf("design.rfdiffusion: run failed: %w\n%s", err, out)
		}
	}
	env.Tick(0.95) // run_inference.py done

	designs, err := parseRFdiffusionOutput(outDir)
	if err != nil {
		return nil, err
	}
	return json.Marshal(designsEnvelope{Designs: designs})
}
