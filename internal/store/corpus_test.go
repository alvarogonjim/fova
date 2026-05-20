package store

import (
	"testing"
	"time"

	"github.com/alvarogonjim/proteus/internal/domain"
)

func TestCorpusInsertListGetDelete(t *testing.T) {
	st := openTestStore(t)

	p1 := domain.CorpusPaper{
		ID: "10.1/abc", ProjectID: DefaultProjectID,
		Title: "Designing Proteins", Authors: "Smith J", Year: 2021,
		Source: "europepmc", FullText: "first body", Metadata: `{"k":"v"}`,
		Added: time.Date(2026, 5, 16, 12, 0, 0, 0, time.UTC),
	}
	p2 := domain.CorpusPaper{
		ID: "PMC2", ProjectID: DefaultProjectID,
		Title: "Folding Studies", Authors: "Lee K", Year: 2022,
		Source: "europepmc", FullText: "second body", Metadata: `{}`,
		Added: time.Date(2026, 5, 16, 12, 5, 0, 0, time.UTC),
	}
	if err := st.InsertCorpusPaper(p1); err != nil {
		t.Fatalf("InsertCorpusPaper p1: %v", err)
	}
	if err := st.InsertCorpusPaper(p2); err != nil {
		t.Fatalf("InsertCorpusPaper p2: %v", err)
	}

	list, err := st.ListCorpusPapers(DefaultProjectID)
	if err != nil {
		t.Fatalf("ListCorpusPapers: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("list len = %d, want 2", len(list))
	}
	if list[0].ID != "10.1/abc" || list[1].ID != "PMC2" {
		t.Fatalf("list order = [%s %s], want [10.1/abc PMC2]", list[0].ID, list[1].ID)
	}

	got, err := st.GetCorpusPaper("PMC2")
	if err != nil {
		t.Fatalf("GetCorpusPaper: %v", err)
	}
	if got.Title != "Folding Studies" || got.FullText != "second body" || got.Year != 2022 {
		t.Fatalf("GetCorpusPaper mismatch: %+v", got)
	}

	if err := st.DeleteCorpusPaper("10.1/abc"); err != nil {
		t.Fatalf("DeleteCorpusPaper: %v", err)
	}
	list, err = st.ListCorpusPapers(DefaultProjectID)
	if err != nil {
		t.Fatalf("ListCorpusPapers after delete: %v", err)
	}
	if len(list) != 1 || list[0].ID != "PMC2" {
		t.Fatalf("after delete list = %+v, want [PMC2]", list)
	}
}

func TestCorpusInsertOrReplace(t *testing.T) {
	st := openTestStore(t)
	base := domain.CorpusPaper{
		ID: "10.1/dup", ProjectID: DefaultProjectID,
		Title: "Original", Source: "europepmc", Metadata: "{}",
		Added: time.Date(2026, 5, 16, 12, 0, 0, 0, time.UTC),
	}
	if err := st.InsertCorpusPaper(base); err != nil {
		t.Fatalf("InsertCorpusPaper: %v", err)
	}
	updated := base
	updated.Title = "Updated"
	updated.FullText = "new text"
	if err := st.InsertCorpusPaper(updated); err != nil {
		t.Fatalf("InsertCorpusPaper (replace): %v", err)
	}
	list, err := st.ListCorpusPapers(DefaultProjectID)
	if err != nil {
		t.Fatalf("ListCorpusPapers: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("list len = %d, want 1 (re-insert must update not duplicate)", len(list))
	}
	if list[0].Title != "Updated" || list[0].FullText != "new text" {
		t.Fatalf("re-insert did not update: %+v", list[0])
	}
}
