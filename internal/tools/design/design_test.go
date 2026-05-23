package design

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alvarogonjim/fova/internal/domain"
	"github.com/alvarogonjim/fova/internal/jobs"
	"github.com/alvarogonjim/fova/internal/store"
	"github.com/alvarogonjim/fova/internal/tools"
)

// stubBackend returns a fixed design-list output, ignoring the request. It
// also records the input the design tool actually handed it so tests can
// assert path-field resolution.
type stubBackend struct {
	output string
	lastIn []byte
}

func (s *stubBackend) Name() string { return "stub" }
func (s *stubBackend) Run(ctx context.Context, tool string, input []byte, log io.Writer, progress func(float64)) ([]byte, error) {
	s.lastIn = append(s.lastIn[:0], input...)
	_, _ = log.Write(input)
	if progress != nil {
		progress(0.5)
	}
	return []byte(s.output), nil
}

func newTestDeps(t *testing.T, backendOutput string) (*jobs.Manager, *store.Store, *stubBackend, string) {
	t.Helper()
	workspace := t.TempDir()
	st, err := store.Open(filepath.Join(workspace, "workspace.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return jobs.NewManager(st, nil), st, &stubBackend{output: backendOutput}, workspace
}

func waitJob(t *testing.T, m *jobs.Manager, id domain.JobID) domain.Job {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		j, _ := m.Status(id)
		switch j.Status {
		case domain.JobSucceeded, domain.JobFailed, domain.JobCancelled:
			return j
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("job did not finish")
	return domain.Job{}
}

func TestDesignToolSubmitsJobAndPersistsDesigns(t *testing.T) {
	out := `{"designs":[
	  {"sequence":{"A":"MAQVQL"},"structure_file":"d1.pdb","scores":{"ipsae":0.71,"plddt_mean":88.0}},
	  {"sequence":{"A":"GSHMKE"},"structure_file":"d2.pdb","scores":{"ipsae":0.55,"plddt_mean":81.0}}
	]}`
	mgr, st, backend, ws := newTestDeps(t, out)
	tool := NewBindCraftTool(ws, mgr, backend, st)

	if tool.Name() != "design.bindcraft" {
		t.Fatalf("Name = %q", tool.Name())
	}
	if !tool.RequiresConfirmation(nil) {
		t.Error("design tools must require confirmation (expensive)")
	}

	res, err := tool.Execute(context.Background(), json.RawMessage(`{"target":"1ZWG"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.JobID == "" {
		t.Fatal("Execute must return a JobID")
	}
	waitJob(t, mgr, res.JobID)

	designs, err := st.ListDesigns(store.DefaultProjectID)
	if err != nil {
		t.Fatal(err)
	}
	if len(designs) != 2 {
		t.Fatalf("expected 2 persisted designs, got %d", len(designs))
	}
	if designs[0].Origin != domain.OriginBindCraft {
		t.Errorf("design origin = %q", designs[0].Origin)
	}
	found := false
	for _, d := range designs {
		if d.Scores["ipsae"] == 0.71 {
			found = true
		}
	}
	if !found {
		t.Error("a design with ipsae 0.71 was not persisted")
	}
}

func TestDesignToolToleratesEmptyOutput(t *testing.T) {
	// An unknown-tool / error backend response has no "designs" array.
	// design.rfdiffusion is now a bespoke tool that validates the input
	// up-front, so the request carries the minimum-valid contigs string;
	// the test still asserts that an error-shaped backend reply persists
	// zero designs.
	mgr, st, backend, ws := newTestDeps(t, `{"error":"unknown tool"}`)
	tool := NewRFdiffusionTool(ws, mgr, backend, st)
	res, err := tool.Execute(context.Background(), json.RawMessage(`{"contigs":"50-100"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	waitJob(t, mgr, res.JobID)
	designs, _ := st.ListDesigns(store.DefaultProjectID)
	if len(designs) != 0 {
		t.Errorf("error output should persist 0 designs, got %d", len(designs))
	}
}

func TestDesignToolsImplementToolInterface(t *testing.T) {
	mgr, st, backend, ws := newTestDeps(t, `{"designs":[]}`)
	var _ tools.Tool = NewBindCraftTool(ws, mgr, backend, st)
	var _ tools.Tool = NewRFdiffusionTool(ws, mgr, backend, st)
	var _ tools.Tool = NewProteinMPNNTool(ws, mgr, backend, st)
	var _ tools.Tool = NewRFAntibodyTool(ws, mgr, backend, st)
	var _ tools.Tool = NewRFdiffusion2Tool(ws, mgr, backend, st)
	var _ tools.Tool = NewLigandMPNNTool(ws, mgr, backend, st)
}

// TestAntibodyEnzymeToolMetadata checks the v0.4 antibody and enzyme design
// tools report the right names and persist designs with the right origin and
// application.
func TestAntibodyEnzymeToolMetadata(t *testing.T) {
	// Every new tool must report its declared name.
	for _, tc := range []struct {
		newTool func(string, *jobs.Manager, *store.Store, *stubBackend) *designTool
		name    string
	}{
		{func(ws string, m *jobs.Manager, s *store.Store, b *stubBackend) *designTool {
			return NewRFdiffusion2Tool(ws, m, b, s)
		}, "design.rfdiffusion2"},
	} {
		mgr, st, backend, ws := newTestDeps(t, `{"designs":[]}`)
		if got := tc.newTool(ws, mgr, st, backend).Name(); got != tc.name {
			t.Errorf("Name = %q, want %q", got, tc.name)
		}
	}

	const stubOut = `{"designs":[{"sequence":{"A":"MAQVQL"},"structure_file":"d.pdb","scores":{"ipsae":0.7}}]}`

	// One enzyme tool must persist designs tagged with the matching origin and
	// application. (design.rfantibody has its own bespoke-tool test coverage.)
	for _, tc := range []struct {
		newTool func(string, *jobs.Manager, *store.Store, *stubBackend) *designTool
		origin  domain.DesignOrigin
		app     domain.Application
	}{
		{func(ws string, m *jobs.Manager, s *store.Store, b *stubBackend) *designTool {
			return NewRFdiffusion2Tool(ws, m, b, s)
		}, domain.OriginRFDiff2MPNN, domain.AppEnzyme},
	} {
		mgr, st, backend, ws := newTestDeps(t, stubOut)
		tool := tc.newTool(ws, mgr, st, backend)
		res, err := tool.Execute(context.Background(), json.RawMessage(`{"target":"1ZWG"}`))
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		waitJob(t, mgr, res.JobID)

		designs, err := st.ListDesigns(store.DefaultProjectID)
		if err != nil {
			t.Fatal(err)
		}
		if len(designs) != 1 {
			t.Fatalf("%s: expected 1 persisted design, got %d", tool.Name(), len(designs))
		}
		if designs[0].Origin != tc.origin {
			t.Errorf("%s: design origin = %q, want %q", tool.Name(), designs[0].Origin, tc.origin)
		}
		if designs[0].Application != tc.app {
			t.Errorf("%s: design application = %q, want %q", tool.Name(), designs[0].Application, tc.app)
		}
	}
}

func TestDesignToolSchemaAdvertisesContigs(t *testing.T) {
	tool := NewRFdiffusionTool("", nil, nil, nil)
	props, ok := tool.InputSchema()["properties"].(map[string]any)
	if !ok {
		t.Fatal("InputSchema has no properties map")
	}
	if _, ok := props["contigs"]; !ok {
		t.Error("InputSchema must advertise the contigs property")
	}
}

// Bug 1 — relative path is resolved against the workspace root before being
// handed to the backend.
func TestDesignToolResolvesRelativeTargetAgainstWorkspace(t *testing.T) {
	mgr, st, backend, ws := newTestDeps(t, `{"designs":[]}`)
	// File exists at <workspace>/inputs/x.pdb.
	if err := os.MkdirAll(filepath.Join(ws, "inputs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, "inputs", "x.pdb"), []byte("ATOM\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	tool := NewProteinMPNNTool(ws, mgr, backend, st)
	res, err := tool.Execute(context.Background(), json.RawMessage(`{"target":"inputs/x.pdb"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	waitJob(t, mgr, res.JobID)

	if backend.lastIn == nil {
		t.Fatal("backend.Run was not called")
	}
	var got map[string]any
	if err := json.Unmarshal(backend.lastIn, &got); err != nil {
		t.Fatalf("backend input is not valid JSON: %v", err)
	}
	want := filepath.Join(ws, "inputs", "x.pdb")
	if got["target"] != want {
		t.Errorf("backend saw target=%q, want %q", got["target"], want)
	}
}

// Bug 1 — an absolute path inside the workspace is passed through unchanged.
func TestDesignToolPassesAbsoluteInsideWorkspaceThrough(t *testing.T) {
	mgr, st, backend, ws := newTestDeps(t, `{"designs":[]}`)
	abs := filepath.Join(ws, "designs", "d.pdb")
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte("ATOM\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	tool := NewProteinMPNNTool(ws, mgr, backend, st)
	body, _ := json.Marshal(map[string]string{"target": abs})
	res, err := tool.Execute(context.Background(), body)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	waitJob(t, mgr, res.JobID)

	var got map[string]any
	if err := json.Unmarshal(backend.lastIn, &got); err != nil {
		t.Fatalf("backend input is not valid JSON: %v", err)
	}
	if got["target"] != abs {
		t.Errorf("backend saw target=%q, want absolute %q", got["target"], abs)
	}
}

// Bug 1 — an absolute path outside the workspace is rejected at submit time.
func TestDesignToolRejectsAbsoluteOutsideWorkspace(t *testing.T) {
	mgr, st, backend, ws := newTestDeps(t, `{"designs":[]}`)
	outside := filepath.Join(t.TempDir(), "outside.pdb")
	if err := os.WriteFile(outside, []byte("ATOM\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	tool := NewProteinMPNNTool(ws, mgr, backend, st)
	body, _ := json.Marshal(map[string]string{"target": outside})
	if _, err := tool.Execute(context.Background(), body); err == nil {
		t.Fatal("expected an 'escapes the workspace' error")
	} else if !strings.Contains(err.Error(), "escapes the workspace") {
		t.Errorf("error %q must mention 'escapes the workspace'", err)
	}
}

// Bug 1 — `../`-style traversal is rejected.
func TestDesignToolRejectsPathTraversal(t *testing.T) {
	mgr, st, backend, ws := newTestDeps(t, `{"designs":[]}`)
	tool := NewProteinMPNNTool(ws, mgr, backend, st)
	if _, err := tool.Execute(context.Background(),
		json.RawMessage(`{"target":"../../etc/passwd"}`)); err == nil {
		t.Fatal("expected an 'escapes the workspace' error")
	} else if !strings.Contains(err.Error(), "escapes the workspace") {
		t.Errorf("error %q must mention 'escapes the workspace'", err)
	}
}

// Bug 1 — an empty target is passed through unchanged (the wrapper doesn't
// validate presence; the adapter does).
func TestDesignToolPassesEmptyTargetThrough(t *testing.T) {
	mgr, st, backend, ws := newTestDeps(t, `{"designs":[]}`)
	tool := NewProteinMPNNTool(ws, mgr, backend, st)
	res, err := tool.Execute(context.Background(), json.RawMessage(`{"target":""}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	waitJob(t, mgr, res.JobID)

	var got map[string]any
	if err := json.Unmarshal(backend.lastIn, &got); err != nil {
		t.Fatalf("backend input is not valid JSON: %v", err)
	}
	if got["target"] != "" {
		t.Errorf("empty target should pass through unchanged, got %q", got["target"])
	}
}

// Bug 1 — nested starting_pdb in BindCraft's settings is also resolved.
func TestDesignToolResolvesNestedStartingPDB(t *testing.T) {
	mgr, st, backend, ws := newTestDeps(t, `{"designs":[]}`)
	if err := os.MkdirAll(filepath.Join(ws, "inputs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, "inputs", "t.pdb"), []byte("ATOM\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	tool := NewBindCraftTool(ws, mgr, backend, st)
	res, err := tool.Execute(context.Background(),
		json.RawMessage(`{"settings":{"starting_pdb":"inputs/t.pdb"}}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	waitJob(t, mgr, res.JobID)

	var got struct {
		Settings map[string]any `json:"settings"`
	}
	if err := json.Unmarshal(backend.lastIn, &got); err != nil {
		t.Fatalf("backend input is not valid JSON: %v", err)
	}
	want := filepath.Join(ws, "inputs", "t.pdb")
	if got.Settings["starting_pdb"] != want {
		t.Errorf("backend saw settings.starting_pdb=%q, want %q",
			got.Settings["starting_pdb"], want)
	}
}
