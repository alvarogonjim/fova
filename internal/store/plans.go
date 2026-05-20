package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/alvarogonjim/proteus/internal/domain"
)

// InsertPlan persists a design plan.
func (s *Store) InsertPlan(p domain.DesignPlan) error {
	body, err := json.Marshal(p)
	if err != nil {
		return err
	}
	approved := 0
	if p.Approved {
		approved = 1
	}
	_, err = s.db.Exec(
		`INSERT INTO plans (id, project_id, created, body, approved) VALUES (?,?,?,?,?)`,
		string(p.ID), string(p.ProjectID), p.Created.UTC().Format(timeLayout),
		string(body), approved,
	)
	return err
}

// GetPlan returns one plan by ID.
func (s *Store) GetPlan(id domain.PlanID) (domain.DesignPlan, error) {
	var body string
	if err := s.db.QueryRow(
		`SELECT body FROM plans WHERE id=?`, string(id),
	).Scan(&body); err != nil {
		return domain.DesignPlan{}, err
	}
	var p domain.DesignPlan
	if err := json.Unmarshal([]byte(body), &p); err != nil {
		return domain.DesignPlan{}, err
	}
	return p, nil
}

// SetPlanApproved marks a plan as approved, stamping the approval time. It
// updates both the approved column and the re-marshalled body JSON.
func (s *Store) SetPlanApproved(id domain.PlanID) error {
	p, err := s.GetPlan(id)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	p.Approved = true
	p.ApprovedAt = &now
	body, err := json.Marshal(p)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(
		`UPDATE plans SET approved=1, body=? WHERE id=?`,
		string(body), string(id),
	)
	return err
}

// LatestPlan returns the most recently created plan for a project. The bool is
// false (with a nil error) when the project has no plans.
func (s *Store) LatestPlan(projectID domain.ProjectID) (domain.DesignPlan, bool, error) {
	var body string
	err := s.db.QueryRow(
		`SELECT body FROM plans WHERE project_id=? ORDER BY created DESC LIMIT 1`,
		string(projectID),
	).Scan(&body)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.DesignPlan{}, false, nil
	}
	if err != nil {
		return domain.DesignPlan{}, false, err
	}
	var p domain.DesignPlan
	if err := json.Unmarshal([]byte(body), &p); err != nil {
		return domain.DesignPlan{}, false, err
	}
	return p, true, nil
}
