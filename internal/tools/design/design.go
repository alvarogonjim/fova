// Package design provides the agent's de-novo protein design tools. Each runs
// as an async job on the selected compute backend and persists the designs it
// produces.
package design

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/google/uuid"

	"github.com/alvarogonjim/fova/internal/backends"
	"github.com/alvarogonjim/fova/internal/domain"
	"github.com/alvarogonjim/fova/internal/jobs"
	"github.com/alvarogonjim/fova/internal/store"
	"github.com/alvarogonjim/fova/internal/tools"
)

// backendOutput is the conventional JSON a backend returns for a design tool.
// A response without a "designs" array (e.g. a tool error) yields zero designs.
type backendOutput struct {
	Designs []struct {
		Sequence      map[string]string  `json:"sequence"`
		StructureFile string             `json:"structure_file"`
		Scores        map[string]float64 `json:"scores"`
	} `json:"designs"`
}

// designTool is the shared implementation of every design.* tool.
type designTool struct {
	name          string
	description   string
	origin        domain.DesignOrigin
	application   domain.Application
	mgr           *jobs.Manager
	backend       backends.Backend
	store         *store.Store
	workspaceRoot string
}

// pathInputFields is the allowlist of JSON keys that the design tool schema
// declares as workspace-relative paths. Execute resolves each present field
// against workspaceRoot before handing the request to the backend so adapters
// always see an absolute, escape-checked path.
//
// Judgment call: kept deliberately small. Adding new path-typed fields to a
// design schema means adding the key here too.
var pathInputFields = []string{"target", "starting_pdb", "contigs_pdb", "theozyme"}

func (t *designTool) Name() string        { return t.name }
func (t *designTool) Description() string { return t.description }
func (t *designTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"target":      map[string]any{"type": "string", "description": "Target PDB ID or file path"},
			"hotspots":    map[string]any{"type": "string", "description": "Target hotspot residues"},
			"num_designs": map[string]any{"type": "integer", "description": "Number of designs to generate"},
			"contigs":     map[string]any{"type": "string", "description": "RFdiffusion contig map (design.rfdiffusion only)"},
			"settings":    map[string]any{"type": "object", "description": "BindCraft target-settings JSON (design.bindcraft only)"},
		},
	}
}

// Design jobs are long and GPU-bound — always require user approval.
func (*designTool) RequiresConfirmation(json.RawMessage) bool       { return true }
func (*designTool) EstimatedCostUSD(json.RawMessage) float64        { return 5.0 }
func (*designTool) EstimatedDuration(json.RawMessage) time.Duration { return 30 * time.Minute }

// Execute validates the request, submits a background job, and returns its ID
// immediately. The job runs the backend, parses the designs, and persists them.
func (t *designTool) Execute(_ context.Context, input json.RawMessage) (tools.Result, error) {
	var probe map[string]any
	if err := json.Unmarshal(input, &probe); err != nil {
		return tools.Result{}, fmt.Errorf("invalid %s request: %w", t.name, err)
	}
	resolved, err := t.resolvePathFields(probe)
	if err != nil {
		return tools.Result{}, fmt.Errorf("%s: %w", t.name, err)
	}
	jobID, err := t.mgr.Submit(jobs.Spec{
		Kind:    domain.JobCompute,
		Tool:    t.name,
		Backend: t.backend.Name(),
		Input:   resolved,
		Run: func(ctx context.Context, progress func(float64), log io.Writer) ([]byte, error) {
			out, err := t.backend.Run(ctx, t.name, resolved, log, progress)
			if err != nil {
				return nil, err
			}
			progress(0.95)
			n, perr := t.persist(out)
			if perr != nil {
				return out, perr
			}
			_ = n
			return out, nil
		},
	})
	if err != nil {
		return tools.Result{}, err
	}
	return tools.Result{
		JobID: jobID,
		Display: fmt.Sprintf("started %s job %s — poll jobs.result for the designs",
			t.name, jobID),
		Provenance: domain.NewToolCallRef(t.name, input),
	}, nil
}

// resolvePathFields walks the parsed JSON request, resolves every present
// path-typed field against the workspace root, and returns the re-marshalled
// JSON. Non-string or empty values are left alone. An empty workspaceRoot
// short-circuits to the original input (covers tests that don't set one).
//
// design.bindcraft nests its `starting_pdb` inside the `settings` object — so
// we also resolve allowlisted keys one level deep into a `settings` map.
func (t *designTool) resolvePathFields(probe map[string]any) (json.RawMessage, error) {
	if t.workspaceRoot == "" {
		return json.Marshal(probe)
	}
	if err := t.resolveAllowlistedKeys(probe); err != nil {
		return nil, err
	}
	if settings, ok := probe["settings"].(map[string]any); ok {
		if err := t.resolveAllowlistedKeys(settings); err != nil {
			return nil, err
		}
	}
	return json.Marshal(probe)
}

// resolveAllowlistedKeys mutates m in place, rewriting every allowlisted path
// field to its absolute workspace-rooted form.
func (t *designTool) resolveAllowlistedKeys(m map[string]any) error {
	for _, key := range pathInputFields {
		raw, ok := m[key]
		if !ok {
			continue
		}
		s, ok := raw.(string)
		if !ok || s == "" {
			continue
		}
		abs, err := tools.ResolveWorkspacePath(t.workspaceRoot, s)
		if err != nil {
			return fmt.Errorf("%s: %w", key, err)
		}
		m[key] = abs
	}
	return nil
}

// persist parses a backend's design-list output and writes each design to the
// store. A response with no "designs" array persists nothing.
func (t *designTool) persist(out []byte) (int, error) {
	var bo backendOutput
	if err := json.Unmarshal(out, &bo); err != nil {
		return 0, fmt.Errorf("%s output is not valid JSON: %w", t.name, err)
	}
	for _, d := range bo.Designs {
		design := domain.Design{
			ID:            domain.DesignID("d_" + uuid.NewString()),
			ProjectID:     store.DefaultProjectID,
			Created:       time.Now().UTC(),
			Origin:        t.origin,
			Application:   t.application,
			Sequence:      domain.Sequence{Chains: d.Sequence},
			StructureFile: d.StructureFile,
			Scores:        d.Scores,
			Provenance:    []domain.ToolCallRef{domain.NewToolCallRef(t.name, nil)},
		}
		if err := t.store.InsertDesign(design); err != nil {
			return 0, err
		}
	}
	return len(bo.Designs), nil
}
