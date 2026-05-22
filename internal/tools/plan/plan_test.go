package plan

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alvarogonjim/fova/internal/backends/local"
	"github.com/alvarogonjim/fova/internal/domain"
	"github.com/alvarogonjim/fova/internal/store"
	"github.com/alvarogonjim/fova/internal/tools"
)

// fakeInstaller is a hand-rolled InstallChecker for plan.create tests. The
// real installer (internal/backends/local.Installer) is heavy — it pulls in
// the container runtime — so tests inject this small stub instead.
type fakeInstaller struct {
	// installed lists the tool names whose Status returns Installed=true.
	installed map[string]bool
}

func newFakeInstaller(names ...string) *fakeInstaller {
	f := &fakeInstaller{installed: make(map[string]bool, len(names))}
	for _, n := range names {
		f.installed[n] = true
	}
	return f
}

func (f *fakeInstaller) Status(name string) local.ToolStatus {
	return local.ToolStatus{Name: name, Installed: f.installed[name]}
}

// installAll is the catch-all InstallChecker used by tests that don't care
// about the install-check rejection path (i.e. legacy tests preserved across
// the Bug 11 changes).
type installAll struct{}

func (installAll) Status(name string) local.ToolStatus {
	return local.ToolStatus{Name: name, Installed: true}
}

// NewPlanCreateTool satisfies the tools.Tool interface.
var _ tools.Tool = NewPlanCreateTool(nil, installAll{})

func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "fova.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestPlanCreatePersistsPlan(t *testing.T) {
	st := newTestStore(t)
	tool := NewPlanCreateTool(st, installAll{})

	input := json.RawMessage(`{
		"target": {"pdb_id": "1ABC", "chain": "A"},
		"application": "binder",
		"method": "design.bindcraft"
	}`)

	res, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(res.Display, "/plan") {
		t.Errorf("Display %q should mention /plan", res.Display)
	}
	if !strings.Contains(res.Display, "p_") {
		t.Errorf("Display %q should contain a p_ plan id", res.Display)
	}

	got, ok, err := st.LatestPlan(store.DefaultProjectID)
	if err != nil {
		t.Fatalf("LatestPlan: %v", err)
	}
	if !ok {
		t.Fatal("LatestPlan: expected a persisted plan")
	}
	if got.Application != domain.AppBinder {
		t.Errorf("Application = %q, want %q", got.Application, domain.AppBinder)
	}
	if got.Method != "design.bindcraft" {
		t.Errorf("Method = %q, want design.bindcraft", got.Method)
	}
	if got.Approved {
		t.Error("new plan should not be approved")
	}
	if got.ApprovedAt != nil {
		t.Error("new plan should have nil ApprovedAt")
	}
	if got.ProjectID != store.DefaultProjectID {
		t.Errorf("ProjectID = %q, want %q", got.ProjectID, store.DefaultProjectID)
	}
	if !strings.HasPrefix(string(got.ID), "p_") {
		t.Errorf("ID = %q, want p_ prefix", got.ID)
	}
	if got.Created.IsZero() {
		t.Error("Created should be set")
	}
	if got.Target.PDBID != "1ABC" {
		t.Errorf("Target.PDBID = %q, want 1ABC", got.Target.PDBID)
	}
}

func TestPlanCreateInvalidApplication(t *testing.T) {
	tool := NewPlanCreateTool(newTestStore(t), installAll{})
	input := json.RawMessage(`{
		"target": {"pdb_id": "1ABC"},
		"application": "nonsense",
		"method": "design.bindcraft"
	}`)
	if _, err := tool.Execute(context.Background(), input); err == nil {
		t.Fatal("expected an error for an invalid application value")
	}
}

func TestPlanCreateMissingMethod(t *testing.T) {
	tool := NewPlanCreateTool(newTestStore(t), installAll{})
	input := json.RawMessage(`{
		"target": {"pdb_id": "1ABC"},
		"application": "binder"
	}`)
	if _, err := tool.Execute(context.Background(), input); err == nil {
		t.Fatal("expected an error for a missing method")
	}
}

// seedCorpusPaper inserts a corpus paper with the given fields and returns it.
// DOI is stored in the Metadata JSON blob alongside the paper.
func seedCorpusPaper(t *testing.T, st *store.Store, id string, authors []string, year int, title, doi string) {
	t.Helper()
	meta := map[string]any{"doi": doi}
	metaJSON, err := json.Marshal(meta)
	if err != nil {
		t.Fatalf("marshal metadata: %v", err)
	}
	p := domain.CorpusPaper{
		ID:        id,
		ProjectID: store.DefaultProjectID,
		Title:     title,
		Authors:   strings.Join(authors, ", "),
		Year:      year,
		Source:    "test",
		Metadata:  string(metaJSON),
		Added:     time.Now().UTC(),
	}
	if err := st.InsertCorpusPaper(p); err != nil {
		t.Fatalf("InsertCorpusPaper: %v", err)
	}
}

func TestPlanCreateRequiresCorpusPaperID(t *testing.T) {
	st := newTestStore(t)
	tool := NewPlanCreateTool(st, installAll{})
	input := json.RawMessage(`{
		"target": {"pdb_id": "1ABC"},
		"application": "binder",
		"method": "design.bindcraft",
		"evidence": [{"excerpt": "Pacesa et al. 2024 — high success rate"}]
	}`)
	_, err := tool.Execute(context.Background(), input)
	if err == nil {
		t.Fatal("expected an error when evidence entry has no corpus_paper_id")
	}
	msg := err.Error()
	if !strings.Contains(msg, "corpus_paper_id") {
		t.Errorf("error %q should name the offending field corpus_paper_id", msg)
	}
	if !strings.Contains(msg, "evidence") {
		t.Errorf("error %q should reference the evidence field", msg)
	}
}

func TestPlanCreateRejectsUnknownCorpusPaperID(t *testing.T) {
	st := newTestStore(t)
	seedCorpusPaper(t, st, "paper_aaa", []string{"Smith", "Doe"}, 2024, "Known paper", "10.1/known")
	tool := NewPlanCreateTool(st, installAll{})
	input := json.RawMessage(`{
		"target": {"pdb_id": "1ABC"},
		"application": "binder",
		"method": "design.bindcraft",
		"evidence": [{"corpus_paper_id": "paper_bbb"}]
	}`)
	_, err := tool.Execute(context.Background(), input)
	if err == nil {
		t.Fatal("expected an error when corpus_paper_id is not in the corpus")
	}
	msg := err.Error()
	if !strings.Contains(msg, "paper_bbb") {
		t.Errorf("error %q should name the offending id paper_bbb", msg)
	}
	if !strings.Contains(msg, "corpus") {
		t.Errorf("error %q should mention the corpus", msg)
	}
}

func TestPlanCreateRendersCitationFromCorpus(t *testing.T) {
	st := newTestStore(t)
	seedCorpusPaper(t, st,
		"paper_aaa",
		[]string{"Pacesa", "Nickel", "Yang"}, 2024,
		"BindCraft: AI-driven binder design",
		"10.1038/s41586-024-bindcraft",
	)
	tool := NewPlanCreateTool(st, installAll{})
	input := json.RawMessage(`{
		"target": {"pdb_id": "1ABC"},
		"application": "binder",
		"method": "design.bindcraft",
		"evidence": [{"corpus_paper_id": "paper_aaa", "excerpt": "ipSAE > 0.5"}]
	}`)
	res, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var got domain.DesignPlan
	if err := json.Unmarshal(res.Output, &got); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if len(got.Evidence) != 1 {
		t.Fatalf("Evidence len = %d, want 1", len(got.Evidence))
	}
	ev := got.Evidence[0]
	if ev.CorpusPaperID != "paper_aaa" {
		t.Errorf("CorpusPaperID = %q, want paper_aaa", ev.CorpusPaperID)
	}
	if ev.Excerpt != "ipSAE > 0.5" {
		t.Errorf("Excerpt = %q, want the user-supplied excerpt to round-trip", ev.Excerpt)
	}
	cite := ev.Citation
	for _, want := range []string{"Pacesa et al. 2024", "BindCraft", "10.1038/s41586-024-bindcraft"} {
		if !strings.Contains(cite, want) {
			t.Errorf("citation %q missing %q", cite, want)
		}
	}
}

func TestPlanCreateIgnoresCallerSuppliedCitation(t *testing.T) {
	st := newTestStore(t)
	seedCorpusPaper(t, st,
		"paper_aaa",
		[]string{"Pacesa", "Nickel"}, 2024,
		"BindCraft",
		"10.1038/x",
	)
	tool := NewPlanCreateTool(st, installAll{})
	input := json.RawMessage(`{
		"target": {"pdb_id": "1ABC"},
		"application": "binder",
		"method": "design.bindcraft",
		"evidence": [{
			"corpus_paper_id": "paper_aaa",
			"citation": "Dunbrack et al. 2024 — fabricated"
		}]
	}`)
	res, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var got domain.DesignPlan
	if err := json.Unmarshal(res.Output, &got); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if len(got.Evidence) != 1 {
		t.Fatalf("Evidence len = %d, want 1", len(got.Evidence))
	}
	cite := got.Evidence[0].Citation
	if strings.Contains(cite, "Dunbrack") {
		t.Errorf("caller-supplied citation must be ignored: %q", cite)
	}
	if !strings.Contains(cite, "Pacesa") {
		t.Errorf("citation should be derived from corpus authors: %q", cite)
	}
}

// --- Bug 11: stricter plan.create validation ---

// TestPlanCreateRejectsUninstalledMethodTool verifies plan.create refuses a
// plan whose method tool isn't installed. The error must name both the
// method and the missing tool so the user knows what to run.
func TestPlanCreateRejectsUninstalledMethodTool(t *testing.T) {
	st := newTestStore(t)
	// Installer reports no tool as installed.
	tool := NewPlanCreateTool(st, newFakeInstaller())
	input := json.RawMessage(`{
		"target": {"pdb_id": "1ABC"},
		"application": "binder",
		"method": "BindCraft"
	}`)
	_, err := tool.Execute(context.Background(), input)
	if err == nil {
		t.Fatal("expected an error when bindcraft is not installed")
	}
	msg := err.Error()
	for _, want := range []string{"bindcraft", "not installed", "/install"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q should contain %q", msg, want)
		}
	}
}

// TestPlanCreateAcceptsInstalledMethodTool is the inverse — once the
// installer reports the tool as installed, the same plan is accepted.
func TestPlanCreateAcceptsInstalledMethodTool(t *testing.T) {
	st := newTestStore(t)
	tool := NewPlanCreateTool(st, newFakeInstaller("bindcraft"))
	input := json.RawMessage(`{
		"target": {"pdb_id": "1ABC"},
		"application": "binder",
		"method": "BindCraft"
	}`)
	if _, err := tool.Execute(context.Background(), input); err != nil {
		t.Fatalf("Execute: unexpected error after install: %v", err)
	}
}

// TestPlanCreateRejectsOutOfRangeFilters exercises every filter-range bound.
// Each row submits a single out-of-range filter and asserts the error names
// the offending field and the valid range.
func TestPlanCreateRejectsOutOfRangeFilters(t *testing.T) {
	cases := []struct {
		name    string
		filters string
		field   string
	}{
		{"ipsae above one", `"min_ipsae": 1.5`, "min_ipsae"},
		{"ipsae negative", `"min_ipsae": -0.1`, "min_ipsae"},
		{"plddt above one hundred", `"min_plddt": 110`, "min_plddt"},
		{"plddt negative", `"min_plddt": -1`, "min_plddt"},
		{"plddt_min above one hundred", `"min_plddt_min": 101`, "min_plddt_min"},
		{"iptm above one", `"min_iptm": 1.2`, "min_iptm"},
		{"pdockq above one", `"min_pdockq": 1.1`, "min_pdockq"},
		{"pae_interface negative", `"max_pae_interface": -0.5`, "max_pae_interface"},
		{"pae_interface huge", `"max_pae_interface": 99`, "max_pae_interface"},
		{"rmsd negative", `"max_rmsd_to_model": -1`, "max_rmsd_to_model"},
		{"motif_rmsd negative", `"max_motif_rmsd": -2`, "max_motif_rmsd"},
		{"esm perplexity negative", `"max_esm_perplexity": -1`, "max_esm_perplexity"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st := newTestStore(t)
			tool := NewPlanCreateTool(st, newFakeInstaller("bindcraft"))
			input := json.RawMessage(`{
				"target": {"pdb_id": "1ABC"},
				"application": "binder",
				"method": "BindCraft",
				"filters": {` + tc.filters + `}
			}`)
			_, err := tool.Execute(context.Background(), input)
			if err == nil {
				t.Fatalf("expected a range error for %s", tc.name)
			}
			if !strings.Contains(err.Error(), tc.field) {
				t.Errorf("error %q should name the offending field %q", err.Error(), tc.field)
			}
		})
	}
}

// TestPlanCreateRejectsShortlistOutOfRange covers the [1, 500] band on
// ShortlistSize. >500 must include the budget message; <1 must mention the
// field name.
func TestPlanCreateRejectsShortlistOutOfRange(t *testing.T) {
	st := newTestStore(t)
	tool := NewPlanCreateTool(st, newFakeInstaller("bindcraft"))
	t.Run("too big", func(t *testing.T) {
		input := json.RawMessage(`{
			"target": {"pdb_id": "1ABC"},
			"application": "binder",
			"method": "BindCraft",
			"shortlist_size": 5000
		}`)
		_, err := tool.Execute(context.Background(), input)
		if err == nil {
			t.Fatal("expected a shortlist range error")
		}
		msg := err.Error()
		if !strings.Contains(msg, "shortlist_size") {
			t.Errorf("error %q should name shortlist_size", msg)
		}
		if !strings.Contains(msg, "compute budget") {
			t.Errorf("error %q should mention the compute budget", msg)
		}
	})
	t.Run("zero", func(t *testing.T) {
		input := json.RawMessage(`{
			"target": {"pdb_id": "1ABC"},
			"application": "binder",
			"method": "BindCraft",
			"shortlist_size": 0
		}`)
		// shortlist_size=0 is the legacy "unset" sentinel, so it's accepted
		// as a hint to use the default rather than a range violation.
		if _, err := tool.Execute(context.Background(), input); err != nil {
			t.Fatalf("shortlist_size=0 must be accepted as unset: %v", err)
		}
	})
	t.Run("negative", func(t *testing.T) {
		input := json.RawMessage(`{
			"target": {"pdb_id": "1ABC"},
			"application": "binder",
			"method": "BindCraft",
			"shortlist_size": -1
		}`)
		_, err := tool.Execute(context.Background(), input)
		if err == nil {
			t.Fatal("expected a shortlist range error for negative size")
		}
		if !strings.Contains(err.Error(), "shortlist_size") {
			t.Errorf("error %q should name shortlist_size", err.Error())
		}
	})
}

// TestPlanCreateRejectsNonPositiveKd verifies the Kd > 0 bound. Plans expose
// Kd via filters.max_kd (with optional max_kd_units). After unit normalisation
// the bound is checked in mol/L.
func TestPlanCreateRejectsNonPositiveKd(t *testing.T) {
	st := newTestStore(t)
	tool := NewPlanCreateTool(st, newFakeInstaller("bindcraft"))
	cases := []struct {
		name    string
		filters string
	}{
		{"zero", `"max_kd": 0, "max_kd_units": "nM"`},
		{"negative", `"max_kd": -5, "max_kd_units": "nM"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			input := json.RawMessage(`{
				"target": {"pdb_id": "1ABC"},
				"application": "binder",
				"method": "BindCraft",
				"filters": {` + tc.filters + `}
			}`)
			_, err := tool.Execute(context.Background(), input)
			if err == nil {
				t.Fatalf("expected a Kd range error for %s", tc.name)
			}
			if !strings.Contains(err.Error(), "max_kd") {
				t.Errorf("error %q should name max_kd", err.Error())
			}
		})
	}
}

// TestPlanCreateRejectsIncompatibleApplicationMethod covers the
// application↔method matrix. BindCraft is binder-only; pairing it with
// the enzyme application must be rejected with a message listing the
// compatible methods for "enzyme".
func TestPlanCreateRejectsIncompatibleApplicationMethod(t *testing.T) {
	st := newTestStore(t)
	tool := NewPlanCreateTool(st, newFakeInstaller("bindcraft"))
	input := json.RawMessage(`{
		"target": {"pdb_id": "1ABC"},
		"application": "enzyme",
		"method": "BindCraft"
	}`)
	_, err := tool.Execute(context.Background(), input)
	if err == nil {
		t.Fatal("expected an incompatible application/method error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "BindCraft") || !strings.Contains(msg, "enzyme") {
		t.Errorf("error %q should name both the method and the application", msg)
	}
	if !strings.Contains(strings.ToLower(msg), "compatible") {
		t.Errorf("error %q should list the compatible methods for the application", msg)
	}
}

// TestPlanCreateAcceptsCompatibleApplicationMethod is the positive control:
// every (application, method) pair listed in compat must round-trip. BoltzGen
// also needs a method_spec_path — the spec gate is exercised separately in
// TestPlanCreateBoltzGen* below — so this control passes it the minimum
// required field.
func TestPlanCreateAcceptsCompatibleApplicationMethod(t *testing.T) {
	for app, methods := range compat {
		for _, m := range methods {
			t.Run(string(app)+"/"+string(m), func(t *testing.T) {
				st := newTestStore(t)
				tool := NewPlanCreateTool(st, newFakeInstaller(toolForMethod(m)))
				extra := ""
				if m == MethodBoltzGen {
					// BoltzGen requires a spec path; with no registry wired the
					// design.boltzgen_check gate is skipped (nil-registry path).
					extra = `, "method_spec_path": "specs/binder.yaml"`
				}
				if m == MethodLigandMPNN {
					// LigandMPNN requires method_params carrying its run
					// configuration — at minimum a pdb backbone path.
					extra = `, "method_params": {"pdb": "bb.pdb"}`
				}
				if m == MethodRFantibody {
					// RFantibody requires method_params carrying its run
					// configuration — at minimum a target antigen PDB and
					// the epitope hotspots.
					extra = `, "method_params": {"target": "ag.pdb", "hotspots": "T10"}`
				}
				input := json.RawMessage(`{
					"target": {"pdb_id": "1ABC"},
					"application": "` + string(app) + `",
					"method": "` + string(m) + `"` + extra + `
				}`)
				if _, err := tool.Execute(context.Background(), input); err != nil {
					t.Fatalf("Execute: %v", err)
				}
			})
		}
	}
}

// fakeRegistry is a ToolRegistry stub reporting a fixed set of tool names as
// registered.
type fakeRegistry map[string]bool

func (f fakeRegistry) Get(name string) (tools.Tool, bool) { return nil, f[name] }

// TestCheckRegisteredRejectsUnwiredMethod guards the design.boltzgen-class
// gap: a method blessed in compat.go whose design.* tool was never wired
// into the registry must be rejected at plan.create time.
func TestCheckRegisteredRejectsUnwiredMethod(t *testing.T) {
	ct := &CreateTool{registry: fakeRegistry{"design.bindcraft": true}}

	if err := ct.checkRegistered(MethodBindCraft); err != nil {
		t.Errorf("a registered method must pass checkRegistered: %v", err)
	}

	err := ct.checkRegistered(MethodBoltzGen)
	if err == nil {
		t.Fatal("a method whose design.* tool is not registered must be rejected")
	}
	if !strings.Contains(err.Error(), "design.boltzgen") {
		t.Errorf("error should name the missing tool; got: %v", err)
	}

	// A nil registry (legacy/test wiring) skips the check.
	ct.registry = nil
	if err := ct.checkRegistered(MethodBoltzGen); err != nil {
		t.Errorf("nil registry must skip the check: %v", err)
	}
}

// TestDesignToolForMethodIsTotal ensures every compat.go Method maps to a
// non-empty design.* tool name — so checkRegistered never trips on its own
// "no mapping" branch for a known method.
func TestDesignToolForMethodIsTotal(t *testing.T) {
	for _, m := range []Method{
		MethodBindCraft, MethodBoltzGen, MethodRFdiffusion, MethodRFdiffusion2,
		MethodProteinMPNN, MethodLigandMPNN, MethodRFantibody,
	} {
		if got := designToolForMethod(m); got == "" {
			t.Errorf("designToolForMethod(%q) is empty — add it to compat.go", m)
		}
	}
}

// --- Task 7: BoltzGen spec + params folded into the plan ---

// fakeTool is a minimal tools.Tool whose Execute returns a canned Result. The
// BoltzGen plan tests inject it as the design.boltzgen_check tool so the
// check gate runs against a fixed {valid,...} contract — no container needed.
type fakeTool struct {
	name      string
	output    json.RawMessage
	execErr   error
	gotInputs []json.RawMessage
}

func (f *fakeTool) Name() string                            { return f.name }
func (f *fakeTool) Description() string                     { return "fake " + f.name }
func (f *fakeTool) InputSchema() map[string]any             { return map[string]any{"type": "object"} }
func (*fakeTool) RequiresConfirmation(json.RawMessage) bool { return false }
func (*fakeTool) EstimatedCostUSD(json.RawMessage) float64  { return 0 }
func (*fakeTool) EstimatedDuration(json.RawMessage) time.Duration {
	return 0
}
func (f *fakeTool) Execute(_ context.Context, in json.RawMessage) (tools.Result, error) {
	f.gotInputs = append(f.gotInputs, in)
	if f.execErr != nil {
		return tools.Result{}, f.execErr
	}
	return tools.Result{Output: f.output}, nil
}

// toolRegistry is a ToolRegistry stub that returns real tools.Tool values —
// unlike fakeRegistry, which only reports presence. plan.create needs a tool
// it can actually Execute for the design.boltzgen_check gate.
type toolRegistry map[string]tools.Tool

func (r toolRegistry) Get(name string) (tools.Tool, bool) {
	t, ok := r[name]
	return t, ok
}

// boltzGenPlanInput builds a BoltzGen plan.create input with the given spec
// path. The design.boltzgen tool must be registered for checkRegistered to
// pass, so callers that want the spec gate to fire register both tools.
func boltzGenPlanInput(specPath string) json.RawMessage {
	return json.RawMessage(`{
		"target": {"pdb_id": "1ABC", "chain": "A"},
		"application": "binder",
		"method": "BoltzGen",
		"method_spec_path": "` + specPath + `",
		"method_params": {"protocol": "protein-anything", "num_designs": 100, "budget": 10}
	}`)
}

// applyLigandMPNNParamsErr runs applyLigandMPNNMethodConfig against a fresh
// LigandMPNN plan and returns the resulting MethodConfig (nil on error) plus
// the error — mirroring how the BoltzGen tests exercise the method-config
// helper. A *CreateTool with no store/installer/registry is enough: the
// LigandMPNN config helper touches none of them.
func applyLigandMPNNParamsErr(input string) (*domain.MethodConfig, error) {
	ct := NewPlanCreateTool(nil, nil)
	p := domain.DesignPlan{Method: string(MethodLigandMPNN)}
	if err := ct.applyLigandMPNNMethodConfig(json.RawMessage(input), &p); err != nil {
		return nil, err
	}
	return p.MethodConfig, nil
}

// applyLigandMPNNParams is applyLigandMPNNParamsErr for the happy path: it
// fails the test on any error and returns the populated MethodConfig.
func applyLigandMPNNParams(t *testing.T, input string) *domain.MethodConfig {
	t.Helper()
	cfg, err := applyLigandMPNNParamsErr(input)
	if err != nil {
		t.Fatalf("applyLigandMPNNMethodConfig: %v", err)
	}
	return cfg
}

// TestPlanCreateLigandMPNNMethodConfig: a LigandMPNN plan with method_params
// must land MethodConfig.LigandMPNN, and an invalid params object is rejected.
func TestPlanCreateLigandMPNNMethodConfig(t *testing.T) {
	// A LigandMPNN plan with method_params must land MethodConfig.LigandMPNN.
	cfg := applyLigandMPNNParams(t, `{"method_params":{"pdb":"bb.pdb","model_type":"ligand_mpnn"}}`)
	if cfg == nil || cfg.LigandMPNN == nil {
		t.Fatal("MethodConfig.LigandMPNN must be populated")
	}
	if cfg.LigandMPNN.PDB != "bb.pdb" {
		t.Errorf("pdb = %q", cfg.LigandMPNN.PDB)
	}
	// An invalid params object (no pdb) must be rejected.
	if _, err := applyLigandMPNNParamsErr(`{"method_params":{"model_type":"ligand_mpnn"}}`); err == nil {
		t.Error("a LigandMPNN plan with no pdb must be rejected")
	}
}

// applyRFantibodyParamsErr runs applyRFantibodyMethodConfig against a fresh
// RFantibody plan and returns the resulting MethodConfig (nil on error) plus
// the error — mirroring how the LigandMPNN tests exercise the method-config
// helper. A *CreateTool with no store/installer/registry is enough: the
// RFantibody config helper touches none of them.
func applyRFantibodyParamsErr(input string) (*domain.MethodConfig, error) {
	ct := NewPlanCreateTool(nil, nil)
	p := domain.DesignPlan{Method: string(MethodRFantibody)}
	if err := ct.applyRFantibodyMethodConfig(json.RawMessage(input), &p); err != nil {
		return nil, err
	}
	return p.MethodConfig, nil
}

// applyRFantibodyParams is applyRFantibodyParamsErr for the happy path: it
// fails the test on any error and returns the populated MethodConfig.
func applyRFantibodyParams(t *testing.T, input string) *domain.MethodConfig {
	t.Helper()
	cfg, err := applyRFantibodyParamsErr(input)
	if err != nil {
		t.Fatalf("applyRFantibodyMethodConfig: %v", err)
	}
	return cfg
}

// TestPlanCreateRFantibodyMethodConfig: an RFantibody plan with method_params
// must land MethodConfig.RFantibody, and an invalid params object is rejected.
func TestPlanCreateRFantibodyMethodConfig(t *testing.T) {
	cfg := applyRFantibodyParams(t, `{"method_params":{"target":"ag.pdb","hotspots":"T10"}}`)
	if cfg == nil || cfg.RFantibody == nil {
		t.Fatal("MethodConfig.RFantibody must be populated")
	}
	if cfg.RFantibody.Target != "ag.pdb" {
		t.Errorf("target = %q", cfg.RFantibody.Target)
	}
	if _, err := applyRFantibodyParamsErr(`{"method_params":{"hotspots":"T10"}}`); err == nil {
		t.Error("an RFantibody plan with no target must be rejected")
	}
}

// TestPlanCreateBoltzGenRequiresSpecPath: a BoltzGen plan with no
// method_spec_path is rejected before anything is persisted.
func TestPlanCreateBoltzGenRequiresSpecPath(t *testing.T) {
	st := newTestStore(t)
	tool := NewPlanCreateTool(st, newFakeInstaller("boltzgen"))
	input := json.RawMessage(`{
		"target": {"pdb_id": "1ABC"},
		"application": "binder",
		"method": "BoltzGen"
	}`)
	_, err := tool.Execute(context.Background(), input)
	if err == nil {
		t.Fatal("expected an error for a BoltzGen plan with no method_spec_path")
	}
	if !strings.Contains(err.Error(), "method_spec_path") {
		t.Errorf("error %q should name method_spec_path", err.Error())
	}
}

// TestPlanCreateBoltzGenValidSpecPopulatesMethodConfig: a BoltzGen plan with a
// spec the check tool reports as valid persists with a populated MethodConfig.
func TestPlanCreateBoltzGenValidSpecPopulatesMethodConfig(t *testing.T) {
	st := newTestStore(t)
	tool := NewPlanCreateTool(st, newFakeInstaller("boltzgen"))
	check := &fakeTool{
		name:   "design.boltzgen_check",
		output: json.RawMessage(`{"valid": true, "visualization_path": "out/spec_viz.cif"}`),
	}
	tool.SetRegistry(toolRegistry{
		"design.boltzgen":       &fakeTool{name: "design.boltzgen"},
		"design.boltzgen_check": check,
	})

	res, err := tool.Execute(context.Background(), boltzGenPlanInput("specs/binder.yaml"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(check.gotInputs) != 1 {
		t.Fatalf("design.boltzgen_check should have been invoked once, got %d", len(check.gotInputs))
	}
	if !strings.Contains(string(check.gotInputs[0]), "specs/binder.yaml") {
		t.Errorf("check input %q should carry the spec path", check.gotInputs[0])
	}

	var got domain.DesignPlan
	if err := json.Unmarshal(res.Output, &got); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if got.MethodConfig == nil {
		t.Fatal("MethodConfig must be populated for a BoltzGen plan")
	}
	if got.MethodConfig.SpecPath != "specs/binder.yaml" {
		t.Errorf("SpecPath = %q, want specs/binder.yaml", got.MethodConfig.SpecPath)
	}
	if got.MethodConfig.BoltzGen == nil {
		t.Fatal("MethodConfig.BoltzGen must carry the run params")
	}
	if got.MethodConfig.BoltzGen.NumDesigns != 100 || got.MethodConfig.BoltzGen.Budget != 10 {
		t.Errorf("BoltzGen params not round-tripped: %+v", got.MethodConfig.BoltzGen)
	}
}

// TestPlanCreateBoltzGenInvalidSpecRejected: when the check tool reports the
// spec as invalid, plan.create rejects the plan with the check errors and
// persists nothing.
func TestPlanCreateBoltzGenInvalidSpecRejected(t *testing.T) {
	st := newTestStore(t)
	tool := NewPlanCreateTool(st, newFakeInstaller("boltzgen"))
	tool.SetRegistry(toolRegistry{
		"design.boltzgen": &fakeTool{name: "design.boltzgen"},
		"design.boltzgen_check": &fakeTool{
			name:   "design.boltzgen_check",
			output: json.RawMessage(`{"valid": false, "errors": ["chain B not found", "bad residue index"]}`),
		},
	})

	_, err := tool.Execute(context.Background(), boltzGenPlanInput("specs/bad.yaml"))
	if err == nil {
		t.Fatal("expected an error for a BoltzGen plan with an invalid spec")
	}
	msg := err.Error()
	for _, want := range []string{"chain B not found", "bad residue index", "design.boltzgen_check"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q should contain %q", msg, want)
		}
	}
	if _, ok, _ := st.LatestPlan(store.DefaultProjectID); ok {
		t.Error("an invalid-spec plan must not be persisted")
	}
}

// TestPlanCreateBoltzGenSkipsGateWhenCheckToolAbsent: when the check tool is
// not registered, the spec gate is skipped (the nil-registry path the install
// and registration guards already take) — the plan still persists with its
// MethodConfig.
func TestPlanCreateBoltzGenSkipsGateWhenCheckToolAbsent(t *testing.T) {
	st := newTestStore(t)
	tool := NewPlanCreateTool(st, newFakeInstaller("boltzgen"))
	// design.boltzgen is registered (so checkRegistered passes) but
	// design.boltzgen_check is not — the gate has nothing to call.
	tool.SetRegistry(toolRegistry{"design.boltzgen": &fakeTool{name: "design.boltzgen"}})

	res, err := tool.Execute(context.Background(), boltzGenPlanInput("specs/binder.yaml"))
	if err != nil {
		t.Fatalf("Execute: a missing check tool must skip the gate, not fail: %v", err)
	}
	var got domain.DesignPlan
	if err := json.Unmarshal(res.Output, &got); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if got.MethodConfig == nil || got.MethodConfig.SpecPath != "specs/binder.yaml" {
		t.Errorf("MethodConfig should still be populated when the gate is skipped: %+v", got.MethodConfig)
	}
}
