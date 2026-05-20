package knowledge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alvarogonjim/proteus/internal/tools"
)

var _ tools.Tool = (*UniProt)(nil)

const uniprotBody = `{
  "primaryAccession": "P0DTC2",
  "proteinDescription": {
    "recommendedName": {
      "fullName": { "value": "Spike glycoprotein" }
    }
  },
  "organism": { "scientificName": "Severe acute respiratory syndrome coronavirus 2" },
  "sequence": { "value": "MFVFLVLLPLVSSQ", "length": 14 }
}`

func TestUniProtExecute(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(uniprotBody))
	}))
	defer srv.Close()

	tool := NewUniProt()
	tool.BaseURL = srv.URL

	res, err := tool.Execute(context.Background(), json.RawMessage(`{"accession":"P0DTC2"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var out struct {
		Accession string `json:"accession"`
		Name      string `json:"name"`
		Organism  string `json:"organism"`
		Sequence  string `json:"sequence"`
		Length    int    `json:"length"`
	}
	if err := json.Unmarshal(res.Output, &out); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if out.Accession != "P0DTC2" {
		t.Errorf("accession = %q, want P0DTC2", out.Accession)
	}
	if out.Name != "Spike glycoprotein" {
		t.Errorf("name = %q, want Spike glycoprotein", out.Name)
	}
	if out.Organism != "Severe acute respiratory syndrome coronavirus 2" {
		t.Errorf("organism = %q", out.Organism)
	}
	if out.Sequence != "MFVFLVLLPLVSSQ" {
		t.Errorf("sequence = %q", out.Sequence)
	}
	if out.Length != 14 {
		t.Errorf("length = %d, want 14", out.Length)
	}
	if res.Display == "" {
		t.Error("Display is empty")
	}
}

func TestUniProtExecuteEmptyAccession(t *testing.T) {
	tool := NewUniProt()
	if _, err := tool.Execute(context.Background(), json.RawMessage(`{"accession":""}`)); err == nil {
		t.Fatal("expected error for empty accession")
	}
}

func TestUniProtExecute404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	tool := NewUniProt()
	tool.BaseURL = srv.URL

	if _, err := tool.Execute(context.Background(), json.RawMessage(`{"accession":"NOPE"}`)); err == nil {
		t.Fatal("expected error for 404 response")
	}
}
