package transport

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// TestDefaultSinkWritesJSONL is the headline test from the plan: a fresh sink
// pointed at a temp path writes one JSON object per line, parseable with
// encoding/json.
func TestDefaultSinkWritesJSONL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "telemetry.jsonl")

	sink := newSink(path)
	sink(Event{Tool: "pmc", URL: "http://example/q", Status: 200, Attempt: 0, DurationMS: 42})
	sink(Event{Tool: "pdb", URL: "http://example/1LYZ", Status: 503, Attempt: 0, DurationMS: 9})

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d: %q", len(lines), string(raw))
	}

	var first Event
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatalf("line 0 not valid JSON: %v (%q)", err, lines[0])
	}
	if first.Tool != "pmc" {
		t.Errorf("first tool = %q, want pmc", first.Tool)
	}
	if first.Status != 200 {
		t.Errorf("first status = %d, want 200", first.Status)
	}

	var second Event
	if err := json.Unmarshal([]byte(lines[1]), &second); err != nil {
		t.Fatalf("line 1 not valid JSON: %v (%q)", err, lines[1])
	}
	if second.Tool != "pdb" || second.Status != 503 {
		t.Errorf("second event = %+v, want pdb/503", second)
	}
}

// TestSinkAppendsAcrossOpens proves the second sink construction on the same
// path does NOT truncate the file written by the first.
func TestSinkAppendsAcrossOpens(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "telemetry.jsonl")

	first := newSink(path)
	first(Event{Tool: "pmc", URL: "http://example/a", Status: 200})

	second := newSink(path)
	second(Event{Tool: "pdb", URL: "http://example/b", Status: 200})

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines after re-open, got %d: %q", len(lines), string(raw))
	}
}

// TestSinkCreatesMissingDirectories proves DefaultSink-style paths whose parent
// directory does not yet exist are still written successfully (the sink mkdirs
// on demand). The first call must create the .fova/logs subtree.
func TestSinkCreatesMissingDirectories(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "deeper", "telemetry.jsonl")

	sink := newSink(path)
	sink(Event{Tool: "pmc", Status: 200})

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file not written under missing parents: %v", err)
	}
}

// TestSinkConcurrentWritesAreSerialised fires N goroutines at one sink and
// verifies every line is a complete, parseable Event — i.e. the mutex prevents
// bytes from interleaving.
func TestSinkConcurrentWritesAreSerialised(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "telemetry.jsonl")
	sink := newSink(path)

	const writers = 16
	const perWriter = 32

	var wg sync.WaitGroup
	wg.Add(writers)
	for i := 0; i < writers; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < perWriter; j++ {
				sink(Event{Tool: "pmc", URL: "http://example", Status: 200, Attempt: id, DurationMS: int64(j)})
			}
		}(i)
	}
	wg.Wait()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	want := writers * perWriter
	if len(lines) != want {
		t.Fatalf("lines = %d, want %d", len(lines), want)
	}
	for i, line := range lines {
		var e Event
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatalf("line %d not valid JSON (interleaved write?): %v: %q", i, err, line)
		}
	}
}

// TestSinkSwallowsErrors proves the sink does not panic when it cannot write —
// e.g. the path points at a directory rather than a file. Telemetry must never
// crash the caller.
func TestSinkSwallowsErrors(t *testing.T) {
	dir := t.TempDir()
	// Path points at the directory itself; OpenFile will fail.
	sink := newSink(dir)

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("sink panicked on unwritable path: %v", r)
		}
	}()
	sink(Event{Tool: "pmc", Status: 200})
}

// TestDefaultSinkResolvesUnderHome proves DefaultSink() returns a non-nil
// handler even when invoked from the test harness — i.e. os.UserHomeDir
// resolution does not block construction. We do not write the real file here;
// we only check that calling the returned function is safe and non-panicking.
// The handler may silently no-op if the home dir is unwritable, which is the
// documented behaviour.
func TestDefaultSinkConstructs(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	sink := DefaultSink()
	if sink == nil {
		t.Fatal("DefaultSink() returned nil")
	}
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("DefaultSink handler panicked: %v", r)
		}
	}()
	sink(Event{Tool: "pmc", Status: 200})
}
