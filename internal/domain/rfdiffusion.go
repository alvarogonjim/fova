package domain

import (
	"fmt"
	"regexp"
	"strings"
)

// RFdiffusionParams is the agent-facing RFdiffusion v1 run configuration.
// Every field maps to a Hydra `key=value` override on
// `python /opt/rfdiffusion/scripts/run_inference.py`. Pointer fields
// distinguish "unset" (omit the override, use RFdiffusion's default) from a
// real zero value. It lives in internal/domain so a DesignPlan's MethodConfig
// can carry it without an import cycle; internal/tools/design references it
// under a package-local alias.
type RFdiffusionParams struct {
	Target            string   `json:"target,omitempty"`   // workspace path to target PDB; empty = unconditional
	Hotspots          string   `json:"hotspots,omitempty"` // "A30,A33"
	Contigs           string   `json:"contigs"`            // required (RFdiffusion contig string)
	NumDesigns        int      `json:"num_designs,omitempty"`
	Deterministic     *bool    `json:"deterministic,omitempty"`
	Symmetric         *bool    `json:"symmetric,omitempty"`
	SymmetryKind      string   `json:"symmetry_kind,omitempty"` // cyclic|dihedral|tetrahedral|octahedral|icosahedral
	NChains           int      `json:"n_chains,omitempty"`
	PartialT          int      `json:"partial_t,omitempty"` // partial-diffusion start step
	NoiseScaleCA      *float64 `json:"noise_scale_ca,omitempty"`
	NoiseScaleFrame   *float64 `json:"noise_scale_frame,omitempty"`
	GuidingPotentials []string `json:"guiding_potentials,omitempty"`
	GuideScale        *float64 `json:"guide_scale,omitempty"`
}

// rfdiffusionSymmetryKinds is the closed set of RFdiffusion symmetry kinds.
var rfdiffusionSymmetryKinds = map[string]bool{
	"cyclic": true, "dihedral": true, "tetrahedral": true,
	"octahedral": true, "icosahedral": true,
}

// rfdHotspotTokenRE matches one hotspot residue reference: chain + number,
// e.g. "A30".
var rfdHotspotTokenRE = regexp.MustCompile(`^[A-Za-z][0-9]+$`)

// Validate checks the value shape of an RFdiffusionParams. It performs no
// filesystem access — workspace-path existence is the caller's job. It
// returns the first violation as a design.rfdiffusion-prefixed error, or
// nil when valid.
func (p RFdiffusionParams) Validate() error {
	if strings.TrimSpace(p.Contigs) == "" {
		return fmt.Errorf("design.rfdiffusion: contigs is required (the RFdiffusion contig map, e.g. \"A1-100/0 50-100\")")
	}
	if p.NumDesigns < 0 {
		return fmt.Errorf("design.rfdiffusion: num_designs must not be negative (got %d)", p.NumDesigns)
	}
	if h := strings.TrimSpace(p.Hotspots); h != "" {
		for _, tok := range strings.Split(h, ",") {
			if tok = strings.TrimSpace(tok); tok == "" {
				continue
			}
			if !rfdHotspotTokenRE.MatchString(tok) {
				return fmt.Errorf("design.rfdiffusion: hotspots: %q is not a residue reference "+
					"(expected chain+number, e.g. A30)", tok)
			}
		}
	}
	if p.Symmetric != nil && *p.Symmetric {
		if p.SymmetryKind == "" || !rfdiffusionSymmetryKinds[p.SymmetryKind] {
			return fmt.Errorf("design.rfdiffusion: symmetric is true but symmetry_kind %q is invalid — "+
				"use cyclic, dihedral, tetrahedral, octahedral, or icosahedral", p.SymmetryKind)
		}
		if p.NChains <= 0 {
			return fmt.Errorf("design.rfdiffusion: symmetric is true but n_chains must be greater than 0")
		}
	} else if p.SymmetryKind != "" && !rfdiffusionSymmetryKinds[p.SymmetryKind] {
		return fmt.Errorf("design.rfdiffusion: symmetry_kind %q is invalid — use cyclic, "+
			"dihedral, tetrahedral, octahedral, or icosahedral", p.SymmetryKind)
	}
	if p.PartialT < 0 {
		return fmt.Errorf("design.rfdiffusion: partial_t must not be negative (got %d)", p.PartialT)
	}
	if p.NoiseScaleCA != nil && *p.NoiseScaleCA <= 0 {
		return fmt.Errorf("design.rfdiffusion: noise_scale_ca must be greater than 0")
	}
	if p.NoiseScaleFrame != nil && *p.NoiseScaleFrame <= 0 {
		return fmt.Errorf("design.rfdiffusion: noise_scale_frame must be greater than 0")
	}
	if p.GuideScale != nil && *p.GuideScale <= 0 {
		return fmt.Errorf("design.rfdiffusion: guide_scale must be greater than 0")
	}
	for i, name := range p.GuidingPotentials {
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("design.rfdiffusion: guiding_potentials[%d] must be a non-empty potential name", i)
		}
	}
	return nil
}
