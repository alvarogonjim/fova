package store

import (
	"database/sql"

	"github.com/alvarogonjim/fova/internal/domain"
)

// InsertCorpusPaper persists a corpus paper. It uses INSERT OR REPLACE so that
// re-adding a paper with the same id updates the existing row.
func (s *Store) InsertCorpusPaper(p domain.CorpusPaper) error {
	_, err := s.db.Exec(
		`INSERT OR REPLACE INTO corpus_papers
		 (id, project_id, title, authors, year, source, full_text, metadata, added)
		 VALUES (?,?,?,?,?,?,?,?,?)`,
		p.ID, string(p.ProjectID), p.Title, nullStr(p.Authors), p.Year,
		p.Source, nullStr(p.FullText), p.Metadata, p.Added.UTC().Format(timeLayout),
	)
	return err
}

// ListCorpusPapers returns all corpus papers for a project, ordered by added.
func (s *Store) ListCorpusPapers(projectID domain.ProjectID) ([]domain.CorpusPaper, error) {
	rows, err := s.db.Query(
		`SELECT id, project_id, title, authors, year, source, full_text, metadata, added
		 FROM corpus_papers WHERE project_id = ? ORDER BY added, rowid`,
		string(projectID),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.CorpusPaper
	for rows.Next() {
		p, err := scanCorpusPaper(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// GetCorpusPaper returns one corpus paper by id.
func (s *Store) GetCorpusPaper(id string) (domain.CorpusPaper, error) {
	row := s.db.QueryRow(
		`SELECT id, project_id, title, authors, year, source, full_text, metadata, added
		 FROM corpus_papers WHERE id = ?`, id,
	)
	return scanCorpusPaper(row)
}

// DeleteCorpusPaper removes a corpus paper by id.
func (s *Store) DeleteCorpusPaper(id string) error {
	_, err := s.db.Exec(`DELETE FROM corpus_papers WHERE id = ?`, id)
	return err
}

// scanner abstracts *sql.Row and *sql.Rows for shared scanning.
type scanner interface {
	Scan(dest ...any) error
}

func scanCorpusPaper(sc scanner) (domain.CorpusPaper, error) {
	var (
		p        domain.CorpusPaper
		project  string
		authors  sql.NullString
		fullText sql.NullString
		added    string
	)
	if err := sc.Scan(&p.ID, &project, &p.Title, &authors, &p.Year,
		&p.Source, &fullText, &p.Metadata, &added); err != nil {
		return domain.CorpusPaper{}, err
	}
	p.ProjectID = domain.ProjectID(project)
	p.Authors = authors.String
	p.FullText = fullText.String
	t, err := parseTime(added)
	if err != nil {
		return domain.CorpusPaper{}, err
	}
	p.Added = t
	return p, nil
}
