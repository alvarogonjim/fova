package domain

import "testing"

func TestBindCraftParamsValidate(t *testing.T) {
	cases := []struct {
		name string
		p    BindCraftParams
		ok   bool
	}{
		{"minimal valid", BindCraftParams{
			StartingPDB: "t.pdb", Chains: "A",
			TargetHotspotResidues: "A30", LengthMin: 80, LengthMax: 120,
		}, true},
		{"valid full-ish", BindCraftParams{
			BinderName: "x", StartingPDB: "t.pdb", Chains: "A",
			TargetHotspotResidues: "A30,A33", LengthMin: 80, LengthMax: 120,
			NumberOfFinalDesigns: 10, BinderChain: "B", DesignRuns: 5,
			ProtocolName: "beta_only", TemplatePDB: "tpl.pdb", OmitAAs: "CG",
		}, true},
		{"missing starting_pdb", BindCraftParams{
			Chains: "A", TargetHotspotResidues: "A30", LengthMin: 80, LengthMax: 120,
		}, false},
		{"missing chains", BindCraftParams{
			StartingPDB: "t.pdb", TargetHotspotResidues: "A30", LengthMin: 80, LengthMax: 120,
		}, false},
		{"missing hotspots", BindCraftParams{
			StartingPDB: "t.pdb", Chains: "A", LengthMin: 80, LengthMax: 120,
		}, false},
		{"bad hotspot token", BindCraftParams{
			StartingPDB: "t.pdb", Chains: "A",
			TargetHotspotResidues: "A30,oops", LengthMin: 80, LengthMax: 120,
		}, false},
		{"length_min < 1", BindCraftParams{
			StartingPDB: "t.pdb", Chains: "A",
			TargetHotspotResidues: "A30", LengthMin: 0, LengthMax: 120,
		}, false},
		{"length_max < length_min", BindCraftParams{
			StartingPDB: "t.pdb", Chains: "A",
			TargetHotspotResidues: "A30", LengthMin: 120, LengthMax: 80,
		}, false},
		{"negative number_of_final_designs", BindCraftParams{
			StartingPDB: "t.pdb", Chains: "A",
			TargetHotspotResidues: "A30", LengthMin: 80, LengthMax: 120,
			NumberOfFinalDesigns: -1,
		}, false},
		{"bad protocol_name", BindCraftParams{
			StartingPDB: "t.pdb", Chains: "A",
			TargetHotspotResidues: "A30", LengthMin: 80, LengthMax: 120,
			ProtocolName: "magic",
		}, false},
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
