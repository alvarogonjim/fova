package local

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// boltzGenDefaultProtocol is BoltzGen's protein-vs-anything binder protocol —
// the right default for "design a protein binder against this target".
const boltzGenDefaultProtocol = "protein-anything"

// init registers the BoltzGen adapter with the local backend.
func init() { registerAdapter(boltzGenAdapter{}) }

// boltzGenAdapter wires design.boltzgen to the installed BoltzGen tool.
type boltzGenAdapter struct{}

func (boltzGenAdapter) AgentTool() string { return "design.boltzgen" }
func (boltzGenAdapter) Recipe() string    { return "boltzgen" }

// boltzGenRequest mirrors design.boltzgen's boltzGenInput: the (already
// workspace-resolved) absolute path to the agent-authored spec YAML plus the
// run-config parameters. Pointer fields distinguish "unset" (omit the flag)
// from a real zero value.
type boltzGenRequest struct {
	SpecPath string `json:"spec_path"`

	Protocol                string   `json:"protocol"`
	NumDesigns              int      `json:"num_designs"`
	Budget                  int      `json:"budget"`
	DiffusionBatchSize      int      `json:"diffusion_batch_size"`
	Steps                   []string `json:"steps"`
	Alpha                   *float64 `json:"alpha"`
	FilterBiased            *bool    `json:"filter_biased"`
	AdditionalFilters       []string `json:"additional_filters"`
	RefoldingRMSDThreshold  *float64 `json:"refolding_rmsd_threshold"`
	InverseFoldNumSequences int      `json:"inverse_fold_num_sequences"`
	InverseFoldAvoid        string   `json:"inverse_fold_avoid"`
	StepScale               *float64 `json:"step_scale"`
	NoiseScale              *float64 `json:"noise_scale"`
	Reuse                   bool     `json:"reuse"`
}

// Invoke runs BoltzGen for one agent-authored spec: it stages the spec plus
// every structure file the spec references into the container workdir,
// translates the run params to `boltzgen run` CLI flags, runs the container,
// and ingests the ranked + scored final design set.
func (boltzGenAdapter) Invoke(ctx context.Context, env AdapterEnv, request []byte) ([]byte, error) {
	var req boltzGenRequest
	if err := json.Unmarshal(request, &req); err != nil {
		return nil, fmt.Errorf("design.boltzgen: invalid request: %w", err)
	}
	specPath := strings.TrimSpace(req.SpecPath)
	if specPath == "" {
		return nil, fmt.Errorf("design.boltzgen: spec_path is required (the BoltzGen specification YAML the agent authored)")
	}
	if info, err := os.Stat(specPath); err != nil || info.IsDir() {
		return nil, fmt.Errorf(
			"design.boltzgen: spec %q not found. Author the BoltzGen specification "+
				"YAML (see the boltzgen-spec skill), validate it with design.boltzgen_check, "+
				"then pass its workspace path.",
			specPath)
	}
	if env.Registry == nil {
		return nil, fmt.Errorf("design.boltzgen: adapter registry unavailable")
	}
	if env.Recipe.ImageTag == "" {
		// BoltzGen is container-only — there is no legacy venv-mode path.
		return nil, fmt.Errorf("design.boltzgen: boltzgen is not installed (run /install boltzgen)")
	}

	// Stage the spec + every structure file it references into env.WorkDir,
	// preserving the relative layout: BoltzGen resolves entities[].file.path
	// relative to the spec file's directory.
	if err := stageBoltzGenSpec(specPath, env.WorkDir); err != nil {
		return nil, fmt.Errorf("design.boltzgen: stage spec: %w", err)
	}
	env.Tick(0.05) // spec + referenced files staged

	rt := Detect()
	if !rt.Available() {
		return nil, fmt.Errorf("design.boltzgen: no container runtime — install podman or docker")
	}
	if ok, _ := rt.ImageExists(env.Recipe.ImageTag); !ok {
		return nil, fmt.Errorf(
			"design.boltzgen: image %s is missing — run /install boltzgen",
			env.Recipe.ImageTag)
	}

	// BoltzGen downloads ~6 GB of weights from HuggingFace on first run via
	// HF_HOME=/models (set in boltzgen.Containerfile). We bind-mount the
	// per-tool cache so weights only download once across runs. The cache is
	// a bind-mount source: an empty directory is the correct pre-state, so
	// create it if absent rather than failing — /install does not pre-fetch
	// runtime-downloaded weights.
	modelsCache := ModelsRoot(env.Registry.Home(), "boltzgen")
	if err := os.MkdirAll(modelsCache, 0o755); err != nil {
		return nil, fmt.Errorf("design.boltzgen: create weights cache %s: %w", modelsCache, err)
	}

	// fova owns the infra flags (--output, --cache) and fixes the spec path
	// at /work/in.yaml; boltzGenArgs maps the agent-facing params.
	cmd := append([]string{
		"run", "/work/in.yaml",
		"--output", "/work/out",
		"--cache", "/models",
	}, boltzGenArgs(req)...)
	mounts := []Mount{
		{HostPath: env.WorkDir, ContainerPath: "/work"},
		{HostPath: modelsCache, ContainerPath: "/models"},
	}
	if _, err := rt.RunContainer(ctx, ContainerRunArgs{
		Image:   env.Recipe.ImageTag,
		Cmd:     cmd,
		GPU:     env.Recipe.GPU,
		Mounts:  mounts,
		Workdir: "/work",
		Log:     env.LogWriter(),
	}); err != nil {
		return nil, fmt.Errorf("design.boltzgen: container run failed: %w", err)
	}
	env.Tick(0.95) // boltzgen pipeline done

	// Outputs land in /work/out on the host (since /work is bind-mounted from
	// env.WorkDir). The final diversity-optimized set is in
	// final_ranked_designs/final_<budget>_designs/.
	outDir := filepath.Join(env.WorkDir, "out")

	// Persist the run outputs under FOVA_HOME/designs/boltzgen-<ns> so the
	// artifacts outlive the temp WorkDir RunDesign removes on return.
	persistedDir := filepath.Join(env.Registry.Home(), "designs",
		fmt.Sprintf("boltzgen-%d", time.Now().UnixNano()))
	if err := os.MkdirAll(persistedDir, 0o755); err != nil {
		return nil, err
	}
	designs, overviewPath, err := ingestBoltzGenOutput(outDir, persistedDir)
	if err != nil {
		return nil, err
	}
	out, err := json.Marshal(designsEnvelope{Designs: designs})
	if err != nil {
		return nil, err
	}
	if overviewPath != "" {
		fmt.Fprintf(env.LogWriter(), "boltzgen: results overview written to %s\n", overviewPath)
	}
	return out, nil
}

// boltzGenArgs maps the agent-facing run params to `boltzgen run` CLI flags.
// It is table-driven: an unset pointer field, an empty slice/string, or a
// non-positive int omits the flag so BoltzGen falls back to its own default.
// fova owns --output / --cache / devices and sets them separately.
func boltzGenArgs(req boltzGenRequest) []string {
	var args []string
	add := func(flag string, vals ...string) { args = append(args, append([]string{flag}, vals...)...) }

	protocol := strings.TrimSpace(req.Protocol)
	if protocol == "" {
		protocol = boltzGenDefaultProtocol
	}
	add("--protocol", protocol)

	if req.NumDesigns > 0 {
		add("--num_designs", strconv.Itoa(req.NumDesigns))
	}
	if req.Budget > 0 {
		add("--budget", strconv.Itoa(req.Budget))
	}
	if req.DiffusionBatchSize > 0 {
		add("--diffusion_batch_size", strconv.Itoa(req.DiffusionBatchSize))
	}
	if len(req.Steps) > 0 {
		// --steps takes a space-separated list of step names.
		add("--steps", req.Steps...)
	}
	if req.Alpha != nil {
		add("--alpha", strconv.FormatFloat(*req.Alpha, 'g', -1, 64))
	}
	if req.FilterBiased != nil {
		add("--filter_biased", strconv.FormatBool(*req.FilterBiased))
	}
	if len(req.AdditionalFilters) > 0 {
		add("--additional_filters", req.AdditionalFilters...)
	}
	if req.RefoldingRMSDThreshold != nil {
		add("--refolding_rmsd_threshold", strconv.FormatFloat(*req.RefoldingRMSDThreshold, 'g', -1, 64))
	}
	if req.InverseFoldNumSequences > 0 {
		add("--inverse_fold_num_sequences", strconv.Itoa(req.InverseFoldNumSequences))
	}
	if strings.TrimSpace(req.InverseFoldAvoid) != "" {
		add("--inverse_fold_avoid", req.InverseFoldAvoid)
	}
	if req.StepScale != nil {
		add("--step_scale", strconv.FormatFloat(*req.StepScale, 'g', -1, 64))
	}
	if req.NoiseScale != nil {
		add("--noise_scale", strconv.FormatFloat(*req.NoiseScale, 'g', -1, 64))
	}
	if req.Reuse {
		args = append(args, "--reuse")
	}
	return args
}

// boltzGenSpec is the minimal view of a BoltzGen specification YAML the
// adapter needs: just the file references inside entities so they can be
// staged alongside the spec. Everything else in the spec is opaque to fova.
type boltzGenSpec struct {
	Entities []struct {
		File *struct {
			Path string `yaml:"path"`
		} `yaml:"file"`
	} `yaml:"entities"`
}

// stageBoltzGenSpec copies the agent-authored spec to workDir/in.yaml and
// every structure file it references into workDir, preserving each file's
// path relative to the spec directory — BoltzGen resolves entities[].file.path
// relative to the spec file's location, so the layout must be reproduced.
func stageBoltzGenSpec(specPath, workDir string) error {
	body, err := os.ReadFile(specPath)
	if err != nil {
		return err
	}
	var spec boltzGenSpec
	if err := yaml.Unmarshal(body, &spec); err != nil {
		return fmt.Errorf("spec %q is not valid YAML: %w", specPath, err)
	}
	// The spec itself goes to a fixed name fova passes on the CLI.
	if err := os.WriteFile(filepath.Join(workDir, "in.yaml"), body, 0o644); err != nil {
		return err
	}
	specDir := filepath.Dir(specPath)
	for _, ent := range spec.Entities {
		if ent.File == nil {
			continue
		}
		rel := strings.TrimSpace(ent.File.Path)
		if rel == "" {
			continue
		}
		// Resolve the referenced file relative to the spec directory.
		src := rel
		if !filepath.IsAbs(src) {
			src = filepath.Join(specDir, rel)
		}
		if info, err := os.Stat(src); err != nil || info.IsDir() {
			return fmt.Errorf("referenced structure file %q (from %q) not found", src, rel)
		}
		// Preserve the relative layout the spec expects. An absolute path in
		// the spec is staged by basename (the in-container spec keeps the
		// original text, so an absolute reference is the agent's choice to
		// own — but staging by basename keeps a single-file run working).
		dstRel := rel
		if filepath.IsAbs(rel) {
			dstRel = filepath.Base(rel)
		}
		dst := filepath.Join(workDir, dstRel)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		if err := copyFile(src, dst); err != nil {
			return fmt.Errorf("stage %q: %w", src, err)
		}
	}
	return nil
}

// ingestBoltzGenOutput reads the final ranked design set BoltzGen wrote under
// outDir/final_ranked_designs: it copies every CIF into persistedDir, attaches
// the per-design scores from final_designs_metrics_<budget>.csv, and copies
// results_overview.pdf out. The budget is discovered from the on-disk
// final_<budget>_designs directory rather than recomputed, so a budget the
// adapter never saw still resolves.
func ingestBoltzGenOutput(outDir, persistedDir string) (designs []designOut, overviewPath string, err error) {
	rankedDir := filepath.Join(outDir, "final_ranked_designs")

	// Locate the final_<budget>_designs directory (there is exactly one).
	finalDirs, err := filepath.Glob(filepath.Join(rankedDir, "final_*_designs"))
	if err != nil {
		return nil, "", err
	}
	if len(finalDirs) == 0 {
		return nil, "", fmt.Errorf("design.boltzgen: no final_<budget>_designs directory under %s", rankedDir)
	}
	finalDir := finalDirs[0]

	cifs, err := filepath.Glob(filepath.Join(finalDir, "*.cif"))
	if err != nil {
		return nil, "", err
	}
	if len(cifs) == 0 {
		return nil, "", fmt.Errorf("design.boltzgen: no CIFs found in %s", finalDir)
	}

	// Metrics: final_designs_metrics_<budget>.csv sits directly under
	// final_ranked_designs. Keyed by the CIF stem (the metrics file_name col).
	scoresByStem := map[string]map[string]float64{}
	if metrics, _ := filepath.Glob(filepath.Join(rankedDir, "final_designs_metrics_*.csv")); len(metrics) > 0 {
		if m, perr := parseBoltzGenMetrics(metrics[0]); perr == nil {
			scoresByStem = m
		}
	}

	for _, cif := range cifs {
		dest := filepath.Join(persistedDir, filepath.Base(cif))
		if err := copyFile(cif, dest); err != nil {
			return nil, "", fmt.Errorf("design.boltzgen: persist %s: %w", cif, err)
		}
		stem := strings.TrimSuffix(filepath.Base(cif), filepath.Ext(cif))
		scores := scoresByStem[stem]
		if scores == nil {
			scores = map[string]float64{}
		}
		designs = append(designs, designOut{
			Sequence:      map[string]string{},
			StructureFile: dest,
			Scores:        scores,
		})
	}

	// results_overview.pdf is written under final_ranked_designs; copy it to a
	// known workspace location so it outlives the temp WorkDir.
	if src := filepath.Join(rankedDir, "results_overview.pdf"); fileExists(src) {
		dst := filepath.Join(persistedDir, "results_overview.pdf")
		if err := copyFile(src, dst); err == nil {
			overviewPath = dst
		}
	}
	return designs, overviewPath, nil
}

// fileExists reports whether path is an existing regular file.
func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// parseBoltzGenMetrics reads a BoltzGen metrics CSV (header row → column
// names; each subsequent row → one design) into a map keyed by design id
// (the CIF stem). The "file_name" column (falling back to "id" / "design" /
// "name", then the first column) supplies the key; every other column whose
// value parses as a float becomes a score. Unknown columns are carried
// through verbatim as raw score keys rather than dropped, per the design doc.
// A missing file yields an empty map and no error so a successful run with no
// metrics still produces designs.
func parseBoltzGenMetrics(path string) (map[string]map[string]float64, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]map[string]float64{}, nil
		}
		return nil, err
	}
	defer f.Close()
	return parseBoltzGenMetricsReader(f)
}

// parseBoltzGenMetricsReader is the testable core of parseBoltzGenMetrics.
func parseBoltzGenMetricsReader(r io.Reader) (map[string]map[string]float64, error) {
	reader := csv.NewReader(r)
	reader.FieldsPerRecord = -1 // tolerate ragged rows
	rows, err := reader.ReadAll()
	if err != nil {
		return nil, err
	}
	out := map[string]map[string]float64{}
	if len(rows) < 2 {
		return out, nil
	}
	header := rows[0]

	// Find the design-name column. file_name is BoltzGen's own key for the
	// final-set CSV; the others are accepted as fallbacks.
	nameCol := -1
	for _, want := range []string{"file_name", "id", "design", "design_name", "name"} {
		for i, col := range header {
			if strings.EqualFold(strings.TrimSpace(col), want) {
				nameCol = i
				break
			}
		}
		if nameCol >= 0 {
			break
		}
	}

	for _, row := range rows[1:] {
		scores := map[string]float64{}
		name := ""
		for i, col := range header {
			if i >= len(row) {
				break
			}
			val := strings.TrimSpace(row[i])
			if i == nameCol {
				name = val
				continue
			}
			// Carry every parseable column through as a raw score key —
			// fova does not curate the column set, so unknown metrics are
			// preserved rather than dropped.
			if v, err := strconv.ParseFloat(val, 64); err == nil {
				scores[strings.TrimSpace(col)] = v
			}
		}
		if name == "" && len(row) > 0 {
			name = strings.TrimSpace(row[0])
		}
		if name == "" {
			continue
		}
		// Key by the CIF stem so it lines up with the staged structure files.
		key := strings.TrimSuffix(name, filepath.Ext(name))
		out[key] = scores
	}
	return out, nil
}
