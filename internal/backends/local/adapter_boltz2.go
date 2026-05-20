package local

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// init registers the Boltz-2 fold adapter with the local backend.
func init() { registerAdapter(boltz2Adapter{}) }

// boltz2Adapter wires fold.boltz2 to the container-mode Boltz-2 image: it
// turns the agent's {sequences, save_as} request into a Boltz-format YAML,
// runs `boltz predict` inside the tool image with the host weights cache
// bind-mounted at /models, and returns the produced PDB(s) in the
// {"designs":[...]} envelope shared with the design adapters.
type boltz2Adapter struct{}

func (boltz2Adapter) AgentTool() string { return "fold.boltz2" }
func (boltz2Adapter) Recipe() string    { return "boltz2" }

// boltz2Request is the subset of the fold.boltz2 input the adapter uses.
//   - Sequences: chain id → amino-acid sequence (every chain becomes a protein
//     record in the Boltz YAML, with `msa: empty` so the in-image MSA dance is
//     skipped — matches the smoke_test).
//   - SaveAs: optional workspace-side path the parsed PDB is copied to so the
//     caller can locate the structure outside env.WorkDir (the temp dir is
//     wiped on return from RunDesign).
type boltz2Request struct {
	Sequences map[string]string `json:"sequences"`
	SaveAs    string            `json:"save_as"`
}

// sortedChains returns the chain IDs in deterministic alphabetical order so
// the YAML written into env.WorkDir is stable across runs (and testable).
func sortedChains(seqs map[string]string) []string {
	keys := make([]string, 0, len(seqs))
	for k := range seqs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// writeBoltz2YAML renders the Boltz-2 input file. The schema mirrors the
// upstream README example (sequences:\n  - protein:\n      id: …\n
// sequence: …\n      msa: empty). msa: empty is critical: it skips the
// in-process MSA server (which we don't have on the GB10) so `boltz predict`
// folds the sequence as-is.
func writeBoltz2YAML(path string, seqs map[string]string) error {
	var b strings.Builder
	b.WriteString("sequences:\n")
	for _, id := range sortedChains(seqs) {
		fmt.Fprintf(&b, "  - protein:\n      id: %s\n      sequence: %s\n      msa: empty\n",
			id, seqs[id])
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

// parseBoltz2Output collects every PDB under outDir (Boltz writes per-input
// subdirectories like predictions/<stem>/<stem>_model_0.pdb), returning one
// designOut per file. The structure_file path is host-side so the caller can
// open it directly. Sequence and scores are left empty — Boltz-2 doesn't emit
// the per-model confidence as part of the PDB header.
func parseBoltz2Output(outDir string) ([]designOut, error) {
	var pdbs []string
	err := filepath.Walk(outDir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if strings.HasSuffix(strings.ToLower(p), ".pdb") {
			pdbs = append(pdbs, p)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(pdbs)
	var designs []designOut
	for _, p := range pdbs {
		designs = append(designs, designOut{
			Sequence:      map[string]string{},
			StructureFile: p,
			Scores:        map[string]float64{},
		})
	}
	if len(designs) == 0 {
		return nil, fmt.Errorf("fold.boltz2: no PDB files found under %s", outDir)
	}
	return designs, nil
}

// Invoke writes the YAML, runs `boltz predict` inside the tool image, and
// parses the produced PDB(s) into the {"designs":[...]} envelope. The image's
// ENTRYPOINT is ["boltz", "predict"], so Cmd starts with the YAML path.
func (boltz2Adapter) Invoke(ctx context.Context, env AdapterEnv, request []byte) ([]byte, error) {
	var req boltz2Request
	if err := json.Unmarshal(request, &req); err != nil {
		return nil, fmt.Errorf("fold.boltz2: invalid request: %w", err)
	}
	if len(req.Sequences) == 0 {
		return nil, fmt.Errorf("fold.boltz2: at least one chain is required in \"sequences\"")
	}
	for id, seq := range req.Sequences {
		if strings.TrimSpace(seq) == "" {
			return nil, fmt.Errorf("fold.boltz2: chain %q has an empty sequence", id)
		}
	}
	if env.Registry == nil {
		return nil, fmt.Errorf("fold.boltz2: adapter registry unavailable")
	}
	if env.Recipe.ImageTag == "" {
		return nil, fmt.Errorf("fold.boltz2: container image is not configured (run /install boltz2)")
	}

	inputYAML := filepath.Join(env.WorkDir, "in.yaml")
	if err := writeBoltz2YAML(inputYAML, req.Sequences); err != nil {
		return nil, fmt.Errorf("fold.boltz2: write input YAML: %w", err)
	}
	outDir := filepath.Join(env.WorkDir, "out")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, err
	}
	env.Tick(0.05) // input staged

	rt := Detect()
	if !rt.Available() {
		return nil, fmt.Errorf("fold.boltz2: no container runtime — install podman or docker")
	}
	if ok, _ := rt.ImageExists(env.Recipe.ImageTag); !ok {
		return nil, fmt.Errorf(
			"fold.boltz2: image %s is missing — run /install boltz2",
			env.Recipe.ImageTag)
	}

	mounts := []Mount{{HostPath: env.WorkDir, ContainerPath: "/work"}}
	modelsCache := ModelsRoot(env.Registry.Home(), "boltz2")
	if info, err := os.Stat(modelsCache); err == nil && info.IsDir() {
		mounts = append(mounts, Mount{HostPath: modelsCache, ContainerPath: "/models"})
	} else {
		return nil, fmt.Errorf(
			"fold.boltz2: weights cache %s missing — run /install boltz2",
			modelsCache)
	}

	// ENTRYPOINT is ["boltz", "predict"]; Cmd carries the YAML and flags.
	// --no_kernels is required on sm_121 (GB10) per upstream issue #663.
	// --override prevents Boltz from skipping the run when /work/out exists
	// (the workdir is fresh per call, but rerunning a cached subdir would
	// be a silent no-op without it).
	cmd := []string{
		"/work/in.yaml",
		"--out_dir", "/work/out",
		"--cache", "/models",
		"--output_format", "pdb",
		"--no_kernels",
		"--override",
	}
	if _, err := rt.RunContainer(ctx, ContainerRunArgs{
		Image:   env.Recipe.ImageTag,
		Cmd:     cmd,
		GPU:     env.Recipe.GPU,
		Mounts:  mounts,
		Workdir: "/work",
		Log:     env.LogWriter(),
	}); err != nil {
		return nil, fmt.Errorf("fold.boltz2: container run failed: %w", err)
	}
	env.Tick(0.95) // boltz predict done

	designs, err := parseBoltz2Output(outDir)
	if err != nil {
		return nil, err
	}

	// Optional: copy the first structure to the workspace-side path the caller
	// requested. The temp WorkDir is removed when RunDesign returns, so without
	// this hop the structure_file path would dangle.
	if dst := strings.TrimSpace(req.SaveAs); dst != "" {
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return nil, fmt.Errorf("fold.boltz2: stage save_as parent: %w", err)
		}
		if err := copyFile(designs[0].StructureFile, dst); err != nil {
			return nil, fmt.Errorf("fold.boltz2: copy structure to %s: %w", dst, err)
		}
		designs[0].StructureFile = dst
	}
	return json.Marshal(designsEnvelope{Designs: designs})
}
