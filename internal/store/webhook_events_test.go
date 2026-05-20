package store

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/alvarogonjim/fova/internal/domain"
)

func TestWebhookEventsInsertList(t *testing.T) {
	st := openTestStore(t)

	e1 := domain.WebhookEvent{
		ID:           "wh_0001",
		ExperimentID: "e_0001",
		EventType:    "results.ready",
		Received:     time.Date(2026, 5, 17, 9, 0, 0, 0, time.UTC),
		Payload:      json.RawMessage(`{"experiment_id":"adaptyv-1","status":"completed"}`),
	}
	e2 := domain.WebhookEvent{
		ID:           "wh_0002",
		ExperimentID: "e_0002",
		EventType:    "status.changed",
		Received:     time.Date(2026, 5, 18, 9, 0, 0, 0, time.UTC),
		Payload:      json.RawMessage(`{"experiment_id":"adaptyv-2","status":"running"}`),
	}
	for _, e := range []domain.WebhookEvent{e1, e2} {
		if err := st.InsertWebhookEvent(e); err != nil {
			t.Fatalf("InsertWebhookEvent(%s): %v", e.ID, err)
		}
	}

	got, err := st.ListWebhookEvents("e_0001")
	if err != nil {
		t.Fatalf("ListWebhookEvents: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("ListWebhookEvents(e_0001): want 1 event, got %d", len(got))
	}
	if got[0].ID != "wh_0001" || got[0].EventType != "results.ready" {
		t.Fatalf("round-trip mismatch: %+v", got[0])
	}
	if !got[0].Received.Equal(e1.Received) {
		t.Fatalf("received mismatch: want %v got %v", e1.Received, got[0].Received)
	}
	if string(got[0].Payload) != string(e1.Payload) {
		t.Fatalf("payload mismatch: %s", got[0].Payload)
	}

	// A non-matching experiment id yields no rows.
	none, err := st.ListWebhookEvents("e_9999")
	if err != nil {
		t.Fatalf("ListWebhookEvents(e_9999): %v", err)
	}
	if len(none) != 0 {
		t.Fatalf("expected no events for unknown experiment, got %d", len(none))
	}
}
