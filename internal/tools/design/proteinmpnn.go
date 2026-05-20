package design

import (
	"github.com/alvarogonjim/proteus/internal/backends"
	"github.com/alvarogonjim/proteus/internal/domain"
	"github.com/alvarogonjim/proteus/internal/jobs"
	"github.com/alvarogonjim/proteus/internal/store"
)

// NewProteinMPNNTool builds the design.proteinmpnn tool — sequence-from-structure.
func NewProteinMPNNTool(mgr *jobs.Manager, backend backends.Backend, st *store.Store) *designTool {
	return &designTool{
		name:        "design.proteinmpnn",
		description: "Design sequences for a protein backbone with ProteinMPNN (runs as an async job).",
		origin:      domain.OriginRFDiffMPNN,
		application: domain.AppBinder,
		mgr:         mgr,
		backend:     backend,
		store:       st,
	}
}
