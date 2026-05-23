package local

import (
	"strconv"

	"github.com/alvarogonjim/fova/internal/domain"
)

// rfdiffusion2HydraOverrides returns the positional Hydra overrides for one
// pipeline.py invocation. motifContainerPath is the /work-rooted path of the
// motif PDB once staged by Invoke; an empty string means no motif override.
//
// Always-on overrides:
//
//	--config-name=... + sweep selection (per Benchmark, or the default)
//	outdir=/work/out + hydra.run.dir=/work/out (so the output tree is deterministic)
//	stop_step='end' (the default, full pipeline; can be overridden)
//
// Conditional overrides — emitted only when the corresponding field is set:
//
//	+inference.input_pdb=<motifContainerPath> contigmap.contigs=[<Contigs>]  (when MotifPDB set)
//	inference.num_designs=<N>
//	seed=<N>
//	inference.guidepost_xyz_as_design_bb=True|False
//	inference.idealize_sidechain_outputs=True|False
//	stop_step='<design|end>' (replaces the default when explicit)
func rfdiffusion2HydraOverrides(p domain.RFdiffusion2Params, motifContainerPath string) []string {
	var args []string

	// --config-name + bundled sweep selection.
	switch p.Benchmark {
	case "enzyme_bench_n41":
		args = append(args,
			"--config-name=enzyme_bench_n41_fixedligand",
			"in_proc=True",
		)
	default: // "" or "active_site_demo"
		args = append(args,
			"--config-name=open_source_demo",
			"sweep.benchmarks=active_site_unindexed_atomic_partial_ligand",
		)
	}

	// Deterministic output landing.
	args = append(args, "outdir=/work/out", "hydra.run.dir=/work/out")

	// Motif override.
	if motifContainerPath != "" {
		args = append(args,
			"+inference.input_pdb="+motifContainerPath,
			"contigmap.contigs=["+p.Contigs+"]",
		)
	}

	// Inference toggles.
	if p.NumDesigns > 0 {
		args = append(args, "inference.num_designs="+strconv.Itoa(p.NumDesigns))
	}
	if p.Seed != nil {
		args = append(args, "seed="+strconv.Itoa(*p.Seed))
	}
	if p.GuidepostXYZAsDesignBB != nil {
		args = append(args, "inference.guidepost_xyz_as_design_bb="+pyBool(*p.GuidepostXYZAsDesignBB))
	}
	if p.IdealizeSidechainOutputs != nil {
		args = append(args, "inference.idealize_sidechain_outputs="+pyBool(*p.IdealizeSidechainOutputs))
	}

	// stop_step — default 'end' (full pipeline) unless explicit.
	stop := p.StopStep
	if stop == "" {
		stop = "end"
	}
	args = append(args, "stop_step='"+stop+"'")

	return args
}

// pyBool returns "True"/"False" — what Hydra/OmegaConf expect for a bool
// override. Lower-case "true"/"false" works in newer OmegaConf releases but
// the Python-style capitalised form is the safe, upstream-documented choice.
func pyBool(v bool) string {
	if v {
		return "True"
	}
	return "False"
}
