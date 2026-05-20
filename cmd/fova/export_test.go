package main

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/alvarogonjim/fova/internal/domain"
	"github.com/alvarogonjim/fova/internal/replay"
	"github.com/alvarogonjim/fova/internal/store"
)

// seedExportSession writes a session with one user turn, one tool call,
// and a terminating assistant text into a fresh store.
func seedExportSession(t *testing.T) (*store.Store, domain.SessionID) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "workspace.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	created := time.Date(2026, 5, 20, 12, 34, 56, 0, time.UTC)
	sess := domain.Session{
		ID: "s_export", ProjectID: store.DefaultProjectID,
		Created: created, Updated: created,
		Model: "claude-opus-4-7", Provider: "anthropic",
	}
	if err := st.InsertSession(sess); err != nil {
		t.Fatalf("InsertSession: %v", err)
	}
	msgs := []domain.Message{
		{ID: "m1", SessionID: "s_export", Role: "user", Content: "fold MAQ", Created: created},
		{ID: "m2", SessionID: "s_export", Role: "assistant", Content: "I'll fold that.",
			ToolCalls: []domain.ToolCall{{ID: "tc1", Name: "fold.esmfold", Input: json.RawMessage(`{"sequence":"MAQ"}`)}},
			Created:   created.Add(time.Second)},
		{ID: "m3", SessionID: "s_export", Role: "tool", Content: "folded (pLDDT 80)",
			ToolCallID: "tc1", Created: created.Add(2 * time.Second)},
		{ID: "m4", SessionID: "s_export", Role: "assistant", Content: "done.", Created: created.Add(3 * time.Second)},
	}
	for _, m := range msgs {
		if err := st.InsertMessage(m); err != nil {
			t.Fatalf("InsertMessage: %v", err)
		}
	}
	return st, sess.ID
}

func TestRunExportRoundTrip(t *testing.T) {
	st, sid := seedExportSession(t)
	out := filepath.Join(t.TempDir(), "session.json")
	if err := runExport(st, string(sid), out); err != nil {
		t.Fatalf("runExport: %v", err)
	}
	doc, err := replay.LoadDocument(out)
	if err != nil {
		t.Fatalf("LoadDocument: %v", err)
	}
	if doc.SessionID != string(sid) {
		t.Errorf("SessionID = %q, want %q", doc.SessionID, sid)
	}
	if doc.Model != "claude-opus-4-7" {
		t.Errorf("Model = %q, want %q", doc.Model, "claude-opus-4-7")
	}
	wantKinds := []replay.Kind{
		replay.KindUserMsg,
		replay.KindAgentText,
		replay.KindToolStart,
		replay.KindToolResult,
		replay.KindAgentText,
		replay.KindTurnDone,
	}
	if len(doc.Events) != len(wantKinds) {
		t.Fatalf("event count = %d, want %d (%+v)", len(doc.Events), len(wantKinds), doc.Events)
	}
	for i, k := range wantKinds {
		if doc.Events[i].Kind != k {
			t.Errorf("event %d kind = %q, want %q", i, doc.Events[i].Kind, k)
		}
	}
	if doc.Events[2].Name != "fold.esmfold" {
		t.Errorf("tool_start name = %q, want fold.esmfold", doc.Events[2].Name)
	}
	if string(doc.Events[2].Input) != `{"sequence":"MAQ"}` {
		t.Errorf("tool_start input = %q, want %q", doc.Events[2].Input, `{"sequence":"MAQ"}`)
	}
	if doc.Events[3].Display != "folded (pLDDT 80)" {
		t.Errorf("tool_result display = %q, want %q", doc.Events[3].Display, "folded (pLDDT 80)")
	}
}

func TestRunExportUnknownSession(t *testing.T) {
	st, _ := seedExportSession(t)
	out := filepath.Join(t.TempDir(), "session.json")
	if err := runExport(st, "s_missing", out); err == nil {
		t.Fatal("expected an error for an unknown session id")
	}
}
