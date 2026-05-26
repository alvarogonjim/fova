package domain

import "testing"

func TestLigandMPNNParamsValidate(t *testing.T) {
	ptrInt := func(v int) *int { return &v }
	ptrF := func(v float64) *float64 { return &v }
	cases := []struct {
		name string
		p    LigandMPNNParams
		ok   bool
	}{
		{"minimal valid", LigandMPNNParams{PDB: "bb.pdb"}, true},
		{"valid full-ish", LigandMPNNParams{
			PDB: "bb.pdb", ModelType: "ligand_mpnn", RedesignedResidues: "A23 B42D",
			BiasAA: "W:3.0,P:-2.0", OmitAA: "CG", SymmetryResidues: "A1,A2|A3,A4",
		}, true},
		{"missing pdb", LigandMPNNParams{ModelType: "ligand_mpnn"}, false},
		{"bad model_type", LigandMPNNParams{PDB: "bb.pdb", ModelType: "magic_mpnn"}, false},
		{"bad residue token", LigandMPNNParams{PDB: "bb.pdb", FixedResidues: "A23 oops"}, false},
		{"bad symmetry token", LigandMPNNParams{PDB: "bb.pdb", SymmetryResidues: "A1,xx"}, false},
		{"empty chain id", LigandMPNNParams{PDB: "bb.pdb", ChainsToDesign: "A,,B"}, false},
		{"bad bias_AA", LigandMPNNParams{PDB: "bb.pdb", BiasAA: "W3.0"}, false},
		{"non-numeric bias weight", LigandMPNNParams{PDB: "bb.pdb", BiasAA: "W:high"}, false},
		{"bad omit_AA", LigandMPNNParams{PDB: "bb.pdb", OmitAA: "C3"}, false},
		{"bad membrane label", LigandMPNNParams{PDB: "bb.pdb", GlobalTransmembraneLabel: ptrInt(2)}, false},
		{"negative num_designs", LigandMPNNParams{PDB: "bb.pdb", NumDesigns: -1}, false},
		{"non-positive temperature", LigandMPNNParams{PDB: "bb.pdb", Temperature: ptrF(0)}, false},
	}
	for _, c := range cases {
		err := c.p.Validate()
		if c.ok && err != nil {
			t.Errorf("%s: want valid, got %v", c.name, err)
		}
		if !c.ok && err == nil {
			t.Errorf("%s: want invalid, got nil", c.name)
		}
	}
}
