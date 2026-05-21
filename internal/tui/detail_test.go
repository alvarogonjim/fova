package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestJobLogReadLog(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "job.log")
	want := "line1\nline2\nline3\n"
	if err := os.WriteFile(path, []byte(want), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	if got := readLog(path); got != want {
		t.Errorf("readLog(existing) = %q, want %q", got, want)
	}

	if got := readLog(filepath.Join(dir, "missing.log")); got != "" {
		t.Errorf("readLog(missing) = %q, want \"\"", got)
	}

	if got := readLog(""); got != "" {
		t.Errorf("readLog(empty path) = %q, want \"\"", got)
	}
}

func TestJobLogTailLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "job.log")
	var sb strings.Builder
	for i := 1; i <= 10; i++ {
		sb.WriteString("line")
		sb.WriteByte(byte('0' + i%10))
		sb.WriteByte('\n')
	}
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	got := tailLines(path, 3)
	want := []string{"line8", "line9", "line0"}
	if len(got) != len(want) {
		t.Fatalf("tailLines(n=3) = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("tailLines[%d] = %q, want %q", i, got[i], want[i])
		}
	}

	if got := tailLines(filepath.Join(dir, "missing.log"), 3); len(got) != 0 {
		t.Errorf("tailLines(missing) = %v, want empty", got)
	}

	if got := tailLines("", 3); len(got) != 0 {
		t.Errorf("tailLines(empty path) = %v, want empty", got)
	}
}

func TestJobLogView(t *testing.T) {
	v := newDetailView(NewTheme())
	v.setSize(80, 20)
	v.setContent("install bindcraft", "line1\nline2")

	out := v.View()
	if !strings.Contains(out, "install bindcraft") {
		t.Errorf("View() missing header; got:\n%s", out)
	}
	if !strings.Contains(out, "line1") {
		t.Errorf("View() missing body line; got:\n%s", out)
	}
}

func TestJobLogViewUpdateScroll(t *testing.T) {
	v := newDetailView(NewTheme())
	v.setSize(40, 5)
	var sb strings.Builder
	for i := 0; i < 50; i++ {
		sb.WriteString("row\n")
	}
	v.setContent("hdr", sb.String())

	// Routing a scroll key through update returns a detailView without panicking.
	v = v.update(tea.KeyMsg{Type: tea.KeyPgDown})
	if got := v.View(); !strings.Contains(got, "hdr") {
		t.Errorf("View() after scroll missing header; got:\n%s", got)
	}
}
