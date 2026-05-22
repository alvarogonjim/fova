package domain

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// rfantibodyFrameworks is the closed set of bundled framework presets.
var rfantibodyFrameworks = map[string]bool{"nanobody": true, "scfv": true}

// rfantibodyCDRs is the closed set of CDR-loop names design_loops may target.
var rfantibodyCDRs = map[string]bool{
	"H1": true, "H2": true, "H3": true, "L1": true, "L2": true, "L3": true,
}

// rfHotspotTokenRE matches one hotspot residue reference: a chain letter and a
// residue number, e.g. "T305".
var rfHotspotTokenRE = regexp.MustCompile(`^[A-Za-z][0-9]+$`)

// Validate checks the value shape of an RFantibodyParams. It performs no
// filesystem access — workspace-path existence (target, framework_pdb) is the
// caller's job (the design tool's Execute, the adapter's Invoke). It returns
// the first violation as a design.rfantibody-prefixed error, or nil when valid.
func (p RFantibodyParams) Validate() error {
	if strings.TrimSpace(p.Target) == "" {
		return fmt.Errorf("design.rfantibody: target is required (workspace path to the antigen .pdb)")
	}
	if strings.TrimSpace(p.Hotspots) == "" {
		return fmt.Errorf("design.rfantibody: hotspots is required (epitope residues, e.g. \"T305,T456\")")
	}
	for _, tok := range strings.Split(p.Hotspots, ",") {
		if tok = strings.TrimSpace(tok); tok == "" {
			continue
		}
		if !rfHotspotTokenRE.MatchString(tok) {
			return fmt.Errorf("design.rfantibody: hotspots: %q is not a residue reference "+
				"(expected chain+number, e.g. T305)", tok)
		}
	}
	if p.FrameworkPDB == "" && p.Framework != "" && !rfantibodyFrameworks[p.Framework] {
		return fmt.Errorf("design.rfantibody: framework %q is invalid — use nanobody or scfv, "+
			"or set framework_pdb to a workspace HLT-format PDB", p.Framework)
	}
	if err := validateDesignLoops(p.DesignLoops); err != nil {
		return err
	}
	for name, v := range map[string]int{
		"num_designs":     p.NumDesigns,
		"seqs_per_struct": p.SeqsPerStruct,
	} {
		if v < 0 {
			return fmt.Errorf("design.rfantibody: %s must not be negative (got %d)", name, v)
		}
	}
	if p.NumRecycles != nil && *p.NumRecycles <= 0 {
		return fmt.Errorf("design.rfantibody: num_recycles must be greater than 0")
	}
	if p.Seed != nil && *p.Seed < 0 {
		return fmt.Errorf("design.rfantibody: seed must not be negative")
	}
	if p.Temperature != nil && *p.Temperature <= 0 {
		return fmt.Errorf("design.rfantibody: temperature must be greater than 0")
	}
	if h := p.HotspotShowProp; h != nil && (*h < 0 || *h > 1) {
		return fmt.Errorf("design.rfantibody: hotspot_show_prop must be in [0, 1] (got %g)", *h)
	}
	return nil
}

// validateDesignLoops checks a design_loops string — comma-separated
// <CDR>:<spec> tokens, where <spec> is an integer or a <min>-<max> range. An
// empty string is valid (RFantibody uses its own loop defaults).
func validateDesignLoops(s string) error {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	for _, tok := range strings.Split(s, ",") {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			continue
		}
		cdr, spec, ok := strings.Cut(tok, ":")
		if !ok || !rfantibodyCDRs[cdr] {
			return fmt.Errorf("design.rfantibody: design_loops: %q must be <CDR>:<length> "+
				"with CDR one of H1,H2,H3,L1,L2,L3 (e.g. H1:7 or H3:5-13)", tok)
		}
		if lo, hi, isRange := strings.Cut(spec, "-"); isRange {
			loN, errLo := strconv.Atoi(strings.TrimSpace(lo))
			hiN, errHi := strconv.Atoi(strings.TrimSpace(hi))
			if errLo != nil || errHi != nil || loN <= 0 || hiN < loN {
				return fmt.Errorf("design.rfantibody: design_loops: %q has an invalid "+
					"range (expected <min>-<max>, min ≤ max, e.g. H3:5-13)", tok)
			}
		} else if n, err := strconv.Atoi(strings.TrimSpace(spec)); err != nil || n <= 0 {
			return fmt.Errorf("design.rfantibody: design_loops: %q has an invalid length "+
				"(expected a positive integer or a <min>-<max> range)", tok)
		}
	}
	return nil
}
