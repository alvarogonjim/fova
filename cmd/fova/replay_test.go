package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alvarogonjim/fova/internal/replay"
)

func writeSampleDoc(t *testing.T) string {
	t.Helper()
	ts := time.Date(2026, 5, 20, 12, 34, 56, 0, time.UTC)
	doc := &replay.Document{
		SessionID: "s_dry",
		Started:   ts,
		Model:     "claude-opus-4-7",
		Events: []replay.Event{
			{Kind: replay.KindUserMsg, TS: ts, Text: "fold MAQ"},
			{Kind: replay.KindAgentText, TS: ts.Add(time.Second), Text: "I'll fold that."},
			{Kind: replay.KindToolStart, TS: ts.Add(time.Second), Name: "fold.esmfold", Input: json.RawMessage(`{"sequence":"MAQ"}`)},
			{Kind: replay.KindToolResult, TS: ts.Add(5 * time.Second), Name: "fold.esmfold", Display: "folded (pLDDT 80)"},
			{Kind: replay.KindTurnDone, TS: ts.Add(5 * time.Second)},
		},
	}
	path := filepath.Join(t.TempDir(), "session.json")
	if err := doc.Write(path); err != nil {
		t.Fatalf("Write: %v", err)
	}
	return path
}

func TestRunReplayDryEmitsOneLinePerEvent(t *testing.T) {
	path := writeSampleDoc(t)
	var buf bytes.Buffer
	if err := runReplayDry(&buf, path); err != nil {
		t.Fatalf("runReplayDry: %v", err)
	}
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 5 {
		t.Fatalf("got %d lines, want 5:\n%s", len(lines), buf.String())
	}
	for i, want := range []string{"user_msg", "agent_text", "tool_start", "tool_result", "turn_done"} {
		if !strings.Contains(lines[i], want) {
			t.Errorf("line %d = %q, want it to contain %q", i, lines[i], want)
		}
	}
	if !strings.Contains(lines[2], "fold.esmfold") {
		t.Errorf("tool_start line missing tool name: %q", lines[2])
	}
}

func TestRunReplayDryRejectsMissingFile(t *testing.T) {
	var buf bytes.Buffer
	if err := runReplayDry(&buf, filepath.Join(t.TempDir(), "nope.json")); err == nil {
		t.Fatal("expected an error for a missing file")
	}
}
