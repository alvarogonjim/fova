package lab

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/alvarogonjim/fova/internal/domain"
	"github.com/alvarogonjim/fova/internal/store"
)

const testWebhookSecret = "test-shared-secret"

// newWebhookTestStore opens a fresh store seeded with one experiment whose
// ExternalID matches the webhook payloads below.
func newWebhookTestStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "workspace.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	exp := domain.Experiment{
		ID:          "e_0001",
		ProjectID:   store.DefaultProjectID,
		Backend:     "adaptyv",
		ExternalID:  "adaptyv-123",
		AssayType:   "binding",
		TargetID:    "1ZWG",
		TargetName:  "test target",
		Designs:     []domain.DesignID{"d_0001"},
		SubmittedAt: time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC),
		Status:      "submitted",
	}
	if err := st.InsertExperiment(exp); err != nil {
		t.Fatalf("InsertExperiment: %v", err)
	}
	return st
}

func TestWebhookValidSignatureAccepted(t *testing.T) {
	st := newWebhookTestStore(t)
	bus := make(chan tea.Msg, 1)
	handler := adaptyvHandler(st, bus, testWebhookSecret)

	body := []byte(`{"event_type":"results.ready","experiment_id":"adaptyv-123","status":"completed","results":[{"sequence_name":"d_0001","kd":1.2e-9,"kd_units":"M"}]}`)
	req := httptest.NewRequest(http.MethodPost, "/webhooks/adaptyv", strings.NewReader(string(body)))
	req.Header.Set(signatureHeader, signBody(body, testWebhookSecret))
	rec := httptest.NewRecorder()

	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}

	events, err := st.ListWebhookEvents("e_0001")
	if err != nil {
		t.Fatalf("ListWebhookEvents: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("want 1 stored webhook event, got %d", len(events))
	}
	if events[0].EventType != "results.ready" {
		t.Fatalf("event type: want results.ready, got %q", events[0].EventType)
	}
	if string(events[0].Payload) != string(body) {
		t.Fatalf("stored payload mismatch: %s", events[0].Payload)
	}

	select {
	case msg := <-bus:
		whMsg, ok := msg.(WebhookEventMsg)
		if !ok {
			t.Fatalf("bus message: want WebhookEventMsg, got %T", msg)
		}
		if whMsg.ExperimentID != "e_0001" {
			t.Fatalf("bus message experiment id: want e_0001, got %q", whMsg.ExperimentID)
		}
	default:
		t.Fatal("expected a WebhookEventMsg on the bus")
	}
}

func TestWebhookBadSignatureRejected(t *testing.T) {
	st := newWebhookTestStore(t)
	bus := make(chan tea.Msg, 1)
	handler := adaptyvHandler(st, bus, testWebhookSecret)

	body := []byte(`{"event_type":"results.ready","experiment_id":"adaptyv-123","status":"completed"}`)
	req := httptest.NewRequest(http.MethodPost, "/webhooks/adaptyv", strings.NewReader(string(body)))
	req.Header.Set(signatureHeader, "deadbeefnotavalidsignature")
	rec := httptest.NewRecorder()

	handler(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status: want 401, got %d", rec.Code)
	}

	events, err := st.ListWebhookEvents("e_0001")
	if err != nil {
		t.Fatalf("ListWebhookEvents: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("bad signature must persist nothing, got %d events", len(events))
	}

	select {
	case msg := <-bus:
		t.Fatalf("bad signature must not emit a bus message, got %T", msg)
	default:
	}
}

func TestWebhookMissingSignatureRejected(t *testing.T) {
	st := newWebhookTestStore(t)
	handler := adaptyvHandler(st, nil, testWebhookSecret)

	body := []byte(`{"event_type":"results.ready","experiment_id":"adaptyv-123"}`)
	req := httptest.NewRequest(http.MethodPost, "/webhooks/adaptyv", strings.NewReader(string(body)))
	rec := httptest.NewRecorder()

	handler(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status: want 401 for missing signature, got %d", rec.Code)
	}
}

// TestWebhookRouteServedViaMux exercises the route through an http.ServeMux —
// the same routing StartReceiver wires — without binding a real port.
func TestWebhookRouteServedViaMux(t *testing.T) {
	st := newWebhookTestStore(t)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /webhooks/adaptyv", adaptyvHandler(st, nil, testWebhookSecret))

	body := []byte(`{"event_type":"status.changed","experiment_id":"adaptyv-123","status":"running"}`)
	req := httptest.NewRequest(http.MethodPost, "/webhooks/adaptyv", strings.NewReader(string(body)))
	req.Header.Set(signatureHeader, signBody(body, testWebhookSecret))
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d", rec.Code)
	}

	// A GET to the same path must not match the POST-only route.
	getRec := httptest.NewRecorder()
	mux.ServeHTTP(getRec, httptest.NewRequest(http.MethodGet, "/webhooks/adaptyv", nil))
	if getRec.Code == http.StatusOK {
		t.Fatalf("GET should not be routed to the webhook handler, got %d", getRec.Code)
	}
}
