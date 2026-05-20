package design

import (
	"github.com/alvarogonjim/fova/internal/backends"
	"github.com/alvarogonjim/fova/internal/domain"
	"github.com/alvarogonjim/fova/internal/jobs"
	"github.com/alvarogonjim/fova/internal/store"
)

// NewProteinMPNNTool builds the design.proteinmpnn tool — sequence-from-structure.
// workspaceRoot scopes all relative path inputs (target, etc.) to the project.
func NewProteinMPNNTool(workspaceRoot string, mgr *jobs.Manager, backend backends.Backend, st *store.Store) *designTool {
	return &designTool{
		name:          "design.proteinmpnn",
		description:   "Design sequences for a protein backbone with ProteinMPNN (runs as an async job).",
		origin:        domain.OriginRFDiffMPNN,
		application:   domain.AppBinder,
		mgr:           mgr,
		backend:       backend,
		store:         st,
		workspaceRoot: workspaceRoot,
	}
}
