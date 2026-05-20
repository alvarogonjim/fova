package store

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/alvarogonjim/fova/internal/domain"
)

// openTestStore opens a fresh Store in a temp directory.
func openTestStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "workspace.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestOpenCreatesSchemaAndDefaultProject(t *testing.T) {
	st := openTestStore(t)

	wantTables := []string{
		"projects", "sessions", "messages", "plans", "designs",
		"jobs", "experiments", "webhook_events", "corpus_papers",
	}
	for _, name := range wantTables {
		var got string
		err := st.db.QueryRow(
			`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, name,
		).Scan(&got)
		if err != nil {
			t.Errorf("table %q missing: %v", name, err)
		}
	}

	var count int
	if err := st.db.QueryRow(
		`SELECT COUNT(*) FROM projects WHERE id=?`, string(DefaultProjectID),
	).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("default project rows = %d, want 1", count)
	}
}

func TestForeignKeysEnforced(t *testing.T) {
	st := openTestStore(t)

	var fk int
	if err := st.db.QueryRow(`PRAGMA foreign_keys`).Scan(&fk); err != nil {
		t.Fatalf("PRAGMA foreign_keys: %v", err)
	}
	if fk != 1 {
		t.Fatalf("foreign_keys pragma = %d, want 1", fk)
	}

	// Inserting a session that references a non-existent project must fail
	// the projects(id) foreign key constraint.
	now := time.Now().UTC()
	orphan := domain.Session{
		ID: "s_orphan", ProjectID: "no-such-project",
		Created: now, Updated: now,
		Model: "claude-opus-4-7", Provider: "anthropic",
	}
	if err := st.InsertSession(orphan); err == nil {
		t.Fatal("InsertSession with unknown ProjectID succeeded, want FK error")
	}
}

func TestOpenIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "workspace.db")
	st1, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	st1.Close()
	st2, err := Open(path) // reopening an existing DB must not error
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	st2.Close()
}
