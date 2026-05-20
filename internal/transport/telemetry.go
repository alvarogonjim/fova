package transport

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// DefaultSink returns an event handler that appends one JSON object per line
// to ~/.fova/logs/telemetry.jsonl. Errors are swallowed — telemetry must
// never crash the caller.
//
// Production constructors install this via transport.WithEvent(transport.DefaultSink()).
// Tests should install their own capture hook instead and use newSink against
// a t.TempDir path when they need to exercise file IO.
func DefaultSink() func(Event) {
	home, _ := os.UserHomeDir()
	return newSink(filepath.Join(home, ".fova", "logs", "telemetry.jsonl"))
}

// newSink builds a JSONL event handler writing to path. The returned closure
// is safe for concurrent use: a mutex serialises file writes so bytes from
// concurrent callers cannot interleave inside a single line. All errors
// (mkdir, open, marshal, write) are silently dropped — see the package-level
// contract that telemetry is best-effort and never propagates.
func newSink(path string) func(Event) {
	var mu sync.Mutex
	return func(e Event) {
		mu.Lock()
		defer mu.Unlock()

		// Create the containing directory on demand. If this fails (e.g.
		// permission denied) the subsequent OpenFile will also fail and we
		// will return without writing.
		_ = os.MkdirAll(filepath.Dir(path), 0o755)

		f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return
		}
		defer f.Close()

		raw, err := json.Marshal(e)
		if err != nil {
			return
		}
		// One JSON object per line. The newline is appended separately so the
		// caller can grep / jq -c the file directly.
		_, _ = f.Write(raw)
		_, _ = f.Write([]byte{'\n'})
	}
}
