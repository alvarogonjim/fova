package store

import (
	"testing"
	"time"

	"github.com/alvarogonjim/proteus/internal/domain"
)

func TestPlanInsertGet(t *testing.T) {
	st := openTestStore(t)
	p := domain.DesignPlan{
		ID: "p_0001", ProjectID: DefaultProjectID,
		Created:     time.Date(2026, 5, 16, 12, 0, 0, 0, time.UTC),
		Application: domain.AppBinder, Method: "design.bindcraft",
		Filters: domain.FilterConfig{MinIPSAE: 0.5}, Approved: true,
	}
	if err := st.InsertPlan(p); err != nil {
		t.Fatalf("InsertPlan: %v", err)
	}
	got, err := st.GetPlan("p_0001")
	if err != nil {
		t.Fatalf("GetPlan: %v", err)
	}
	if got.Method != "design.bindcraft" || got.Filters.MinIPSAE != 0.5 {
		t.Fatalf("plan mismatch: %+v", got)
	}
}

func TestSetPlanApproved(t *testing.T) {
	st := openTestStore(t)
	p := domain.DesignPlan{
		ID: "p_appr", ProjectID: DefaultProjectID,
		Created:     time.Date(2026, 5, 16, 12, 0, 0, 0, time.UTC),
		Application: domain.AppBinder, Method: "design.bindcraft",
	}
	if err := st.InsertPlan(p); err != nil {
		t.Fatalf("InsertPlan: %v", err)
	}
	if err := st.SetPlanApproved("p_appr"); err != nil {
		t.Fatalf("SetPlanApproved: %v", err)
	}
	got, err := st.GetPlan("p_appr")
	if err != nil {
		t.Fatalf("GetPlan: %v", err)
	}
	if !got.Approved {
		t.Error("plan should be approved after SetPlanApproved")
	}
	if got.ApprovedAt == nil {
		t.Error("ApprovedAt should be non-nil after SetPlanApproved")
	}
}

func TestLatestPlan(t *testing.T) {
	st := openTestStore(t)
	if _, ok, err := st.LatestPlan(DefaultProjectID); err != nil || ok {
		t.Fatalf("empty project: ok=%v err=%v", ok, err)
	}
	for i, id := range []domain.PlanID{"p1", "p2"} {
		p := domain.DesignPlan{
			ID: id, ProjectID: DefaultProjectID,
			Created: time.Date(2026, 5, 16, 12, i, 0, 0, time.UTC),
		}
		if err := st.InsertPlan(p); err != nil {
			t.Fatal(err)
		}
	}
	got, ok, err := st.LatestPlan(DefaultProjectID)
	if err != nil || !ok {
		t.Fatalf("LatestPlan: ok=%v err=%v", ok, err)
	}
	if got.ID != "p2" {
		t.Errorf("latest plan = %q, want p2", got.ID)
	}
}
