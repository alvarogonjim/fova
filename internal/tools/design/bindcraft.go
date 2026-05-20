package design

import (
	"github.com/alvarogonjim/proteus/internal/backends"
	"github.com/alvarogonjim/proteus/internal/domain"
	"github.com/alvarogonjim/proteus/internal/jobs"
	"github.com/alvarogonjim/proteus/internal/store"
)

// NewBindCraftTool builds the design.bindcraft tool — the primary binder method.
func NewBindCraftTool(mgr *jobs.Manager, backend backends.Backend, st *store.Store) *designTool {
	return &designTool{
		name:        "design.bindcraft",
		description: "Design de novo protein binders against a target with BindCraft (runs as an async job).",
		origin:      domain.OriginBindCraft,
		application: domain.AppBinder,
		mgr:         mgr,
		backend:     backend,
		store:       st,
	}
}
