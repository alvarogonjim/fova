package backends

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alvarogonjim/fova/internal/backends/modal"
)

func TestSelectLocal(t *testing.T) {
	b, err := Select("local", t.TempDir())
	if err != nil {
		t.Fatalf("Select local: %v", err)
	}
	if b.Name() != "local" {
		t.Errorf("Name = %q, want local", b.Name())
	}
}

func TestSelectModal(t *testing.T) {
	b, err := Select("modal", t.TempDir())
	if err != nil {
		t.Fatalf("Select modal: %v", err)
	}
	if b.Name() != "modal" {
		t.Errorf("Name = %q, want modal", b.Name())
	}
}

func TestSelectDefaultsToLocal(t *testing.T) {
	b, err := Select("", t.TempDir())
	if err != nil || b.Name() != "local" {
		t.Fatalf("empty backend should default to local: %v / %v", b, err)
	}
}

func TestSelectUnknown(t *testing.T) {
	if _, err := Select("nonsense", t.TempDir()); err == nil {
		t.Error("an unknown backend name should error")
	}
}

func TestLocalBackendRunNoAdapterIsClear(t *testing.T) {
	b, err := Select("local", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_, err = b.Run(context.Background(), "design.nonesuch", []byte(`{}`), io.Discard, nil)
	if err == nil || !strings.Contains(err.Error(), "no local adapter") {
		t.Fatalf("want a 'no local adapter' error, got: %v", err)
	}
}

func TestLocalBackendRunReachesProteinMPNNAdapter(t *testing.T) {
	b, err := Select("local", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// A bad target makes the adapter fail fast — but the error must be the
	// adapter's, proving dispatch reached design.proteinmpnn (not "no adapter").
	_, err = b.Run(context.Background(), "design.proteinmpnn", []byte(`{"target":"/no/such/file.pdb"}`), io.Discard, nil)
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), "no local adapter") {
		t.Fatalf("design.proteinmpnn should dispatch to its adapter, got: %v", err)
	}
}

// TestModalBackendRunStreamsLogAndTicksProgress is the Bug 6 contract test:
// on a successful HTTP round-trip, modalBackend.Run must write a dispatch
// line to log, tick progress to 0.05 before the call, tick to 0.9 after,
// and write the returned payload to log.
func TestModalBackendRunStreamsLogAndTicksProgress(t *testing.T) {
	const responseBody = `{"exit_code":0,"stdout":"ok"}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(responseBody))
	}))
	defer srv.Close()

	b := &modalBackend{client: modal.NewClient(srv.URL)}
	var log bytes.Buffer
	var ticks []float64
	out, err := b.Run(context.Background(), "fold.boltz2",
		[]byte(`{"sequences":{"A":"MAQVQL"}}`),
		&log, func(f float64) { ticks = append(ticks, f) })
	if err != nil {
		t.Fatalf("modalBackend.Run: %v", err)
	}
	if string(out) != responseBody {
		t.Errorf("Run returned %q, want %q", out, responseBody)
	}
	if !strings.Contains(log.String(), "modal: dispatching fold.boltz2") {
		t.Errorf("log missing dispatch line, got: %q", log.String())
	}
	if !strings.Contains(log.String(), responseBody) {
		t.Errorf("log missing response payload, got: %q", log.String())
	}
	if len(ticks) != 2 || ticks[0] != 0.05 || ticks[1] != 0.9 {
		t.Errorf("progress ticks = %v, want [0.05 0.9]", ticks)
	}
}

// TestModalBackendRunReportsFailureToLog: on an HTTP error, the backend
// records the failure to the log and propagates the error.
func TestModalBackendRunReportsFailureToLog(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	b := &modalBackend{client: modal.NewClient(srv.URL)}
	var log bytes.Buffer
	var ticks []float64
	_, err := b.Run(context.Background(), "fold.boltz2", []byte(`{}`),
		&log, func(f float64) { ticks = append(ticks, f) })
	if err == nil {
		t.Fatal("expected an error on a 500 response")
	}
	if !strings.Contains(log.String(), "modal: dispatching fold.boltz2") {
		t.Errorf("log should still hold the dispatch line, got: %q", log.String())
	}
	if !strings.Contains(log.String(), "failed") {
		t.Errorf("log should hold a failure line, got: %q", log.String())
	}
	// Only the pre-dispatch 0.05 tick should fire on failure.
	if len(ticks) != 1 || ticks[0] != 0.05 {
		t.Errorf("progress ticks = %v, want [0.05] (no 0.9 on failure)", ticks)
	}
}

// TestModalBackendRunNilLogAndProgress confirms the backend tolerates nil
// log and progress (per the Backend interface contract).
func TestModalBackendRunNilLogAndProgress(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	b := &modalBackend{client: modal.NewClient(srv.URL)}
	if _, err := b.Run(context.Background(), "fold.boltz2", []byte(`{}`), nil, nil); err != nil {
		t.Fatalf("Run with nil log/progress: %v", err)
	}
}
