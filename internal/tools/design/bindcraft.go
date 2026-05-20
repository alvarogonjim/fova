package design

import (
	"github.com/alvarogonjim/fova/internal/backends"
	"github.com/alvarogonjim/fova/internal/domain"
	"github.com/alvarogonjim/fova/internal/jobs"
	"github.com/alvarogonjim/fova/internal/store"
)

// NewBindCraftTool builds the design.bindcraft tool — the primary binder method.
// workspaceRoot scopes all relative path inputs (settings.starting_pdb, etc.).
func NewBindCraftTool(workspaceRoot string, mgr *jobs.Manager, backend backends.Backend, st *store.Store) *designTool {
	return &designTool{
		name:          "design.bindcraft",
		description:   "Design de novo protein binders against a target with BindCraft (runs as an async job).",
		origin:        domain.OriginBindCraft,
		application:   domain.AppBinder,
		mgr:           mgr,
		backend:       backend,
		store:         st,
		workspaceRoot: workspaceRoot,
	}
}
