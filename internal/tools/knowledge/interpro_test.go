package knowledge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alvarogonjim/fova/internal/tools"
)

var _ tools.Tool = (*InterPro)(nil)

const interproBody = `{
  "results": [
    {
      "metadata": {
        "accession": "IPR000719",
        "name": "Protein kinase domain",
        "type": "domain"
      }
    },
    {
      "metadata": {
        "accession": "IPR011009",
        "name": "Protein kinase-like domain superfamily",
        "type": "homologous_superfamily"
      }
    }
  ]
}`

func TestInterProExecute(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(interproBody))
	}))
	defer srv.Close()

	tool := NewInterPro()
	tool.BaseURL = srv.URL

	res, err := tool.Execute(context.Background(), json.RawMessage(`{"accession":"P00533"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var out struct {
		Accession string `json:"accession"`
		Domains   []struct {
			InterProID string `json:"interpro_id"`
			Name       string `json:"name"`
			Type       string `json:"type"`
		} `json:"domains"`
	}
	if err := json.Unmarshal(res.Output, &out); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if out.Accession != "P00533" {
		t.Errorf("accession = %q, want P00533", out.Accession)
	}
	if len(out.Domains) != 2 {
		t.Fatalf("domains = %d, want 2", len(out.Domains))
	}
	if out.Domains[0].InterProID != "IPR000719" {
		t.Errorf("domain[0].interpro_id = %q", out.Domains[0].InterProID)
	}
	if out.Domains[0].Name != "Protein kinase domain" {
		t.Errorf("domain[0].name = %q", out.Domains[0].Name)
	}
	if out.Domains[0].Type != "domain" {
		t.Errorf("domain[0].type = %q", out.Domains[0].Type)
	}
	if res.Display == "" {
		t.Error("Display is empty")
	}
}

func TestInterProExecuteEmptyAccession(t *testing.T) {
	tool := NewInterPro()
	if _, err := tool.Execute(context.Background(), json.RawMessage(`{"accession":""}`)); err == nil {
		t.Fatal("expected error for empty accession")
	}
}
