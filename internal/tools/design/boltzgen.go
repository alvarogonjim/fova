package design

import (
	"github.com/alvarogonjim/fova/internal/backends"
	"github.com/alvarogonjim/fova/internal/domain"
	"github.com/alvarogonjim/fova/internal/jobs"
	"github.com/alvarogonjim/fova/internal/store"
)

// NewBoltzGenTool builds the design.boltzgen tool — the SPECS-blessed binder
// method that runs on aarch64/Grace, where BindCraft (PyRosetta) is
// unavailable. workspaceRoot scopes all relative path inputs (target, etc.).
func NewBoltzGenTool(workspaceRoot string, mgr *jobs.Manager, backend backends.Backend, st *store.Store) *designTool {
	return &designTool{
		name: "design.boltzgen",
		description: "Design de novo protein binders against a target with BoltzGen " +
			"(Boltz-2 based; runs as an async job). Primary binder method on " +
			"aarch64/Grace, where BindCraft is unavailable.",
		origin:        domain.OriginBoltzGen,
		application:   domain.AppBinder,
		mgr:           mgr,
		backend:       backend,
		store:         st,
		workspaceRoot: workspaceRoot,
	}
}
