package local

import (
	"os"
	"path/filepath"
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

func TestParseRFdiffusion2Output(t *testing.T) {
	outDir := t.TempDir()

	// A run directory like pipeline_outputs/<timestamp>_<config>/ with PDBs
	// and a metrics CSV. The parser glob-searches; we exercise that here.
	runDir := filepath.Join(outDir, "pipeline_outputs", "2026-05-23_demo")
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"design_0.pdb", "design_1.pdb"} {
		if err := os.WriteFile(filepath.Join(runDir, name), []byte("ATOM\nEND\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	csv := "name,metrics.IdealizedResidueRMSD.rmsd_constellation,motif_ideality_diff,contig_rmsd_des_ref_motif_atom,extra_score\n" +
		"design_0,0.42,0.11,0.38,0.91\n" +
		"design_1,0.55,0.18,0.61,0.82\n"
	if err := os.WriteFile(filepath.Join(runDir, "metrics.csv"), []byte(csv), 0o644); err != nil {
		t.Fatal(err)
	}

	designs, err := parseRFdiffusion2Output(outDir)
	if err != nil {
		t.Fatalf("parseRFdiffusion2Output: %v", err)
	}
	if len(designs) != 2 {
		t.Fatalf("want 2 designs, got %d", len(designs))
	}

	// design_0 — sorted first.
	d0 := designs[0]
	if d0.StructureFile == "" || !strings.HasSuffix(d0.StructureFile, "design_0.pdb") {
		t.Errorf("design_0 structure_file = %q", d0.StructureFile)
	}
	for k, want := range map[string]float64{
		"idealized_residue_rmsd": 0.42,
		"motif_ideality_diff":    0.11,
		"motif_rmsd":             0.38,
		"extra_score":            0.91,
	} {
		if got := d0.Scores[k]; got != want {
			t.Errorf("design_0 score %q = %v, want %v", k, got, want)
		}
	}
}

func TestParseRFdiffusion2OutputEmptyErrors(t *testing.T) {
	if _, err := parseRFdiffusion2Output(t.TempDir()); err == nil {
		t.Fatal("expected an error when no prediction PDBs are present")
	}
}

func TestParseRFdiffusion2OutputMissingCSVIsNotFatal(t *testing.T) {
	outDir := t.TempDir()
	runDir := filepath.Join(outDir, "pipeline_outputs", "2026-05-23_demo")
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runDir, "design_0.pdb"), []byte("ATOM\nEND\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	designs, err := parseRFdiffusion2Output(outDir)
	if err != nil {
		t.Fatalf("parseRFdiffusion2Output: %v", err)
	}
	if len(designs) != 1 || len(designs[0].Scores) != 0 {
		t.Errorf("missing CSV ⇒ designs with empty Scores, got %+v", designs)
	}
}
