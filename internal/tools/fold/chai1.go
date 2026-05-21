package fold

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/alvarogonjim/fova/internal/backends"
	"github.com/alvarogonjim/fova/internal/domain"
	"github.com/alvarogonjim/fova/internal/jobs"
	"github.com/alvarogonjim/fova/internal/tools"
)

// chai1Entity is one molecular component of the predicted complex.
type chai1Entity struct {
	Type     string `json:"type"`     // protein | dna | rna | ligand | glycan
	ID       string `json:"id"`       // one chain id
	Sequence string `json:"sequence"` // protein/dna/rna
	SMILES   string `json:"smiles"`   // ligand
	Glycan   string `json:"glycan"`   // glycan
}

// chai1Restraint is one inter-chain restraint. Pointer fields are optional
// numerics: nil ⇒ the CSV cell is left blank.
type chai1Restraint struct {
	ConnectionType string   `json:"connection_type"` // contact | pocket | covalent
	ChainA         string   `json:"chain_a"`
	ResA           string   `json:"res_a"`
	ChainB         string   `json:"chain_b"`
	ResB           string   `json:"res_b"`
	MinDistance    *float64 `json:"min_distance"`
	MaxDistance    *float64 `json:"max_distance"`
	Confidence     *float64 `json:"confidence"`
	Comment        string   `json:"comment"`
}

// chai1Templates is the optional request-level template configuration.
type chai1Templates struct {
	Server   bool   `json:"server"`
	HitsPath string `json:"hits_path"`
}

// chai1Request is the full fold.chai1 input. Pointer fields are model
// parameters: nil ⇒ omit the CLI flag and let Chai-1 use its own default.
type chai1Request struct {
	Entities            []chai1Entity    `json:"entities"`
	MSA                 string           `json:"msa"` // "default" | "server" | workspace path
	Restraints          []chai1Restraint `json:"restraints"`
	Templates           *chai1Templates  `json:"templates"`
	NumTrunkRecycles    *int             `json:"num_trunk_recycles"`
	NumDiffnTimesteps   *int             `json:"num_diffn_timesteps"`
	NumDiffnSamples     *int             `json:"num_diffn_samples"`
	NumTrunkSamples     *int             `json:"num_trunk_samples"`
	RecycleMSASubsample *int             `json:"recycle_msa_subsample"`
	Seed                *int             `json:"seed"`
	SaveAs              string           `json:"save_as"`
}

// validSeq reports whether every rune of seq (already upper-cased) is in the
// given residue alphabet, skipping any parenthesised modified-residue token —
// an unbalanced or empty "()" run is rejected.
func validSeq(seq, alphabet string) bool {
	depth := 0
	tokenLen := 0
	for _, ch := range seq {
		switch {
		case ch == '(':
			depth++
			tokenLen = 0
		case ch == ')':
			if depth == 0 || tokenLen == 0 {
				return false
			}
			depth--
		case depth > 0:
			tokenLen++
		case !strings.ContainsRune(alphabet, ch):
			return false
		}
	}
	return depth == 0
}

// preflightChai1 validates a request's value shape before any job is
// submitted. It returns the first violation as a fold.chai1-prefixed error
// describing the problem and how to fix it, or nil when the request is valid.
// MSA path existence is NOT checked here — that needs the workspace root and
// is deferred to Execute.
func preflightChai1(req chai1Request) error {
	if len(req.Entities) < 1 {
		return fmt.Errorf("fold.chai1: at least one entity is required in \"entities\"")
	}
	ids := map[string]bool{}
	for i, e := range req.Entities {
		switch e.Type {
		case "protein", "dna", "rna":
			if e.Sequence == "" {
				return fmt.Errorf("fold.chai1: entity %d (%s): \"sequence\" must be non-empty", i, e.Type)
			}
			var alphabet string
			switch e.Type {
			case "protein":
				alphabet = aminoAcids
			case "dna":
				alphabet = dnaBases
			case "rna":
				alphabet = rnaBases
			}
			if !validSeq(strings.ToUpper(e.Sequence), alphabet) {
				return fmt.Errorf("fold.chai1: entity %d (%s): \"sequence\" must use only %s "+
					"(modified residues written inline as parenthesised tokens)", i, e.Type, alphabet)
			}
		case "ligand":
			if e.SMILES == "" {
				return fmt.Errorf("fold.chai1: entity %d (ligand): \"smiles\" must be non-empty", i)
			}
		case "glycan":
			if e.Glycan == "" {
				return fmt.Errorf("fold.chai1: entity %d (glycan): \"glycan\" must be non-empty", i)
			}
		default:
			return fmt.Errorf("fold.chai1: entity %d: \"type\" must be protein, dna, rna, "+
				"ligand, or glycan (got %q)", i, e.Type)
		}
		if e.ID == "" {
			return fmt.Errorf("fold.chai1: entity %d: \"id\" must be a non-empty chain id", i)
		}
		if ids[e.ID] {
			return fmt.Errorf("fold.chai1: chain id %q is used more than once — chain ids must be unique", e.ID)
		}
		ids[e.ID] = true
	}
	for i, r := range req.Restraints {
		switch r.ConnectionType {
		case "contact", "pocket", "covalent":
		default:
			return fmt.Errorf("fold.chai1: restraint %d: \"connection_type\" must be "+
				"contact, pocket, or covalent (got %q)", i, r.ConnectionType)
		}
		if !ids[r.ChainA] {
			return fmt.Errorf("fold.chai1: restraint %d: \"chain_a\" %q does not match any declared entity id", i, r.ChainA)
		}
		if !ids[r.ChainB] {
			return fmt.Errorf("fold.chai1: restraint %d: \"chain_b\" %q does not match any declared entity id", i, r.ChainB)
		}
		if r.MinDistance != nil && r.MaxDistance != nil && *r.MaxDistance < *r.MinDistance {
			return fmt.Errorf("fold.chai1: restraint %d: \"max_distance\" (%g) must be >= \"min_distance\" (%g)",
				i, *r.MaxDistance, *r.MinDistance)
		}
		if r.Confidence != nil && (*r.Confidence < 0 || *r.Confidence > 1) {
			return fmt.Errorf("fold.chai1: restraint %d: \"confidence\" must be in [0, 1] (got %g)", i, *r.Confidence)
		}
	}
	for name, v := range map[string]*int{
		"num_trunk_recycles":  req.NumTrunkRecycles,
		"num_diffn_timesteps": req.NumDiffnTimesteps,
		"num_diffn_samples":   req.NumDiffnSamples,
		"num_trunk_samples":   req.NumTrunkSamples,
	} {
		if v != nil && *v <= 0 {
			return fmt.Errorf("fold.chai1: %q must be greater than 0 (got %d)", name, *v)
		}
	}
	if req.RecycleMSASubsample != nil && *req.RecycleMSASubsample < 0 {
		return fmt.Errorf("fold.chai1: \"recycle_msa_subsample\" must be >= 0 (got %d)", *req.RecycleMSASubsample)
	}
	return nil
}

// chai1Tool is the bespoke agent tool for Chai-1 biomolecular complex
// structure prediction. Unlike the retired shared foldJobTool, it accepts a
// typed multi-entity request (protein/DNA/RNA/ligand/glycan) with restraints,
// templates and MSA control, validates it in preflight, resolves workspace
// paths, and submits a background compute job.
type chai1Tool struct {
	workspaceRoot string
	mgr           *jobs.Manager
	backend       backends.Backend
}

// NewChai1 returns the fold.chai1 tool: Chai-1 complex structure prediction on
// the selected compute backend, run as an async job.
func NewChai1(workspaceRoot string, mgr *jobs.Manager, backend backends.Backend) *chai1Tool {
	return &chai1Tool{
		workspaceRoot: workspaceRoot,
		mgr:           mgr,
		backend:       backend,
	}
}

func (*chai1Tool) Name() string { return "fold.chai1" }

func (*chai1Tool) Description() string {
	return "Predict the 3D structure of a biomolecular complex " +
		"(protein/DNA/RNA/ligand/glycan entities) with Chai-1; runs as an async job."
}

// InputSchema describes the typed multi-entity Chai-1 request.
func (*chai1Tool) InputSchema() map[string]any {
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
							"enum":        []string{"protein", "dna", "rna", "ligand", "glycan"},
							"description": "Entity kind: protein, dna, rna, ligand, or glycan",
						},
						"id": map[string]any{
							"type":        "string",
							"description": "Chain id for this entity (unique across all entities)",
						},
						"sequence": map[string]any{
							"type":        "string",
							"description": "Residue sequence (protein/dna/rna)",
						},
						"smiles": map[string]any{
							"type":        "string",
							"description": "Ligand SMILES string",
						},
						"glycan": map[string]any{
							"type":        "string",
							"description": "Glycan string in Chai-1 glycan notation",
						},
					},
				},
			},
			"msa": map[string]any{
				"type":        "string",
				"description": "MSA mode: 'default' (ESM embeddings, offline), 'server' (ColabFold MSA server), or a workspace path to a precomputed MSA directory",
			},
			"restraints": map[string]any{
				"type":        "array",
				"description": "Optional inter-chain restraints",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"connection_type": map[string]any{
							"type":        "string",
							"enum":        []string{"contact", "pocket", "covalent"},
							"description": "Restraint kind: contact, pocket, or covalent",
						},
						"chain_a": map[string]any{
							"type":        "string",
							"description": "First chain id",
						},
						"res_a": map[string]any{
							"type":        "string",
							"description": "Residue on the first chain (e.g. 'A219', atoms as 'A219@CA')",
						},
						"chain_b": map[string]any{
							"type":        "string",
							"description": "Second chain id",
						},
						"res_b": map[string]any{
							"type":        "string",
							"description": "Residue on the second chain",
						},
						"min_distance": map[string]any{
							"type":        "number",
							"description": "Optional minimum distance in angstroms",
						},
						"max_distance": map[string]any{
							"type":        "number",
							"description": "Optional maximum distance in angstroms",
						},
						"confidence": map[string]any{
							"type":        "number",
							"description": "Optional restraint confidence in [0, 1] (default 1.0)",
						},
						"comment": map[string]any{
							"type":        "string",
							"description": "Optional free-text comment",
						},
					},
				},
			},
			"templates": map[string]any{
				"type":        "object",
				"description": "Optional structural template configuration",
				"properties": map[string]any{
					"server": map[string]any{
						"type":        "boolean",
						"description": "Use the Chai-1 template server",
					},
					"hits_path": map[string]any{
						"type":        "string",
						"description": "Workspace path to a precomputed template hits file",
					},
				},
			},
			"num_trunk_recycles": map[string]any{
				"type":        "integer",
				"description": "Number of trunk recycling iterations (optional; Chai-1 default)",
			},
			"num_diffn_timesteps": map[string]any{
				"type":        "integer",
				"description": "Number of diffusion timesteps (optional; Chai-1 default)",
			},
			"num_diffn_samples": map[string]any{
				"type":        "integer",
				"description": "Number of diffusion samples / predicted models to rank (optional; Chai-1 default)",
			},
			"num_trunk_samples": map[string]any{
				"type":        "integer",
				"description": "Number of trunk samples (optional; Chai-1 default)",
			},
			"recycle_msa_subsample": map[string]any{
				"type":        "integer",
				"description": "MSA subsample size on recycling (optional; Chai-1 default)",
			},
			"seed": map[string]any{
				"type":        "integer",
				"description": "Random seed for reproducible predictions (optional)",
			},
			"save_as": map[string]any{
				"type":        "string",
				"description": "Optional workspace-relative path to save the predicted structure",
			},
		},
	}
}

// Chai-1 prediction is a long, GPU-bound job — always require user approval.
func (*chai1Tool) RequiresConfirmation(json.RawMessage) bool       { return true }
func (*chai1Tool) EstimatedCostUSD(json.RawMessage) float64        { return 0.25 }
func (*chai1Tool) EstimatedDuration(json.RawMessage) time.Duration { return 3 * time.Minute }

// isMSAPath reports whether an MSA value names a workspace path — i.e. it is
// neither empty, the "default" mode, nor the "server" mode.
func isMSAPath(msa string) bool {
	return msa != "" && msa != "default" && msa != "server"
}

// Execute validates the request in preflight, resolves every workspace path
// against the workspace root, and submits a background compute job. The job
// runs the backend and returns its raw output; nothing is persisted (this is
// a structure predictor, not a design generator).
func (t *chai1Tool) Execute(_ context.Context, input json.RawMessage) (tools.Result, error) {
	var req chai1Request
	if err := json.Unmarshal(input, &req); err != nil {
		return tools.Result{}, fmt.Errorf("invalid fold.chai1 request: %w", err)
	}
	if err := preflightChai1(req); err != nil {
		return tools.Result{}, err
	}
	// Resolve workspace paths. An empty workspaceRoot short-circuits — mirrors
	// boltz2Tool, and covers tests that do not set one.
	if t.workspaceRoot != "" {
		if isMSAPath(req.MSA) {
			abs, err := tools.ResolveWorkspacePath(t.workspaceRoot, req.MSA)
			if err != nil {
				return tools.Result{}, fmt.Errorf("fold.chai1: %w", err)
			}
			req.MSA = abs
		}
		if req.Templates != nil && req.Templates.HitsPath != "" {
			abs, err := tools.ResolveWorkspacePath(t.workspaceRoot, req.Templates.HitsPath)
			if err != nil {
				return tools.Result{}, fmt.Errorf("fold.chai1: %w", err)
			}
			req.Templates.HitsPath = abs
		}
		if req.SaveAs != "" {
			abs, err := tools.ResolveWorkspacePath(t.workspaceRoot, req.SaveAs)
			if err != nil {
				return tools.Result{}, fmt.Errorf("fold.chai1: %w", err)
			}
			req.SaveAs = abs
		}
	}
	resolved, err := json.Marshal(req)
	if err != nil {
		return tools.Result{}, fmt.Errorf("fold.chai1: %w", err)
	}
	jobID, err := t.mgr.Submit(jobs.Spec{
		Kind:    domain.JobCompute,
		Tool:    "fold.chai1",
		Backend: t.backend.Name(),
		Input:   resolved,
		Run: func(ctx context.Context, progress func(float64), log io.Writer) ([]byte, error) {
			out, err := t.backend.Run(ctx, "fold.chai1", resolved, log, progress)
			if err != nil {
				return nil, err
			}
			progress(1)
			return out, nil
		},
	})
	if err != nil {
		return tools.Result{}, err
	}
	return tools.Result{
		JobID:      jobID,
		Display:    "started fold.chai1 job " + string(jobID) + " — poll jobs.result for the predicted structure",
		Provenance: domain.NewToolCallRef("fold.chai1", input),
	}, nil
}
