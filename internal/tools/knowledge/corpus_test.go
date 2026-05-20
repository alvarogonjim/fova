package knowledge

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/alvarogonjim/proteus/internal/store"
	"github.com/alvarogonjim/proteus/internal/tools"
)

var _ tools.Tool = (*Corpus)(nil)

// stubMapper is an offline Mapper that returns a fixed answer.
type stubMapper struct{ answer string }

func (m stubMapper) Map(ctx context.Context, prompt, text string) (string, error) {
	return m.answer, nil
}

// newTestCorpus builds a Corpus backed by a temp store and temp index, with
// FetchText stubbed to return canned full text per paper id.
func newTestCorpus(t *testing.T, texts map[string]string) (*Corpus, *Results) {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "proteus.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	res := NewResults()
	c := NewCorpus(st, res, stubMapper{answer: "fixed answer"}, filepath.Join(dir, "corpus.bleve"))
	c.FetchText = func(ctx context.Context, paperID string) (string, error) {
		return texts[paperID], nil
	}
	return c, res
}

func runCmd(t *testing.T, c *Corpus, in string) map[string]any {
	t.Helper()
	res, err := c.Execute(context.Background(), json.RawMessage(in))
	if err != nil {
		t.Fatalf("Execute(%s): %v", in, err)
	}
	var out map[string]any
	if err := json.Unmarshal(res.Output, &out); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	return out
}

func TestCorpusAddAndList(t *testing.T) {
	c, _ := newTestCorpus(t, map[string]string{
		"10.1/aaa": "alpha helix folding kinetics",
		"10.1/bbb": "beta sheet aggregation propensity",
	})
	out := runCmd(t, c, `{"command":"add","paper_ids":["10.1/aaa","10.1/bbb"]}`)
	if int(out["added"].(float64)) != 2 {
		t.Fatalf("added = %v, want 2", out["added"])
	}
	listed := runCmd(t, c, `{"command":"list"}`)
	if int(listed["count"].(float64)) != 2 {
		t.Fatalf("list count = %v, want 2", listed["count"])
	}
}

func TestCorpusSearchAndGrepConsistency(t *testing.T) {
	c, _ := newTestCorpus(t, map[string]string{
		"10.1/aaa": "alpha helix folding kinetics",
		"10.1/bbb": "beta sheet aggregation propensity",
	})
	runCmd(t, c, `{"command":"add","paper_ids":["10.1/aaa","10.1/bbb"]}`)

	searched := runCmd(t, c, `{"command":"search","query":"folding"}`)
	if int(searched["count"].(float64)) != 1 {
		t.Fatalf("search count = %v, want 1", searched["count"])
	}
	if searched["results_id"] == nil || searched["results_id"].(string) == "" {
		t.Fatalf("search results_id missing")
	}
	sPapers := searched["papers"].([]any)
	sID := sPapers[0].(map[string]any)["id"].(string)
	if sID != "10.1/aaa" {
		t.Fatalf("search matched %q, want 10.1/aaa", sID)
	}

	grepped := runCmd(t, c, `{"command":"grep","pattern":"folding"}`)
	if int(grepped["count"].(float64)) != 1 {
		t.Fatalf("grep count = %v, want 1", grepped["count"])
	}
	gPapers := grepped["papers"].([]any)
	gID := gPapers[0].(map[string]any)["id"].(string)
	if gID != sID {
		t.Fatalf("grep matched %q but search matched %q — inconsistent", gID, sID)
	}
}

func TestCorpusGrepIgnoreCase(t *testing.T) {
	c, _ := newTestCorpus(t, map[string]string{
		"10.1/aaa": "ALPHA helix folding",
	})
	runCmd(t, c, `{"command":"add","paper_ids":["10.1/aaa"]}`)

	out := runCmd(t, c, `{"command":"grep","pattern":"alpha","ignore_case":true}`)
	if int(out["count"].(float64)) != 1 {
		t.Fatalf("grep ignore_case count = %v, want 1", out["count"])
	}
	out = runCmd(t, c, `{"command":"grep","pattern":"alpha"}`)
	if int(out["count"].(float64)) != 0 {
		t.Fatalf("grep case-sensitive count = %v, want 0", out["count"])
	}
}

func TestCorpusMap(t *testing.T) {
	c, _ := newTestCorpus(t, map[string]string{
		"10.1/aaa": "alpha helix folding kinetics",
		"10.1/bbb": "beta sheet aggregation propensity",
	})
	runCmd(t, c, `{"command":"add","paper_ids":["10.1/aaa","10.1/bbb"]}`)

	out := runCmd(t, c, `{"command":"map","prompt":"summarize"}`)
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

func TestCorpusReduce(t *testing.T) {
	c, _ := newTestCorpus(t, nil)
	out := runCmd(t, c, `{"command":"reduce","prompt":"combine","map_results":[{"paper_id":"x","answer":"a1"},{"paper_id":"y","answer":"a2"}]}`)
	if out["summary"].(string) != "fixed answer" {
		t.Fatalf("summary = %q, want 'fixed answer'", out["summary"])
	}
}

func TestCorpusRead(t *testing.T) {
	c, _ := newTestCorpus(t, map[string]string{
		"10.1/aaa": "alpha helix folding kinetics",
	})
	runCmd(t, c, `{"command":"add","paper_ids":["10.1/aaa"]}`)
	out := runCmd(t, c, `{"command":"read","paper_id":"10.1/aaa"}`)
	if out["full_text"].(string) != "alpha helix folding kinetics" {
		t.Fatalf("read full_text = %q", out["full_text"])
	}
	if out["paper_id"].(string) != "10.1/aaa" {
		t.Fatalf("read paper_id = %q", out["paper_id"])
	}
}

func TestCorpusRemove(t *testing.T) {
	c, _ := newTestCorpus(t, map[string]string{
		"10.1/aaa": "alpha helix folding kinetics",
		"10.1/bbb": "beta sheet aggregation propensity",
	})
	runCmd(t, c, `{"command":"add","paper_ids":["10.1/aaa","10.1/bbb"]}`)
	out := runCmd(t, c, `{"command":"remove","paper_ids":["10.1/aaa"]}`)
	if int(out["removed"].(float64)) != 1 {
		t.Fatalf("removed = %v, want 1", out["removed"])
	}
	listed := runCmd(t, c, `{"command":"list"}`)
	if int(listed["count"].(float64)) != 1 {
		t.Fatalf("list count after remove = %v, want 1", listed["count"])
	}
	// removed paper no longer found by search
	searched := runCmd(t, c, `{"command":"search","query":"folding"}`)
	if int(searched["count"].(float64)) != 0 {
		t.Fatalf("search count after remove = %v, want 0", searched["count"])
	}
}

func TestCorpusAddFromSearch(t *testing.T) {
	c, res := newTestCorpus(t, map[string]string{
		"10.1/aaa": "alpha helix folding kinetics",
	})
	rid := res.Put("europepmc", []Paper{
		{ID: "10.1/aaa", Title: "Folding paper", Source: "europepmc"},
	})
	out := runCmd(t, c, `{"command":"add","from_search":"`+rid+`"}`)
	if int(out["added"].(float64)) != 1 {
		t.Fatalf("added from_search = %v, want 1", out["added"])
	}
}

func TestCorpusUnknownCommand(t *testing.T) {
	c, _ := newTestCorpus(t, nil)
	if _, err := c.Execute(context.Background(), json.RawMessage(`{"command":"bogus"}`)); err == nil {
		t.Fatal("expected error for unknown command")
	}
}

// TestCorpusAddFallsBackToAbstract verifies that a paper whose FetchText
// returns empty is still stored and indexed using its abstract, so it stays
// searchable, greppable and readable.
func TestCorpusAddFallsBackToAbstract(t *testing.T) {
	c, res := newTestCorpus(t, map[string]string{
		"10.1/abs": "", // FetchText returns ("", nil)
	})
	rid := res.Put("europepmc", []Paper{
		{ID: "10.1/abs", Title: "Folding paper", Source: "europepmc",
			Abstract: "alpha helix folding kinetics"},
	})
	out := runCmd(t, c, `{"command":"add","from_search":"`+rid+`"}`)
	if int(out["added"].(float64)) != 1 {
		t.Fatalf("added = %v, want 1", out["added"])
	}

	read := runCmd(t, c, `{"command":"read","paper_id":"10.1/abs"}`)
	if read["full_text"].(string) != "alpha helix folding kinetics" {
		t.Fatalf("read full_text = %q, want the abstract", read["full_text"])
	}

	grepped := runCmd(t, c, `{"command":"grep","pattern":"folding"}`)
	if int(grepped["count"].(float64)) != 1 {
		t.Fatalf("grep count = %v, want 1", grepped["count"])
	}

	searched := runCmd(t, c, `{"command":"search","query":"folding"}`)
	if int(searched["count"].(float64)) != 1 {
		t.Fatalf("search count = %v, want 1", searched["count"])
	}
}

// TestCorpusClose verifies Close is nil-safe before the index is opened and
// after an add (which lazily opens the index).
func TestCorpusClose(t *testing.T) {
	c, _ := newTestCorpus(t, nil)
	if err := c.Close(); err != nil {
		t.Fatalf("Close on fresh corpus: %v", err)
	}

	c2, _ := newTestCorpus(t, map[string]string{"10.1/aaa": "alpha helix"})
	runCmd(t, c2, `{"command":"add","paper_ids":["10.1/aaa"]}`)
	if err := c2.Close(); err != nil {
		t.Fatalf("Close after add: %v", err)
	}
}
