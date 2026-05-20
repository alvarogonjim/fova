package design

import (
	"github.com/alvarogonjim/proteus/internal/backends"
	"github.com/alvarogonjim/proteus/internal/domain"
	"github.com/alvarogonjim/proteus/internal/jobs"
	"github.com/alvarogonjim/proteus/internal/store"
)

// NewRFdiffusion2Tool builds the design.rfdiffusion2 tool — enzyme backbone scaffolding.
func NewRFdiffusion2Tool(mgr *jobs.Manager, backend backends.Backend, st *store.Store) *designTool {
	return &designTool{
		name:        "design.rfdiffusion2",
		description: "Scaffold enzyme backbones around a catalytic motif with RFdiffusion2 (runs as an async job).",
		origin:      domain.OriginRFDiff2MPNN,
		application: domain.AppEnzyme,
		mgr:         mgr,
		backend:     backend,
		store:       st,
	}
}
