package local

import (
	"strings"
	"testing"

	"github.com/alvarogonjim/fova/internal/domain"
)

func TestRFdiffusion2HydraOverridesActiveSiteDemo(t *testing.T) {
	args := rfdiffusion2HydraOverrides(domain.RFdiffusion2Params{
		Benchmark: "active_site_demo",
	}, "")
	joined := strings.Join(args, " ")
	for _, want := range []string{
		"--config-name=open_source_demo",
		"sweep.benchmarks=active_site_unindexed_atomic_partial_ligand",
		"outdir=/work/out",
		"hydra.run.dir=/work/out",
		"stop_step='end'", // default
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("overrides missing %q in:\n%s", want, joined)
		}
	}
}

func TestRFdiffusion2HydraOverridesEnzymeBench(t *testing.T) {
	args := rfdiffusion2HydraOverrides(domain.RFdiffusion2Params{
		Benchmark: "enzyme_bench_n41",
	}, "")
	joined := strings.Join(args, " ")
	for _, want := range []string{
		"--config-name=enzyme_bench_n41_fixedligand",
		"in_proc=True",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("overrides missing %q in:\n%s", want, joined)
		}
	}
}

func TestRFdiffusion2HydraOverridesMotifOverride(t *testing.T) {
	args := rfdiffusion2HydraOverrides(domain.RFdiffusion2Params{
		Benchmark: "active_site_demo",
		MotifPDB:  "/host/triad.pdb",
		Contigs:   "5-15,A10-30,5-15",
	}, "/work/triad.pdb")
	joined := strings.Join(args, " ")
	for _, want := range []string{
		"+inference.input_pdb=/work/triad.pdb",
		"contigmap.contigs=[5-15,A10-30,5-15]",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("overrides missing %q in:\n%s", want, joined)
		}
	}
}

func TestBuildRFdiffusion2Driver(t *testing.T) {
	script := buildRFdiffusion2Driver([]string{
		"--config-name=open_source_demo",
		"sweep.benchmarks=active_site_unindexed_atomic_partial_ligand",
		"outdir=/work/out",
		"stop_step='end'",
	})
	for _, want := range []string{
		"#!/bin/bash",
		"set -euo pipefail",
		"mkdir -p /work/out",
		"python /opt/rfdiffusion2/rf_diffusion/benchmark/pipeline.py",
		"--config-name=open_source_demo",
		"sweep.benchmarks=active_site_unindexed_atomic_partial_ligand",
		"outdir=/work/out",
		"stop_step='end'",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("driver missing %q in:\n%s", want, script)
		}
	}
}

func TestRFdiffusion2HydraOverridesToggles(t *testing.T) {
	tru := true
	fls := false
	seed := 7
	args := rfdiffusion2HydraOverrides(domain.RFdiffusion2Params{
		NumDesigns:               8,
		Seed:                     &seed,
		GuidepostXYZAsDesignBB:   &tru,
		IdealizeSidechainOutputs: &fls,
		StopStep:                 "design",
	}, "")
	joined := strings.Join(args, " ")
	for _, want := range []string{
		"inference.num_designs=8",
		"seed=7",
		"inference.guidepost_xyz_as_design_bb=True",
		"inference.idealize_sidechain_outputs=False",
		"stop_step='design'",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("overrides missing %q in:\n%s", want, joined)
		}
	}
}
