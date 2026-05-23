package tools

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

// fakeTool is a minimal Tool used to exercise the registry.
type fakeTool struct{ name string }

func (f fakeTool) Name() string                                    { return f.name }
func (f fakeTool) Description() string                             { return "fake" }
func (f fakeTool) InputSchema() map[string]any                     { return map[string]any{"type": "object"} }
func (f fakeTool) RequiresConfirmation(json.RawMessage) bool       { return false }
func (f fakeTool) EstimatedCostUSD(json.RawMessage) float64        { return 0 }
func (f fakeTool) EstimatedDuration(json.RawMessage) time.Duration { return time.Second }
func (f fakeTool) Execute(_ context.Context, in json.RawMessage) (Result, error) {
	return Result{Display: "ran " + f.name + " " + string(in)}, nil
}

func TestRegistryRegisterAndGet(t *testing.T) {
	r := NewRegistry()
	r.Register(fakeTool{name: "a.b"})
	if _, ok := r.Get("a.b"); !ok {
		t.Fatal("registered tool not found")
	}
	if _, ok := r.Get("missing"); ok {
		t.Fatal("missing tool reported as found")
	}
}

func TestRegistrySpecs(t *testing.T) {
	r := NewRegistry()
	r.Register(fakeTool{name: "a.b"})
	specs := r.Specs()
	if len(specs) != 1 || specs[0].Name != "a.b" {
		t.Fatalf("unexpected specs: %+v", specs)
	}
}

func TestRegistryExecute(t *testing.T) {
	r := NewRegistry()
	r.Register(fakeTool{name: "a.b"})
	res, err := r.Execute(context.Background(), "a.b", json.RawMessage(`{"x":1}`))
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	if res.Display != `ran a.b {"x":1}` {
		t.Fatalf("unexpected result: %q", res.Display)
	}
	if _, err := r.Execute(context.Background(), "missing", nil); err == nil {
		t.Fatal("executing a missing tool should error")
	}
}

func TestSafeJoin(t *testing.T) {
	root := t.TempDir()
	if _, err := SafeJoin(root, "designs/a.pdb"); err != nil {
		t.Errorf("valid path rejected: %v", err)
	}
	if _, err := SafeJoin(root, "../escape"); err == nil {
		t.Error("path escaping the workspace was allowed")
	}
	if _, err := SafeJoin(root, "/etc/passwd"); err == nil {
		t.Error("absolute path escaping the workspace was allowed")
	}
}

// concurrentTool implements both Tool and Concurrent (Concurrent()=true).
type concurrentTool struct{}

func (concurrentTool) Name() string                                                   { return "fake.concurrent" }
func (concurrentTool) Description() string                                            { return "" }
func (concurrentTool) InputSchema() map[string]any                                    { return map[string]any{} }
func (concurrentTool) Execute(_ context.Context, _ json.RawMessage) (Result, error)   { return Result{}, nil }
func (concurrentTool) RequiresConfirmation(json.RawMessage) bool                      { return false }
func (concurrentTool) EstimatedCostUSD(json.RawMessage) float64                       { return 0 }
func (concurrentTool) EstimatedDuration(json.RawMessage) time.Duration                { return 0 }
func (concurrentTool) Concurrent() bool                                               { return true }

// serialTool implements Tool but NOT Concurrent.
type serialTool struct{}

func (serialTool) Name() string                                                   { return "fake.serial" }
func (serialTool) Description() string                                            { return "" }
func (serialTool) InputSchema() map[string]any                                    { return map[string]any{} }
func (serialTool) Execute(_ context.Context, _ json.RawMessage) (Result, error)   { return Result{}, nil }
func (serialTool) RequiresConfirmation(json.RawMessage) bool                      { return false }
func (serialTool) EstimatedCostUSD(json.RawMessage) float64                       { return 0 }
func (serialTool) EstimatedDuration(json.RawMessage) time.Duration                { return 0 }

func TestIsConcurrent(t *testing.T) {
	if !IsConcurrent(concurrentTool{}) {
		t.Errorf("IsConcurrent(concurrentTool) = false, want true")
	}
	if IsConcurrent(serialTool{}) {
		t.Errorf("IsConcurrent(serialTool) = true, want false")
	}
}
