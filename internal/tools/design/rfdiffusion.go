package design

import (
	"github.com/alvarogonjim/proteus/internal/backends"
	"github.com/alvarogonjim/proteus/internal/domain"
	"github.com/alvarogonjim/proteus/internal/jobs"
	"github.com/alvarogonjim/proteus/internal/store"
)

// NewRFdiffusionTool builds the design.rfdiffusion tool — backbone generation.
func NewRFdiffusionTool(mgr *jobs.Manager, backend backends.Backend, st *store.Store) *designTool {
	return &designTool{
		name:        "design.rfdiffusion",
		description: "Generate protein backbones against a target with RFdiffusion (runs as an async job).",
		origin:      domain.OriginRFDiffMPNN,
		application: domain.AppBinder,
		mgr:         mgr,
		backend:     backend,
		store:       st,
	}
}
