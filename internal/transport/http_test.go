package transport

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// fastBackoff replaces the 1s/3s/9s schedule with near-zero delays so tests
// run in milliseconds instead of seconds.
func fastBackoff(int) time.Duration { return time.Millisecond }

func TestRetryThen200(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := atomic.AddInt32(&hits, 1)
		if n == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	var events []Event
	c := New(WithBackoff(fastBackoff), WithEvent(func(e Event) { events = append(events, e) }))

	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	resp, err := c.Do(context.Background(), req, "test")
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("final status = %d, want 200", resp.StatusCode)
	}
	if got := atomic.LoadInt32(&hits); got != 2 {
		t.Errorf("hits = %d, want 2 (one 503 then one 200)", got)
	}
	if len(events) != 2 {
		t.Fatalf("events = %d, want 2", len(events))
	}
	if events[0].Status != 503 || events[1].Status != 200 {
		t.Errorf("event statuses = %d %d", events[0].Status, events[1].Status)
	}
	if events[0].Tool != "test" {
		t.Errorf("event tool = %q", events[0].Tool)
	}
}

func TestNoRetryOn400(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	c := New(WithBackoff(fastBackoff))
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	resp, err := c.Do(context.Background(), req, "test")
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Errorf("hits = %d, want 1 (4xx must not retry)", got)
	}
}

func TestRetriesExhausted(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	c := New(WithBackoff(fastBackoff), WithRetries(2))
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	if _, err := c.Do(context.Background(), req, "test"); err == nil {
		t.Fatal("Do: expected error after retries exhausted")
	}
	// Retries=2 means 3 attempts (initial + 2 retries).
	if got := atomic.LoadInt32(&hits); got != 3 {
		t.Errorf("hits = %d, want 3", got)
	}
}

func TestContextCancellationStopsRetries(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c := New(WithBackoff(func(int) time.Duration { return 10 * time.Second }))
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before the first retry can sleep

	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	_, err := c.Do(ctx, req, "test")
	if err == nil {
		t.Fatal("Do: expected context.Canceled or similar")
	}
}

func TestTelemetryRecordsAllAttempts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	var events []Event
	c := New(
		WithBackoff(fastBackoff),
		WithRetries(2),
		WithEvent(func(e Event) { events = append(events, e) }),
	)
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	_, _ = c.Do(context.Background(), req, "pdb")

	if len(events) != 3 {
		t.Fatalf("events = %d, want 3 (one per attempt)", len(events))
	}
	for i, e := range events {
		if e.Attempt != i {
			t.Errorf("event[%d].Attempt = %d", i, e.Attempt)
		}
		if e.Tool != "pdb" {
			t.Errorf("event[%d].Tool = %q", i, e.Tool)
		}
		if e.Status != 503 {
			t.Errorf("event[%d].Status = %d", i, e.Status)
		}
	}
}
