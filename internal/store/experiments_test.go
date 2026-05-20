package store

import (
	"testing"
	"time"

	"github.com/alvarogonjim/proteus/internal/domain"
)

func TestExperimentInsertGetList(t *testing.T) {
	st := openTestStore(t)
	e := domain.Experiment{
		ID: "e_0001", ProjectID: DefaultProjectID,
		Backend: "adaptyv", ExternalID: "adaptyv-123", AssayType: "binding",
		TargetID: "1ZWG", TargetName: "test target",
		Designs:     []domain.DesignID{"d_0001", "d_0002"},
		SubmittedAt: time.Date(2026, 5, 16, 12, 0, 0, 0, time.UTC),
		Status:      "submitted",
	}
	if err := st.InsertExperiment(e); err != nil {
		t.Fatalf("InsertExperiment: %v", err)
	}
	got, err := st.GetExperiment("e_0001")
	if err != nil {
		t.Fatalf("GetExperiment: %v", err)
	}
	if got.ExternalID != "adaptyv-123" || len(got.Designs) != 2 {
		t.Fatalf("experiment mismatch: %+v", got)
	}
	list, err := st.ListExperiments(DefaultProjectID)
	if err != nil || len(list) != 1 {
		t.Fatalf("ListExperiments: n=%d err=%v", len(list), err)
	}
}
