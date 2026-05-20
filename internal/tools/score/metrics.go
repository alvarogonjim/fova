package score

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/alvarogonjim/fova/internal/domain"
	"github.com/alvarogonjim/fova/internal/tools"
)

// MetricsTool implements score.metrics: extract confidence metrics from a
// predicted structure (pLDDT from the PDB B-factor column of CA atoms).
type MetricsTool struct{}

// NewMetricsTool builds the score.metrics tool.
func NewMetricsTool() *MetricsTool { return &MetricsTool{} }

func (*MetricsTool) Name() string { return "score.metrics" }
func (*MetricsTool) Description() string {
	return "Extract confidence metrics (pLDDT mean/min) from a predicted PDB structure."
}
func (*MetricsTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"structure_file": map[string]any{"type": "string", "description": "Path to a PDB file"},
		},
		"required": []string{"structure_file"},
	}
}
func (*MetricsTool) RequiresConfirmation(json.RawMessage) bool       { return false }
func (*MetricsTool) EstimatedCostUSD(json.RawMessage) float64        { return 0 }
func (*MetricsTool) EstimatedDuration(json.RawMessage) time.Duration { return 100 * time.Millisecond }

func (t *MetricsTool) Execute(_ context.Context, input json.RawMessage) (tools.Result, error) {
	var in struct {
		StructureFile string `json:"structure_file"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return tools.Result{}, err
	}
	pdb, err := os.ReadFile(in.StructureFile)
	if err != nil {
		return tools.Result{}, fmt.Errorf("read structure: %w", err)
	}
	mean, min, n := parsePLDDT(string(pdb))
	if n == 0 {
		return tools.Result{}, fmt.Errorf("no CA atoms found in %s", in.StructureFile)
	}
	metrics := map[string]float64{"plddt_mean": mean, "plddt_min": min}
	out, _ := json.Marshal(map[string]any{"metrics": metrics})
	return tools.Result{
		Output:     out,
		Display:    fmt.Sprintf("pLDDT mean %.1f, min %.1f (%d residues)", mean, min, n),
		Provenance: domain.NewToolCallRef("score.metrics", input),
	}, nil
}

// parsePLDDT reads per-residue pLDDT from the B-factor column of CA atoms.
// PDB fixed columns: atom name is slice [12:16], B-factor is slice [60:66].
func parsePLDDT(pdb string) (mean, min float64, n int) {
	min = 1e9
	var sum float64
	for _, line := range strings.Split(pdb, "\n") {
		if !strings.HasPrefix(line, "ATOM") || len(line) < 66 {
			continue
		}
		if strings.TrimSpace(line[12:16]) != "CA" {
			continue
		}
		b, err := strconv.ParseFloat(strings.TrimSpace(line[60:66]), 64)
		if err != nil {
			continue
		}
		sum += b
		if b < min {
			min = b
		}
		n++
	}
	if n == 0 {
		return 0, 0, 0
	}
	return sum / float64(n), min, n
}
