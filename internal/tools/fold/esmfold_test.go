package fold

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestParsePLDDT(t *testing.T) {
	pdb, err := os.ReadFile("testdata/sample.pdb")
	if err != nil {
		t.Fatal(err)
	}
	mean, min, n := parsePLDDT(string(pdb))
	if n != 3 {
		t.Fatalf("expected 3 CA atoms, got %d", n)
	}
	if mean != 80.0 {
		t.Errorf("plddt_mean = %v, want 80.0", mean)
	}
	if min != 70.0 {
		t.Errorf("plddt_min = %v, want 70.0", min)
	}
}

func TestEsmfoldExecute(t *testing.T) {
	pdb, _ := os.ReadFile("testdata/sample.pdb")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(pdb)
	}))
	defer srv.Close()

	root := t.TempDir()
	tool := NewESMFold(root)
	tool.Endpoint = srv.URL

	res, err := tool.Execute(context.Background(),
		json.RawMessage(`{"sequence":"MGS"}`))
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	var out esmfoldOutput
	if err := json.Unmarshal(res.Output, &out); err != nil {
		t.Fatalf("output not JSON: %v", err)
	}
	if out.Metrics.PLDDTMean != 80.0 {
		t.Errorf("plddt_mean = %v, want 80.0", out.Metrics.PLDDTMean)
	}
	saved := filepath.Join(root, out.StructureFile)
	if _, err := os.Stat(saved); err != nil {
		t.Errorf("structure file not saved: %v", err)
	}
}

func TestEsmfoldRejectsInvalidSequence(t *testing.T) {
	tool := NewESMFold(t.TempDir())
	if _, err := tool.Execute(context.Background(),
		json.RawMessage(`{"sequence":"MGSXB"}`)); err == nil {
		t.Fatal("invalid sequence should be rejected before any network call")
	}
}
