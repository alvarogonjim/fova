package design

import (
	"context"
	"encoding/json"
	"io"
	"path/filepath"
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

// The path-resolution tests that lived here previously (relative/absolute/
// traversal handling for the shared *designTool wrapper, parameterised over
// NewRFdiffusion2Tool / NewProteinMPNNTool) are gone with that wrapper.
// All six design tools (boltzgen, ligandmpnn, rfantibody, rfdiffusion,
// rfdiffusion2, proteinmpnn, bindcraft) are now bespoke; each owns its own
// path-resolution test in its tool-specific *_test.go file (e.g.
// proteinmpnn_test.go::TestProteinMPNNResolvesRelativePDBAgainstWorkspace).
