// Package plan provides the agent's design-planning tools.
package plan

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/alvarogonjim/fova/internal/backends/local"
	"github.com/alvarogonjim/fova/internal/domain"
	"github.com/alvarogonjim/fova/internal/store"
	"github.com/alvarogonjim/fova/internal/tools"
)

// TODO(spec): SPECS §7 has no plan.* tool table; see §8.2 plan-from-target.md and §20 v0.3 AC1.

// InstallChecker reports whether a named tool's local install artefact (image
// for container-mode tools, lock file for legacy ones) is present. It is the
// minimal surface plan.create needs from *local.Installer — using an
// interface lets tests inject a fake instead of dragging in the real
// container runtime.
type InstallChecker interface {
	Status(name string) local.ToolStatus
}

// ToolRegistry reports whether an agent tool is registered, and lets
// plan.create reach a registered tool to invoke it. plan.create uses it to
// reject a method whose design.* tool has no executable implementation — a
// method blessed in compat.go and installed locally but never wired into the
// registry (the design.boltzgen gap, 2026-05-21) — and to run
// design.boltzgen_check on a BoltzGen plan's spec. *tools.Registry satisfies
// this; tests can inject a fake.
type ToolRegistry interface {
	Get(name string) (tools.Tool, bool)
}

// boltzGenCheckToolName is the registered name of the spec-validation tool.
// plan.create and /plan approve invoke it by name through the registry so
// this package stays decoupled from the check tool's own package — only the
// JSON contract below is shared.
const boltzGenCheckToolName = "design.boltzgen_check"

// BoltzGenCheckResult is the pinned JSON contract returned in the
// design.boltzgen_check tool's Result.Output. plan.create rejects a BoltzGen
// plan whose spec is not Valid; /plan renders the result for review.
type BoltzGenCheckResult struct {
	Valid             bool     `json:"valid"`
	Errors            []string `json:"errors,omitempty"`
	VisualizationPath string   `json:"visualization_path,omitempty"`
}

// CreateTool implements plan.create: build a DesignPlan from a target and
// persist it for the user to review and approve.
type CreateTool struct {
	store     *store.Store
	installer InstallChecker
	registry  ToolRegistry
}

// NewPlanCreateTool builds the plan.create tool.
//
// installer is consulted to reject plans whose method tool isn't installed
// (Bug 11). Passing nil disables that check — only the in-memory schema
// validation runs — which is useful for tests that don't care about the
// install path, but production wiring always supplies a real installer.
//
// The returned *CreateTool satisfies tools.Tool. Production wiring should
// also call SetRegistry so plan.create can reject methods with no registered
// design.* tool.
func NewPlanCreateTool(st *store.Store, installer InstallChecker) *CreateTool {
	return &CreateTool{store: st, installer: installer}
}

// SetRegistry wires the tools registry so plan.create can verify a method's
// design.* tool is actually registered. A nil registry (the default) skips
// that check.
func (t *CreateTool) SetRegistry(r ToolRegistry) { t.registry = r }

func (*CreateTool) Name() string { return "plan.create" }
func (*CreateTool) Description() string {
	return "Creates and persists a DesignPlan from a target for the user to review and approve."
}

func (*CreateTool) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"target": map[string]any{
				"type":        "object",
				"description": "The design target structure.",
				"properties": map[string]any{
					"pdb_id":    map[string]any{"type": "string"},
					"file_path": map[string]any{"type": "string"},
					"chain":     map[string]any{"type": "string"},
				},
			},
			"application": map[string]any{
				"type":        "string",
				"enum":        []any{"binder", "antibody", "enzyme", "redesign"},
				"description": "The protein-design application area.",
			},
			"method": map[string]any{
				"type": "string",
				"description": "The primary design method/tool to run. Accepted: " +
					"BindCraft, BoltzGen, RFdiffusion, RFdiffusion2, ProteinMPNN, " +
					"LigandMPNN, RFantibody (the design.* registered " +
					"name and lowercase tool name are also accepted). The " +
					"method must be compatible with the chosen application " +
					"and the underlying tool must be installed locally " +
					"(plan.create rejects plans that fail either check).",
			},
			"fallback_method": map[string]any{
				"type":        "string",
				"description": "An optional fallback design method (same naming rules as method).",
			},
			"filters": map[string]any{
				"type": "object",
				"description": "FilterConfig thresholds for shortlisting. Each field " +
					"is bounded by its physically meaningful range: " +
					"min_ipsae, min_iptm, min_pdockq in [0, 1]; " +
					"min_plddt, min_plddt_min in [0, 100]; " +
					"max_pae_interface in [0, 32]; " +
					"max_rmsd_to_model, max_motif_rmsd, max_esm_perplexity > 0. " +
					"Optional: max_kd (> 0, with max_kd_units one of " +
					"M, mM, uM, nM, pM, fM).",
			},
			"shortlist_size": map[string]any{
				"type": "integer",
				"description": "Number of designs to keep on the shortlist. " +
					"Must be in [1, 500] (the soft cap protects against " +
					"unbounded compute spend); 0 is accepted as 'use the " +
					"default'.",
			},
			"compute_backend": map[string]any{
				"type":        "string",
				"description": "The compute backend the plan should run on.",
			},
			"method_spec_path": map[string]any{
				"type": "string",
				"description": "Workspace-relative path to the design specification " +
					"YAML. REQUIRED when method resolves to BoltzGen — author the " +
					"spec first (see the boltzgen-spec skill) and validate it with " +
					"design.boltzgen_check. plan.create re-runs the check and " +
					"rejects the plan if the spec is invalid. Ignored for other " +
					"methods.",
			},
			"method_params": map[string]any{
				"type": "object",
				"description": "Method-specific run configuration. For BoltzGen " +
					"these are the BoltzGenParams run flags folded into the plan " +
					"for /plan review and /plan approve. For LigandMPNN these are " +
					"the LigandMPNNParams run flags (at minimum a pdb backbone " +
					"path); method_params is REQUIRED for a LigandMPNN method. " +
					"Ignored for other methods.",
				"properties": map[string]any{
					"protocol": map[string]any{
						"type": "string",
						"enum": []any{
							"protein-anything", "peptide-anything",
							"protein-small_molecule", "antibody-anything",
							"nanobody-anything", "protein-redesign",
						},
						"description": "BoltzGen protocol; default protein-anything.",
					},
					"num_designs":          map[string]any{"type": "integer"},
					"budget":               map[string]any{"type": "integer"},
					"diffusion_batch_size": map[string]any{"type": "integer"},
					"steps": map[string]any{
						"type": "array",
						"items": map[string]any{
							"type": "string",
							"enum": []any{
								"design", "inverse_folding", "design_folding",
								"folding", "affinity", "analysis", "filtering",
							},
						},
					},
					"alpha":                      map[string]any{"type": "number"},
					"filter_biased":              map[string]any{"type": "boolean"},
					"additional_filters":         map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
					"refolding_rmsd_threshold":   map[string]any{"type": "number"},
					"inverse_fold_num_sequences": map[string]any{"type": "integer"},
					"inverse_fold_avoid":         map[string]any{"type": "string"},
					"step_scale":                 map[string]any{"type": "number"},
					"noise_scale":                map[string]any{"type": "number"},
					"reuse":                      map[string]any{"type": "boolean"},
				},
			},
			"estimated_cost_usd": map[string]any{
				"type":        "number",
				"description": "Estimated total cost in USD.",
			},
			"estimated_time": map[string]any{
				"type":        "string",
				"description": "Human-readable estimated wall-clock time.",
			},
			"rationale": map[string]any{
				"type":        "string",
				"description": "Why this plan was chosen.",
			},
			"evidence": map[string]any{
				"type": "array",
				"description": "Supporting literature references. Every entry must " +
					"reference a paper that already exists in the active project's " +
					"corpus via corpus_paper_id (the id returned by knowledge.corpus " +
					"add/search). plan.create formats the citation itself from the " +
					"stored paper metadata; any caller-supplied citation field is " +
					"ignored.",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"corpus_paper_id": map[string]any{
							"type":        "string",
							"description": "REQUIRED. Paper id in the active project's corpus.",
						},
						"excerpt": map[string]any{
							"type":        "string",
							"description": "Optional quote or note about why this paper is cited.",
						},
					},
					"required": []any{"corpus_paper_id"},
				},
			},
		},
		"required": []any{"target", "application", "method"},
	}
}

func (*CreateTool) RequiresConfirmation(json.RawMessage) bool       { return false }
func (*CreateTool) EstimatedCostUSD(json.RawMessage) float64        { return 0 }
func (*CreateTool) EstimatedDuration(json.RawMessage) time.Duration { return 100 * time.Millisecond }

func (t *CreateTool) Execute(ctx context.Context, input json.RawMessage) (tools.Result, error) {
	var p domain.DesignPlan
	if err := json.Unmarshal(input, &p); err != nil {
		return tools.Result{}, fmt.Errorf("plan.create: invalid input: %w", err)
	}

	switch p.Application {
	case domain.AppBinder, domain.AppAntibody, domain.AppEnzyme, domain.AppRedesign:
		// valid
	default:
		return tools.Result{}, fmt.Errorf(
			"plan.create: application %q must be one of binder, antibody, enzyme, redesign",
			p.Application)
	}
	if p.Method == "" {
		return tools.Result{}, fmt.Errorf("plan.create: method is required")
	}

	// Bug 11: cross-check method against the compat table, the installed-tool
	// set, and the filter range bounds before persisting the plan.
	method, ok := parseMethod(p.Method)
	if !ok {
		return tools.Result{}, fmt.Errorf(
			"plan.create: method %q is not a known design method — accepted "+
				"names: BindCraft, BoltzGen, RFdiffusion, RFdiffusion2, ProteinMPNN, "+
				"LigandMPNN, RFantibody (lower-case and design.* "+
				"forms also accepted)", p.Method)
	}
	if !methodAllowed(p.Application, method) {
		return tools.Result{}, fmt.Errorf(
			"plan.create: method %q is not compatible with application %q "+
				"— compatible methods for %q: %s. Pick one of those or "+
				"change the application.",
			method, p.Application, p.Application,
			strings.Join(compatibleMethods(p.Application), ", "))
	}
	if err := t.checkInstalled(method); err != nil {
		return tools.Result{}, err
	}
	if err := t.checkRegistered(method); err != nil {
		return tools.Result{}, err
	}
	if err := validateFilters(input, p.Filters); err != nil {
		return tools.Result{}, err
	}
	if err := validateShortlist(p.ShortlistSize); err != nil {
		return tools.Result{}, err
	}

	// BoltzGen folds a design specification YAML + run params into the plan.
	// For a BoltzGen method, method_spec_path is required; the spec is then
	// validated via design.boltzgen_check (consistent with the install +
	// registration guards above — an invalid spec rejects the plan).
	if method == MethodBoltzGen {
		if err := t.applyBoltzGenMethodConfig(ctx, input, &p); err != nil {
			return tools.Result{}, err
		}
	}

	// LigandMPNN folds its run configuration (the LigandMPNNParams) into the
	// plan so /plan can render it and /plan approve can run it. Unlike
	// BoltzGen there is no spec file or external check — method_params alone
	// carries the configuration.
	if method == MethodLigandMPNN {
		if err := t.applyLigandMPNNMethodConfig(input, &p); err != nil {
			return tools.Result{}, err
		}
	}

	// RFantibody folds its run configuration (the RFantibodyParams) into the
	// plan so /plan can render it and /plan approve can run it. Like LigandMPNN
	// there is no spec file or external check — method_params alone carries the
	// 3-stage pipeline configuration.
	if method == MethodRFantibody {
		if err := t.applyRFantibodyMethodConfig(input, &p); err != nil {
			return tools.Result{}, err
		}
	}

	// Ground every evidence entry in the corpus. The Citation field is
	// computed here; any value the caller supplied for it is discarded.
	if err := t.resolveEvidence(&p); err != nil {
		return tools.Result{}, err
	}

	// Server-controlled fields.
	p.ID = domain.PlanID("p_" + uuid.NewString())
	p.ProjectID = store.DefaultProjectID
	p.Created = time.Now().UTC()
	p.Approved = false
	p.ApprovedAt = nil

	if err := t.store.InsertPlan(p); err != nil {
		return tools.Result{}, fmt.Errorf("plan.create: persist plan: %w", err)
	}

	out, err := json.Marshal(p)
	if err != nil {
		return tools.Result{}, fmt.Errorf("plan.create: marshal plan: %w", err)
	}
	return tools.Result{
		Output:     out,
		Display:    fmt.Sprintf("created plan %s — review it with /plan", p.ID),
		Provenance: domain.NewToolCallRef("plan.create", input),
	}, nil
}

// resolveEvidence validates each evidence entry against the active project's
// corpus and overwrites Citation with a string formatted from the stored
// paper metadata. Caller-supplied Citation values are intentionally discarded
// — only the corpus is allowed to ground a citation.
func (t *CreateTool) resolveEvidence(p *domain.DesignPlan) error {
	for i := range p.Evidence {
		ev := &p.Evidence[i]
		id := strings.TrimSpace(ev.CorpusPaperID)
		if id == "" {
			return fmt.Errorf(
				"plan.create: evidence[%d].corpus_paper_id is required — "+
					"every evidence entry must reference a paper in the active "+
					"project's corpus (use knowledge.corpus.search or "+
					"knowledge.corpus.list to find ids; free-text citations are "+
					"not accepted)", i)
		}
		paper, err := t.store.GetCorpusPaper(id)
		if err != nil {
			return fmt.Errorf(
				"plan.create: evidence[%d].corpus_paper_id %q is not in the "+
					"active project's corpus — add it first with "+
					"knowledge.corpus.add or pick an id returned by "+
					"knowledge.corpus.search/list (underlying error: %w)",
				i, id, err)
		}
		ev.CorpusPaperID = paper.ID
		// Caller-supplied citation is discarded; we format our own.
		ev.Citation = formatCitation(paper)
	}
	return nil
}

// formatCitation renders a single human-readable citation string from a
// corpus paper's stored metadata. The order is:
//
//	"<author> et al. <year>. <Title>. <DOI>"
//
// Each component is included only if present in the stored record. The DOI
// is sourced from the paper's metadata JSON when available; otherwise — when
// the paper's id is itself a DOI (the europepmc/openalex convention) — the id
// is used.
func formatCitation(p domain.CorpusPaper) string {
	var parts []string

	authorYear := strings.TrimSpace(formatAuthorYear(p.Authors, p.Year))
	if authorYear != "" {
		parts = append(parts, authorYear)
	}

	if title := strings.TrimSpace(p.Title); title != "" {
		parts = append(parts, title)
	}

	if doi := extractDOI(p); doi != "" {
		parts = append(parts, doi)
	}

	return strings.Join(parts, ". ")
}

// formatAuthorYear renders the leading "Lastname et al. YYYY" segment. Authors
// is the comma-separated string we get from europepmc-style sources.
func formatAuthorYear(authors string, year int) string {
	first := firstAuthorSurname(authors)
	switch {
	case first != "" && year > 0:
		// "Pacesa et al. 2024"
		// We always use the "et al." form so the citation is short and
		// stable even if downstream code only got the first author.
		return first + " et al. " + strconv.Itoa(year)
	case first != "":
		return first + " et al."
	case year > 0:
		return strconv.Itoa(year)
	}
	return ""
}

// firstAuthorSurname pulls the surname of the first author out of a
// comma-separated authors string. Common shapes from corpus sources:
//
//	"Pacesa, Nickel, Yang"    -> "Pacesa"
//	"Pacesa M, Nickel L, ..." -> "Pacesa"
//	"Pacesa M"                -> "Pacesa"
//	"Pacesa"                  -> "Pacesa"
func firstAuthorSurname(authors string) string {
	authors = strings.TrimSpace(authors)
	if authors == "" {
		return ""
	}
	first := authors
	if idx := strings.IndexAny(authors, ",;"); idx >= 0 {
		first = authors[:idx]
	}
	first = strings.TrimSpace(first)
	if first == "" {
		return ""
	}
	// Drop trailing initials like "Pacesa M" -> "Pacesa".
	if idx := strings.IndexByte(first, ' '); idx >= 0 {
		first = first[:idx]
	}
	return strings.TrimSpace(first)
}

// extractDOI returns the paper's DOI. CorpusPaper.ID is itself a DOI when the
// paper came from a DOI-keyed source (europepmc, openalex); otherwise we try
// the metadata JSON which sources like the test seed and knowledge.crossref
// store there.
func extractDOI(p domain.CorpusPaper) string {
	if looksLikeDOI(p.ID) {
		return p.ID
	}
	if p.Metadata == "" {
		return ""
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(p.Metadata), &raw); err != nil {
		return ""
	}
	for _, key := range []string{"doi", "DOI"} {
		if v, ok := raw[key]; ok {
			if s, ok := v.(string); ok && s != "" {
				return s
			}
		}
	}
	return ""
}

// looksLikeDOI is a cheap heuristic: real DOIs start with "10." followed by a
// registrant code and a slash.
func looksLikeDOI(s string) bool {
	if !strings.HasPrefix(s, "10.") {
		return false
	}
	return strings.Contains(s, "/")
}

// checkInstalled returns an error if the local tool implementing method m is
// not installed. When the installer is nil (legacy test paths) the check is
// skipped — production wiring always supplies one.
func (t *CreateTool) checkInstalled(m Method) error {
	if t.installer == nil {
		return nil
	}
	tool := toolForMethod(m)
	if tool == "" {
		// Defensive: parseMethod accepted m but no tools.toml mapping
		// exists. Surface that as a configuration error rather than a
		// silent skip.
		return fmt.Errorf("plan.create: method %q has no local tool mapping — extend toolForMethod in compat.go", m)
	}
	if t.installer.Status(tool).Installed {
		return nil
	}
	return fmt.Errorf(
		"plan.create: method %q requires tool %q which is not installed "+
			"— run /install %s or pick a different method (see /doctor "+
			"for the full tool status)",
		m, tool, tool)
}

// checkRegistered returns an error if the agent-facing design.* tool for
// method m is not in the tools registry. This catches a method that is
// blessed in compat.go and installed locally but never wired up as an
// executable tool — the design.boltzgen gap found on 2026-05-21, where an
// approved BoltzGen plan could not run because no design.boltzgen tool
// existed. A nil registry (legacy/test paths) skips the check.
func (t *CreateTool) checkRegistered(m Method) error {
	if t.registry == nil {
		return nil
	}
	name := designToolForMethod(m)
	if name == "" {
		return fmt.Errorf(
			"plan.create: method %q has no design.* tool mapping — extend "+
				"designToolForMethod in compat.go", m)
	}
	if _, ok := t.registry.Get(name); !ok {
		return fmt.Errorf(
			"plan.create: method %q resolves to tool %q, which is not registered "+
				"— the method is listed in compat.go but no executable tool is "+
				"wired into the registry. Pick a different method, or register %s.",
			m, name, name)
	}
	return nil
}

// applyBoltzGenMethodConfig parses the optional method_spec_path +
// method_params inputs into DesignPlan.MethodConfig and gates the plan on a
// design.boltzgen_check of the spec. method_spec_path is required for a
// BoltzGen plan; the spec must pass the check or the plan is rejected.
func (t *CreateTool) applyBoltzGenMethodConfig(ctx context.Context, input json.RawMessage, p *domain.DesignPlan) error {
	var envelope struct {
		SpecPath string                 `json:"method_spec_path"`
		Params   *domain.BoltzGenParams `json:"method_params"`
	}
	if err := json.Unmarshal(input, &envelope); err != nil {
		return fmt.Errorf("plan.create: invalid method_params: %w", err)
	}
	specPath := strings.TrimSpace(envelope.SpecPath)
	if specPath == "" {
		return fmt.Errorf(
			"plan.create: method BoltzGen requires method_spec_path — author " +
				"the design specification YAML first (see the boltzgen-spec " +
				"skill), validate it with design.boltzgen_check, then pass its " +
				"workspace-relative path as method_spec_path")
	}

	mc := &domain.MethodConfig{SpecPath: specPath, BoltzGen: envelope.Params}

	// Check gate: validate the spec via the design.boltzgen_check tool. A nil
	// registry or an unregistered check tool skips the gate (the nil-registry
	// path used by the install + registration guards above).
	res, ran, err := t.runBoltzGenCheck(ctx, specPath)
	if err != nil {
		return err
	}
	if ran && !res.Valid {
		errs := strings.Join(res.Errors, "; ")
		if errs == "" {
			errs = "(no detail reported)"
		}
		return fmt.Errorf(
			"plan.create: BoltzGen spec %q failed design.boltzgen_check — fix "+
				"the spec and retry. Errors: %s", specPath, errs)
	}

	p.MethodConfig = mc
	return nil
}

// applyLigandMPNNMethodConfig parses the method_params input into a
// LigandMPNNParams and folds it into DesignPlan.MethodConfig. method_params is
// required for a LigandMPNN plan — it carries the run configuration (at
// minimum a pdb backbone path). The params are value-shape validated via
// domain.LigandMPNNParams.Validate; there is no external check tool.
func (t *CreateTool) applyLigandMPNNMethodConfig(input json.RawMessage, p *domain.DesignPlan) error {
	var envelope struct {
		Params *domain.LigandMPNNParams `json:"method_params"`
	}
	if err := json.Unmarshal(input, &envelope); err != nil {
		return fmt.Errorf("plan.create: invalid method_params: %w", err)
	}
	if envelope.Params == nil {
		return fmt.Errorf(
			"plan.create: method LigandMPNN requires method_params — the " +
				"LigandMPNN run configuration (at minimum a pdb backbone path)")
	}
	if err := envelope.Params.Validate(); err != nil {
		return err
	}
	p.MethodConfig = &domain.MethodConfig{LigandMPNN: envelope.Params}
	return nil
}

// applyRFantibodyMethodConfig parses the method_params input into an
// RFantibodyParams and folds it into DesignPlan.MethodConfig. method_params is
// required for an RFantibody plan — it carries the run configuration (at
// minimum the target antigen PDB and the epitope hotspots). The params are
// value-shape validated via domain.RFantibodyParams.Validate; there is no
// external check tool.
func (t *CreateTool) applyRFantibodyMethodConfig(input json.RawMessage, p *domain.DesignPlan) error {
	var envelope struct {
		Params *domain.RFantibodyParams `json:"method_params"`
	}
	if err := json.Unmarshal(input, &envelope); err != nil {
		return fmt.Errorf("plan.create: invalid method_params: %w", err)
	}
	if envelope.Params == nil {
		return fmt.Errorf(
			"plan.create: method RFantibody requires method_params — the " +
				"RFantibody run configuration (at minimum target and hotspots)")
	}
	if err := envelope.Params.Validate(); err != nil {
		return err
	}
	p.MethodConfig = &domain.MethodConfig{RFantibody: envelope.Params}
	return nil
}

// runBoltzGenCheck invokes the design.boltzgen_check tool through the registry
// and decodes its pinned JSON result. ran is false (with a nil error) when the
// check could not run — a nil registry or no registered check tool — so the
// caller skips the gate, matching the install/registration guards' behaviour.
func (t *CreateTool) runBoltzGenCheck(ctx context.Context, specPath string) (res BoltzGenCheckResult, ran bool, err error) {
	if t.registry == nil {
		return BoltzGenCheckResult{}, false, nil
	}
	tool, ok := t.registry.Get(boltzGenCheckToolName)
	if !ok {
		return BoltzGenCheckResult{}, false, nil
	}
	in, merr := json.Marshal(map[string]string{"spec_path": specPath})
	if merr != nil {
		return BoltzGenCheckResult{}, false, fmt.Errorf("plan.create: marshal boltzgen check input: %w", merr)
	}
	out, eerr := tool.Execute(ctx, in)
	if eerr != nil {
		return BoltzGenCheckResult{}, false, fmt.Errorf(
			"plan.create: design.boltzgen_check failed for spec %q: %w", specPath, eerr)
	}
	if uerr := json.Unmarshal(out.Output, &res); uerr != nil {
		return BoltzGenCheckResult{}, false, fmt.Errorf(
			"plan.create: design.boltzgen_check returned an unparsable result for spec %q: %w",
			specPath, uerr)
	}
	return res, true, nil
}

// validateFilters bounds-checks every populated field in FilterConfig and
// the optional Kd budget. The Kd parse is intentionally pulled from the raw
// JSON because domain.FilterConfig has no Kd field — adding one is a v0.8
// concern. Until then, callers express the Kd cap as filters.max_kd with an
// optional filters.max_kd_units (default "M").
func validateFilters(input json.RawMessage, f domain.FilterConfig) error {
	// FilterConfig fields. Each row is "name, current value, lo, hi" where lo
	// and hi are inclusive bounds. A value of 0 means "unset" (the legacy
	// FilterConfig sentinel) and is skipped.
	type bound struct {
		name string
		v    float64
		lo   float64
		hi   float64
	}
	checks := []bound{
		{"min_ipsae", f.MinIPSAE, 0, 1},
		{"min_iptm", f.MinIPTM, 0, 1},
		{"min_pdockq", f.MinPDockQ, 0, 1},
		{"min_plddt", f.MinPLDDT, 0, 100},
		{"min_plddt_min", f.MinPLDDTMin, 0, 100},
		{"max_pae_interface", f.MaxPAEInterface, 0, 32},
		{"max_rmsd_to_model", f.MaxRMSDtoModel, 0, math.MaxFloat64},
		{"max_motif_rmsd", f.MaxMotifRMSD, 0, math.MaxFloat64},
		{"max_esm_perplexity", f.MaxESMPerplexity, 0, math.MaxFloat64},
	}
	for _, c := range checks {
		if c.v == 0 {
			continue // unset sentinel
		}
		if c.v < c.lo || c.v > c.hi {
			return fmt.Errorf(
				"plan.create: filters.%s = %g is out of range [%g, %g] "+
					"— check the value or omit the filter",
				c.name, c.v, c.lo, c.hi)
		}
	}
	// Kd: parsed from the raw filters JSON. We accept any number (with or
	// without units) and reject only Kd <= 0, which is physically
	// impossible. Kd unit normalisation happens via kdToMolar.
	kd, kdUnits, hasKd, err := extractKdFromFilters(input)
	if err != nil {
		return err
	}
	if hasKd {
		molar, err := kdToMolar(kd, kdUnits)
		if err != nil {
			return fmt.Errorf("plan.create: filters.max_kd_units %q is not a known unit — accepted: M, mM, uM, nM, pM, fM", kdUnits)
		}
		if molar <= 0 {
			return fmt.Errorf(
				"plan.create: filters.max_kd = %g %s must be > 0 — Kd is a "+
					"dissociation constant and is always positive",
				kd, kdUnits)
		}
	}
	return nil
}

// validateShortlist enforces the [1, 500] band on ShortlistSize. The legacy
// "0 == unset" sentinel passes through untouched so callers that don't set
// the field aren't punished.
func validateShortlist(n int) error {
	if n == 0 {
		return nil
	}
	if n < 0 {
		return fmt.Errorf(
			"plan.create: shortlist_size = %d must be > 0 — pick a positive "+
				"integer or omit the field to use the default",
			n)
	}
	if n > 500 {
		return fmt.Errorf(
			"plan.create: shortlist_size = %d exceeds compute budget — "+
				"confirm or trim to <= 500 (the soft cap protects against "+
				"unbounded compute spend; raise it intentionally by setting "+
				"a smaller number)",
			n)
	}
	return nil
}

// extractKdFromFilters reads filters.max_kd and filters.max_kd_units out of
// the raw plan.create input. Returns hasKd=false when the field is absent —
// the Kd bound is opt-in and FilterConfig has no place for it (see the
// v0.7 spec note on Bug 11).
func extractKdFromFilters(input json.RawMessage) (kd float64, units string, hasKd bool, err error) {
	var envelope struct {
		Filters json.RawMessage `json:"filters"`
	}
	if uerr := json.Unmarshal(input, &envelope); uerr != nil {
		// Already validated upstream; treat as "no filters block".
		return 0, "", false, nil
	}
	if len(envelope.Filters) == 0 {
		return 0, "", false, nil
	}
	var raw map[string]any
	if uerr := json.Unmarshal(envelope.Filters, &raw); uerr != nil {
		return 0, "", false, nil
	}
	v, ok := raw["max_kd"]
	if !ok {
		return 0, "", false, nil
	}
	f, ok := toFloat64(v)
	if !ok {
		return 0, "", false, fmt.Errorf("plan.create: filters.max_kd must be a number")
	}
	units = "M"
	if u, ok := raw["max_kd_units"]; ok {
		if s, ok := u.(string); ok {
			units = s
		}
	}
	return f, units, true, nil
}

// toFloat64 coerces a JSON-unmarshalled value (float64 or json.Number) to a
// float64. Returns ok=false for any other type.
func toFloat64(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case json.Number:
		f, err := x.Float64()
		return f, err == nil
	}
	return 0, false
}

// kdToMolar normalises a Kd value with an SI prefix to mol/L. The accepted
// units mirror the ones the lab side already uses for ExperimentResult.
func kdToMolar(kd float64, units string) (float64, error) {
	switch strings.ToLower(strings.TrimSpace(units)) {
	case "", "m":
		return kd, nil
	case "mm":
		return kd * 1e-3, nil
	case "um", "µm", "μm":
		return kd * 1e-6, nil
	case "nm":
		return kd * 1e-9, nil
	case "pm":
		return kd * 1e-12, nil
	case "fm":
		return kd * 1e-15, nil
	}
	return 0, fmt.Errorf("unknown unit %q", units)
}
