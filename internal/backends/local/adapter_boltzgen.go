package local

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// boltzGenDefaultProtocol is BoltzGen's protein-vs-anything binder protocol —
// the right default for "design a protein binder against this target", which
// is the shape the agent's design tool exposes.
const boltzGenDefaultProtocol = "protein-anything"

// boltzGenMaxBudget is the upper bound on the diversity-optimized final set:
// BoltzGen ranks up to this many designs even if num_designs is much larger.
// Picked to match the README's "first run with --num_designs 50, --budget 2"
// guidance scaled up: 10 is a sensible cap for agent-driven runs.
const boltzGenMaxBudget = 10

// init registers the BoltzGen adapter with the local backend.
func init() { registerAdapter(boltzGenAdapter{}) }

// boltzGenAdapter wires design.boltzgen to the installed BoltzGen tool.
type boltzGenAdapter struct{}

func (boltzGenAdapter) AgentTool() string { return "design.boltzgen" }
func (boltzGenAdapter) Recipe() string    { return "boltzgen" }

// boltzGenRequest is the subset of the design.boltzgen input the adapter uses.
// Mirrors the design.rfdiffusion shape (target + hotspots + num_designs) with
// an optional protocol override.
type boltzGenRequest struct {
	Target     string `json:"target"`
	Hotspots   string `json:"hotspots"`
	NumDesigns int    `json:"num_designs"`
	Protocol   string `json:"protocol"`
}

// Invoke runs BoltzGen for one target: writes a design-spec YAML, runs the
// container, and collects the final diversity-optimized CIFs into the
// {"designs":[...]} schema.
func (boltzGenAdapter) Invoke(ctx context.Context, env AdapterEnv, request []byte) ([]byte, error) {
	var req boltzGenRequest
	if err := json.Unmarshal(request, &req); err != nil {
		return nil, fmt.Errorf("design.boltzgen: invalid request: %w", err)
	}
	target := strings.TrimSpace(req.Target)
	if target == "" {
		return nil, fmt.Errorf("design.boltzgen: target is required (path to a .pdb or .cif target structure)")
	}
	low := strings.ToLower(target)
	if !strings.HasSuffix(low, ".pdb") && !strings.HasSuffix(low, ".cif") {
		return nil, fmt.Errorf("design.boltzgen: target %q must be a .pdb or .cif file", target)
	}
	if info, err := os.Stat(target); err != nil || info.IsDir() {
		return nil, fmt.Errorf(
			"design.boltzgen: target %q not found (workspace root). "+
				"Use fs.read_structure or fs.bash to confirm the file exists, "+
				"or pass an absolute path.",
			target)
	}
	if env.Registry == nil {
		return nil, fmt.Errorf("design.boltzgen: adapter registry unavailable")
	}
	if env.Recipe.ImageTag == "" {
		// BoltzGen is container-only — there is no legacy venv-mode path.
		return nil, fmt.Errorf("design.boltzgen: boltzgen is not installed (run /install boltzgen)")
	}

	numDesigns := req.NumDesigns
	if numDesigns < 1 {
		numDesigns = 1
	}
	budget := numDesigns
	if budget > boltzGenMaxBudget {
		budget = boltzGenMaxBudget
	}
	protocol := strings.TrimSpace(req.Protocol)
	if protocol == "" {
		protocol = boltzGenDefaultProtocol
	}

	// Stage the target into env.WorkDir so BoltzGen can resolve the file
	// reference inside the yaml (file paths are interpreted relative to the
	// yaml directory per the upstream README).
	stagedTarget := filepath.Base(target)
	if err := copyFile(target, filepath.Join(env.WorkDir, stagedTarget)); err != nil {
		return nil, fmt.Errorf("design.boltzgen: stage target: %w", err)
	}

	yamlBody, err := buildBoltzGenSpec(stagedTarget, req.Hotspots)
	if err != nil {
		return nil, fmt.Errorf("design.boltzgen: build yaml: %w", err)
	}
	specPath := filepath.Join(env.WorkDir, "in.yaml")
	if err := os.WriteFile(specPath, []byte(yamlBody), 0o644); err != nil {
		return nil, fmt.Errorf("design.boltzgen: write yaml: %w", err)
	}
	env.Tick(0.05) // yaml + target staged

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

	cmd := []string{
		"run", "/work/in.yaml",
		"--output", "/work/out",
		"--protocol", protocol,
		"--num_designs", strconv.Itoa(numDesigns),
		"--budget", strconv.Itoa(budget),
	}
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
	finalDir := filepath.Join(outDir, "final_ranked_designs",
		fmt.Sprintf("final_%d_designs", budget))

	// Persist the run outputs under FOVA_HOME/designs/boltzgen-<ns> so the
	// CIFs outlive the temp WorkDir RunDesign removes on return.
	persistedDir := filepath.Join(env.Registry.Home(), "designs",
		fmt.Sprintf("boltzgen-%d", time.Now().UnixNano()))
	if err := os.MkdirAll(persistedDir, 0o755); err != nil {
		return nil, err
	}
	designs, err := parseBoltzGenOutput(finalDir, outDir, persistedDir)
	if err != nil {
		return nil, err
	}
	return json.Marshal(designsEnvelope{Designs: designs})
}

// buildBoltzGenSpec writes a minimal BoltzGen design-spec YAML for a single
// designed protein chain (id "B") plus the target loaded from a .cif/.pdb
// file. When hotspots is non-empty, it becomes the target's binding_types,
// telling BoltzGen which target residues the designed protein should bind.
//
// Hotspots are accepted in the design.rfdiffusion format ("A30,A33" — chain
// letters allowed) but BoltzGen expects residue indices only, so we strip the
// chain letters before emitting them.
func buildBoltzGenSpec(targetFile, hotspots string) (string, error) {
	if targetFile == "" {
		return "", fmt.Errorf("target file is required")
	}
	var b strings.Builder
	b.WriteString("entities:\n")
	// Designed protein chain — BoltzGen samples a length between 80 and 140,
	// which mirrors the upstream vanilla_protein/1g13prot.yaml default.
	b.WriteString("  - protein:\n")
	b.WriteString("      id: B\n")
	b.WriteString("      sequence: 80..140\n")
	// Target from file.
	b.WriteString("  - file:\n")
	b.WriteString("      path: " + targetFile + "\n")
	if h := normalizeBoltzGenHotspots(hotspots); h != "" {
		// binding_types only meaningful when we have a chain to scope to;
		// BoltzGen's example beetletert.yaml scopes to chain A — same default
		// here (the include block also pins chain A so the indices match).
		b.WriteString("      include:\n")
		b.WriteString("        - chain:\n")
		b.WriteString("            id: A\n")
		b.WriteString("      binding_types:\n")
		b.WriteString("        - chain:\n")
		b.WriteString("            id: A\n")
		b.WriteString("            binding: " + h + "\n")
	}
	return b.String(), nil
}

// normalizeBoltzGenHotspots strips chain prefixes from a comma-separated list
// of residue tokens ("A30, A33,B12") and returns "30,33,12". Empty input or
// tokens without trailing digits are skipped.
func normalizeBoltzGenHotspots(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	var out []string
	for _, tok := range strings.Split(raw, ",") {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			continue
		}
		// Trim a single chain letter prefix if present.
		digits := tok
		for len(digits) > 0 && (digits[0] < '0' || digits[0] > '9') {
			digits = digits[1:]
		}
		if digits == "" {
			continue
		}
		// Make sure the residue index actually parses as an int — guards
		// against weird tokens like "A30-35" leaking into the yaml.
		if _, err := strconv.Atoi(digits); err != nil {
			continue
		}
		out = append(out, digits)
	}
	return strings.Join(out, ",")
}

// parseBoltzGenOutput collects the CIF files BoltzGen wrote into the final
// diversity-optimized directory, copying each one into persistedDir so the
// path outlives the adapter's temp WorkDir. Scores come from
// final_designs_metrics_<budget>.csv (sibling of finalDir) when present;
// when the CSV is missing the scores map is left empty.
func parseBoltzGenOutput(finalDir, outDir, persistedDir string) ([]designOut, error) {
	cifs, err := filepath.Glob(filepath.Join(finalDir, "*.cif"))
	if err != nil {
		return nil, err
	}
	if len(cifs) == 0 {
		return nil, fmt.Errorf("design.boltzgen: no CIFs found in %s", finalDir)
	}
	// Scores: pulled from final_ranked_designs/final_designs_metrics_<budget>.csv
	// when present. The CSV is in the parent dir (outDir/final_ranked_designs)
	// and keyed by design stem.
	scoresByStem := map[string]map[string]float64{}
	parent := filepath.Dir(finalDir) // .../final_ranked_designs
	matches, _ := filepath.Glob(filepath.Join(parent, "final_designs_metrics_*.csv"))
	if len(matches) > 0 {
		if m, err := parseBoltzGenMetricsCSV(matches[0]); err == nil {
			scoresByStem = m
		}
	}

	var designs []designOut
	for _, cif := range cifs {
		dest := filepath.Join(persistedDir, filepath.Base(cif))
		if err := copyFile(cif, dest); err != nil {
			return nil, fmt.Errorf("design.boltzgen: persist %s: %w", cif, err)
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
	return designs, nil
}

// parseBoltzGenMetricsCSV reads final_designs_metrics_<budget>.csv into a map
// keyed by design name (the CIF stem). Numeric columns become scores; the
// "design" column (or first column when no explicit name column is present)
// becomes the key. A missing file yields an empty map and no error so a
// successful run without metrics still produces designs.
func parseBoltzGenMetricsCSV(path string) (map[string]map[string]float64, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]map[string]float64{}, nil
		}
		return nil, err
	}
	defer f.Close()
	rows, err := csv.NewReader(f).ReadAll()
	if err != nil {
		return nil, err
	}
	out := map[string]map[string]float64{}
	if len(rows) < 2 {
		return out, nil
	}
	header := rows[0]
	for _, row := range rows[1:] {
		scores := map[string]float64{}
		name := ""
		for i, col := range header {
			if i >= len(row) {
				break
			}
			val := strings.TrimSpace(row[i])
			switch strings.ToLower(strings.TrimSpace(col)) {
			case "design", "design_name", "name":
				if name == "" {
					name = val
				}
			default:
				if v, err := strconv.ParseFloat(val, 64); err == nil {
					scores[strings.TrimSpace(col)] = v
				}
			}
		}
		if name == "" && len(row) > 0 {
			name = strings.TrimSpace(row[0])
		}
		if name != "" {
			out[strings.TrimSuffix(name, ".cif")] = scores
		}
	}
	return out, nil
}
