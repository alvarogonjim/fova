package store

import (
	"testing"
	"time"

	"github.com/alvarogonjim/fova/internal/domain"
)

func sampleDesign(id domain.DesignID) domain.Design {
	return domain.Design{
		ID:          id,
		ProjectID:   DefaultProjectID,
		Created:     time.Date(2026, 5, 16, 12, 0, 0, 0, time.UTC),
		Origin:      domain.OriginBindCraft,
		Application: domain.AppBinder,
		Sequence:    domain.Sequence{Chains: map[string]string{"A": "MAQVQL"}},
		Scores:      map[string]float64{"ipsae": 0.71, "plddt_mean": 88.2},
		Provenance:  []domain.ToolCallRef{{Tool: "design.bindcraft"}},
	}
}

func TestDesignInsertGet(t *testing.T) {
	st := openTestStore(t)
	want := sampleDesign("d_0001")
	if err := st.InsertDesign(want); err != nil {
		t.Fatalf("InsertDesign: %v", err)
	}
	got, err := st.GetDesign("d_0001")
	if err != nil {
		t.Fatalf("GetDesign: %v", err)
	}
	if got.ID != want.ID || got.Origin != domain.OriginBindCraft {
		t.Fatalf("design mismatch: %+v", got)
	}
	if got.Scores["ipsae"] != 0.71 {
		t.Errorf("ipsae = %v, want 0.71", got.Scores["ipsae"])
	}
}

func TestDesignListByProject(t *testing.T) {
	st := openTestStore(t)
	for _, id := range []domain.DesignID{"d_0001", "d_0002", "d_0003"} {
		if err := st.InsertDesign(sampleDesign(id)); err != nil {
			t.Fatal(err)
		}
	}
	got, err := st.ListDesigns(DefaultProjectID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("ListDesigns returned %d, want 3", len(got))
	}
}
