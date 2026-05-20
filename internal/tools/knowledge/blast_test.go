package knowledge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alvarogonjim/fova/internal/tools"
)

var _ tools.Tool = (*BLAST)(nil)

// blastFakeServer returns an httptest server that:
//   - replies to POST CMD=Put with a tiny HTML fragment containing RID = FAKE123
//   - replies to GET CMD=Get with HTML "Status=WAITING" the first `waits` times,
//     then a canned JSON2_S body.
func blastFakeServer(waits int) (*httptest.Server, *int32) {
	var polls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cmd := r.FormValue("CMD")
		switch cmd {
		case "Put":
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte(`<html><body><!-- QBlastInfoBegin
    RID = FAKE123
    RTOE = 12
QBlastInfoEnd
--></body></html>`))
		case "Get":
			n := atomic.AddInt32(&polls, 1)
			if int(n) <= waits {
				w.Header().Set("Content-Type", "text/html")
				_, _ = w.Write([]byte(`<html>QBlastInfo Status=WAITING</html>`))
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
  "BlastOutput2": [
    {"report": {"results": {"search": {"hits": [
      {"description": [{"accession": "P12345", "title": "Hit one"}],
       "hsps": [{"evalue": 1.0e-30, "identity": 200, "align_len": 220}]},
      {"description": [{"accession": "Q67890", "title": "Hit two"}],
       "hsps": [{"evalue": 1.0e-10, "identity": 100, "align_len": 220}]}
    ]}}}}
  ]
}`))
		default:
			http.Error(w, "unknown CMD", http.StatusBadRequest)
		}
	}))
	return srv, &polls
}

func TestBLASTSubmitAndPoll(t *testing.T) {
	srv, polls := blastFakeServer(2)
	defer srv.Close()

	tool := NewBLAST()
	tool.BaseURL = srv.URL
	tool.PollInterval = 1 * time.Millisecond
	tool.MaxWait = 5 * time.Second

	out, err := tool.Execute(context.Background(),
		json.RawMessage(`{"sequence":"MKTAYIAKQRQISFVKSHFSRQLEERLGLIEVQAPILSRVGDGTQDNLSGAEK","max_hits":2}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var parsed struct {
		RID   string `json:"rid"`
		Count int    `json:"count"`
		Hits  []struct {
			Accession string  `json:"accession"`
			EValue    float64 `json:"evalue"`
			Identity  float64 `json:"identity_pct"`
		} `json:"hits"`
	}
	if err := json.Unmarshal(out.Output, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if parsed.RID != "FAKE123" {
		t.Errorf("RID = %q, want FAKE123", parsed.RID)
	}
	if parsed.Count != 2 || len(parsed.Hits) != 2 {
		t.Fatalf("hits = %d/%d, want 2/2", parsed.Count, len(parsed.Hits))
	}
	if parsed.Hits[0].Accession != "P12345" {
		t.Errorf("hit[0].Accession = %q", parsed.Hits[0].Accession)
	}
	if parsed.Hits[0].Identity == 0 {
		t.Errorf("hit[0].Identity should be derived from identity/align_len")
	}
	if atomic.LoadInt32(polls) < 3 {
		t.Errorf("polls = %d, want >=3 (two WAITING + one ready)", atomic.LoadInt32(polls))
	}
	if !strings.Contains(out.Display, "FAKE123") {
		t.Errorf("Display should include the RID, got %q", out.Display)
	}
}

func TestBLASTRespectsContextCancel(t *testing.T) {
	srv, _ := blastFakeServer(1000) // never returns JSON
	defer srv.Close()

	tool := NewBLAST()
	tool.BaseURL = srv.URL
	tool.PollInterval = 5 * time.Millisecond
	tool.MaxWait = 5 * time.Second

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	_, err := tool.Execute(ctx, json.RawMessage(`{"sequence":"MKT"}`))
	if err == nil {
		t.Fatal("expected an error on context cancellation")
	}
	if !strings.Contains(err.Error(), "context") {
		t.Errorf("err = %v, expected a context error", err)
	}
}

func TestBLASTRejectsEmptySequence(t *testing.T) {
	tool := NewBLAST()
	if _, err := tool.Execute(context.Background(), json.RawMessage(`{}`)); err == nil {
		t.Fatal("expected an error for empty sequence")
	}
}

func TestBLASTRejectsBadProgram(t *testing.T) {
	tool := NewBLAST()
	_, err := tool.Execute(context.Background(),
		json.RawMessage(`{"sequence":"MKT","program":"bogus"}`))
	if err == nil {
		t.Fatal("expected an error for an unknown program")
	}
}
