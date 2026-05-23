package store

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"
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

func TestStoreWALEnabled(t *testing.T) {
	st := openTestStore(t)
	var mode string
	if err := st.db.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
		t.Fatalf("PRAGMA journal_mode: %v", err)
	}
	if mode != "wal" {
		t.Errorf("journal_mode = %q, want \"wal\"", mode)
	}
}

func TestStoreBusyTimeoutSet(t *testing.T) {
	st := openTestStore(t)
	var ms int
	if err := st.db.QueryRow("PRAGMA busy_timeout").Scan(&ms); err != nil {
		t.Fatalf("PRAGMA busy_timeout: %v", err)
	}
	if ms != 5000 {
		t.Errorf("busy_timeout = %d, want 5000", ms)
	}
}

func TestStoreSynchronousNormal(t *testing.T) {
	st := openTestStore(t)
	var mode int
	if err := st.db.QueryRow("PRAGMA synchronous").Scan(&mode); err != nil {
		t.Fatalf("PRAGMA synchronous: %v", err)
	}
	// synchronous=NORMAL is 1 in SQLite.
	if mode != 1 {
		t.Errorf("synchronous = %d, want 1 (NORMAL)", mode)
	}
}

func TestStoreConcurrentWrites(t *testing.T) {
	st := openTestStore(t)

	const goroutines = 4
	const perGoroutine = 50

	var wg sync.WaitGroup
	errs := make(chan error, goroutines*perGoroutine)
	for g := 0; g < goroutines; g++ {
		g := g
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < perGoroutine; i++ {
				j := domain.Job{
					ID:      domain.JobID(fmt.Sprintf("job-%d-%d", g, i)),
					Kind:    domain.JobCompute,
					Tool:    "concurrent-writer-test",
					Status:  domain.JobQueued,
					Created: time.Now().UTC(),
				}
				if err := st.InsertJob(j); err != nil {
					errs <- err
				}
			}
		}()
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("concurrent InsertJob: %v", err)
	}

	var n int
	if err := st.db.QueryRow(
		"SELECT COUNT(*) FROM jobs WHERE tool = 'concurrent-writer-test'",
	).Scan(&n); err != nil {
		t.Fatalf("count jobs: %v", err)
	}
	if want := goroutines * perGoroutine; n != want {
		t.Errorf("jobs count = %d, want %d", n, want)
	}
}

// queryPlanContains runs EXPLAIN QUERY PLAN and reports whether the textual
// plan mentions the given index name. SQLite includes the index name in the
// "detail" column when the plan uses an index.
func queryPlanContains(t *testing.T, st *Store, indexName, query string, args ...any) bool {
	t.Helper()
	rows, err := st.db.Query("EXPLAIN QUERY PLAN "+query, args...)
	if err != nil {
		t.Fatalf("EXPLAIN QUERY PLAN: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		// SQLite returns (id INTEGER, parent INTEGER, notused INTEGER, detail TEXT).
		var id, parent, notused int
		var detail string
		if err := rows.Scan(&id, &parent, &notused, &detail); err != nil {
			t.Fatalf("scan EXPLAIN row: %v", err)
		}
		if strings.Contains(detail, indexName) {
			return true
		}
	}
	return false
}

func TestListJobsUsesProjectIndex(t *testing.T) {
	st := openTestStore(t)
	if !queryPlanContains(t, st, "idx_jobs_project",
		`SELECT id FROM jobs WHERE project_id=? ORDER BY created DESC, rowid DESC`,
		string(DefaultProjectID),
	) {
		t.Errorf("ListJobs query plan does not use idx_jobs_project")
	}
}

func TestListExperimentsUsesProjectIndex(t *testing.T) {
	st := openTestStore(t)
	if !queryPlanContains(t, st, "idx_experiments_project",
		`SELECT body FROM experiments WHERE project_id=? ORDER BY submitted DESC, rowid DESC`,
		string(DefaultProjectID),
	) {
		t.Errorf("ListExperiments query plan does not use idx_experiments_project")
	}
}
