// Package store persists Proteus domain objects in a per-project SQLite DB.
package store

import (
	"database/sql"
	_ "embed"
	"fmt"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"

	"github.com/alvarogonjim/proteus/internal/domain"
)

//go:embed schema.sql
var schemaSQL string

// DefaultProjectID is the single project used in v0.2.
const DefaultProjectID domain.ProjectID = "default"

// timeLayout is the format used for every TEXT timestamp column. It is a
// fixed-width nanosecond layout (always 9 fractional digits) so that lexical
// ordering of the stored strings matches chronological order — `ORDER BY
// created` then sorts correctly. All times are stored in UTC.
const timeLayout = "2006-01-02T15:04:05.000000000Z07:00"

// Store is a connection to a project's SQLite database.
type Store struct {
	db *sql.DB
}

// Open opens (creating if needed) the SQLite database at dbPath, applies the
// schema idempotently, and ensures the default project row exists.
func Open(dbPath string) (*Store, error) {
	db, err := sql.Open("sqlite", "file:"+dbPath+"?_pragma=foreign_keys(1)")
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	if _, err := db.Exec(schemaSQL); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	if err := migrate(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate schema: %w", err)
	}
	// One connection: SQLite serializes writers anyway, and a single-user TUI
	// has no need for a pool. This eliminates SQLITE_BUSY when several job
	// goroutines write concurrently.
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.ensureDefaultProject(filepath.Dir(dbPath)); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// migrate applies idempotent column additions that `CREATE TABLE IF NOT EXISTS`
// cannot make to an already-existing table. Each ALTER is tolerated when the
// column is already present, so this is safe to run on every Open.
func migrate(db *sql.DB) error {
	if hasColumn(db, "jobs", "log_file") {
		return nil
	}
	if _, err := db.Exec(`ALTER TABLE jobs ADD COLUMN log_file TEXT`); err != nil {
		return err
	}
	return nil
}

// hasColumn reports whether table has a column named col.
func hasColumn(db *sql.DB, table, col string) bool {
	rows, err := db.Query(`SELECT name FROM pragma_table_info(?)`, table)
	if err != nil {
		return false
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return false
		}
		if name == col {
			return true
		}
	}
	return false
}

func (s *Store) ensureDefaultProject(workspace string) error {
	_, err := s.db.Exec(
		`INSERT OR IGNORE INTO projects (id, name, created, workspace) VALUES (?,?,?,?)`,
		string(DefaultProjectID), "default",
		time.Now().UTC().Format(timeLayout), workspace,
	)
	if err != nil {
		return fmt.Errorf("ensure default project: %w", err)
	}
	return nil
}

// --- shared timestamp helpers ---

// nullTime renders an optional time for a nullable TEXT column.
func nullTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UTC().Format(timeLayout)
}

// nullStr renders an optional string for a nullable TEXT column.
func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// parseTime parses a stored timestamp.
func parseTime(s string) (time.Time, error) { return time.Parse(timeLayout, s) }

// scanTime parses an optional stored timestamp into a *time.Time.
func scanTime(s sql.NullString) (*time.Time, error) {
	if !s.Valid {
		return nil, nil
	}
	t, err := time.Parse(timeLayout, s.String)
	if err != nil {
		return nil, err
	}
	return &t, nil
}
