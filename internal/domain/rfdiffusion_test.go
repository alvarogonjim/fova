package domain

import "testing"

func TestRFdiffusionParamsValidate(t *testing.T) {
	ptrB := func(v bool) *bool { return &v }
	ptrF := func(v float64) *float64 { return &v }
	cases := []struct {
		name string
		p    RFdiffusionParams
		ok   bool
	}{
		{"minimal valid", RFdiffusionParams{Contigs: "50-100"}, true},
		{"valid full-ish", RFdiffusionParams{
			Target: "t.pdb", Hotspots: "A30,A33", Contigs: "A1-100/0 60-80",
			NumDesigns: 10, Deterministic: ptrB(true), Symmetric: ptrB(true),
			SymmetryKind: "cyclic", NChains: 4, PartialT: 12,
			GuidingPotentials: []string{"binder_ROG"}, GuideScale: ptrF(5.0),
		}, true},
		{"missing contigs", RFdiffusionParams{}, false},
		{"bad hotspot token", RFdiffusionParams{Contigs: "X", Hotspots: "A30,oops"}, false},
		{"symmetric but bad kind", RFdiffusionParams{Contigs: "X", Symmetric: ptrB(true), SymmetryKind: "weird", NChains: 4}, false},
		{"symmetric but no n_chains", RFdiffusionParams{Contigs: "X", Symmetric: ptrB(true), SymmetryKind: "cyclic"}, false},
		{"bad symmetry_kind alone", RFdiffusionParams{Contigs: "X", SymmetryKind: "weird"}, false},
		{"negative num_designs", RFdiffusionParams{Contigs: "X", NumDesigns: -1}, false},
		{"non-positive noise_scale_ca", RFdiffusionParams{Contigs: "X", NoiseScaleCA: ptrF(0)}, false},
		{"empty guiding_potential", RFdiffusionParams{Contigs: "X", GuidingPotentials: []string{""}}, false},
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
