package fold

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/alvarogonjim/fova/internal/backends"
	"github.com/alvarogonjim/fova/internal/jobs"
	"github.com/alvarogonjim/fova/internal/tools"
)

// Residue alphabets, per entity type, for preflight sequence validation.
const (
	aminoAcids = "ACDEFGHIKLMNPQRSTVWY"
	dnaBases   = "ACGT"
	rnaBases   = "ACGU"
)

// preflightBoltz2 validates a request's value shape before any job is
// submitted. It returns the first violation as a fold.boltz2-prefixed error
// describing the problem and how to fix it, or nil when the request is valid.
// MSA path existence is NOT checked here — that needs the workspace root and
// is deferred to Execute.
func preflightBoltz2(req boltz2Request) error {
	if len(req.Entities) < 1 {
		return fmt.Errorf("fold.boltz2: at least one entity is required in \"entities\"")
	}
	seen := map[string]bool{}
	for i, e := range req.Entities {
		switch e.Type {
		case "protein", "dna", "rna":
			if e.Sequence == "" {
				return fmt.Errorf("fold.boltz2: entity %d (%s): \"sequence\" must be non-empty", i, e.Type)
			}
			seq := strings.ToUpper(e.Sequence)
			var alphabet string
			switch e.Type {
			case "protein":
				alphabet = aminoAcids
			case "dna":
				alphabet = dnaBases
			case "rna":
				alphabet = rnaBases
			}
			for _, ch := range seq {
				if !strings.ContainsRune(alphabet, ch) {
					return fmt.Errorf("fold.boltz2: entity %d (%s): invalid residue %q — sequence must use only %s",
						i, e.Type, string(ch), alphabet)
				}
			}
		case "ligand":
			if (e.SMILES == "") == (e.CCD == "") {
				return fmt.Errorf("fold.boltz2: entity %d (ligand): set exactly one of \"smiles\" or \"ccd\"", i)
			}
			if e.MSA != "" || e.Cyclic {
				return fmt.Errorf("fold.boltz2: entity %d (ligand): \"msa\" and \"cyclic\" apply to protein/dna/rna only", i)
			}
		default:
			return fmt.Errorf("fold.boltz2: entity %d: \"type\" must be protein, dna, rna, or ligand (got %q)", i, e.Type)
		}
		for _, id := range e.ID {
			if seen[id] {
				return fmt.Errorf("fold.boltz2: chain id %q is used more than once — chain ids must be unique", id)
			}
			seen[id] = true
		}
	}
	if req.StepScale != nil && (*req.StepScale < 1 || *req.StepScale > 2) {
		return fmt.Errorf("fold.boltz2: \"step_scale\" must be in [1, 2] (got %g)", *req.StepScale)
	}
	for name, v := range map[string]*int{
		"recycling_steps":   req.RecyclingSteps,
		"sampling_steps":    req.SamplingSteps,
		"diffusion_samples": req.DiffusionSamples,
	} {
		if v != nil && *v <= 0 {
			return fmt.Errorf("fold.boltz2: %q must be greater than 0 (got %d)", name, *v)
		}
	}
	switch req.OutputFormat {
	case "", "pdb", "mmcif":
	default:
		return fmt.Errorf("fold.boltz2: \"output_format\" must be pdb or mmcif (got %q)", req.OutputFormat)
	}
	return nil
}

// chainIDs unmarshals a JSON chain id given either as a string ("A") or a
// string array (["B","C"]) into a uniform []string.
type chainIDs []string

func (c *chainIDs) UnmarshalJSON(b []byte) error {
	var one string
	if err := json.Unmarshal(b, &one); err == nil {
		*c = chainIDs{one}
		return nil
	}
	var many []string
	if err := json.Unmarshal(b, &many); err != nil {
		return fmt.Errorf("id must be a string or a list of strings: %w", err)
	}
	*c = chainIDs(many)
	return nil
}

// boltz2Entity is one molecular component of the predicted complex.
type boltz2Entity struct {
	Type     string   `json:"type"`     // protein | dna | rna | ligand
	ID       chainIDs `json:"id"`       // one or more chain ids
	Sequence string   `json:"sequence"` // protein/dna/rna
	SMILES   string   `json:"smiles"`   // ligand (exclusive with ccd)
	CCD      string   `json:"ccd"`      // ligand (exclusive with smiles)
	MSA      string   `json:"msa"`      // "empty" | "server" | workspace path; protein/dna/rna
	Cyclic   bool     `json:"cyclic"`   // protein/dna/rna
}

// boltz2Request is the full fold.boltz2 input. Pointer fields are model
// parameters: nil ⇒ omit the CLI flag and let Boltz-2 use its own default.
type boltz2Request struct {
	Entities         []boltz2Entity `json:"entities"`
	RecyclingSteps   *int           `json:"recycling_steps"`
	SamplingSteps    *int           `json:"sampling_steps"`
	DiffusionSamples *int           `json:"diffusion_samples"`
	StepScale        *float64       `json:"step_scale"`
	OutputFormat     string         `json:"output_format"` // "pdb" (default) | "mmcif"
	SaveAs           string         `json:"save_as"`
}

// boltz2Tool is the bespoke agent tool for Boltz-2 biomolecular complex
// structure prediction. Unlike the shared foldJobTool, it accepts a typed
// multi-entity request (protein/DNA/RNA/ligand), validates it in preflight,
// resolves workspace paths, and submits a background compute job.
type boltz2Tool struct {
	workspaceRoot string
	mgr           *jobs.Manager
	backend       backends.Backend
}

// NewBoltz2 returns the fold.boltz2 tool: Boltz-2 complex structure prediction
// on the selected compute backend, run as an async job.
func NewBoltz2(workspaceRoot string, mgr *jobs.Manager, backend backends.Backend) *boltz2Tool {
	return &boltz2Tool{
		workspaceRoot: workspaceRoot,
		mgr:           mgr,
		backend:       backend,
	}
}

func (*boltz2Tool) Name() string { return "fold.boltz2" }

func (*boltz2Tool) Description() string {
	return "Predict the 3D structure of a biomolecular complex " +
		"(protein/DNA/RNA/ligand entities) with Boltz-2; runs as an async job."
}

// InputSchema describes the typed multi-entity Boltz-2 request.
func (*boltz2Tool) InputSchema() map[string]any {
	return map[string]any{
		"type":     "object",
		"required": []string{"entities"},
		"properties": map[string]any{
			"entities": map[string]any{
				"type":        "array",
				"description": "Molecular components of the complex to predict",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"type": map[string]any{
							"type":        "string",
							"enum":        []string{"protein", "dna", "rna", "ligand"},
							"description": "Entity kind: protein, dna, rna, or ligand",
						},
						"id": map[string]any{
							"description": "Chain id, or a list of chain ids for identical copies",
						},
						"sequence": map[string]any{
							"type":        "string",
							"description": "Residue sequence (protein/dna/rna)",
						},
						"smiles": map[string]any{
							"type":        "string",
							"description": "Ligand SMILES string (exclusive with ccd)",
						},
						"ccd": map[string]any{
							"type":        "string",
							"description": "Ligand CCD code (exclusive with smiles)",
						},
						"msa": map[string]any{
							"type":        "string",
							"description": "MSA mode: 'empty', 'server', or a workspace path to a precomputed MSA (protein/dna/rna)",
						},
						"cyclic": map[string]any{
							"type":        "boolean",
							"description": "Treat the chain as cyclic (protein/dna/rna)",
						},
					},
				},
			},
			"recycling_steps": map[string]any{
				"type":        "integer",
				"description": "Number of recycling iterations (optional; Boltz-2 default 3)",
			},
			"sampling_steps": map[string]any{
				"type":        "integer",
				"description": "Number of diffusion sampling steps (optional; Boltz-2 default 200)",
			},
			"diffusion_samples": map[string]any{
				"type":        "integer",
				"description": "Number of diffusion samples / predicted models (optional; Boltz-2 default 1)",
			},
			"step_scale": map[string]any{
				"type":        "number",
				"description": "Diffusion step scale, useful range 1–2 (optional; Boltz-2 default 1.638)",
			},
			"output_format": map[string]any{
				"type":        "string",
				"enum":        []string{"pdb", "mmcif"},
				"description": "Structure output format (optional; default pdb)",
			},
			"save_as": map[string]any{
				"type":        "string",
				"description": "Optional workspace-relative path to save the predicted structure",
			},
		},
	}
}

// Boltz-2 prediction is a long, GPU-bound job — always require user approval.
func (*boltz2Tool) RequiresConfirmation(json.RawMessage) bool       { return true }
func (*boltz2Tool) EstimatedCostUSD(json.RawMessage) float64        { return 0.25 }
func (*boltz2Tool) EstimatedDuration(json.RawMessage) time.Duration { return 3 * time.Minute }

// Execute is implemented in Task A3.
func (t *boltz2Tool) Execute(_ context.Context, _ json.RawMessage) (tools.Result, error) {
	return tools.Result{}, nil
}
