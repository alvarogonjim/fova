package tui

import (
	"strings"
	"testing"
)

func TestSlashMenuSetFilterNarrows(t *testing.T) {
	m := newSlashMenu()
	m.setFilter("mode")
	if len(m.entries) != 1 {
		t.Fatalf("setFilter(\"mode\"): got %d entries, want 1", len(m.entries))
	}
	if m.entries[0].Label != "/model" {
		t.Fatalf("setFilter(\"mode\"): got %q, want \"/model\"", m.entries[0].Label)
	}
}

func TestSlashMenuSetFilterEmptyShowsAll(t *testing.T) {
	m := newSlashMenu()
	m.setFilter("")
	if len(m.entries) != len(slashCommands) {
		t.Fatalf("setFilter(\"\"): got %d entries, want %d", len(m.entries), len(slashCommands))
	}
}

func TestSlashMenuNextPrevClamp(t *testing.T) {
	m := newSlashMenu()
	m.setFilter("")

	// prev at the top stays at 0.
	m.prev()
	if m.cur != 0 {
		t.Fatalf("prev() at top: cur=%d, want 0", m.cur)
	}

	// next stops at the last entry.
	for i := 0; i < len(m.entries)+5; i++ {
		m.next()
	}
	if m.cur != len(m.entries)-1 {
		t.Fatalf("next() at bottom: cur=%d, want %d", m.cur, len(m.entries)-1)
	}

	// prev walks back up and clamps at 0.
	for i := 0; i < len(m.entries)+5; i++ {
		m.prev()
	}
	if m.cur != 0 {
		t.Fatalf("prev() to top: cur=%d, want 0", m.cur)
	}
}

func TestSlashMenuNextPrevEmptyNoPanic(t *testing.T) {
	m := newSlashMenu()
	m.setFilter("zzz")
	// Must not panic on an empty list.
	m.next()
	m.prev()
	if m.cur != 0 {
		t.Fatalf("empty list: cur=%d, want 0", m.cur)
	}
}

func TestSlashMenuNoMatchNotVisible(t *testing.T) {
	m := newSlashMenu()
	m.setFilter("zzz")
	if m.visible() {
		t.Fatalf("setFilter(\"zzz\"): visible()=true, want false")
	}
	if _, ok := m.selected(); ok {
		t.Fatalf("setFilter(\"zzz\"): selected() ok=true, want false")
	}
}

func TestSlashMenuVisibleWhenMatches(t *testing.T) {
	m := newSlashMenu()
	m.setFilter("mo")
	if !m.visible() {
		t.Fatalf("setFilter(\"mo\"): visible()=false, want true")
	}
	got, ok := m.selected()
	if !ok {
		t.Fatalf("setFilter(\"mo\"): selected() ok=false, want true")
	}
	if got.Label != "/model" {
		t.Fatalf("setFilter(\"mo\"): selected()=%q, want \"/model\"", got.Label)
	}
}

func TestSlashMenuViewContainsCommand(t *testing.T) {
	m := newSlashMenu()
	m.setFilter("")
	out := m.view(NewTheme(), 60)
	if !strings.Contains(out, "/model") {
		t.Fatalf("view: missing \"/model\"\n%s", out)
	}
	if !strings.Contains(out, "switch the model") {
		t.Fatalf("view: missing model description\n%s", out)
	}
}
