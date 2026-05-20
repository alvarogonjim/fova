package design

import (
	"context"
	"encoding/json"
	"io"
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
func (s stubBackend) Run(ctx context.Context, tool string, input []byte, log io.Writer) ([]byte, error) {
	_, _ = log.Write(input)
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
	var _ tools.Tool = NewRFAntibodyTool(mgr, backend, st)
	var _ tools.Tool = NewChai2Tool(mgr, backend, st)
	var _ tools.Tool = NewRFdiffusion2Tool(mgr, backend, st)
	var _ tools.Tool = NewLigandMPNNTool(mgr, backend, st)
}

// TestAntibodyEnzymeToolMetadata checks the v0.4 antibody and enzyme design
// tools report the right names and persist designs with the right origin and
// application.
func TestAntibodyEnzymeToolMetadata(t *testing.T) {
	// Every new tool must report its declared name.
	for _, tc := range []struct {
		newTool func(*jobs.Manager, *store.Store, stubBackend) *designTool
		name    string
	}{
		{func(m *jobs.Manager, s *store.Store, b stubBackend) *designTool {
			return NewRFAntibodyTool(m, b, s)
		}, "design.rfantibody"},
		{func(m *jobs.Manager, s *store.Store, b stubBackend) *designTool {
			return NewChai2Tool(m, b, s)
		}, "design.chai2"},
		{func(m *jobs.Manager, s *store.Store, b stubBackend) *designTool {
			return NewRFdiffusion2Tool(m, b, s)
		}, "design.rfdiffusion2"},
		{func(m *jobs.Manager, s *store.Store, b stubBackend) *designTool {
			return NewLigandMPNNTool(m, b, s)
		}, "design.ligandmpnn"},
	} {
		mgr, st, backend := newTestDeps(t, `{"designs":[]}`)
		if got := tc.newTool(mgr, st, backend).Name(); got != tc.name {
			t.Errorf("Name = %q, want %q", got, tc.name)
		}
	}

	const stubOut = `{"designs":[{"sequence":{"A":"MAQVQL"},"structure_file":"d.pdb","scores":{"ipsae":0.7}}]}`

	// One antibody tool and one enzyme tool must persist designs tagged with
	// the matching origin and application.
	for _, tc := range []struct {
		newTool func(*jobs.Manager, *store.Store, stubBackend) *designTool
		origin  domain.DesignOrigin
		app     domain.Application
	}{
		{func(m *jobs.Manager, s *store.Store, b stubBackend) *designTool {
			return NewRFAntibodyTool(m, b, s)
		}, domain.OriginRFAntibody, domain.AppAntibody},
		{func(m *jobs.Manager, s *store.Store, b stubBackend) *designTool {
			return NewRFdiffusion2Tool(m, b, s)
		}, domain.OriginRFDiff2MPNN, domain.AppEnzyme},
	} {
		mgr, st, backend := newTestDeps(t, stubOut)
		tool := tc.newTool(mgr, st, backend)
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
	tool := NewRFdiffusionTool(nil, nil, nil)
	props, ok := tool.InputSchema()["properties"].(map[string]any)
	if !ok {
		t.Fatal("InputSchema has no properties map")
	}
	if _, ok := props["contigs"]; !ok {
		t.Error("InputSchema must advertise the contigs property")
	}
}

func TestDesignToolSchemaAdvertisesSettings(t *testing.T) {
	tool := NewBindCraftTool(nil, nil, nil)
	props, ok := tool.InputSchema()["properties"].(map[string]any)
	if !ok {
		t.Fatal("InputSchema has no properties map")
	}
	if _, ok := props["settings"]; !ok {
		t.Error("InputSchema must advertise the settings property")
	}
}
