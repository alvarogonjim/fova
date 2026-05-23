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

	"github.com/alvarogonjim/fova/internal/domain"
)

// bindCraftStats is one design's CSV-derived sequence and scores.
type bindCraftStats struct {
	sequence string
	scores   map[string]float64
}

// parseBindCraftStatsCSV reads final_design_stats.csv into a map keyed by
// design name (the PDB stem). A missing file yields an empty map and no error.
func parseBindCraftStatsCSV(path string) (map[string]bindCraftStats, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]bindCraftStats{}, nil
		}
		return nil, err
	}
	defer f.Close()
	rows, err := csv.NewReader(f).ReadAll()
	if err != nil {
		return nil, err
	}
	out := map[string]bindCraftStats{}
	if len(rows) < 2 {
		return out, nil
	}
	header := rows[0]
	for _, row := range rows[1:] {
		st := bindCraftStats{scores: map[string]float64{}}
		name := ""
		for i, col := range header {
			if i >= len(row) {
				break
			}
			val := strings.TrimSpace(row[i])
			switch strings.ToLower(strings.TrimSpace(col)) {
			case "design", "design_name":
				if name == "" {
					name = val
				}
			case "sequence":
				st.sequence = val
			default:
				if v, err := strconv.ParseFloat(val, 64); err == nil {
					st.scores[strings.TrimSpace(col)] = v
				}
			}
		}
		if name == "" && len(row) > 0 {
			name = strings.TrimSpace(row[0])
		}
		if name != "" {
			out[strings.TrimSuffix(name, ".pdb")] = st
		}
	}
	return out, nil
}

// parseBindCraftOutput collects accepted BindCraft designs from designPath:
// the PDBs in Accepted/, enriched with sequence and scores from
// final_design_stats.csv when that file is present.
func parseBindCraftOutput(designPath string) ([]designOut, error) {
	pdbs, err := filepath.Glob(filepath.Join(designPath, "Accepted", "*.pdb"))
	if err != nil {
		return nil, err
	}
	stats, err := parseBindCraftStatsCSV(filepath.Join(designPath, "final_design_stats.csv"))
	if err != nil {
		return nil, err
	}
	var designs []designOut
	for _, pdb := range pdbs {
		stem := strings.TrimSuffix(filepath.Base(pdb), ".pdb")
		d := designOut{
			Sequence:      map[string]string{},
			StructureFile: pdb,
			Scores:        map[string]float64{},
		}
		if st, ok := stats[stem]; ok {
			if st.sequence != "" {
				d.Sequence["A"] = st.sequence
			}
			d.Scores = st.scores
		}
		designs = append(designs, d)
	}
	if len(designs) == 0 {
		return nil, fmt.Errorf("design.bindcraft: no accepted designs found in %s", designPath)
	}
	return designs, nil
}

// buildBindCraftSettingsJSON compiles a BindCraft target-settings JSON from
// the typed BindCraftParams. Zero-value fields are omitted so BindCraft
// applies its own defaults — fova never advertises an opaque settings blob.
func buildBindCraftSettingsJSON(p domain.BindCraftParams) string {
	m := map[string]any{
		"starting_pdb":            p.StartingPDB,
		"chains":                  p.Chains,
		"target_hotspot_residues": p.TargetHotspotResidues,
		"lengths":                 []int{p.LengthMin, p.LengthMax},
	}
	if p.BinderName != "" {
		m["binder_name"] = p.BinderName
	}
	if p.NumberOfFinalDesigns > 0 {
		m["number_of_final_designs"] = p.NumberOfFinalDesigns
	}
	if p.BinderChain != "" {
		m["binder_chain"] = p.BinderChain
	}
	if p.DesignRuns > 0 {
		m["design_runs"] = p.DesignRuns
	}
	if p.ProtocolName != "" {
		m["protocol_name"] = p.ProtocolName
	}
	if p.TemplatePDB != "" {
		m["template_pdb"] = p.TemplatePDB
	}
	if p.OmitAAs != "" {
		m["omit_AAs"] = p.OmitAAs
	}
	b, _ := json.MarshalIndent(m, "", "  ")
	return string(b)
}

// init registers the BindCraft adapter with the local backend.
func init() { registerAdapter(bindCraftAdapter{}) }

// bindCraftAdapter wires design.bindcraft to the installed BindCraft tool.
// The request is the typed BindCraftParams; the adapter stages the input
// PDBs into the workdir, compiles a target-settings JSON via
// buildBindCraftSettingsJSON (no more opaque pass-through), runs
// bindcraft.py against it, and returns the accepted designs in the
// {"designs":[...]} envelope.
type bindCraftAdapter struct{}

func (bindCraftAdapter) AgentTool() string { return "design.bindcraft" }
func (bindCraftAdapter) Recipe() string    { return "bindcraft" }

// bindCraftRequest is the typed BindCraft run configuration the adapter
// consumes — same struct the design.bindcraft tool already validated.
type bindCraftRequest = domain.BindCraftParams

// Invoke compiles the agent-supplied typed params into a BindCraft
// settings.json (with design_path overridden), runs bindcraft.py against it,
// and parses the accepted designs.
func (bindCraftAdapter) Invoke(ctx context.Context, env AdapterEnv, request []byte) ([]byte, error) {
	var req bindCraftRequest
	if err := json.Unmarshal(request, &req); err != nil {
		return nil, fmt.Errorf("design.bindcraft: invalid request: %w", err)
	}
	// Defensive backstop on the required fields — the tool's preflight is
	// the primary guard, but a direct adapter call should still fail clean.
	if strings.TrimSpace(req.StartingPDB) == "" {
		return nil, fmt.Errorf("design.bindcraft: starting_pdb is required")
	}
	if strings.TrimSpace(req.Chains) == "" {
		return nil, fmt.Errorf("design.bindcraft: chains is required")
	}
	if strings.TrimSpace(req.TargetHotspotResidues) == "" {
		return nil, fmt.Errorf("design.bindcraft: target_hotspot_residues is required")
	}
	if req.LengthMin < 1 || req.LengthMax < req.LengthMin {
		return nil, fmt.Errorf("design.bindcraft: length_min/length_max are required (1 ≤ min ≤ max)")
	}

	// starting_pdb must exist before staging — the agent gets a clear hint
	// to use fs.read_structure to confirm the path.
	if info, err := os.Stat(req.StartingPDB); err != nil || info.IsDir() {
		return nil, fmt.Errorf(
			"design.bindcraft: starting_pdb %q not found (workspace root). "+
				"Use fs.read_structure or fs.bash to confirm the file exists, "+
				"or pass an absolute path.",
			req.StartingPDB)
	}

	if info, err := os.Stat(env.Recipe.InstallDir); err != nil || !info.IsDir() {
		return nil, fmt.Errorf("design.bindcraft: bindcraft is not installed (run /install bindcraft)")
	}
	if info, err := os.Stat(env.Recipe.VenvDir); err != nil || !info.IsDir() {
		return nil, fmt.Errorf("design.bindcraft: bindcraft is not installed (run /install bindcraft)")
	}
	if env.Registry == nil {
		return nil, fmt.Errorf("design.bindcraft: adapter registry unavailable")
	}
	asset, ok := env.Registry.DataAsset("alphafold_params")
	if !ok {
		return nil, fmt.Errorf("design.bindcraft: the alphafold_params data asset is not registered")
	}
	if info, err := os.Stat(asset.ExtractTo); err != nil || !info.IsDir() {
		return nil, fmt.Errorf("design.bindcraft: AlphaFold params missing — install the alphafold_params data asset")
	}

	// Stage starting_pdb (and template_pdb when set) into the workdir so the
	// settings.json references a path the BindCraft container/process can
	// see. Rewrite the typed params to point at the staged copies before
	// compiling the settings JSON.
	startingBase := filepath.Base(req.StartingPDB)
	stagedStarting := filepath.Join(env.WorkDir, startingBase)
	if err := copyFile(req.StartingPDB, stagedStarting); err != nil {
		return nil, fmt.Errorf("design.bindcraft: stage starting_pdb: %w", err)
	}
	req.StartingPDB = stagedStarting
	if tp := strings.TrimSpace(req.TemplatePDB); tp != "" {
		if info, err := os.Stat(tp); err != nil || info.IsDir() {
			return nil, fmt.Errorf("design.bindcraft: template_pdb %q not found", tp)
		}
		stagedTemplate := filepath.Join(env.WorkDir, filepath.Base(tp))
		if err := copyFile(tp, stagedTemplate); err != nil {
			return nil, fmt.Errorf("design.bindcraft: stage template_pdb: %w", err)
		}
		req.TemplatePDB = stagedTemplate
	}

	designPath := filepath.Join(env.Registry.Home(), "designs",
		fmt.Sprintf("bindcraft-%d", time.Now().UnixNano()))
	if err := os.MkdirAll(designPath, 0o755); err != nil {
		return nil, err
	}

	// Compile the typed params, then inject design_path so accepted designs
	// land under FOVA_HOME (outliving the temp WorkDir).
	settingsBody := buildBindCraftSettingsJSON(req)
	var settings map[string]any
	if err := json.Unmarshal([]byte(settingsBody), &settings); err != nil {
		return nil, fmt.Errorf("design.bindcraft: compiled settings JSON is invalid: %w", err)
	}
	settings["design_path"] = designPath
	settingsJSON, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return nil, err
	}
	settingsFile := filepath.Join(env.WorkDir, "settings.json")
	if err := os.WriteFile(settingsFile, settingsJSON, 0o644); err != nil {
		return nil, err
	}
	env.Tick(0.05) // settings file staged

	cmd := fmt.Sprintf("%s %s --settings %s",
		filepath.Join(env.Recipe.VenvDir, "bin", "python"),
		filepath.Join(env.Recipe.InstallDir, "bindcraft.py"),
		settingsFile)
	if out, err := env.Run(ctx, env.Recipe.InstallDir, cmd, env.LogWriter()); err != nil {
		return nil, fmt.Errorf("design.bindcraft: run failed: %w\n%s", err, out)
	}
	env.Tick(0.95) // bindcraft.py done

	designs, err := parseBindCraftOutput(designPath)
	if err != nil {
		return nil, err
	}
	return json.Marshal(designsEnvelope{Designs: designs})
}
