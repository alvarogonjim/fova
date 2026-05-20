package store

import (
	"testing"
	"time"

	"github.com/alvarogonjim/fova/internal/domain"
)

func TestSessionInsertGet(t *testing.T) {
	st := openTestStore(t)
	created := time.Date(2026, 5, 16, 12, 0, 0, 0, time.UTC)
	sess := domain.Session{
		ID: "s_0001", ProjectID: DefaultProjectID,
		Created: created, Updated: created,
		Model: "claude-opus-4-7", Provider: "anthropic",
	}
	if err := st.InsertSession(sess); err != nil {
		t.Fatalf("InsertSession: %v", err)
	}
	got, err := st.GetSession("s_0001")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got.Model != "claude-opus-4-7" || got.Provider != "anthropic" {
		t.Fatalf("session mismatch: %+v", got)
	}
}

func TestMessageInsertList(t *testing.T) {
	st := openTestStore(t)
	created := time.Date(2026, 5, 16, 12, 0, 0, 0, time.UTC)
	sess := domain.Session{ID: "s_0001", ProjectID: DefaultProjectID, Created: created, Updated: created}
	if err := st.InsertSession(sess); err != nil {
		t.Fatal(err)
	}
	msgs := []domain.Message{
		{ID: "m1", SessionID: "s_0001", Role: "user", Content: "fold MAQ", Created: created},
		{ID: "m2", SessionID: "s_0001", Role: "assistant", Content: "ok", Created: created},
	}
	for _, m := range msgs {
		if err := st.InsertMessage(m); err != nil {
			t.Fatalf("InsertMessage: %v", err)
		}
	}
	got, err := st.ListMessages("s_0001")
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(got) != 2 || got[0].Role != "user" || got[1].Content != "ok" {
		t.Fatalf("messages mismatch: %+v", got)
	}
}
