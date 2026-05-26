package domain

import (
	"fmt"
	"regexp"
	"strings"
)

// BindCraftParams is the agent-facing BindCraft run configuration. fova
// compiles a target-settings JSON from these typed fields and feeds it to
// BindCraft (replacing the historical opaque settings map). It lives in
// internal/domain so a DesignPlan's MethodConfig can carry it without an
// import cycle.
type BindCraftParams struct {
	BinderName            string `json:"binder_name,omitempty"`
	StartingPDB           string `json:"starting_pdb"`            // workspace path (required)
	Chains                string `json:"chains"`                  // target chain(s), required
	TargetHotspotResidues string `json:"target_hotspot_residues"` // "A30,A33" (required)
	LengthMin             int    `json:"length_min"`              // ≥1, required
	LengthMax             int    `json:"length_max"`              // ≥LengthMin, required
	NumberOfFinalDesigns  int    `json:"number_of_final_designs,omitempty"`
	BinderChain           string `json:"binder_chain,omitempty"` // default "B"
	DesignRuns            int    `json:"design_runs,omitempty"`
	ProtocolName          string `json:"protocol_name,omitempty"` // beta_only | ss_only | fixed_seq
	TemplatePDB           string `json:"template_pdb,omitempty"`  // workspace path
	OmitAAs               string `json:"omit_aas,omitempty"`
}

// bindCraftProtocols is the closed set of BindCraft protocol names fova
// advertises.
var bindCraftProtocols = map[string]bool{
	"beta_only": true, "ss_only": true, "fixed_seq": true,
}

// bcHotspotTokenRE matches one hotspot residue reference: chain + number.
var bcHotspotTokenRE = regexp.MustCompile(`^[A-Za-z][0-9]+$`)

// Validate checks the value shape of a BindCraftParams. It performs no
// filesystem access — workspace-path existence is the caller's job.
// Returns the first violation as a design.bindcraft-prefixed error, or
// nil when valid.
func (p BindCraftParams) Validate() error {
	if strings.TrimSpace(p.StartingPDB) == "" {
		return fmt.Errorf("design.bindcraft: starting_pdb is required (workspace path to the target .pdb)")
	}
	if strings.TrimSpace(p.Chains) == "" {
		return fmt.Errorf("design.bindcraft: chains is required (the target chain id(s), e.g. \"A\")")
	}
	if strings.TrimSpace(p.TargetHotspotResidues) == "" {
		return fmt.Errorf("design.bindcraft: target_hotspot_residues is required (the epitope, e.g. \"A30,A33\")")
	}
	for _, tok := range strings.Split(p.TargetHotspotResidues, ",") {
		if tok = strings.TrimSpace(tok); tok == "" {
			continue
		}
		if !bcHotspotTokenRE.MatchString(tok) {
			return fmt.Errorf("design.bindcraft: target_hotspot_residues: %q is not a residue "+
				"reference (expected chain+number, e.g. A30)", tok)
		}
	}
	if p.LengthMin < 1 {
		return fmt.Errorf("design.bindcraft: length_min must be at least 1 (got %d)", p.LengthMin)
	}
	if p.LengthMax < p.LengthMin {
		return fmt.Errorf("design.bindcraft: length_max (%d) must be >= length_min (%d)",
			p.LengthMax, p.LengthMin)
	}
	if p.NumberOfFinalDesigns < 0 {
		return fmt.Errorf("design.bindcraft: number_of_final_designs must not be negative (got %d)",
			p.NumberOfFinalDesigns)
	}
	if p.DesignRuns < 0 {
		return fmt.Errorf("design.bindcraft: design_runs must not be negative (got %d)", p.DesignRuns)
	}
	if p.ProtocolName != "" && !bindCraftProtocols[p.ProtocolName] {
		return fmt.Errorf("design.bindcraft: protocol_name %q is invalid — use beta_only, "+
			"ss_only, or fixed_seq", p.ProtocolName)
	}
	return nil
}
