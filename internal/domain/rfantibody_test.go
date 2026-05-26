package domain

import "testing"

func TestRFantibodyParamsValidate(t *testing.T) {
	ptrInt := func(v int) *int { return &v }
	ptrF := func(v float64) *float64 { return &v }
	cases := []struct {
		name string
		p    RFantibodyParams
		ok   bool
	}{
		{"minimal valid", RFantibodyParams{Target: "ag.pdb", Hotspots: "T10"}, true},
		{"valid full-ish", RFantibodyParams{
			Target: "ag.pdb", Hotspots: "T305,T456", Framework: "nanobody",
			DesignLoops: "H1:7,H3:5-13,L3:9-11", NumDesigns: 100,
		}, true},
		{"valid framework_pdb override", RFantibodyParams{
			Target: "ag.pdb", Hotspots: "T10", FrameworkPDB: "fw.pdb", Framework: "",
		}, true},
		{"missing target", RFantibodyParams{Hotspots: "T10"}, false},
		{"missing hotspots", RFantibodyParams{Target: "ag.pdb"}, false},
		{"bad hotspot token", RFantibodyParams{Target: "ag.pdb", Hotspots: "T10,oops"}, false},
		{"bad framework", RFantibodyParams{Target: "ag.pdb", Hotspots: "T10", Framework: "fab"}, false},
		{"bad design_loops CDR", RFantibodyParams{
			Target: "ag.pdb", Hotspots: "T10", DesignLoops: "X9:7"}, false},
		{"bad design_loops range", RFantibodyParams{
			Target: "ag.pdb", Hotspots: "T10", DesignLoops: "H3:13-5"}, false},
		{"bad design_loops length", RFantibodyParams{
			Target: "ag.pdb", Hotspots: "T10", DesignLoops: "H1:abc"}, false},
		{"negative num_designs", RFantibodyParams{
			Target: "ag.pdb", Hotspots: "T10", NumDesigns: -1}, false},
		{"non-positive num_recycles", RFantibodyParams{
			Target: "ag.pdb", Hotspots: "T10", NumRecycles: ptrInt(0)}, false},
		{"hotspot_show_prop out of range", RFantibodyParams{
			Target: "ag.pdb", Hotspots: "T10", HotspotShowProp: ptrF(1.5)}, false},
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
