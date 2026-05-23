package domain

import "testing"

func TestProteinMPNNParamsValidate(t *testing.T) {
	ptrInt := func(v int) *int { return &v }
	ptrF := func(v float64) *float64 { return &v }
	cases := []struct {
		name string
		p    ProteinMPNNParams
		ok   bool
	}{
		{"minimal valid", ProteinMPNNParams{PDB: "bb.pdb"}, true},
		{"valid full-ish", ProteinMPNNParams{
			PDB: "bb.pdb", NumDesigns: 8, BatchSize: 2, SamplingTemp: ptrF(0.2),
			Seed: ptrInt(42), ChainsToDesign: "A", OmitAAs: "CG",
			FixedPositions: "fixed.jsonl", BiasAA: "bias_AA.jsonl",
			BiasByResidue: "bias_res.jsonl", TiedPositions: "tied.jsonl",
		}, true},
		{"missing pdb", ProteinMPNNParams{}, false},
		{"negative num_designs", ProteinMPNNParams{PDB: "bb.pdb", NumDesigns: -1}, false},
		{"negative batch_size", ProteinMPNNParams{PDB: "bb.pdb", BatchSize: -1}, false},
		{"non-positive sampling_temp", ProteinMPNNParams{PDB: "bb.pdb", SamplingTemp: ptrF(0)}, false},
		{"negative seed", ProteinMPNNParams{PDB: "bb.pdb", Seed: ptrInt(-1)}, false},
		{"non-letter omit_AAs", ProteinMPNNParams{PDB: "bb.pdb", OmitAAs: "C3"}, false},
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
