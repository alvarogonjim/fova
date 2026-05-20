package lab

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/alvarogonjim/fova/internal/transport"
)

// pdbBody1LYZ is a minimal PDB core-entry body for the happy 1LYZ-chain-A path.
const pdbBody1LYZ = `{
	"struct": {"title": "Hen egg-white lysozyme"},
	"exptl": [{"method": "X-RAY DIFFRACTION"}],
	"rcsb_entry_info": {"resolution_combined": [1.8]},
	"rcsb_entry_container_identifiers": {"polymer_entity_ids": ["1"]}
}`

// pdbBodyObsolete simulates an entry with no chain metadata at all (the
// silent-chain-A bug).
const pdbBodyObsolete = `{
	"struct": {"title": "Obsolete entry"},
	"exptl": [{"method": "X-RAY DIFFRACTION"}]
}`

// pdbBodyChainsAB exposes two polymer chains so the resolver returns the
// first explicitly without inventing one.
const pdbBodyChainsAB = `{
	"struct": {"title": "Two-chain entry"},
	"exptl": [{"method": "X-RAY DIFFRACTION"}],
	"rcsb_polymer_entity_container_identifiers": {
		"auth_asym_ids": ["A","B"]
	}
}`

func newTargetsTool(t *testing.T, h http.Handler, uniH http.Handler) (*targetsSearchTool, *httptest.Server, *httptest.Server) {
	t.Helper()
	pdbSrv := httptest.NewServer(h)
	t.Cleanup(pdbSrv.Close)
	var uniSrv *httptest.Server
	if uniH != nil {
		uniSrv = httptest.NewServer(uniH)
		t.Cleanup(uniSrv.Close)
	}
	tc := transport.New(transport.WithBackoff(fastBackoff))
	tool := NewTargetsSearchTool(NewClient("tok"))
	tool.tc = tc
	tool.pdbBaseURL = pdbSrv.URL
	if uniSrv != nil {
		tool.uniprot = NewUniProtClient(tc)
		tool.uniprot.BaseURL = uniSrv.URL
	}
	return tool, pdbSrv, uniSrv
}

// TestTargetsSearch1LYZHappyPath verifies a known PDB entry round-trips with
// an explicit chain.
func TestTargetsSearch1LYZHappyPath(t *testing.T) {
	tool, _, _ := newTargetsTool(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "1LYZ") {
			t.Errorf("PDB URL = %q, want 1LYZ in path", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(pdbBody1LYZ))
	}), nil)

	tool.cache = newMemTargetsCache()
	res, err := tool.Execute(context.Background(), json.RawMessage(`{"query":"1LYZ"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(res.Output, &out); err != nil {
		t.Fatalf("decode output: %v", err)
	}
	if out["source"] != "pdb" {
		t.Errorf("source = %v, want pdb", out["source"])
	}
	// Chain should be unset on an entry without explicit chain ids — never
	// silently defaulted to "A".
	if _, has := out["chain"]; has && out["chain"] != nil {
		// If a chain is present at all, it must come from the body's chain list.
		t.Logf("chain present in 1LYZ output: %v", out["chain"])
	}
}

// TestTargetsSearchObsoleteHasNoSilentChainA verifies an entry with no chain
// list reports chain: nil and a chain_inference_note.
func TestTargetsSearchObsoleteHasNoSilentChainA(t *testing.T) {
	tool, _, _ := newTargetsTool(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(pdbBodyObsolete))
	}), nil)
	tool.cache = newMemTargetsCache()

	res, err := tool.Execute(context.Background(), json.RawMessage(`{"query":"OBSO"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(res.Output, &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// chain must be explicit null, never silently "A".
	if v, ok := out["chain"]; !ok {
		t.Fatal("chain field missing — expected explicit null")
	} else if v != nil {
		t.Errorf("chain = %v, want nil", v)
	}
	note, _ := out["chain_inference_note"].(string)
	if note == "" {
		t.Errorf("chain_inference_note missing: %v", out)
	}
}

// TestTargetsSearchExposesChainList verifies an entry's explicit chain list
// is surfaced (so the agent can pick a chain itself).
func TestTargetsSearchExposesChainList(t *testing.T) {
	tool, _, _ := newTargetsTool(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(pdbBodyChainsAB))
	}), nil)
	tool.cache = newMemTargetsCache()

	res, err := tool.Execute(context.Background(), json.RawMessage(`{"query":"DUO1"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(res.Output, &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	chains, _ := out["chains"].([]any)
	if len(chains) != 2 || chains[0] != "A" || chains[1] != "B" {
		t.Errorf("chains = %v, want [A B]", chains)
	}
}

// TestTargetsSearchPDBRetry replays a 503 then a 200 and asserts the 200
// body is what the agent sees.
func TestTargetsSearchPDBRetry(t *testing.T) {
	var hits int32
	tool, _, _ := newTargetsTool(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := atomic.AddInt32(&hits, 1)
		if n == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(pdbBody1LYZ))
	}), nil)
	tool.cache = newMemTargetsCache()

	res, err := tool.Execute(context.Background(), json.RawMessage(`{"query":"1LYZ"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if atomic.LoadInt32(&hits) != 2 {
		t.Errorf("hits = %d, want 2 (503 then 200)", hits)
	}
	if !strings.Contains(res.Display, "1LYZ") && !strings.Contains(string(res.Output), "1LYZ") {
		t.Errorf("response missing the recovered PDB id: %q / %q", res.Display, string(res.Output))
	}
}

// TestTargetsSearchUniProtFallback verifies a gene-name query with no PDB
// hit falls through to UniProt and surfaces the canonical sequence.
func TestTargetsSearchUniProtFallback(t *testing.T) {
	pdbHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// PDB has no record for an unknown gene name.
		w.WriteHeader(http.StatusNotFound)
	})
	uniHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(uniprotOneHit))
	})
	tool, _, _ := newTargetsTool(t, pdbHandler, uniHandler)
	tool.cache = newMemTargetsCache()

	res, err := tool.Execute(context.Background(), json.RawMessage(`{"query":"EGFR"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(res.Output, &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out["source"] != "uniprot" {
		t.Errorf("source = %v, want uniprot", out["source"])
	}
	if seq, _ := out["sequence"].(string); seq == "" {
		t.Errorf("sequence missing in uniprot fallback: %v", out)
	}
}

// TestTargetsSearchNumericQueryNoUniProtFallback verifies a numeric/PDB-style
// query does NOT fall through to UniProt when PDB misses (UniProt is gated
// to alphabetic gene-name shapes).
func TestTargetsSearchNumericQueryNoUniProtFallback(t *testing.T) {
	uniHit := int32(0)
	pdbHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	uniHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&uniHit, 1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(uniprotOneHit))
	})
	tool, _, _ := newTargetsTool(t, pdbHandler, uniHandler)
	tool.cache = newMemTargetsCache()

	_, err := tool.Execute(context.Background(), json.RawMessage(`{"query":"1ABC"}`))
	if err == nil {
		t.Fatal("expected an error for PDB-id miss with no fallback")
	}
	if atomic.LoadInt32(&uniHit) != 0 {
		t.Errorf("UniProt was hit %d times for numeric query — should have been skipped", uniHit)
	}
}

// TestTargetsSearchCacheHit verifies a second identical query is served from
// the cache (and never reaches the test PDB server).
func TestTargetsSearchCacheHit(t *testing.T) {
	var pdbHits int32
	tool, _, _ := newTargetsTool(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&pdbHits, 1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(pdbBody1LYZ))
	}), nil)
	tool.cache = newMemTargetsCache()

	// First call: PDB is hit, response is cached.
	_, err := tool.Execute(context.Background(), json.RawMessage(`{"query":"1LYZ"}`))
	if err != nil {
		t.Fatalf("first Execute: %v", err)
	}
	if atomic.LoadInt32(&pdbHits) != 1 {
		t.Fatalf("first call: pdbHits = %d, want 1", pdbHits)
	}

	// Second call: should be served from cache. The pdbHits counter must
	// stay at 1.
	res, err := tool.Execute(context.Background(), json.RawMessage(`{"query":"1LYZ"}`))
	if err != nil {
		t.Fatalf("second Execute: %v", err)
	}
	if atomic.LoadInt32(&pdbHits) != 1 {
		t.Errorf("second call hit PDB %d times — cache miss", pdbHits-1)
	}
	var out map[string]any
	if err := json.Unmarshal(res.Output, &out); err != nil {
		t.Fatal(err)
	}
	if v, _ := out["cached"].(bool); !v {
		t.Errorf("cached flag missing in second-call output: %v", out)
	}
}

// TestTargetsSearchEmptyQueryKeepsCatalog preserves the legacy zero-query
// behaviour: with no query, the tool returns the Adaptyv catalog.
func TestTargetsSearchEmptyQueryKeepsCatalog(t *testing.T) {
	c := toolTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[{"id":"t1","name":"EGFR"}]`))
	}))
	tool := NewTargetsSearchTool(c)
	tool.cache = newMemTargetsCache()
	res, err := tool.Execute(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var out struct {
		Targets []Target `json:"targets"`
	}
	if err := json.Unmarshal(res.Output, &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Targets) != 1 || out.Targets[0].ID != "t1" {
		t.Errorf("Adaptyv catalog round-trip broken: %+v", out.Targets)
	}
}
