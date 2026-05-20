package knowledge

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetJSONDecodesAndSetsUserAgent(t *testing.T) {
	var gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		w.Write([]byte(`{"hits":3}`))
	}))
	defer srv.Close()

	var out struct {
		Hits int `json:"hits"`
	}
	if err := getJSON(context.Background(), srv.URL, &out); err != nil {
		t.Fatalf("getJSON: %v", err)
	}
	if out.Hits != 3 {
		t.Errorf("Hits = %d, want 3", out.Hits)
	}
	if len(gotUA) < 7 || gotUA[:7] != "Proteus" {
		t.Errorf("User-Agent = %q, want a Proteus/... agent", gotUA)
	}
}

func TestGetJSONNon200IsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusInternalServerError)
	}))
	defer srv.Close()
	var out map[string]any
	if err := getJSON(context.Background(), srv.URL, &out); err == nil {
		t.Error("a 500 response must be an error")
	}
}

func TestResultsStoreRoundTrip(t *testing.T) {
	r := NewResults()
	papers := []Paper{{ID: "10.1/x", Title: "A"}, {ID: "10.1/y", Title: "B"}}
	id := r.Put("europepmc", papers)
	if id == "" {
		t.Fatal("Put must return a non-empty results_id")
	}
	got, ok := r.Get(id)
	if !ok || len(got) != 2 || got[0].ID != "10.1/x" {
		t.Errorf("Get(%q) = %+v, %v", id, got, ok)
	}
	if _, ok := r.Get("missing"); ok {
		t.Error("Get of an unknown id must report ok=false")
	}
}
