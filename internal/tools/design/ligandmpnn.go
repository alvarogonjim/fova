package design

import (
	"github.com/alvarogonjim/proteus/internal/backends"
	"github.com/alvarogonjim/proteus/internal/domain"
	"github.com/alvarogonjim/proteus/internal/jobs"
	"github.com/alvarogonjim/proteus/internal/store"
)

// NewLigandMPNNTool builds the design.ligandmpnn tool — enzyme sequence design.
func NewLigandMPNNTool(mgr *jobs.Manager, backend backends.Backend, st *store.Store) *designTool {
	return &designTool{
		name:        "design.ligandmpnn",
		description: "Design enzyme sequences around a bound ligand with LigandMPNN (runs as an async job).",
		origin:      domain.OriginRFDiff2MPNN,
		application: domain.AppEnzyme,
		mgr:         mgr,
		backend:     backend,
		store:       st,
	}
}
