package knowledge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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
	if len(gotUA) < 4 || gotUA[:4] != "fova" {
		t.Errorf("User-Agent = %q, want a fova/... agent", gotUA)
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

func TestPostJSON_OK(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", ct)
		}
		if r.Header.Get("User-Agent") == "" {
			t.Error("User-Agent missing")
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode request body: %v", err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	var out struct {
		OK bool `json:"ok"`
	}
	if err := postJSON(context.Background(), srv.URL, map[string]any{"q": "x"}, &out); err != nil {
		t.Fatalf("postJSON: %v", err)
	}
	if !out.OK {
		t.Error("did not decode body")
	}
	if got["q"] != "x" {
		t.Errorf("request body not received: %v", got)
	}
}

func TestPostJSON_NoContent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	var out struct {
		X int `json:"x"`
	}
	out.X = 7 // sentinel; must remain untouched
	if err := postJSON(context.Background(), srv.URL, map[string]any{}, &out); err != nil {
		t.Fatalf("postJSON 204: %v", err)
	}
	if out.X != 7 {
		t.Errorf("out mutated on 204: X = %d, want 7", out.X)
	}
}

func TestPostJSON_NotOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("upstream broken"))
	}))
	defer srv.Close()

	var out map[string]any
	err := postJSON(context.Background(), srv.URL, map[string]any{}, &out)
	if err == nil {
		t.Fatal("expected error for 502")
	}
	if !strings.Contains(err.Error(), "502") {
		t.Errorf("error does not mention status: %v", err)
	}
}
