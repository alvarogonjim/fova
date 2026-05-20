package knowledge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alvarogonjim/fova/internal/tools"
)

var _ tools.Tool = (*PDB)(nil)

const pdbBody = `{
  "struct": { "title": "Crystal structure of T4 lysozyme" },
  "exptl": [ { "method": "X-RAY DIFFRACTION" } ],
  "rcsb_entry_info": { "resolution_combined": [ 1.85 ] }
}`

func TestPDBExecute(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(pdbBody))
	}))
	defer srv.Close()

	tool := NewPDB()
	tool.BaseURL = srv.URL

	res, err := tool.Execute(context.Background(), json.RawMessage(`{"pdb_id":"2LZM"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var out struct {
		PDBID      string  `json:"pdb_id"`
		Title      string  `json:"title"`
		Method     string  `json:"method"`
		Resolution float64 `json:"resolution"`
	}
	if err := json.Unmarshal(res.Output, &out); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if out.PDBID != "2LZM" {
		t.Errorf("pdb_id = %q, want 2LZM", out.PDBID)
	}
	if out.Title != "Crystal structure of T4 lysozyme" {
		t.Errorf("title = %q", out.Title)
	}
	if out.Method != "X-RAY DIFFRACTION" {
		t.Errorf("method = %q", out.Method)
	}
	if out.Resolution != 1.85 {
		t.Errorf("resolution = %v, want 1.85", out.Resolution)
	}
	if res.Display == "" {
		t.Error("Display is empty")
	}
}

func TestPDBExecuteEmptyID(t *testing.T) {
	tool := NewPDB()
	if _, err := tool.Execute(context.Background(), json.RawMessage(`{"pdb_id":""}`)); err == nil {
		t.Fatal("expected error for empty pdb_id")
	}
}
