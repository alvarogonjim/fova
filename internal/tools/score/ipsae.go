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

	"github.com/alvarogonjim/fova/internal/backends/local"
	"github.com/alvarogonjim/fova/internal/domain"
	"github.com/alvarogonjim/fova/internal/tools"
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

// realIPSAERunner invokes the installed ipsae container via the local
// package's container-mode helper. The Runner can't be reused for ipsae:
// its entrypoint takes positional args (pae_file, pdb_file, pae_cutoff,
// dist_cutoff) that don't match the workspace-mounted placeholder model,
// and the two input paths live anywhere on the host. RunIPSAE stages
// both into a temp dir mounted at /work and runs the container directly.
func realIPSAERunner(ctx context.Context, scoresJSON, structureFile string) (string, error) {
	reg, err := local.LoadRegistry(fovaHome())
	if err != nil {
		return "", err
	}
	rec, ok := reg.Tool("ipsae")
	if !ok {
		return "", fmt.Errorf("ipsae: recipe not found in registry (run /install ipsae)")
	}
	return local.RunIPSAE(ctx, scoresJSON, structureFile, rec, nil)
}

// fovaHome resolves the fova home directory ($FOVA_HOME or ~/fova).
func fovaHome() string {
	if h := os.Getenv("FOVA_HOME"); h != "" {
		return h
	}
	uh, err := os.UserHomeDir()
	if err != nil {
		return "fova"
	}
	return filepath.Join(uh, "fova")
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
func (*IPSAETool) Concurrent() bool                                { return true }
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
		return tools.Result{}, fmt.Errorf("score.ipsae failed (is ipsae installed? run `fova install ipsae`): %w", err)
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
