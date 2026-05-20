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

// parseBindCraftOutput collects accepted BindCraft designs from designPath: the
// PDBs in Accepted/, enriched with sequence and scores from
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

// init registers the BindCraft adapter with the local backend.
func init() { registerAdapter(bindCraftAdapter{}) }

// bindCraftAdapter wires design.bindcraft to the installed BindCraft tool.
type bindCraftAdapter struct{}

func (bindCraftAdapter) AgentTool() string { return "design.bindcraft" }
func (bindCraftAdapter) Recipe() string    { return "bindcraft" }

// bindCraftRequest is the subset of the design.bindcraft input the adapter
// uses: BindCraft's target-settings object, passed through verbatim.
type bindCraftRequest struct {
	Settings json.RawMessage `json:"settings"`
}

// Invoke writes the agent-supplied BindCraft settings (with design_path
// overridden), runs bindcraft.py, and parses the accepted designs.
func (bindCraftAdapter) Invoke(ctx context.Context, env AdapterEnv, request []byte) ([]byte, error) {
	var req bindCraftRequest
	if err := json.Unmarshal(request, &req); err != nil {
		return nil, fmt.Errorf("design.bindcraft: invalid request: %w", err)
	}
	var settings map[string]any
	if err := json.Unmarshal(req.Settings, &settings); err != nil || len(settings) == 0 {
		return nil, fmt.Errorf("design.bindcraft: settings is required (a BindCraft target-settings JSON object)")
	}
	if sp, ok := settings["starting_pdb"].(string); ok && strings.TrimSpace(sp) != "" {
		if info, err := os.Stat(sp); err != nil || info.IsDir() {
			return nil, fmt.Errorf("design.bindcraft: starting_pdb %q does not exist", sp)
		}
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

	designPath := filepath.Join(env.Registry.Home(), "designs",
		fmt.Sprintf("bindcraft-%d", time.Now().UnixNano()))
	if err := os.MkdirAll(designPath, 0o755); err != nil {
		return nil, err
	}
	settings["design_path"] = designPath

	settingsJSON, err := json.Marshal(settings)
	if err != nil {
		return nil, err
	}
	settingsFile := filepath.Join(env.WorkDir, "settings.json")
	if err := os.WriteFile(settingsFile, settingsJSON, 0o644); err != nil {
		return nil, err
	}

	cmd := fmt.Sprintf("%s %s --settings %s",
		filepath.Join(env.Recipe.VenvDir, "bin", "python"),
		filepath.Join(env.Recipe.InstallDir, "bindcraft.py"),
		settingsFile)
	if out, err := env.Run(ctx, env.Recipe.InstallDir, cmd); err != nil {
		return nil, fmt.Errorf("design.bindcraft: run failed: %w\n%s", err, out)
	}

	designs, err := parseBindCraftOutput(designPath)
	if err != nil {
		return nil, err
	}
	return json.Marshal(designsEnvelope{Designs: designs})
}
