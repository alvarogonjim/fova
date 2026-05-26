package domain

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// ligandMPNNModelTypes is the closed set of LigandMPNN run.py model types.
var ligandMPNNModelTypes = map[string]bool{
	"ligand_mpnn":                     true,
	"protein_mpnn":                    true,
	"soluble_mpnn":                    true,
	"global_label_membrane_mpnn":      true,
	"per_residue_label_membrane_mpnn": true,
}

// residueTokenRE matches a LigandMPNN residue reference: a chain letter, a
// 1-indexed residue number, and an optional insertion-code letter — e.g.
// "A23" or "B42D".
var residueTokenRE = regexp.MustCompile(`^[A-Za-z][0-9]+[A-Za-z]?$`)

// Validate checks the value shape of a LigandMPNNParams. It performs no
// filesystem access — workspace-path existence is the caller's job (the
// design tool's Execute, the adapter's Invoke). It returns the first
// violation as a design.ligandmpnn-prefixed error, or nil when valid.
func (p LigandMPNNParams) Validate() error {
	if strings.TrimSpace(p.PDB) == "" {
		return fmt.Errorf("design.ligandmpnn: pdb is required (workspace path to the input backbone .pdb)")
	}
	if p.ModelType != "" && !ligandMPNNModelTypes[p.ModelType] {
		return fmt.Errorf("design.ligandmpnn: model_type %q is invalid — use ligand_mpnn, "+
			"protein_mpnn, soluble_mpnn, global_label_membrane_mpnn, or "+
			"per_residue_label_membrane_mpnn", p.ModelType)
	}
	// Space-separated residue lists.
	for name, list := range map[string]string{
		"fixed_residues":          p.FixedResidues,
		"redesigned_residues":     p.RedesignedResidues,
		"transmembrane_buried":    p.TransmembraneBuried,
		"transmembrane_interface": p.TransmembraneInterface,
	} {
		for _, tok := range strings.Fields(list) {
			if !residueTokenRE.MatchString(tok) {
				return fmt.Errorf("design.ligandmpnn: %s: %q is not a residue reference "+
					"(expected chain+number, e.g. A23 or B42D)", name, tok)
			}
		}
	}
	// symmetry_residues is pipe-separated groups of comma-separated residues.
	for _, group := range strings.Split(p.SymmetryResidues, "|") {
		for _, tok := range strings.Split(group, ",") {
			if tok = strings.TrimSpace(tok); tok == "" {
				continue
			}
			if !residueTokenRE.MatchString(tok) {
				return fmt.Errorf("design.ligandmpnn: symmetry_residues: %q is not a "+
					"residue reference (expected chain+number, e.g. A23)", tok)
			}
		}
	}
	if p.ChainsToDesign != "" {
		for _, ch := range strings.Split(p.ChainsToDesign, ",") {
			if strings.TrimSpace(ch) == "" {
				return fmt.Errorf("design.ligandmpnn: chains_to_design has an empty chain id " +
					"— use a comma-separated list like \"A,B\"")
			}
		}
	}
	if p.BiasAA != "" {
		for _, pair := range strings.Split(p.BiasAA, ",") {
			kv := strings.SplitN(strings.TrimSpace(pair), ":", 2)
			if len(kv) != 2 {
				return fmt.Errorf("design.ligandmpnn: bias_AA token %q must be "+
					"<AA>:<weight> (e.g. W:3.0)", pair)
			}
			if _, err := strconv.ParseFloat(strings.TrimSpace(kv[1]), 64); err != nil {
				return fmt.Errorf("design.ligandmpnn: bias_AA token %q has a non-numeric weight", pair)
			}
		}
	}
	for _, ch := range p.OmitAA {
		if !((ch >= 'A' && ch <= 'Z') || (ch >= 'a' && ch <= 'z')) {
			return fmt.Errorf("design.ligandmpnn: omit_AA must be one-letter amino-acid "+
				"codes only (got %q)", p.OmitAA)
		}
	}
	if l := p.GlobalTransmembraneLabel; l != nil && *l != 0 && *l != 1 {
		return fmt.Errorf("design.ligandmpnn: global_transmembrane_label must be 0 or 1 (got %d)", *l)
	}
	for name, v := range map[string]int{
		"num_designs":                p.NumDesigns,
		"batch_size":                 p.BatchSize,
		"number_of_packs_per_design": p.NumberOfPacksPerDesign,
	} {
		if v < 0 {
			return fmt.Errorf("design.ligandmpnn: %s must not be negative (got %d)", name, v)
		}
	}
	if p.Temperature != nil && *p.Temperature <= 0 {
		return fmt.Errorf("design.ligandmpnn: temperature must be greater than 0")
	}
	if p.LigandCutoff != nil && *p.LigandCutoff <= 0 {
		return fmt.Errorf("design.ligandmpnn: ligand_cutoff must be greater than 0")
	}
	return nil
}
