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
// every (application, method) pair listed in compat must round-trip.
func TestPlanCreateAcceptsCompatibleApplicationMethod(t *testing.T) {
	for app, methods := range compat {
		for _, m := range methods {
			t.Run(string(app)+"/"+string(m), func(t *testing.T) {
				st := newTestStore(t)
				tool := NewPlanCreateTool(st, newFakeInstaller(toolForMethod(m)))
				input := json.RawMessage(`{
					"target": {"pdb_id": "1ABC"},
					"application": "` + string(app) + `",
					"method": "` + string(m) + `"
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
		MethodProteinMPNN, MethodLigandMPNN, MethodRFantibody, MethodChai2,
	} {
		if got := designToolForMethod(m); got == "" {
			t.Errorf("designToolForMethod(%q) is empty — add it to compat.go", m)
		}
	}
}
