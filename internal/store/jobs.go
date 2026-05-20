package store

import (
	"database/sql"
	"time"

	"github.com/alvarogonjim/proteus/internal/domain"
)

const jobColumns = `id, kind, tool, status, created, started, finished,
	progress, backend, cost_usd, input, output, error, log_file`

// InsertJob persists a new job. project_id is always the default project (v0.2).
func (s *Store) InsertJob(j domain.Job) error {
	_, err := s.db.Exec(
		`INSERT INTO jobs (id, project_id, kind, tool, status, created, started,
		   finished, progress, backend, cost_usd, input, output, error, log_file)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		string(j.ID), string(DefaultProjectID), string(j.Kind), j.Tool, string(j.Status),
		j.Created.UTC().Format(timeLayout), nullTime(j.Started), nullTime(j.Finished),
		j.Progress, j.Backend, j.CostUSD, string(j.Input),
		nullStr(string(j.Output)), nullStr(j.Error), nullStr(j.LogFile),
	)
	return err
}

// UpdateJob overwrites the mutable columns of an existing job.
func (s *Store) UpdateJob(j domain.Job) error {
	_, err := s.db.Exec(
		`UPDATE jobs SET status=?, started=?, finished=?, progress=?,
		   cost_usd=?, output=?, error=?, log_file=? WHERE id=?`,
		string(j.Status), nullTime(j.Started), nullTime(j.Finished), j.Progress,
		j.CostUSD, nullStr(string(j.Output)), nullStr(j.Error), nullStr(j.LogFile),
		string(j.ID),
	)
	return err
}

// GetJob returns one job by ID.
func (s *Store) GetJob(id domain.JobID) (domain.Job, error) {
	return scanJob(s.db.QueryRow(`SELECT `+jobColumns+` FROM jobs WHERE id=?`, string(id)))
}

// ListJobs returns all jobs for a project, newest first.
func (s *Store) ListJobs(projectID domain.ProjectID) ([]domain.Job, error) {
	rows, err := s.db.Query(
		`SELECT `+jobColumns+` FROM jobs WHERE project_id=? ORDER BY created DESC, rowid DESC`,
		string(projectID),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Job
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

// MarkRunningJobsInterrupted flips any job left "running" (e.g. after a crash)
// to "failed". Call once on startup.
func (s *Store) MarkRunningJobsInterrupted() error {
	_, err := s.db.Exec(
		`UPDATE jobs SET status=?, error=?, finished=? WHERE status=?`,
		string(domain.JobFailed), "interrupted: process exited before completion",
		time.Now().UTC().Format(timeLayout),
		string(domain.JobRunning),
	)
	return err
}

// rowScanner is satisfied by both *sql.Row and *sql.Rows.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanJob(row rowScanner) (domain.Job, error) {
	var (
		j                         domain.Job
		created                   string
		started, finished, output sql.NullString
		errText, logFile          sql.NullString
	)
	if err := row.Scan(
		&j.ID, &j.Kind, &j.Tool, &j.Status, &created, &started, &finished,
		&j.Progress, &j.Backend, &j.CostUSD, &j.Input, &output, &errText, &logFile,
	); err != nil {
		return domain.Job{}, err
	}
	var err error
	if j.Created, err = parseTime(created); err != nil {
		return domain.Job{}, err
	}
	if j.Started, err = scanTime(started); err != nil {
		return domain.Job{}, err
	}
	if j.Finished, err = scanTime(finished); err != nil {
		return domain.Job{}, err
	}
	if output.Valid {
		j.Output = []byte(output.String)
	}
	if errText.Valid {
		j.Error = errText.String
	}
	if logFile.Valid {
		j.LogFile = logFile.String
	}
	return j, nil
}
