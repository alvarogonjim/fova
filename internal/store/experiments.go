package store

import (
	"encoding/json"

	"github.com/alvarogonjim/proteus/internal/domain"
)

// InsertExperiment persists a wet-lab experiment.
func (s *Store) InsertExperiment(e domain.Experiment) error {
	body, err := json.Marshal(e)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(
		`INSERT INTO experiments (id, project_id, backend, external_id, assay_type,
		   target_id, target_name, submitted, status, cost_usd, body)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		string(e.ID), string(e.ProjectID), e.Backend, e.ExternalID, e.AssayType,
		e.TargetID, e.TargetName, e.SubmittedAt.UTC().Format(timeLayout),
		e.Status, e.CostUSD, string(body),
	)
	return err
}

// UpdateExperiment overwrites the stored row for e.ID with e's current state.
// It is used by the webhook receiver to apply status and result updates.
func (s *Store) UpdateExperiment(e domain.Experiment) error {
	body, err := json.Marshal(e)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(
		`UPDATE experiments SET backend=?, external_id=?, assay_type=?, target_id=?,
		   target_name=?, submitted=?, status=?, cost_usd=?, body=? WHERE id=?`,
		e.Backend, e.ExternalID, e.AssayType, e.TargetID, e.TargetName,
		e.SubmittedAt.UTC().Format(timeLayout), e.Status, e.CostUSD, string(body),
		string(e.ID),
	)
	return err
}

// GetExperiment returns one experiment by ID.
func (s *Store) GetExperiment(id domain.ExperimentID) (domain.Experiment, error) {
	var body string
	if err := s.db.QueryRow(
		`SELECT body FROM experiments WHERE id=?`, string(id),
	).Scan(&body); err != nil {
		return domain.Experiment{}, err
	}
	var e domain.Experiment
	if err := json.Unmarshal([]byte(body), &e); err != nil {
		return domain.Experiment{}, err
	}
	return e, nil
}

// ListExperiments returns all experiments for a project, newest first.
func (s *Store) ListExperiments(projectID domain.ProjectID) ([]domain.Experiment, error) {
	rows, err := s.db.Query(
		`SELECT body FROM experiments WHERE project_id=? ORDER BY submitted DESC, rowid DESC`,
		string(projectID),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Experiment
	for rows.Next() {
		var body string
		if err := rows.Scan(&body); err != nil {
			return nil, err
		}
		var e domain.Experiment
		if err := json.Unmarshal([]byte(body), &e); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
