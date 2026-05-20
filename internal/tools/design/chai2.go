package design

import (
	"github.com/alvarogonjim/fova/internal/backends"
	"github.com/alvarogonjim/fova/internal/domain"
	"github.com/alvarogonjim/fova/internal/jobs"
	"github.com/alvarogonjim/fova/internal/store"
)

// NewChai2Tool builds the design.chai2 tool — the fallback antibody method.
// workspaceRoot scopes all relative path inputs (target, etc.).
func NewChai2Tool(workspaceRoot string, mgr *jobs.Manager, backend backends.Backend, st *store.Store) *designTool {
	return &designTool{
		name:          "design.chai2",
		description:   "Design de novo antibodies against a target with Chai-2 (runs as an async job).",
		origin:        domain.OriginChai2,
		application:   domain.AppAntibody,
		mgr:           mgr,
		backend:       backend,
		store:         st,
		workspaceRoot: workspaceRoot,
	}
}
