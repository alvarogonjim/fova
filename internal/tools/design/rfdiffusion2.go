package design

import (
	"github.com/alvarogonjim/fova/internal/backends"
	"github.com/alvarogonjim/fova/internal/domain"
	"github.com/alvarogonjim/fova/internal/jobs"
	"github.com/alvarogonjim/fova/internal/store"
)

// NewRFdiffusion2Tool builds the design.rfdiffusion2 tool — enzyme backbone scaffolding.
// workspaceRoot scopes all relative path inputs (target, theozyme, etc.).
func NewRFdiffusion2Tool(workspaceRoot string, mgr *jobs.Manager, backend backends.Backend, st *store.Store) *designTool {
	return &designTool{
		name:          "design.rfdiffusion2",
		description:   "Scaffold enzyme backbones around a catalytic motif with RFdiffusion2 (runs as an async job).",
		origin:        domain.OriginRFDiff2MPNN,
		application:   domain.AppEnzyme,
		mgr:           mgr,
		backend:       backend,
		store:         st,
		workspaceRoot: workspaceRoot,
	}
}
