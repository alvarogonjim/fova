package plan

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alvarogonjim/proteus/internal/domain"
	"github.com/alvarogonjim/proteus/internal/store"
	"github.com/alvarogonjim/proteus/internal/tools"
)

// NewPlanCreateTool satisfies the tools.Tool interface.
var _ tools.Tool = NewPlanCreateTool(nil)

func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "proteus.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestPlanCreatePersistsPlan(t *testing.T) {
	st := newTestStore(t)
	tool := NewPlanCreateTool(st)

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
	tool := NewPlanCreateTool(newTestStore(t))
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
	tool := NewPlanCreateTool(newTestStore(t))
	input := json.RawMessage(`{
		"target": {"pdb_id": "1ABC"},
		"application": "binder"
	}`)
	if _, err := tool.Execute(context.Background(), input); err == nil {
		t.Fatal("expected an error for a missing method")
	}
}
