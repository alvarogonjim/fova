package design

import (
	"github.com/alvarogonjim/fova/internal/backends"
	"github.com/alvarogonjim/fova/internal/domain"
	"github.com/alvarogonjim/fova/internal/jobs"
	"github.com/alvarogonjim/fova/internal/store"
)

// NewRFAntibodyTool builds the design.rfantibody tool — the primary antibody method.
// workspaceRoot scopes all relative path inputs (target, etc.).
func NewRFAntibodyTool(workspaceRoot string, mgr *jobs.Manager, backend backends.Backend, st *store.Store) *designTool {
	return &designTool{
		name:          "design.rfantibody",
		description:   "Design de novo antibodies (VHH / scFv) against a target with RFantibody (runs as an async job).",
		origin:        domain.OriginRFAntibody,
		application:   domain.AppAntibody,
		mgr:           mgr,
		backend:       backend,
		store:         st,
		workspaceRoot: workspaceRoot,
	}
}
