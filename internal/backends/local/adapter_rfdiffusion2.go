package local

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/alvarogonjim/fova/internal/domain"
)

// rfdiffusion2HydraOverrides returns the positional Hydra overrides for one
// pipeline.py invocation. motifContainerPath is the /work-rooted path of the
// motif PDB once staged by Invoke; an empty string means no motif override.
//
// Always-on overrides:
//
//	--config-name=... + sweep selection (per Benchmark, or the default)
//	outdir=/work/out + hydra.run.dir=/work/out (so the output tree is deterministic)
//	stop_step='end' (the default, full pipeline; can be overridden)
//
// Conditional overrides — emitted only when the corresponding field is set:
//
//	+inference.input_pdb=<motifContainerPath> contigmap.contigs=[<Contigs>]  (when MotifPDB set)
//	inference.num_designs=<N>
//	seed=<N>
//	inference.guidepost_xyz_as_design_bb=True|False
//	inference.idealize_sidechain_outputs=True|False
//	stop_step='<design|end>' (replaces the default when explicit)
func rfdiffusion2HydraOverrides(p domain.RFdiffusion2Params, motifContainerPath string) []string {
	var args []string

	// --config-name + bundled sweep selection.
	switch p.Benchmark {
	case "enzyme_bench_n41":
		args = append(args,
			"--config-name=enzyme_bench_n41_fixedligand",
			"in_proc=True",
		)
	default: // "" or "active_site_demo"
		args = append(args,
			"--config-name=open_source_demo",
			"sweep.benchmarks=active_site_unindexed_atomic_partial_ligand",
		)
	}

	// Deterministic output landing.
	args = append(args, "outdir=/work/out", "hydra.run.dir=/work/out")

	// Motif override.
	if motifContainerPath != "" {
		args = append(args,
			"+inference.input_pdb="+motifContainerPath,
			"contigmap.contigs=["+p.Contigs+"]",
		)
	}

	// Inference toggles.
	if p.NumDesigns > 0 {
		args = append(args, "inference.num_designs="+strconv.Itoa(p.NumDesigns))
	}
	if p.Seed != nil {
		args = append(args, "seed="+strconv.Itoa(*p.Seed))
	}
	if p.GuidepostXYZAsDesignBB != nil {
		args = append(args, "inference.guidepost_xyz_as_design_bb="+pyBool(*p.GuidepostXYZAsDesignBB))
	}
	if p.IdealizeSidechainOutputs != nil {
		args = append(args, "inference.idealize_sidechain_outputs="+pyBool(*p.IdealizeSidechainOutputs))
	}

	// stop_step — default 'end' (full pipeline) unless explicit.
	stop := p.StopStep
	if stop == "" {
		stop = "end"
	}
	args = append(args, "stop_step='"+stop+"'")

	return args
}

// buildRFdiffusion2Driver renders the bash script that drives one pipeline.py
// invocation inside the tool image. The script mkdirs the deterministic
// /work/out landing dir then execs python pipeline.py with the assembled
// Hydra overrides. The container is run with Entrypoint=bash because the
// image ENTRYPOINT is `python /opt/rfdiffusion2/rf_diffusion/benchmark/pipeline.py`
// — we override it so the script can prepare /work/out before exec and so the
// argv shape stays uniform across benchmark/motif variants.
func buildRFdiffusion2Driver(hydraOverrides []string) string {
	var b strings.Builder
	b.WriteString("#!/bin/bash\n")
	b.WriteString("set -euo pipefail\n")
	b.WriteString("mkdir -p /work/out\n")
	b.WriteString("python /opt/rfdiffusion2/rf_diffusion/benchmark/pipeline.py")
	for _, arg := range hydraOverrides {
		b.WriteString(" ")
		b.WriteString(arg)
	}
	b.WriteString("\n")
	return b.String()
}

// pyBool returns "True"/"False" — what Hydra/OmegaConf expect for a bool
// override. Lower-case "true"/"false" works in newer OmegaConf releases but
// the Python-style capitalised form is the safe, upstream-documented choice.
func pyBool(v bool) string {
	if v {
		return "True"
	}
	return "False"
}

// rfdiffusion2ScoreKey folds a CSV header column to a canonical Scores key.
// Unknown columns are returned lower-cased and as-is, so any numeric column
// pipeline.py emits is carried through under its header name.
func rfdiffusion2ScoreKey(col string) string {
	switch strings.TrimSpace(col) {
	case "metrics.IdealizedResidueRMSD.rmsd_constellation":
		return "idealized_residue_rmsd"
	case "motif_ideality_diff":
		return "motif_ideality_diff"
	case "contig_rmsd_des_ref_motif_atom":
		return "motif_rmsd"
	default:
		return strings.ToLower(strings.TrimSpace(col))
	}
}

// readRFdiffusion2Scores parses the metrics CSV emitted by pipeline.py into
// tag -> score map. The first row is the header; the first column ("name" or
// "design", case-insensitive) keys each data row. Numeric columns become
// scores, with the canonical-key folding in rfdiffusion2ScoreKey + everything
// else carried through. A missing or unreadable file yields an empty map —
// never an error, because a dropped score must not fail an otherwise-
// successful design run.
func readRFdiffusion2Scores(csvPath string) map[string]map[string]float64 {
	out := map[string]map[string]float64{}
	f, err := os.Open(csvPath)
	if err != nil {
		return out
	}
	defer f.Close()
	r := csv.NewReader(f)
	r.FieldsPerRecord = -1 // tolerate ragged rows; we key by column index
	rows, err := r.ReadAll()
	if err != nil || len(rows) < 2 {
		return out
	}
	header := rows[0]
	tagCol := -1
	for i, col := range header {
		c := strings.ToLower(strings.TrimSpace(col))
		if c == "name" || c == "design" || c == "tag" {
			tagCol = i
			break
		}
	}
	if tagCol < 0 {
		// Convention violation; nothing useful to extract.
		return out
	}
	for _, row := range rows[1:] {
		if tagCol >= len(row) {
			continue
		}
		tag := strings.TrimSpace(row[tagCol])
		if tag == "" {
			continue
		}
		scores := map[string]float64{}
		for i, col := range header {
			if i == tagCol || i >= len(row) {
				continue
			}
			v, err := strconv.ParseFloat(strings.TrimSpace(row[i]), 64)
			if err != nil {
				continue
			}
			scores[rfdiffusion2ScoreKey(col)] = v
		}
		out[tag] = scores
	}
	return out
}

// parseRFdiffusion2Output walks the pipeline.py output tree under outDir and
// returns one designOut per prediction PDB. The pipeline writes a metrics CSV
// somewhere under outDir/pipeline_outputs/<timestamp>_<config>/; the parser
// glob-searches for the first *.csv it finds and uses its tag column to
// associate scores with PDBs. A missing CSV yields scoreless designs (not an
// error). An empty PDB set is an error.
func parseRFdiffusion2Output(outDir string) ([]designOut, error) {
	pdbs, err := filepath.Glob(filepath.Join(outDir, "**", "*.pdb"))
	if err != nil {
		return nil, err
	}
	// Go's filepath.Glob does not recurse; walk for "**" semantics.
	if len(pdbs) == 0 {
		pdbs, err = walkGlob(outDir, ".pdb")
		if err != nil {
			return nil, err
		}
	}
	sort.Strings(pdbs)
	if len(pdbs) == 0 {
		return nil, fmt.Errorf("design.rfdiffusion2: no prediction PDBs found under %s", outDir)
	}

	csvs, err := walkGlob(outDir, ".csv")
	if err != nil {
		return nil, err
	}
	sort.Strings(csvs)
	scores := map[string]map[string]float64{}
	if len(csvs) > 0 {
		scores = readRFdiffusion2Scores(csvs[0])
	}

	designs := make([]designOut, 0, len(pdbs))
	for _, pdb := range pdbs {
		tag := strings.TrimSuffix(filepath.Base(pdb), filepath.Ext(pdb))
		row := scores[tag]
		if row == nil {
			row = map[string]float64{}
		}
		designs = append(designs, designOut{
			Sequence:      map[string]string{},
			StructureFile: pdb,
			Scores:        row,
		})
	}
	return designs, nil
}

// walkGlob walks root and returns every file whose name ends in suffix.
// Used in lieu of `**` globbing, which Go's filepath.Glob doesn't support.
func walkGlob(root, suffix string) ([]string, error) {
	var matches []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, suffix) {
			matches = append(matches, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return matches, nil
}

// init registers the RFdiffusion2 design adapter with the local backend.
func init() { registerAdapter(rfdiffusion2Adapter{}) }

// rfdiffusion2Adapter wires design.rfdiffusion2 to the container-mode
// RFdiffusion2 image. The image ENTRYPOINT is `python pipeline.py`, but we
// override it to bash so a small driver script can mkdir /work/out and then
// exec pipeline.py with the assembled Hydra overrides — letting the output
// landing tree stay deterministic across benchmark/motif variants. The
// metrics CSV emitted under /work/out/pipeline_outputs/<timestamp>_<config>/
// is parsed back into the {"designs":[...]} envelope.
type rfdiffusion2Adapter struct{}

func (rfdiffusion2Adapter) AgentTool() string { return "design.rfdiffusion2" }
func (rfdiffusion2Adapter) Recipe() string    { return "rfdiffusion2" }

// Invoke stages the motif PDB (when set), assembles the Hydra overrides,
// writes the driver script, runs the RFdiffusion2 image with the entrypoint
// overridden to bash, and parses the metrics CSV + prediction PDBs into the
// {"designs":[...]} envelope.
func (rfdiffusion2Adapter) Invoke(ctx context.Context, env AdapterEnv, request []byte) ([]byte, error) {
	var req domain.RFdiffusion2Params
	if err := json.Unmarshal(request, &req); err != nil {
		return nil, fmt.Errorf("design.rfdiffusion2: invalid request: %w", err)
	}

	if env.Registry == nil {
		return nil, fmt.Errorf("design.rfdiffusion2: adapter registry unavailable")
	}
	if env.Recipe.ImageTag == "" {
		return nil, fmt.Errorf("design.rfdiffusion2: container image is not configured (run /install rfdiffusion2)")
	}

	// Stage the motif PDB into the workdir when set; remember the /work path
	// for the Hydra override.
	motifContainerPath := ""
	if motif := strings.TrimSpace(req.MotifPDB); motif != "" {
		if !strings.HasSuffix(motif, ".pdb") {
			return nil, fmt.Errorf("design.rfdiffusion2: motif_pdb %q must be a .pdb file", motif)
		}
		if info, err := os.Stat(motif); err != nil || info.IsDir() {
			return nil, fmt.Errorf("design.rfdiffusion2: motif_pdb %q not found", motif)
		}
		base := filepath.Base(motif)
		if err := copyFile(motif, filepath.Join(env.WorkDir, base)); err != nil {
			return nil, fmt.Errorf("design.rfdiffusion2: stage motif_pdb: %w", err)
		}
		motifContainerPath = "/work/" + base
	}

	outDir := filepath.Join(env.WorkDir, "out")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, err
	}
	env.Tick(0.05) // input staged

	rt := Detect()
	if !rt.Available() {
		return nil, fmt.Errorf("design.rfdiffusion2: no container runtime — install podman or docker")
	}
	if ok, _ := rt.ImageExists(env.Recipe.ImageTag); !ok {
		return nil, fmt.Errorf(
			"design.rfdiffusion2: image %s is missing — run /install rfdiffusion2",
			env.Recipe.ImageTag)
	}

	// Weights cache (RFD checkpoints + bundled LigandMPNN tied weights) is
	// fetched at /install time. A missing cache means install did not
	// complete, so validate it exists rather than creating it.
	modelsCache := ModelsRoot(env.Registry.Home(), "rfdiffusion2")
	if info, err := os.Stat(modelsCache); err != nil || !info.IsDir() {
		return nil, fmt.Errorf(
			"design.rfdiffusion2: weights cache %s missing — run /install rfdiffusion2",
			modelsCache)
	}

	// Write the driver script with the assembled Hydra overrides.
	overrides := rfdiffusion2HydraOverrides(req, motifContainerPath)
	driver := filepath.Join(env.WorkDir, "run.sh")
	if err := os.WriteFile(driver, []byte(buildRFdiffusion2Driver(overrides)), 0o755); err != nil {
		return nil, fmt.Errorf("design.rfdiffusion2: write driver: %w", err)
	}

	// The recipe declares weights_paths = ["/models/rfdiffusion2"], so the
	// host cache mounts to /models/rfdiffusion2 (not /models).
	mounts := []Mount{
		{HostPath: env.WorkDir, ContainerPath: "/work"},
		{HostPath: modelsCache, ContainerPath: "/models/rfdiffusion2"},
	}
	if _, err := rt.RunContainer(ctx, ContainerRunArgs{
		Image:      env.Recipe.ImageTag,
		Entrypoint: "bash",
		Cmd:        []string{"/work/run.sh"},
		GPU:        env.Recipe.GPU,
		Mounts:     mounts,
		Workdir:    "/work",
		Log:        env.LogWriter(),
	}); err != nil {
		return nil, fmt.Errorf("design.rfdiffusion2: container run failed: %w", err)
	}
	env.Tick(0.95) // pipeline.py done

	designs, err := parseRFdiffusion2Output(outDir)
	if err != nil {
		return nil, err
	}
	return json.Marshal(designsEnvelope{Designs: designs})
}
