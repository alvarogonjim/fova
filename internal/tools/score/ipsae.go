package score

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/alvarogonjim/proteus/internal/backends/local"
	"github.com/alvarogonjim/proteus/internal/domain"
	"github.com/alvarogonjim/proteus/internal/tools"
)

// ipsaeRunner runs the installed ipsae tool on a scores file and structure,
// returning its stdout. It is a field so tests can stub it.
type ipsaeRunner func(ctx context.Context, scoresJSON, structureFile string) (string, error)

// IPSAETool implements score.ipsae: the primary interface metric (Dunbrack 2025).
type IPSAETool struct {
	run ipsaeRunner
}

// NewIPSAETool builds the score.ipsae tool wired to the installed ipsae tool.
func NewIPSAETool() *IPSAETool {
	return &IPSAETool{run: realIPSAERunner}
}

// realIPSAERunner invokes the uv-installed ipsae tool via the local runner.
func realIPSAERunner(ctx context.Context, scoresJSON, structureFile string) (string, error) {
	reg, err := local.LoadRegistry(proteusHome())
	if err != nil {
		return "", err
	}
	return local.NewRunner(reg).Run(ctx, "ipsae", map[string]string{
		"scores_json":    scoresJSON,
		"structure_file": structureFile,
		"pae_cutoff":     "10",
		"plddt_cutoff":   "70",
	})
}

// proteusHome resolves the Proteus home directory ($PROTEUS_HOME or ~/proteus).
func proteusHome() string {
	if h := os.Getenv("PROTEUS_HOME"); h != "" {
		return h
	}
	uh, err := os.UserHomeDir()
	if err != nil {
		return "proteus"
	}
	return filepath.Join(uh, "proteus")
}

func (*IPSAETool) Name() string { return "score.ipsae" }
func (*IPSAETool) Description() string {
	return "Compute ipSAE, the primary interprotein interface metric, for a predicted complex."
}
func (*IPSAETool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"scores_json":    map[string]any{"type": "string", "description": "AF2/Boltz/Chai scores JSON path"},
			"structure_file": map[string]any{"type": "string", "description": "Predicted complex PDB/mmCIF path"},
		},
		"required": []string{"scores_json", "structure_file"},
	}
}
func (*IPSAETool) RequiresConfirmation(json.RawMessage) bool       { return false }
func (*IPSAETool) EstimatedCostUSD(json.RawMessage) float64        { return 0 }
func (*IPSAETool) EstimatedDuration(json.RawMessage) time.Duration { return 5 * time.Second }

func (t *IPSAETool) Execute(ctx context.Context, input json.RawMessage) (tools.Result, error) {
	var in struct {
		ScoresJSON    string `json:"scores_json"`
		StructureFile string `json:"structure_file"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return tools.Result{}, err
	}
	out, err := t.run(ctx, in.ScoresJSON, in.StructureFile)
	if err != nil {
		return tools.Result{}, fmt.Errorf("score.ipsae failed (is ipsae installed? run `proteus install ipsae`): %w", err)
	}
	score, ok := parseIPSAE(out)
	if !ok {
		return tools.Result{}, fmt.Errorf("score.ipsae: could not parse an ipSAE value from the tool output")
	}
	metrics := map[string]float64{"ipsae": score}
	res, _ := json.Marshal(map[string]any{"metrics": metrics})
	return tools.Result{
		Output:     res,
		Display:    fmt.Sprintf("ipSAE %.2f", score),
		Provenance: domain.NewToolCallRef("score.ipsae", input),
	}, nil
}

// parseIPSAE extracts the highest ipSAE value from the ipsae tool's stdout.
// It scans for tokens after a case-insensitive "ipsae" marker.
func parseIPSAE(out string) (float64, bool) {
	best, found := 0.0, false
	for _, line := range strings.Split(out, "\n") {
		low := strings.ToLower(line)
		if !strings.Contains(low, "ipsae") {
			continue
		}
		for _, tok := range strings.Fields(line) {
			if v, err := strconv.ParseFloat(tok, 64); err == nil {
				if !found || v > best {
					best, found = v, true
				}
			}
		}
	}
	return best, found
}
