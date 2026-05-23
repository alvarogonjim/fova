package domain

import (
	"fmt"
	"regexp"
	"strings"
)

// ProteinMPNNParams is the agent-facing ProteinMPNN run configuration. Every
// field maps to a `protein_mpnn_run.py` flag (or, for the chain/positions
// JSONLs, to a workspace path fova stages into the container workdir).
// Pointer fields distinguish "unset" (apply fova-side default) from a real
// zero value. It lives in internal/domain so a DesignPlan's MethodConfig can
// carry it without an import cycle.
type ProteinMPNNParams struct {
	PDB            string   `json:"pdb"`                   // workspace path (required)
	NumDesigns     int      `json:"num_designs,omitempty"` // → --num_seq_per_target
	BatchSize      int      `json:"batch_size,omitempty"`
	SamplingTemp   *float64 `json:"sampling_temp,omitempty"`
	Seed           *int     `json:"seed,omitempty"`
	ChainsToDesign string   `json:"chains_to_design,omitempty"` // comma-separated chain ids; fova generates the JSONL
	FixedPositions string   `json:"fixed_positions,omitempty"`  // workspace path to a JSONL
	OmitAAs        string   `json:"omit_AAs,omitempty"`         // inline one-letter codes
	BiasAA         string   `json:"bias_AA,omitempty"`          // workspace path to a JSONL (FILE — not inline, unlike LigandMPNN)
	BiasByResidue  string   `json:"bias_by_residue,omitempty"`  // workspace path to a JSONL
	TiedPositions  string   `json:"tied_positions,omitempty"`   // workspace path to a JSONL
	SaveScore      *bool    `json:"save_score,omitempty"`
}

// proteinMPNNOmitRE matches OmitAAs: one-letter amino-acid codes only.
var proteinMPNNOmitRE = regexp.MustCompile(`^[A-Za-z]*$`)

// Validate checks the value shape of a ProteinMPNNParams. No filesystem
// access — JSONL-path existence is checked in Execute / Invoke. Returns the
// first violation as a design.proteinmpnn-prefixed error, or nil when valid.
func (p ProteinMPNNParams) Validate() error {
	if strings.TrimSpace(p.PDB) == "" {
		return fmt.Errorf("design.proteinmpnn: pdb is required (workspace path to the input backbone .pdb)")
	}
	if p.NumDesigns < 0 {
		return fmt.Errorf("design.proteinmpnn: num_designs must not be negative (got %d)", p.NumDesigns)
	}
	if p.BatchSize < 0 {
		return fmt.Errorf("design.proteinmpnn: batch_size must not be negative (got %d)", p.BatchSize)
	}
	if p.SamplingTemp != nil && *p.SamplingTemp <= 0 {
		return fmt.Errorf("design.proteinmpnn: sampling_temp must be greater than 0")
	}
	if p.Seed != nil && *p.Seed < 0 {
		return fmt.Errorf("design.proteinmpnn: seed must not be negative")
	}
	if !proteinMPNNOmitRE.MatchString(p.OmitAAs) {
		return fmt.Errorf("design.proteinmpnn: omit_AAs must be one-letter amino-acid codes only (got %q)", p.OmitAAs)
	}
	return nil
}
