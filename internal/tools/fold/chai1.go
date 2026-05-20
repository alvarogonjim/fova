package fold

import (
	"github.com/alvarogonjim/fova/internal/backends"
	"github.com/alvarogonjim/fova/internal/jobs"
)

// NewChai1 returns the fold.chai1 tool: Chai-1 complex structure prediction on
// the Modal compute backend, run as an async job.
func NewChai1(mgr *jobs.Manager, backend backends.Backend) *foldJobTool {
	return &foldJobTool{
		name: "fold.chai1",
		description: "Predict the 3D structure of a protein complex from its chain " +
			"sequences using Chai-1 (runs as an async job).",
		mgr:     mgr,
		backend: backend,
	}
}
