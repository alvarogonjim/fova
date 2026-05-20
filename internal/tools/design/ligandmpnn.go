package design

import (
	"github.com/alvarogonjim/fova/internal/backends"
	"github.com/alvarogonjim/fova/internal/domain"
	"github.com/alvarogonjim/fova/internal/jobs"
	"github.com/alvarogonjim/fova/internal/store"
)

// NewLigandMPNNTool builds the design.ligandmpnn tool — enzyme sequence design.
// workspaceRoot scopes all relative path inputs (target, etc.).
func NewLigandMPNNTool(workspaceRoot string, mgr *jobs.Manager, backend backends.Backend, st *store.Store) *designTool {
	return &designTool{
		name:          "design.ligandmpnn",
		description:   "Design enzyme sequences around a bound ligand with LigandMPNN (runs as an async job).",
		origin:        domain.OriginRFDiff2MPNN,
		application:   domain.AppEnzyme,
		mgr:           mgr,
		backend:       backend,
		store:         st,
		workspaceRoot: workspaceRoot,
	}
}
