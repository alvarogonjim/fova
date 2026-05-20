package store

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/alvarogonjim/fova/internal/domain"
)

func TestDataSurvivesReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workspace.db")
	created := time.Date(2026, 5, 16, 12, 0, 0, 0, time.UTC)

	// First run: write a session, a message, and a design, then close.
	st1, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := st1.InsertSession(domain.Session{
		ID: "s1", ProjectID: DefaultProjectID, Created: created, Updated: created,
		Model: "claude-opus-4-7", Provider: "anthropic",
	}); err != nil {
		t.Fatal(err)
	}
	if err := st1.InsertMessage(domain.Message{
		ID: "m1", SessionID: "s1", Role: "user", Content: "fold MAQ", Created: created,
	}); err != nil {
		t.Fatal(err)
	}
	if err := st1.InsertDesign(sampleDesign("d_0001")); err != nil {
		t.Fatal(err)
	}
	if err := st1.Close(); err != nil {
		t.Fatal(err)
	}

	// Second run: reopen the same file and read everything back.
	st2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer st2.Close()

	sess, err := st2.GetSession("s1")
	if err != nil || sess.Model != "claude-opus-4-7" {
		t.Fatalf("session lost across reopen: %+v err=%v", sess, err)
	}
	msgs, err := st2.ListMessages("s1")
	if err != nil || len(msgs) != 1 || msgs[0].Content != "fold MAQ" {
		t.Fatalf("messages lost across reopen: %+v err=%v", msgs, err)
	}
	d, err := st2.GetDesign("d_0001")
	if err != nil || d.Scores["ipsae"] != 0.71 {
		t.Fatalf("design lost across reopen: %+v err=%v", d, err)
	}
}
