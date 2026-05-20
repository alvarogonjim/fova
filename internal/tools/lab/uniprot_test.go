package lab

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alvarogonjim/fova/internal/transport"
)

// fastBackoff replaces the production schedule with a millisecond delay so
// tests run quickly without papering over retry behaviour.
func fastBackoff(int) time.Duration { return time.Millisecond }

const uniprotOneHit = `{"results":[{
	"primaryAccession":"P12345",
	"proteinDescription":{"recommendedName":{"fullName":{"value":"Test protein"}}},
	"organism":{"scientificName":"Homo sapiens"},
	"sequence":{"value":"MAQVQL","length":6},
	"uniProtKBCrossReferences":[
		{"database":"PDB","id":"1ABC"},
		{"database":"Pfam","id":"PF000"}
	]
}]}`

const uniprotEmpty = `{"results":[]}`

// TestUniProtSearchHit decodes a one-hit response and surfaces PDB cross-refs.
func TestUniProtSearchHit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(uniprotOneHit))
	}))
	defer srv.Close()

	tc := transport.New(transport.WithBackoff(fastBackoff))
	u := NewUniProtClient(tc)
	u.BaseURL = srv.URL
	rec, err := u.Search(context.Background(), "EGFR")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if rec == nil {
		t.Fatal("Search returned nil for hit body")
	}
	if rec.Accession != "P12345" {
		t.Errorf("Accession = %q, want P12345", rec.Accession)
	}
	if rec.Sequence != "MAQVQL" || rec.Length != 6 {
		t.Errorf("Sequence/Length = %q/%d", rec.Sequence, rec.Length)
	}
	if len(rec.PDBCrossRefs) != 1 || rec.PDBCrossRefs[0] != "1ABC" {
		t.Errorf("PDBCrossRefs = %v, want [1ABC]", rec.PDBCrossRefs)
	}
}

// TestUniProtSearchEmpty returns nil with no error when there are no hits.
func TestUniProtSearchEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(uniprotEmpty))
	}))
	defer srv.Close()

	tc := transport.New(transport.WithBackoff(fastBackoff))
	u := NewUniProtClient(tc)
	u.BaseURL = srv.URL
	rec, err := u.Search(context.Background(), "made-up-gene")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if rec != nil {
		t.Errorf("Search returned %+v for empty body", rec)
	}
}
