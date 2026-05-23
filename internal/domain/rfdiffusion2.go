package domain

import (
	"fmt"
	"strings"
)

// rfdiffusion2Benchmarks is the closed set of bundled active-site sweeps.
var rfdiffusion2Benchmarks = map[string]bool{
	"active_site_demo": true,
	"enzyme_bench_n41": true,
}

// rfdiffusion2StopSteps is the closed set of pipeline stop points.
var rfdiffusion2StopSteps = map[string]bool{
	"design": true, // backbone diffusion + idealization only
	"end":    true, // full pipeline (default) — design + LigandMPNN + Chai-1
}

// Validate checks the value shape of an RFdiffusion2Params. It performs no
// filesystem access — workspace-path existence (motif_pdb) is the caller's
// job (the design tool's Execute, the adapter's Invoke). It returns the first
// violation as a design.rfdiffusion2-prefixed error, or nil when valid.
func (p RFdiffusion2Params) Validate() error {
	if b := strings.TrimSpace(p.Benchmark); b != "" && !rfdiffusion2Benchmarks[b] {
		return fmt.Errorf("design.rfdiffusion2: benchmark %q is invalid — "+
			"use active_site_demo (default) or enzyme_bench_n41", p.Benchmark)
	}
	if motif := strings.TrimSpace(p.MotifPDB); motif != "" {
		if !strings.HasSuffix(motif, ".pdb") {
			return fmt.Errorf("design.rfdiffusion2: motif_pdb %q must be a .pdb file", p.MotifPDB)
		}
		if strings.TrimSpace(p.Contigs) == "" {
			return fmt.Errorf("design.rfdiffusion2: contigs is required when motif_pdb is set " +
				"(give the Hydra contigmap.contigs string, e.g. 5-15,A10-30,5-15)")
		}
	}
	if p.NumDesigns < 0 {
		return fmt.Errorf("design.rfdiffusion2: num_designs must not be negative (got %d)", p.NumDesigns)
	}
	if p.Seed != nil && *p.Seed < 0 {
		return fmt.Errorf("design.rfdiffusion2: seed must not be negative (got %d)", *p.Seed)
	}
	if s := strings.TrimSpace(p.StopStep); s != "" && !rfdiffusion2StopSteps[s] {
		return fmt.Errorf("design.rfdiffusion2: stop_step %q is invalid — "+
			"use design (backbone only) or end (full pipeline, default)", p.StopStep)
	}
	return nil
}
