package lab

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/alvarogonjim/fova/internal/domain"
	"github.com/alvarogonjim/fova/internal/store"
	"github.com/alvarogonjim/fova/internal/tools"
	"github.com/alvarogonjim/fova/internal/transport"
)

// --- lab.targets_search ---

// pdbBaseURL is the RCSB PDB core-entry endpoint queried for chain metadata.
const pdbBaseURL = "https://data.rcsb.org/rest/v1/core/entry"

// pdbIDPattern reports whether the query string looks like a PDB ID (4 chars,
// starts with a digit per RCSB convention). Anything else that's purely
// alphabetic is treated as a gene-name candidate for UniProt fallback.
func looksLikePDBID(q string) bool {
	if len(q) != 4 {
		return false
	}
	if q[0] < '0' || q[0] > '9' {
		return false
	}
	for _, r := range q[1:] {
		if !((r >= '0' && r <= '9') || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z')) {
			return false
		}
	}
	return true
}

// looksLikeGeneName approximates UniProt's gene-symbol shape: at least two
// letters, no digits leading. This gates the UniProt fallback so PDB-id
// misses (numeric prefix) don't waste a round-trip.
func looksLikeGeneName(q string) bool {
	if q == "" {
		return false
	}
	if q[0] >= '0' && q[0] <= '9' {
		return false
	}
	for _, r := range q {
		if r == ' ' || r == '-' || r == '_' {
			continue
		}
		if !((r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')) {
			return false
		}
	}
	return true
}

// targetsSearchTool resolves a query against PDB (chain-aware) or UniProt
// (gene-name fallback). With an empty query it preserves the legacy
// behaviour: list the Adaptyv Foundry target catalog.
type targetsSearchTool struct {
	c          *Client
	tc         *transport.Client
	pdbBaseURL string
	uniprot    *UniProtClient
	cache      targetsCache
}

// NewTargetsSearchTool returns the lab.targets_search tool. The shared
// transport.Client carries retry + telemetry for both the PDB and UniProt
// hops.
func NewTargetsSearchTool(c *Client) *targetsSearchTool {
	tc := transport.New()
	return &targetsSearchTool{
		c:          c,
		tc:         tc,
		pdbBaseURL: pdbBaseURL,
		uniprot:    NewUniProtClient(tc),
	}
}

// WithCache wires in an explicit cache. Production wires the BoltDB cache;
// tests inject the in-memory implementation.
func (t *targetsSearchTool) WithCache(c targetsCache) *targetsSearchTool {
	t.cache = c
	return t
}

func (*targetsSearchTool) Name() string { return "lab.targets_search" }
func (*targetsSearchTool) Description() string {
	return "Resolve a target by PDB ID or gene name. Returns chain metadata " +
		"from RCSB PDB; falls back to UniProtKB for gene names with no " +
		"structure. With no query, lists the Adaptyv Foundry target catalog."
}
func (*targetsSearchTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "PDB ID (e.g. 1LYZ) or gene/protein name. Omit to list the Adaptyv target catalog.",
			},
		},
	}
}
func (*targetsSearchTool) RequiresConfirmation(json.RawMessage) bool       { return false }
func (*targetsSearchTool) EstimatedCostUSD(json.RawMessage) float64        { return 0 }
func (*targetsSearchTool) EstimatedDuration(json.RawMessage) time.Duration { return 5 * time.Second }

// targetResolveOutput is the structured response for a resolved query.
// chain is a pointer so a null value survives JSON encoding (no silent "A").
type targetResolveOutput struct {
	Source             string         `json:"source"`
	Query              string         `json:"query"`
	PDBID              string         `json:"pdb_id,omitempty"`
	Title              string         `json:"title,omitempty"`
	Method             string         `json:"method,omitempty"`
	Resolution         float64        `json:"resolution,omitempty"`
	Chain              *string        `json:"chain"`
	Chains             []string       `json:"chains,omitempty"`
	ChainInferenceNote string         `json:"chain_inference_note,omitempty"`
	UniProt            *UniProtRecord `json:"uniprot,omitempty"`
	Sequence           string         `json:"sequence,omitempty"`
	Cached             bool           `json:"cached,omitempty"`
}

// pdbCoreEntry mirrors the subset of the RCSB JSON response we read.
type pdbCoreEntry struct {
	Struct struct {
		Title string `json:"title"`
	} `json:"struct"`
	Exptl []struct {
		Method string `json:"method"`
	} `json:"exptl"`
	RCSBEntryInfo struct {
		ResolutionCombined []float64 `json:"resolution_combined"`
	} `json:"rcsb_entry_info"`
	// auth_asym_ids is the most reliable chain list across both modern and
	// legacy entries; absent on obsolete or chainless entries (the silent-"A"
	// trap we're fixing).
	PolymerEntityIdentifiers struct {
		AuthAsymIDs []string `json:"auth_asym_ids"`
	} `json:"rcsb_polymer_entity_container_identifiers"`
	EntryContainerIdentifiers struct {
		PolymerEntityIDs []string `json:"polymer_entity_ids"`
	} `json:"rcsb_entry_container_identifiers"`
}

// Execute resolves the input. Behaviour matrix:
//   - no query → Adaptyv catalog (legacy compatibility for the wet-lab loop)
//   - PDB-id-shaped query → PDB lookup, no UniProt fallback
//   - gene-name-shaped query → PDB miss falls through to UniProt
func (t *targetsSearchTool) Execute(ctx context.Context, input json.RawMessage) (tools.Result, error) {
	var in struct {
		Query string `json:"query"`
	}
	if len(input) > 0 {
		if err := json.Unmarshal(input, &in); err != nil {
			return tools.Result{}, err
		}
	}
	q := strings.TrimSpace(in.Query)
	if q == "" {
		return t.executeCatalog(ctx, input)
	}
	return t.executeResolve(ctx, q, input)
}

// executeCatalog preserves the v0.6 Adaptyv-catalog behaviour when no query
// is supplied.
func (t *targetsSearchTool) executeCatalog(ctx context.Context, input json.RawMessage) (tools.Result, error) {
	targets, err := t.c.ListTargets(ctx)
	if err != nil {
		return tools.Result{}, err
	}
	out, _ := json.Marshal(map[string]any{"targets": targets})
	return tools.Result{
		Output:     out,
		Display:    fmt.Sprintf("found %d Adaptyv target(s)", len(targets)),
		Provenance: domain.NewToolCallRef(t.Name(), input),
	}, nil
}

// executeResolve handles a non-empty query — PDB first, UniProt fallback.
func (t *targetsSearchTool) executeResolve(ctx context.Context, q string, input json.RawMessage) (tools.Result, error) {
	cacheKey := strings.ToUpper(q)
	if t.cache != nil {
		if raw, ok, _ := t.cache.Get(cacheKey); ok {
			var cached targetResolveOutput
			if err := json.Unmarshal(raw, &cached); err == nil {
				cached.Cached = true
				body, _ := json.Marshal(cached)
				return tools.Result{
					Output:     body,
					Display:    fmt.Sprintf("targets_search: %s (cached, source=%s)", q, cached.Source),
					Provenance: domain.NewToolCallRef(t.Name(), input),
				}, nil
			}
		}
	}

	pdbID := strings.ToUpper(q)
	pdbHit, pdbErr := t.fetchPDB(ctx, pdbID)

	// Treat 404 / not-found as a miss; other PDB errors propagate.
	if pdbErr != nil && !isPDBNotFound(pdbErr) {
		return tools.Result{}, pdbErr
	}

	if pdbHit != nil {
		out := targetResolveOutput{
			Source: "pdb",
			Query:  q,
			PDBID:  pdbID,
			Title:  pdbHit.Struct.Title,
			Chain:  nil,
		}
		if len(pdbHit.Exptl) > 0 {
			out.Method = pdbHit.Exptl[0].Method
		}
		if len(pdbHit.RCSBEntryInfo.ResolutionCombined) > 0 {
			out.Resolution = pdbHit.RCSBEntryInfo.ResolutionCombined[0]
		}
		chains := pdbHit.PolymerEntityIdentifiers.AuthAsymIDs
		if len(chains) > 0 {
			out.Chains = chains
			// Surface the chain list; do NOT pick one silently.
		} else {
			out.ChainInferenceNote = "PDB entry has no chain metadata; specify a chain explicitly"
		}
		return t.respond(out, input)
	}

	// PDB miss. UniProt fallback only for gene-name-shaped queries.
	if !looksLikeGeneName(q) || looksLikePDBID(q) {
		return tools.Result{}, fmt.Errorf("lab.targets_search: PDB has no entry for %q "+
			"and the query does not look like a gene name — supply a UniProt accession "+
			"or correct the PDB id", q)
	}
	if t.uniprot == nil {
		return tools.Result{}, fmt.Errorf("lab.targets_search: PDB miss for %q and "+
			"UniProt client is not configured", q)
	}
	rec, err := t.uniprot.Search(ctx, q)
	if err != nil {
		return tools.Result{}, fmt.Errorf("lab.targets_search: PDB miss for %q; UniProt: %w", q, err)
	}
	if rec == nil {
		return tools.Result{}, fmt.Errorf("lab.targets_search: no PDB entry and no UniProt match for %q", q)
	}
	out := targetResolveOutput{
		Source:   "uniprot",
		Query:    q,
		Chain:    nil,
		UniProt:  rec,
		Sequence: rec.Sequence,
	}
	if len(rec.PDBCrossRefs) > 0 {
		// The user can pick a PDB cross-ref themselves; do not infer a chain.
		out.ChainInferenceNote = fmt.Sprintf("UniProt cross-refs %d PDB entries; specify one explicitly", len(rec.PDBCrossRefs))
	}
	return t.respond(out, input)
}

// respond marshals out, caches it, and packages a tools.Result.
func (t *targetsSearchTool) respond(out targetResolveOutput, input json.RawMessage) (tools.Result, error) {
	body, err := json.Marshal(out)
	if err != nil {
		return tools.Result{}, err
	}
	if t.cache != nil {
		// Store the canonical version (without cached:true) so a follow-up
		// read sees a fresh response with cached:true set on the way out.
		_ = t.cache.Put(strings.ToUpper(out.Query), body)
	}
	display := fmt.Sprintf("targets_search: %s (source=%s)", out.Query, out.Source)
	return tools.Result{
		Output:     body,
		Display:    display,
		Provenance: domain.NewToolCallRef(t.Name(), input),
	}, nil
}

// pdbNotFoundErr wraps the 404 case so callers can branch on it.
type pdbNotFoundErr struct{ id string }

func (e *pdbNotFoundErr) Error() string { return fmt.Sprintf("pdb entry %q not found", e.id) }

func isPDBNotFound(err error) bool {
	_, ok := err.(*pdbNotFoundErr)
	return ok
}

// fetchPDB queries RCSB for one entry. Returns (nil, pdbNotFoundErr) on 404.
func (t *targetsSearchTool) fetchPDB(ctx context.Context, id string) (*pdbCoreEntry, error) {
	endpoint := t.pdbBaseURL
	if endpoint == "" {
		endpoint = pdbBaseURL
	}
	u := endpoint + "/" + url.PathEscape(id)
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := t.tc.Do(ctx, req, "pdb")
	if err != nil {
		return nil, fmt.Errorf("pdb: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusNotFound {
		return nil, &pdbNotFoundErr{id: id}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("pdb %s returned %d: %s", id, resp.StatusCode,
			strings.TrimSpace(string(body)))
	}
	var entry pdbCoreEntry
	if err := json.Unmarshal(body, &entry); err != nil {
		return nil, fmt.Errorf("pdb decode: %w", err)
	}
	return &entry, nil
}

// --- lab.cost_estimate ---

// costEstimateTool prices an assay before submission.
type costEstimateTool struct{ c *Client }

// NewCostEstimateTool returns the lab.cost_estimate tool.
func NewCostEstimateTool(c *Client) *costEstimateTool { return &costEstimateTool{c: c} }

func (*costEstimateTool) Name() string { return "lab.cost_estimate" }
func (*costEstimateTool) Description() string {
	return "Estimate the cost and turnaround of an Adaptyv assay before submitting it."
}
func (*costEstimateTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"target_id":  map[string]any{"type": "string", "description": "Adaptyv target ID"},
			"assay_type": map[string]any{"type": "string", "description": "Assay type to run"},
			"sequences": map[string]any{
				"type":        "array",
				"description": "Design sequences to assay",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"name":     map[string]any{"type": "string"},
						"sequence": map[string]any{"type": "string"},
					},
					"required": []string{"name", "sequence"},
				},
			},
		},
		"required": []string{"target_id", "assay_type", "sequences"},
	}
}
func (*costEstimateTool) RequiresConfirmation(json.RawMessage) bool       { return false }
func (*costEstimateTool) EstimatedCostUSD(json.RawMessage) float64        { return 0 }
func (*costEstimateTool) EstimatedDuration(json.RawMessage) time.Duration { return 5 * time.Second }

// Execute asks Adaptyv to price the requested assay.
func (t *costEstimateTool) Execute(ctx context.Context, input json.RawMessage) (tools.Result, error) {
	var req CostRequest
	if err := json.Unmarshal(input, &req); err != nil {
		return tools.Result{}, fmt.Errorf("invalid lab.cost_estimate request: %w", err)
	}
	est, err := t.c.EstimateCost(ctx, req)
	if err != nil {
		return tools.Result{}, err
	}
	out, _ := json.Marshal(est)
	return tools.Result{
		Output: out,
		Display: fmt.Sprintf("estimated cost $%.2f, turnaround ~%d days",
			est.TotalUSD, est.TurnaroundDays),
		Cost:       est.TotalUSD,
		Provenance: domain.NewToolCallRef(t.Name(), input),
	}, nil
}

// --- lab.experiment_status ---

// experimentStatusTool fetches one Adaptyv experiment's current state.
type experimentStatusTool struct{ c *Client }

// NewExperimentStatusTool returns the lab.experiment_status tool.
func NewExperimentStatusTool(c *Client) *experimentStatusTool {
	return &experimentStatusTool{c: c}
}

func (*experimentStatusTool) Name() string { return "lab.experiment_status" }
func (*experimentStatusTool) Description() string {
	return "Fetch the current status of an Adaptyv experiment by its experiment ID."
}
func (*experimentStatusTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"experiment_id": map[string]any{
				"type":        "string",
				"description": "Adaptyv experiment ID",
			},
		},
		"required": []string{"experiment_id"},
	}
}
func (*experimentStatusTool) RequiresConfirmation(json.RawMessage) bool { return false }
func (*experimentStatusTool) EstimatedCostUSD(json.RawMessage) float64  { return 0 }
func (*experimentStatusTool) EstimatedDuration(json.RawMessage) time.Duration {
	return 5 * time.Second
}

// Execute retrieves the experiment record from Adaptyv.
func (t *experimentStatusTool) Execute(ctx context.Context, input json.RawMessage) (tools.Result, error) {
	var in struct {
		ExperimentID string `json:"experiment_id"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return tools.Result{}, fmt.Errorf("invalid lab.experiment_status request: %w", err)
	}
	if in.ExperimentID == "" {
		return tools.Result{}, fmt.Errorf("experiment_id is required")
	}
	exp, err := t.c.GetExperiment(ctx, in.ExperimentID)
	if err != nil {
		return tools.Result{}, err
	}
	out, _ := json.Marshal(exp)
	return tools.Result{
		Output:     out,
		Display:    fmt.Sprintf("experiment %s is %q", exp.ID, exp.Status),
		Provenance: domain.NewToolCallRef(t.Name(), input),
	}, nil
}

// --- lab.results ---

// resultsTool fetches the measured kinetics of an Adaptyv experiment.
type resultsTool struct{ c *Client }

// NewResultsTool returns the lab.results tool.
func NewResultsTool(c *Client) *resultsTool { return &resultsTool{c: c} }

func (*resultsTool) Name() string { return "lab.results" }
func (*resultsTool) Description() string {
	return "Fetch the measured wet-lab results (kinetics) for a completed Adaptyv experiment."
}
func (*resultsTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"experiment_id": map[string]any{
				"type":        "string",
				"description": "Adaptyv experiment ID",
			},
		},
		"required": []string{"experiment_id"},
	}
}
func (*resultsTool) RequiresConfirmation(json.RawMessage) bool       { return false }
func (*resultsTool) EstimatedCostUSD(json.RawMessage) float64        { return 0 }
func (*resultsTool) EstimatedDuration(json.RawMessage) time.Duration { return 5 * time.Second }

// Execute retrieves the measured results from Adaptyv.
func (t *resultsTool) Execute(ctx context.Context, input json.RawMessage) (tools.Result, error) {
	var in struct {
		ExperimentID string `json:"experiment_id"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return tools.Result{}, fmt.Errorf("invalid lab.results request: %w", err)
	}
	if in.ExperimentID == "" {
		return tools.Result{}, fmt.Errorf("experiment_id is required")
	}
	results, err := t.c.GetResults(ctx, in.ExperimentID)
	if err != nil {
		return tools.Result{}, err
	}
	out, _ := json.Marshal(map[string]any{"results": results})
	return tools.Result{
		Output:     out,
		Display:    fmt.Sprintf("experiment %s has %d result(s)", in.ExperimentID, len(results)),
		Provenance: domain.NewToolCallRef(t.Name(), input),
	}, nil
}

// --- lab.submit_experiment ---

// submitExperimentTool submits sequences to Adaptyv and records the experiment.
type submitExperimentTool struct {
	c                 *Client
	st                *store.Store
	defaultWebhookURL string
}

// NewSubmitExperimentTool returns the lab.submit_experiment tool. It persists a
// domain.Experiment to st on every successful submission. defaultWebhookURL is
// the Adaptyv callback URL used when a submission omits its own webhook_url.
func NewSubmitExperimentTool(c *Client, st *store.Store, defaultWebhookURL string) *submitExperimentTool {
	return &submitExperimentTool{c: c, st: st, defaultWebhookURL: defaultWebhookURL}
}

func (*submitExperimentTool) Name() string { return "lab.submit_experiment" }
func (*submitExperimentTool) Description() string {
	return "Submit design sequences to Adaptyv Foundry for a wet-lab assay against a target."
}
func (*submitExperimentTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"target_id":  map[string]any{"type": "string", "description": "Adaptyv target ID"},
			"assay_type": map[string]any{"type": "string", "description": "Assay type to run"},
			"sequences": map[string]any{
				"type":        "array",
				"description": "Design sequences to submit",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"name":     map[string]any{"type": "string"},
						"sequence": map[string]any{"type": "string"},
					},
					"required": []string{"name", "sequence"},
				},
			},
			"webhook_url": map[string]any{
				"type":        "string",
				"description": "Optional URL Adaptyv calls when results are ready",
			},
		},
		"required": []string{"target_id", "assay_type", "sequences"},
	}
}

// Submitting a wet-lab experiment spends real money — always confirm.
func (*submitExperimentTool) RequiresConfirmation(json.RawMessage) bool { return true }
func (*submitExperimentTool) EstimatedCostUSD(json.RawMessage) float64  { return 0 }
func (*submitExperimentTool) EstimatedDuration(json.RawMessage) time.Duration {
	return 10 * time.Second
}

// Execute submits the assay to Adaptyv and persists a domain.Experiment record
// carrying the returned Adaptyv id in ExternalID.
func (t *submitExperimentTool) Execute(ctx context.Context, input json.RawMessage) (tools.Result, error) {
	var req SubmitRequest
	if err := json.Unmarshal(input, &req); err != nil {
		return tools.Result{}, fmt.Errorf("invalid lab.submit_experiment request: %w", err)
	}
	if req.WebhookURL == "" {
		req.WebhookURL = t.defaultWebhookURL
	}
	exp, err := t.c.SubmitExperiment(ctx, req)
	if err != nil {
		return tools.Result{}, err
	}

	record := domain.Experiment{
		ID:          domain.ExperimentID(uuid.NewString()),
		ProjectID:   store.DefaultProjectID,
		Backend:     "adaptyv",
		ExternalID:  exp.ID,
		AssayType:   firstNonEmpty(exp.AssayType, req.AssayType),
		TargetID:    firstNonEmpty(exp.TargetID, req.TargetID),
		TargetName:  exp.TargetName,
		SubmittedAt: time.Now().UTC(),
		Status:      firstNonEmpty(exp.Status, "submitted"),
		CostUSD:     exp.CostUSD,
	}
	if t.st != nil {
		if err := t.st.InsertExperiment(record); err != nil {
			return tools.Result{}, fmt.Errorf("persist experiment: %w", err)
		}
	}

	out, _ := json.Marshal(map[string]any{
		"experiment_id": string(record.ID),
		"external_id":   exp.ID,
		"status":        record.Status,
	})
	return tools.Result{
		Output: out,
		Display: fmt.Sprintf("submitted experiment %s to Adaptyv (external id %s)",
			record.ID, exp.ID),
		Cost:       exp.CostUSD,
		Provenance: domain.NewToolCallRef(t.Name(), input),
	}, nil
}

// firstNonEmpty returns the first non-empty string of its arguments.
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
