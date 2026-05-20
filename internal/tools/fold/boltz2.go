package fold

import (
	"github.com/alvarogonjim/proteus/internal/backends"
	"github.com/alvarogonjim/proteus/internal/jobs"
)

// NewBoltz2 returns the fold.boltz2 tool: Boltz-2 complex structure prediction
// on the Modal compute backend, run as an async job.
func NewBoltz2(mgr *jobs.Manager, backend backends.Backend) *foldJobTool {
	return &foldJobTool{
		name: "fold.boltz2",
		description: "Predict the 3D structure of a protein complex from its chain " +
			"sequences using Boltz-2 (runs as an async job).",
		mgr:     mgr,
		backend: backend,
	}
}
