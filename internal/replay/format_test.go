package replay

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func sampleDocument() *Document {
	ts := time.Date(2026, 5, 20, 12, 34, 56, 0, time.UTC)
	return &Document{
		SessionID: "s_0001",
		Started:   ts,
		Model:     "claude-opus-4-7",
		Events: []Event{
			{Kind: KindUserMsg, TS: ts, Text: "fold MAQ"},
			{Kind: KindAgentText, TS: ts.Add(time.Second), Text: "I'll fold that."},
			{Kind: KindToolStart, TS: ts.Add(time.Second), Name: "fold.esmfold", Input: json.RawMessage(`{"sequence":"MAQ"}`)},
			{Kind: KindToolResult, TS: ts.Add(5 * time.Second), Name: "fold.esmfold", Display: "folded (pLDDT 80)"},
			{Kind: KindTurnDone, TS: ts.Add(5 * time.Second)},
		},
	}
}

func TestDocumentRoundTrip(t *testing.T) {
	src := sampleDocument()
	path := filepath.Join(t.TempDir(), "session.json")
	if err := src.Write(path); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got, err := LoadDocument(path)
	if err != nil {
		t.Fatalf("LoadDocument: %v", err)
	}
	if got.SessionID != src.SessionID || got.Model != src.Model {
		t.Fatalf("header mismatch: %+v", got)
	}
	if !got.Started.Equal(src.Started) {
		t.Fatalf("Started mismatch: got %v want %v", got.Started, src.Started)
	}
	if len(got.Events) != len(src.Events) {
		t.Fatalf("event count = %d, want %d", len(got.Events), len(src.Events))
	}
	for i := range src.Events {
		if got.Events[i].Kind != src.Events[i].Kind {
			t.Errorf("event %d kind = %q, want %q", i, got.Events[i].Kind, src.Events[i].Kind)
		}
		if got.Events[i].Text != src.Events[i].Text {
			t.Errorf("event %d text = %q, want %q", i, got.Events[i].Text, src.Events[i].Text)
		}
		if got.Events[i].Name != src.Events[i].Name {
			t.Errorf("event %d name = %q, want %q", i, got.Events[i].Name, src.Events[i].Name)
		}
		if string(got.Events[i].Input) != string(src.Events[i].Input) {
			t.Errorf("event %d input = %q, want %q", i, got.Events[i].Input, src.Events[i].Input)
		}
	}
}

func TestLoadDocumentRejectsMissingFile(t *testing.T) {
	if _, err := LoadDocument(filepath.Join(t.TempDir(), "nope.json")); err == nil {
		t.Fatal("expected an error for a missing file")
	}
}

func TestLoadDocumentRejectsUnknownKind(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.json")
	body := []byte(`{"session_id":"x","started":"2026-05-20T00:00:00Z","events":[{"kind":"banana","ts":"2026-05-20T00:00:00Z"}]}`)
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadDocument(path); err == nil {
		t.Fatal("expected an error for an unknown event kind")
	}
}

func TestLoadDocumentRejectsMalformedJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadDocument(path); err == nil {
		t.Fatal("expected an error for malformed JSON")
	}
}
