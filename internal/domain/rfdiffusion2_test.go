package domain

import (
	"strings"
	"testing"
)

func TestRFdiffusion2ParamsValidateValid(t *testing.T) {
	cases := []struct {
		name string
		p    RFdiffusion2Params
	}{
		{"defaults", RFdiffusion2Params{}},
		{"explicit_active_site", RFdiffusion2Params{Benchmark: "active_site_demo"}},
		{"enzyme_bench", RFdiffusion2Params{Benchmark: "enzyme_bench_n41"}},
		{"motif_with_contigs", RFdiffusion2Params{
			MotifPDB: "inputs/triad.pdb", Contigs: "5-15,A10-30,5-15",
		}},
		{"all_toggles", RFdiffusion2Params{
			NumDesigns: 8, Seed: rfdiff2IntPtr(7), StopStep: "design",
			GuidepostXYZAsDesignBB:   rfdiff2BoolPtr(true),
			IdealizeSidechainOutputs: rfdiff2BoolPtr(false),
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := c.p.Validate(); err != nil {
				t.Errorf("Validate: %v", err)
			}
		})
	}
}

func TestRFdiffusion2ParamsValidateInvalid(t *testing.T) {
	cases := []struct {
		name    string
		p       RFdiffusion2Params
		wantSub string
	}{
		{"unknown_benchmark", RFdiffusion2Params{Benchmark: "made_up"}, "benchmark"},
		{"motif_without_contigs", RFdiffusion2Params{MotifPDB: "x.pdb"}, "contigs"},
		{"motif_bad_extension", RFdiffusion2Params{MotifPDB: "x.txt", Contigs: "1-1"}, "motif_pdb"},
		{"negative_num_designs", RFdiffusion2Params{NumDesigns: -1}, "num_designs"},
		{"negative_seed", RFdiffusion2Params{Seed: rfdiff2IntPtr(-3)}, "seed"},
		{"bad_stop_step", RFdiffusion2Params{StopStep: "halfway"}, "stop_step"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.p.Validate()
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), c.wantSub) {
				t.Errorf("error %q must mention %q", err.Error(), c.wantSub)
			}
		})
	}
}

func rfdiff2IntPtr(v int) *int    { return &v }
func rfdiff2BoolPtr(v bool) *bool { return &v }
