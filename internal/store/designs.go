package store

import (
	"encoding/json"

	"github.com/alvarogonjim/fova/internal/domain"
)

// InsertDesign persists a design.
func (s *Store) InsertDesign(d domain.Design) error {
	body, err := json.Marshal(d)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(
		`INSERT INTO designs (id, project_id, plan_id, created, body) VALUES (?,?,?,?,?)`,
		string(d.ID), string(d.ProjectID), nullStr(string(d.PlanID)),
		d.Created.UTC().Format(timeLayout), string(body),
	)
	return err
}

// GetDesign returns the design with the given ID.
func (s *Store) GetDesign(id domain.DesignID) (domain.Design, error) {
	var body string
	if err := s.db.QueryRow(
		`SELECT body FROM designs WHERE id = ?`, string(id),
	).Scan(&body); err != nil {
		return domain.Design{}, err
	}
	var d domain.Design
	if err := json.Unmarshal([]byte(body), &d); err != nil {
		return domain.Design{}, err
	}
	return d, nil
}

// ListDesigns returns all designs for a project, newest first.
func (s *Store) ListDesigns(projectID domain.ProjectID) ([]domain.Design, error) {
	rows, err := s.db.Query(
		`SELECT body FROM designs WHERE project_id = ? ORDER BY created DESC, rowid DESC`,
		string(projectID),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Design
	for rows.Next() {
		var body string
		if err := rows.Scan(&body); err != nil {
			return nil, err
		}
		var d domain.Design
		if err := json.Unmarshal([]byte(body), &d); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
