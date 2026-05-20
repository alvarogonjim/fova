package design

import (
	"github.com/alvarogonjim/fova/internal/backends"
	"github.com/alvarogonjim/fova/internal/domain"
	"github.com/alvarogonjim/fova/internal/jobs"
	"github.com/alvarogonjim/fova/internal/store"
)

// NewRFdiffusionTool builds the design.rfdiffusion tool — backbone generation.
// workspaceRoot scopes all relative path inputs (target, etc.).
func NewRFdiffusionTool(workspaceRoot string, mgr *jobs.Manager, backend backends.Backend, st *store.Store) *designTool {
	return &designTool{
		name:          "design.rfdiffusion",
		description:   "Generate protein backbones against a target with RFdiffusion (runs as an async job).",
		origin:        domain.OriginRFDiffMPNN,
		application:   domain.AppBinder,
		mgr:           mgr,
		backend:       backend,
		store:         st,
		workspaceRoot: workspaceRoot,
	}
}
