package store

import (
	"encoding/json"

	"github.com/alvarogonjim/fova/internal/domain"
)

// webhookSource labels every row fova writes to webhook_events. The schema
// has no experiment_id / event_type columns, so the full domain.WebhookEvent
// is JSON-encoded into the payload column (mirroring experiments.body).
const webhookSource = "adaptyv"

// InsertWebhookEvent persists one inbound webhook delivery.
func (s *Store) InsertWebhookEvent(e domain.WebhookEvent) error {
	payload, err := json.Marshal(e)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(
		`INSERT INTO webhook_events (id, received, source, signature, payload, processed)
		 VALUES (?,?,?,?,?,?)`,
		e.ID, e.Received.UTC().Format(timeLayout), webhookSource, nil,
		string(payload), 1,
	)
	return err
}

// ListWebhookEvents returns every webhook event for an experiment, newest first.
func (s *Store) ListWebhookEvents(experimentID domain.ExperimentID) ([]domain.WebhookEvent, error) {
	rows, err := s.db.Query(
		`SELECT payload FROM webhook_events ORDER BY received DESC, rowid DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.WebhookEvent
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		var e domain.WebhookEvent
		if err := json.Unmarshal([]byte(payload), &e); err != nil {
			return nil, err
		}
		if e.ExperimentID == experimentID {
			out = append(out, e)
		}
	}
	return out, rows.Err()
}
