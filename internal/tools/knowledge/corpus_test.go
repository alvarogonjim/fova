package knowledge

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alvarogonjim/fova/internal/domain"
	"github.com/alvarogonjim/fova/internal/jobs"
	"github.com/alvarogonjim/fova/internal/store"
	"github.com/alvarogonjim/fova/internal/tools"
)

// Pre-refactor actions of knowledge.corpus (umbrella tool, now removed):
//   add, search, grep, map, reduce, list, read, remove
// where `add` accepted either paper_ids or from_search on the same input.
//
// Post-refactor (v0.7 Bug 3): the umbrella is gone. Each action is its own
// flat tool. The eight tools are:
//   knowledge.corpus_add, knowledge.corpus_search, knowledge.corpus_grep,
//   knowledge.corpus_map, knowledge.corpus_reduce, knowledge.corpus_read,
//   knowledge.corpus_remove, knowledge.corpus_add_from_search.
// `add` now only takes paper_ids; `add_from_search` takes from_search.
// `list` was dropped — it had no callers in skills/prompts and the spec
// fixes the action set to eight.

// stubMapper is an offline Mapper that returns a fixed answer.
type stubMapper struct{ answer string }

func (m stubMapper) Map(ctx context.Context, prompt, text string) (string, error) {
	return m.answer, nil
}

// newTestCorpus builds a Corpus backed by a temp store and temp index, with
// FetchText stubbed to return canned full text per paper id.
func newTestCorpus(t *testing.T, texts map[string]string) (*Corpus, *Results, *jobs.Manager) {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "fova.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	res := NewResults()
	mgr := jobs.NewManager(st, nil)
	c := NewCorpus(st, res, stubMapper{answer: "fixed answer"}, mgr, filepath.Join(dir, "corpus.bleve"), 0)
	c.FetchText = func(ctx context.Context, paperID string) (string, error) {
		return texts[paperID], nil
	}
	return c, res, mgr
}

// waitJob polls until the job reaches a terminal state or the deadline fires.
func waitJob(t *testing.T, mgr *jobs.Manager, id domain.JobID) domain.Job {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		j, _ := mgr.Status(id)
		switch j.Status {
		case domain.JobSucceeded, domain.JobFailed, domain.JobCancelled:
			return j
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("job %s did not finish in time", id)
	return domain.Job{}
}

// newTestRegistry builds a tools.Registry with the Corpus's eight per-action
// tools registered. It is the regression vehicle for the flattening refactor.
// The fourth return is the jobs.Manager driving the async corpus_map path.
func newTestRegistry(t *testing.T, texts map[string]string) (*tools.Registry, *Corpus, *Results, *jobs.Manager) {
	t.Helper()
	c, res, mgr := newTestCorpus(t, texts)
	reg := tools.NewRegistry()
	c.Register(reg)
	return reg, c, res, mgr
}

func runTool(t *testing.T, reg *tools.Registry, name, in string) map[string]any {
	t.Helper()
	res, err := reg.Execute(context.Background(), name, json.RawMessage(in))
	if err != nil {
		t.Fatalf("Execute(%s, %s): %v", name, in, err)
	}
	if len(res.Output) == 0 {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal(res.Output, &out); err != nil {
		t.Fatalf("unmarshal output of %s: %v", name, err)
	}
	return out
}

// TestCorpusFlatRegistration is the headline test for Bug 3: every per-action
// tool name the LLM might guess via OpenAI-style dotted naming is registered.
func TestCorpusFlatRegistration(t *testing.T) {
	reg, _, _, _ := newTestRegistry(t, nil)
	actions := []string{
		"add", "search", "grep", "map", "reduce",
		"read", "remove", "add_from_search",
	}
	for _, a := range actions {
		name := "knowledge.corpus_" + a
		if _, ok := reg.Get(name); !ok {
			t.Errorf("missing tool %s", name)
		}
	}
	// The umbrella is removed entirely — its absence is part of the fix.
	if _, ok := reg.Get("knowledge.corpus"); ok {
		t.Error("umbrella knowledge.corpus should no longer be registered")
	}
}

func TestCorpusAdd(t *testing.T) {
	reg, _, _, _ := newTestRegistry(t, map[string]string{
		"10.1/aaa": "alpha helix folding kinetics",
		"10.1/bbb": "beta sheet aggregation propensity",
	})
	out := runTool(t, reg, "knowledge.corpus_add",
		`{"paper_ids":["10.1/aaa","10.1/bbb"]}`)
	if int(out["added"].(float64)) != 2 {
		t.Fatalf("added = %v, want 2", out["added"])
	}
}

// TestCorpusGrepLocal verifies grep matches a literal regex against stored
// papers. Search is now an external (PMC) call covered by the dedicated
// search tests below.
func TestCorpusGrepLocal(t *testing.T) {
	reg, _, _, _ := newTestRegistry(t, map[string]string{
		"10.1/aaa": "alpha helix folding kinetics",
		"10.1/bbb": "beta sheet aggregation propensity",
	})
	runTool(t, reg, "knowledge.corpus_add", `{"paper_ids":["10.1/aaa","10.1/bbb"]}`)

	grepped := runTool(t, reg, "knowledge.corpus_grep", `{"pattern":"folding"}`)
	if int(grepped["count"].(float64)) != 1 {
		t.Fatalf("grep count = %v, want 1", grepped["count"])
	}
	gPapers := grepped["papers"].([]any)
	gID := gPapers[0].(map[string]any)["id"].(string)
	if gID != "10.1/aaa" {
		t.Fatalf("grep matched %q, want 10.1/aaa", gID)
	}
}

func TestCorpusGrepIgnoreCase(t *testing.T) {
	reg, _, _, _ := newTestRegistry(t, map[string]string{
		"10.1/aaa": "ALPHA helix folding",
	})
	runTool(t, reg, "knowledge.corpus_add", `{"paper_ids":["10.1/aaa"]}`)

	out := runTool(t, reg, "knowledge.corpus_grep", `{"pattern":"alpha","ignore_case":true}`)
	if int(out["count"].(float64)) != 1 {
		t.Fatalf("grep ignore_case count = %v, want 1", out["count"])
	}
	out = runTool(t, reg, "knowledge.corpus_grep", `{"pattern":"alpha"}`)
	if int(out["count"].(float64)) != 0 {
		t.Fatalf("grep case-sensitive count = %v, want 0", out["count"])
	}
}

// TestCorpusMap exercises the full async path through the registry: corpus_map
// returns a JobID, the test polls the job to completion, then unmarshals the
// job output and asserts the per-paper answers.
func TestCorpusMap(t *testing.T) {
	reg, _, _, mgr := newTestRegistry(t, map[string]string{
		"10.1/aaa": "alpha helix folding kinetics",
		"10.1/bbb": "beta sheet aggregation propensity",
	})
	runTool(t, reg, "knowledge.corpus_add", `{"paper_ids":["10.1/aaa","10.1/bbb"]}`)

	res, err := reg.Execute(context.Background(), "knowledge.corpus_map", json.RawMessage(`{"prompt":"summarize"}`))
	if err != nil {
		t.Fatalf("Execute corpus_map: %v", err)
	}
	if res.JobID == "" {
		t.Fatal("corpus_map must return a JobID")
	}
	job := waitJob(t, mgr, res.JobID)
	if job.Status != domain.JobSucceeded {
		t.Fatalf("job status = %q, want succeeded (err=%q)", job.Status, job.Error)
	}
	var out map[string]any
	if err := json.Unmarshal(job.Output, &out); err != nil {
		t.Fatalf("unmarshal job output: %v", err)
	}
	perPaper := out["per_paper"].([]any)
	if len(perPaper) != 2 {
		t.Fatalf("per_paper len = %d, want 2", len(perPaper))
	}
	for _, e := range perPaper {
		m := e.(map[string]any)
		if m["answer"].(string) != "fixed answer" {
			t.Fatalf("answer = %q, want 'fixed answer'", m["answer"])
		}
		if m["paper_id"].(string) == "" {
			t.Fatalf("paper_id missing")
		}
	}
}

// TestCorpusMapReturnsJobID asserts the map action submits an async job and
// returns its id immediately, without blocking on the per-paper fan-out.
func TestCorpusMapReturnsJobID(t *testing.T) {
	reg, _, _, mgr := newTestRegistry(t, map[string]string{
		"10.1/aaa": "alpha helix folding kinetics",
		"10.1/bbb": "beta sheet aggregation propensity",
		"10.1/ccc": "gamma turn",
	})
	runTool(t, reg, "knowledge.corpus_add", `{"paper_ids":["10.1/aaa","10.1/bbb","10.1/ccc"]}`)

	res, err := reg.Execute(context.Background(), "knowledge.corpus_map", json.RawMessage(`{"prompt":"summarize"}`))
	if err != nil {
		t.Fatalf("Execute corpus_map: %v", err)
	}
	if res.JobID == "" {
		t.Fatal("expected non-empty JobID from corpus_map")
	}
	job, err := mgr.Status(res.JobID)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if job.Status != domain.JobQueued && job.Status != domain.JobRunning && job.Status != domain.JobSucceeded {
		t.Errorf("job status = %q, want queued/running/succeeded", job.Status)
	}
	if job.Tool != "knowledge.corpus_map" {
		t.Errorf("job tool = %q, want knowledge.corpus_map", job.Tool)
	}
	// Drain to keep the test hermetic.
	waitJob(t, mgr, res.JobID)
}

// blockingMapper waits on per-paper signals so the test can observe
// progress ticks and exercise cancellation between calls.
type blockingMapper struct {
	calls   chan struct{} // signalled before each Map waits
	release chan struct{} // tester sends one element to release each Map
	ctxErrs atomic.Int32  // counts ctx.Err() observed
}

func (m *blockingMapper) Map(ctx context.Context, prompt, text string) (string, error) {
	// Check ctx first so cancellation between papers is observed promptly.
	if err := ctx.Err(); err != nil {
		m.ctxErrs.Add(1)
		return "", err
	}
	select {
	case m.calls <- struct{}{}:
	case <-ctx.Done():
		m.ctxErrs.Add(1)
		return "", ctx.Err()
	}
	select {
	case <-m.release:
		return "ok", nil
	case <-ctx.Done():
		m.ctxErrs.Add(1)
		return "", ctx.Err()
	}
}

// blockingRegistry wires a Corpus that uses bm as its mapper into a registry,
// so map-job tests can drive the fan-out through the flat corpus_map tool.
func blockingRegistry(t *testing.T, bm *blockingMapper, paperIDs []string) (*tools.Registry, *jobs.Manager) {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "fova.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	res := NewResults()
	mgr := jobs.NewManager(st, nil)
	c := NewCorpus(st, res, bm, mgr, filepath.Join(dir, "corpus.bleve"), 0)
	c.FetchText = func(ctx context.Context, paperID string) (string, error) {
		return "text " + paperID, nil
	}
	reg := tools.NewRegistry()
	c.Register(reg)

	payload, err := json.Marshal(map[string]any{"paper_ids": paperIDs})
	if err != nil {
		t.Fatalf("marshal paper_ids: %v", err)
	}
	runTool(t, reg, "knowledge.corpus_add", string(payload))
	return reg, mgr
}

// TestCorpusMapEmitsPerPaperProgress drives the fan-out one paper at a time
// and asserts the job's progress fraction grows as each paper finishes.
func TestCorpusMapEmitsPerPaperProgress(t *testing.T) {
	bm := &blockingMapper{calls: make(chan struct{}, 4), release: make(chan struct{}, 4)}
	reg, mgr := blockingRegistry(t, bm, []string{"p1", "p2", "p3", "p4"})

	// Force serial execution so we can step progress deterministically.
	out, err := reg.Execute(context.Background(), "knowledge.corpus_map",
		json.RawMessage(`{"prompt":"q","concurrency":1}`))
	if err != nil {
		t.Fatalf("Execute corpus_map: %v", err)
	}

	for i := 1; i <= 4; i++ {
		// Wait until the worker reaches its Map call, then release it.
		<-bm.calls
		bm.release <- struct{}{}
		// Poll briefly for the progress to update past the prior fraction.
		want := float64(i) / 4.0
		deadline := time.Now().Add(time.Second)
		var seen float64
		for time.Now().Before(deadline) {
			j, _ := mgr.Status(out.JobID)
			seen = j.Progress
			if seen >= want-1e-9 {
				break
			}
			time.Sleep(2 * time.Millisecond)
		}
		if seen < want-1e-9 {
			t.Fatalf("after paper %d: progress = %v, want >= %v", i, seen, want)
		}
	}
	waitJob(t, mgr, out.JobID)
}

// TestCorpusMapCancellationObserved asserts that cancelling the job is
// surfaced into outstanding paper-level LLM calls via ctx.Err().
func TestCorpusMapCancellationObserved(t *testing.T) {
	bm := &blockingMapper{calls: make(chan struct{}, 4), release: make(chan struct{}, 4)}
	reg, mgr := blockingRegistry(t, bm, []string{"p1", "p2", "p3"})

	out, err := reg.Execute(context.Background(), "knowledge.corpus_map",
		json.RawMessage(`{"prompt":"q","concurrency":1}`))
	if err != nil {
		t.Fatalf("Execute corpus_map: %v", err)
	}
	// Wait until the first worker is blocked inside Map, then cancel.
	<-bm.calls
	if err := mgr.Cancel(out.JobID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	// Releasing remaining tokens is harmless — workers should observe ctx.
	close(bm.release)

	job := waitJob(t, mgr, out.JobID)
	if job.Status != domain.JobCancelled {
		t.Fatalf("job status = %q, want cancelled (err=%q)", job.Status, job.Error)
	}
	if bm.ctxErrs.Load() == 0 {
		t.Error("expected at least one ctx.Err() observed inside Mapper.Map")
	}
}

func TestCorpusReduce(t *testing.T) {
	reg, _, _, _ := newTestRegistry(t, nil)
	out := runTool(t, reg, "knowledge.corpus_reduce",
		`{"prompt":"combine","map_results":[{"paper_id":"x","answer":"a1"},{"paper_id":"y","answer":"a2"}]}`)
	if out["summary"].(string) != "fixed answer" {
		t.Fatalf("summary = %q, want 'fixed answer'", out["summary"])
	}
}

func TestCorpusRead(t *testing.T) {
	reg, _, _, _ := newTestRegistry(t, map[string]string{
		"10.1/aaa": "alpha helix folding kinetics",
	})
	runTool(t, reg, "knowledge.corpus_add", `{"paper_ids":["10.1/aaa"]}`)
	out := runTool(t, reg, "knowledge.corpus_read", `{"paper_id":"10.1/aaa"}`)
	if out["full_text"].(string) != "alpha helix folding kinetics" {
		t.Fatalf("read full_text = %q", out["full_text"])
	}
	if out["paper_id"].(string) != "10.1/aaa" {
		t.Fatalf("read paper_id = %q", out["paper_id"])
	}
}

func TestCorpusRemove(t *testing.T) {
	reg, _, _, _ := newTestRegistry(t, map[string]string{
		"10.1/aaa": "alpha helix folding kinetics",
		"10.1/bbb": "beta sheet aggregation propensity",
	})
	runTool(t, reg, "knowledge.corpus_add", `{"paper_ids":["10.1/aaa","10.1/bbb"]}`)
	out := runTool(t, reg, "knowledge.corpus_remove", `{"paper_ids":["10.1/aaa"]}`)
	if int(out["removed"].(float64)) != 1 {
		t.Fatalf("removed = %v, want 1", out["removed"])
	}
	// removed paper no longer found by grep (search is now external, so we
	// verify against the local grep path instead).
	grepped := runTool(t, reg, "knowledge.corpus_grep", `{"pattern":"folding"}`)
	if int(grepped["count"].(float64)) != 0 {
		t.Fatalf("grep count after remove = %v, want 0", grepped["count"])
	}
}

func TestCorpusAddFromSearch(t *testing.T) {
	reg, _, res, _ := newTestRegistry(t, map[string]string{
		"10.1/aaa": "alpha helix folding kinetics",
	})
	rid := res.Put("europepmc", []Paper{
		{ID: "10.1/aaa", Title: "Folding paper", Source: "europepmc"},
	})
	out := runTool(t, reg, "knowledge.corpus_add_from_search",
		`{"from_search":"`+rid+`"}`)
	if int(out["added"].(float64)) != 1 {
		t.Fatalf("added from_search = %v, want 1", out["added"])
	}
}

// TestCorpusAddFallsBackToAbstract verifies that a paper whose FetchText
// returns empty is still stored and indexed using its abstract, so it stays
// searchable, greppable and readable. Uses add_from_search since the abstract
// only travels with the Results-cached Paper records.
func TestCorpusAddFallsBackToAbstract(t *testing.T) {
	reg, _, res, _ := newTestRegistry(t, map[string]string{
		"10.1/abs": "", // FetchText returns ("", nil)
	})
	rid := res.Put("europepmc", []Paper{
		{ID: "10.1/abs", Title: "Folding paper", Source: "europepmc",
			Abstract: "alpha helix folding kinetics"},
	})
	out := runTool(t, reg, "knowledge.corpus_add_from_search",
		`{"from_search":"`+rid+`"}`)
	if int(out["added"].(float64)) != 1 {
		t.Fatalf("added = %v, want 1", out["added"])
	}

	read := runTool(t, reg, "knowledge.corpus_read", `{"paper_id":"10.1/abs"}`)
	if read["full_text"].(string) != "alpha helix folding kinetics" {
		t.Fatalf("read full_text = %q, want the abstract", read["full_text"])
	}

	grepped := runTool(t, reg, "knowledge.corpus_grep", `{"pattern":"folding"}`)
	if int(grepped["count"].(float64)) != 1 {
		t.Fatalf("grep count = %v, want 1", grepped["count"])
	}
}

// pmcEmptyBody is a valid Europe PMC response with zero hits.
const pmcEmptyBody = `{"resultList":{"result":[]}}`

// pmcOneHitBody is a valid Europe PMC response with one hit.
const pmcOneHitBody = `{"resultList":{"result":[` +
	`{"doi":"10.1/aaa","pmcid":"PMC1","title":"Folding kinetics","authorString":"Smith","pubYear":"2024","abstractText":"alpha helix folding"}` +
	`]}}`

// newPMCTestRegistry builds a tools.Registry with knowledge.corpus_* tools
// whose PMC search calls hit the given httptest URL instead of the live API.
func newPMCTestRegistry(t *testing.T, pmcURL string) *tools.Registry {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "fova.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	res := NewResults()
	mgr := jobs.NewManager(st, nil)
	c := NewCorpus(st, res, stubMapper{answer: "fixed answer"}, mgr, filepath.Join(dir, "corpus.bleve"), 0)
	c.PMCBaseURL = pmcURL
	c.searchBackoff = fastSearchBackoff
	reg := tools.NewRegistry()
	c.Register(reg)
	return reg
}

// fastSearchBackoff makes retry tests run in milliseconds, not seconds.
func fastSearchBackoff(attempt int) time.Duration { return time.Millisecond }

// TestCorpusSearchRetryThen200 verifies a 503 followed by a 200 yields the 200 body.
func TestCorpusSearchRetryThen200(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := atomic.AddInt32(&hits, 1)
		if n == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(pmcOneHitBody))
	}))
	defer srv.Close()

	reg := newPMCTestRegistry(t, srv.URL)
	out := runTool(t, reg, "knowledge.corpus_search", `{"query":"folding"}`)
	if int(out["count"].(float64)) != 1 {
		t.Fatalf("count = %v, want 1 (retry must succeed)", out["count"])
	}
	if int32(atomic.LoadInt32(&hits)) != 2 {
		t.Fatalf("hits = %d, want 2 (one 503 then one 200)", hits)
	}
	if out["query_sent"] == nil {
		t.Fatalf("query_sent missing in output: %v", out)
	}
}

// TestCorpusSearchEmptyHasNote checks an empty PMC response surfaces a hint.
func TestCorpusSearchEmptyHasNote(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(pmcEmptyBody))
	}))
	defer srv.Close()

	reg := newPMCTestRegistry(t, srv.URL)
	out := runTool(t, reg, "knowledge.corpus_search", `{"query":"the absolutely impossible TIM-barrel binder"}`)
	if int(out["count"].(float64)) != 0 {
		t.Fatalf("count = %v, want 0", out["count"])
	}
	note, _ := out["note"].(string)
	if note == "" {
		t.Fatalf("note missing in empty response: %v", out)
	}
	qs, _ := out["query_sent"].(string)
	if qs == "" {
		t.Fatalf("query_sent missing: %v", out)
	}
}

// TestCorpusSearchSanitisationStripsStopwords checks the sent query drops
// common stop tokens and collapses whitespace.
func TestCorpusSearchSanitisationStripsStopwords(t *testing.T) {
	var lastQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lastQuery = r.URL.Query().Get("query")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(pmcEmptyBody))
	}))
	defer srv.Close()

	reg := newPMCTestRegistry(t, srv.URL)
	runTool(t, reg, "knowledge.corpus_search", `{"query":"   the  folding   of a   protein  "}`)
	if lastQuery != "folding protein" {
		t.Errorf("PMC received query = %q, want %q", lastQuery, "folding protein")
	}
}

// TestCorpusSearchTruncatedAt50 verifies a > 50-hit response is capped.
func TestCorpusSearchTruncatedAt50(t *testing.T) {
	var sb strings.Builder
	sb.WriteString(`{"resultList":{"result":[`)
	for i := 0; i < 60; i++ {
		if i > 0 {
			sb.WriteByte(',')
		}
		fmt.Fprintf(&sb, `{"doi":"10.1/p%d","title":"paper %d","authorString":"X","pubYear":"2024","abstractText":"a"}`, i, i)
	}
	sb.WriteString(`]}}`)
	body := sb.String()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	reg := newPMCTestRegistry(t, srv.URL)
	out := runTool(t, reg, "knowledge.corpus_search", `{"query":"folding"}`)
	if int(out["count"].(float64)) != 50 {
		t.Fatalf("count = %v, want 50 (truncation)", out["count"])
	}
	if int(out["truncated_at"].(float64)) != 50 {
		t.Fatalf("truncated_at = %v, want 50", out["truncated_at"])
	}
	papers := out["papers"].([]any)
	if len(papers) != 50 {
		t.Fatalf("len(papers) = %d, want 50", len(papers))
	}
}

// TestCorpusSearchDropsJunk verifies entries without a title or abstract are dropped.
func TestCorpusSearchDropsJunk(t *testing.T) {
	body := `{"resultList":{"result":[
		{"doi":"10.1/ok","title":"Ok paper","authorString":"X","pubYear":"2024","abstractText":"x"},
		{"doi":"10.1/notitle","title":"","authorString":"Y","pubYear":"2024","abstractText":""},
		{"doi":"10.1/okabs","title":"","authorString":"Z","pubYear":"2024","abstractText":"with abstract"}
	]}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	reg := newPMCTestRegistry(t, srv.URL)
	out := runTool(t, reg, "knowledge.corpus_search", `{"query":"folding"}`)
	// The empty-title-and-empty-abstract entry is dropped; the others survive.
	if int(out["count"].(float64)) != 2 {
		t.Fatalf("count = %v, want 2 (junk row dropped)", out["count"])
	}
}

// TestCorpusClose verifies Close is nil-safe before the index is opened and
// after an add (which lazily opens the index).
func TestCorpusClose(t *testing.T) {
	c, _, _ := newTestCorpus(t, nil)
	if err := c.Close(); err != nil {
		t.Fatalf("Close on fresh corpus: %v", err)
	}

	reg, c2, _, _ := newTestRegistry(t, map[string]string{"10.1/aaa": "alpha helix"})
	runTool(t, reg, "knowledge.corpus_add", `{"paper_ids":["10.1/aaa"]}`)
	if err := c2.Close(); err != nil {
		t.Fatalf("Close after add: %v", err)
	}
}

// Each sub-tool must implement tools.Tool — the asserts below fail at compile
// time if any one of them drifts.
var (
	_ tools.Tool = (*corpusAdd)(nil)
	_ tools.Tool = (*corpusAddFromSearch)(nil)
	_ tools.Tool = (*corpusSearch)(nil)
	_ tools.Tool = (*corpusGrep)(nil)
	_ tools.Tool = (*corpusMap)(nil)
	_ tools.Tool = (*corpusReduce)(nil)
	_ tools.Tool = (*corpusRead)(nil)
	_ tools.Tool = (*corpusRemove)(nil)
)
