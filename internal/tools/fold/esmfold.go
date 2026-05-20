// Package fold holds structure-prediction tools.
package fold

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/alvarogonjim/fova/internal/domain"
	"github.com/alvarogonjim/fova/internal/tools"
)

// esmAtlasEndpoint is the public ESM Atlas folding endpoint (SPECS §7.2.2).
const esmAtlasEndpoint = "https://api.esmatlas.com/foldSequence/v1/pdb/"

// esmfoldClient is a dedicated HTTP client so a stuck connection cannot hang
// indefinitely even if the caller's context has no deadline. Folds typically
// finish in well under a minute; 10 minutes is the upper bound the public
// endpoint accepts.
var esmfoldClient = &http.Client{Timeout: 10 * time.Minute}

// ESMFold implements the fold.esmfold tool.
type ESMFold struct {
	workspaceRoot string
	Endpoint      string // overridable for tests
	counter       int    // designs/d_NNNN.pdb sequence
}

// NewESMFold returns the fold.esmfold tool bound to a workspace root.
func NewESMFold(workspaceRoot string) *ESMFold {
	return &ESMFold{workspaceRoot: workspaceRoot, Endpoint: esmAtlasEndpoint}
}

type esmfoldMetrics struct {
	PLDDTMean float64 `json:"plddt_mean"`
	PLDDTMin  float64 `json:"plddt_min"`
}

type esmfoldOutput struct {
	DesignID      string         `json:"design_id"`
	StructureFile string         `json:"structure_file"`
	Metrics       esmfoldMetrics `json:"metrics"`
	ElapsedS      float64        `json:"elapsed_s"`
}

func (*ESMFold) Name() string { return "fold.esmfold" }
func (*ESMFold) Description() string {
	return "Predict a monomer structure from an amino-acid sequence using ESMFold. " +
		"Returns a PDB file path and pLDDT confidence."
}
func (*ESMFold) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"sequence": map[string]any{
				"type":        "string",
				"pattern":     "^[ACDEFGHIKLMNPQRSTVWY]+$",
				"description": "Single-chain amino-acid sequence",
			},
			"save_as": map[string]any{
				"type":        "string",
				"description": "Optional PDB output path within the workspace",
			},
		},
		"required": []string{"sequence"},
	}
}
func (*ESMFold) RequiresConfirmation(json.RawMessage) bool       { return false }
func (*ESMFold) EstimatedCostUSD(json.RawMessage) float64        { return 0 }
func (*ESMFold) EstimatedDuration(json.RawMessage) time.Duration { return 15 * time.Second }

// Execute folds the sequence and writes a PDB file into the workspace.
func (e *ESMFold) Execute(ctx context.Context, input json.RawMessage) (tools.Result, error) {
	var in struct {
		Sequence string `json:"sequence"`
		SaveAs   string `json:"save_as"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return tools.Result{}, err
	}
	if !domain.ValidAA(in.Sequence) {
		return tools.Result{}, fmt.Errorf("invalid amino-acid sequence")
	}

	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.Endpoint,
		strings.NewReader(in.Sequence))
	if err != nil {
		return tools.Result{}, err
	}
	req.Header.Set("Content-Type", "text/plain")
	resp, err := esmfoldClient.Do(req)
	if err != nil {
		return tools.Result{}, fmt.Errorf("ESM Atlas request failed: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return tools.Result{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return tools.Result{}, fmt.Errorf("ESM Atlas returned %d: %s",
			resp.StatusCode, strings.TrimSpace(string(body)))
	}

	e.counter++
	designID := fmt.Sprintf("d_%04d", e.counter)
	relPath := in.SaveAs
	if relPath == "" {
		relPath = "designs/" + designID + ".pdb"
	}
	abs, err := tools.SafeJoin(e.workspaceRoot, relPath)
	if err != nil {
		return tools.Result{}, err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return tools.Result{}, err
	}
	if err := os.WriteFile(abs, body, 0o644); err != nil {
		return tools.Result{}, err
	}

	mean, min, n := parsePLDDT(string(body))
	if n == 0 {
		return tools.Result{}, fmt.Errorf("no CA atoms found in ESMFold output")
	}
	out := esmfoldOutput{
		DesignID:      designID,
		StructureFile: relPath,
		Metrics:       esmfoldMetrics{PLDDTMean: round2(mean), PLDDTMin: round2(min)},
		ElapsedS:      round2(time.Since(start).Seconds()),
	}
	outJSON, _ := json.Marshal(out)
	return tools.Result{
		Output: outJSON,
		Display: fmt.Sprintf("folded %s → %s (pLDDT mean %.1f, min %.1f)",
			designID, relPath, out.Metrics.PLDDTMean, out.Metrics.PLDDTMin),
		Provenance: domain.NewToolCallRef("fold.esmfold", input),
	}, nil
}

// parsePLDDT reads per-residue pLDDT from the B-factor column of CA atoms.
// PDB fixed columns: B-factor is characters 61-66 (0-indexed slice [60:66]),
// atom name is characters 13-16 (slice [12:16]).
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

func round2(v float64) float64 {
	return float64(int(v*100+0.5)) / 100
}
