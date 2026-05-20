package design

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/alvarogonjim/proteus/internal/domain"
	"github.com/alvarogonjim/proteus/internal/jobs"
	"github.com/alvarogonjim/proteus/internal/store"
	"github.com/alvarogonjim/proteus/internal/tools"
)

// stubBackend returns a fixed design-list output, ignoring the request.
type stubBackend struct{ output string }

func (s stubBackend) Name() string { return "stub" }
func (s stubBackend) Run(ctx context.Context, tool string, input []byte) ([]byte, error) {
	return []byte(s.output), nil
}

func newTestDeps(t *testing.T, backendOutput string) (*jobs.Manager, *store.Store, stubBackend) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "workspace.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return jobs.NewManager(st, nil), st, stubBackend{output: backendOutput}
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
	mgr, st, backend := newTestDeps(t, out)
	tool := NewBindCraftTool(mgr, backend, st)

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
	mgr, st, backend := newTestDeps(t, `{"error":"unknown tool"}`)
	tool := NewRFdiffusionTool(mgr, backend, st)
	res, err := tool.Execute(context.Background(), json.RawMessage(`{}`))
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
	mgr, st, backend := newTestDeps(t, `{"designs":[]}`)
	var _ tools.Tool = NewBindCraftTool(mgr, backend, st)
	var _ tools.Tool = NewRFdiffusionTool(mgr, backend, st)
	var _ tools.Tool = NewProteinMPNNTool(mgr, backend, st)
}
