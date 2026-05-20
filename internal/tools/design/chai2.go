package design

import (
	"github.com/alvarogonjim/proteus/internal/backends"
	"github.com/alvarogonjim/proteus/internal/domain"
	"github.com/alvarogonjim/proteus/internal/jobs"
	"github.com/alvarogonjim/proteus/internal/store"
)

// NewChai2Tool builds the design.chai2 tool — the fallback antibody method.
func NewChai2Tool(mgr *jobs.Manager, backend backends.Backend, st *store.Store) *designTool {
	return &designTool{
		name:        "design.chai2",
		description: "Design de novo antibodies against a target with Chai-2 (runs as an async job).",
		origin:      domain.OriginChai2,
		application: domain.AppAntibody,
		mgr:         mgr,
		backend:     backend,
		store:       st,
	}
}
