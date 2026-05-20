package design

import (
	"github.com/alvarogonjim/proteus/internal/backends"
	"github.com/alvarogonjim/proteus/internal/domain"
	"github.com/alvarogonjim/proteus/internal/jobs"
	"github.com/alvarogonjim/proteus/internal/store"
)

// NewRFAntibodyTool builds the design.rfantibody tool — the primary antibody method.
func NewRFAntibodyTool(mgr *jobs.Manager, backend backends.Backend, st *store.Store) *designTool {
	return &designTool{
		name:        "design.rfantibody",
		description: "Design de novo antibodies (VHH / scFv) against a target with RFantibody (runs as an async job).",
		origin:      domain.OriginRFAntibody,
		application: domain.AppAntibody,
		mgr:         mgr,
		backend:     backend,
		store:       st,
	}
}
