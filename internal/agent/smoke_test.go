package agent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/alvarogonjim/fova/internal/llm"
	"github.com/alvarogonjim/fova/internal/tools"
	"github.com/alvarogonjim/fova/internal/tools/fold"
)

// samplePDB has three CA atoms with B-factors 90/80/70 → mean 80, min 70.
const samplePDB = `ATOM      1  N   MET A   1      10.000  10.000  10.000  1.00 50.00
ATOM      2  CA  MET A   1      11.000  12.000  13.000  1.00 90.00
ATOM      3  CA  GLY A   2      14.000  15.000  16.000  1.00 80.00
ATOM      4  CA  SER A   3      17.000  18.000  19.000  1.00 70.00
TER
END
`

func TestSmoke_FoldAndScore(t *testing.T) {
	// Fixture ESM Atlas server.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(samplePDB))
	}))
	defer srv.Close()

	// Registry with fold.esmfold pointed at the fixture.
	reg := tools.NewRegistry()
	esm := fold.NewESMFold(t.TempDir())
	esm.Endpoint = srv.URL
	reg.Register(esm)

	// Mock LLM: turn 1 calls fold.esmfold; turn 2 finishes.
	prov := &mockProvider{responses: []llm.ChatResponse{
		{ToolCalls: []llm.ToolCall{{
			ID:    "c1",
			Name:  "fold.esmfold",
			Input: map[string]any{"sequence": "MGS"},
		}}},
		{Text: "Folded. pLDDT mean 80.0.", StopReason: "end_turn"},
	}}

	bus := make(chan tea.Msg, 64)
	loop := NewLoop(prov, "mock", reg, NewSession(BuildSystemPrompt(nil, fakeTemplate)), bus,
		func(string) bool { return true })

	go func() {
		loop.Run(context.Background(), "fold MGS")
		close(bus)
	}()

	var foldStarted, turnDone bool
	var plddtSeen bool
	for m := range bus {
		switch v := m.(type) {
		case ToolStartMsg:
			if v.Name == "fold.esmfold" {
				foldStarted = true
			}
		case ToolDoneMsg:
			if v.Name == "fold.esmfold" {
				if v.Err != nil {
					t.Fatalf("fold.esmfold failed: %v", v.Err)
				}
				if strings.Contains(strings.ToLower(v.Display), "plddt") {
					plddtSeen = true
				}
			}
		case TurnDoneMsg:
			turnDone = true
		case TurnErrorMsg:
			t.Fatalf("turn error: %v", v.Err)
		}
	}

	if !foldStarted {
		t.Error("fold.esmfold was never invoked")
	}
	if !plddtSeen {
		t.Error("fold.esmfold result did not report pLDDT")
	}
	if !turnDone {
		t.Error("agent turn did not complete")
	}
}
