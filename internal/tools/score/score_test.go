package score

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alvarogonjim/fova/internal/domain"
	"github.com/alvarogonjim/fova/internal/store"
)

func openStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "workspace.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func putDesign(t *testing.T, st *store.Store, id string, scores map[string]float64) {
	t.Helper()
	err := st.InsertDesign(domain.Design{
		ID: domain.DesignID(id), ProjectID: store.DefaultProjectID,
		Created: time.Now().UTC(), Origin: domain.OriginBindCraft,
		Sequence: domain.Sequence{Chains: map[string]string{"A": "MAQ"}},
		Scores:   scores,
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestFilterShortlistsAndRanksByIPSAE(t *testing.T) {
	st := openStore(t)
	putDesign(t, st, "d_hi", map[string]float64{"ipsae": 0.80, "plddt_mean": 90})
	putDesign(t, st, "d_mid", map[string]float64{"ipsae": 0.60, "plddt_mean": 85})
	putDesign(t, st, "d_lowipsae", map[string]float64{"ipsae": 0.30, "plddt_mean": 88})
	putDesign(t, st, "d_lowplddt", map[string]float64{"ipsae": 0.70, "plddt_mean": 50})

	res, err := NewFilterTool(st).Execute(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var out struct {
		Shortlist []struct {
			ID    string             `json:"id"`
			Score map[string]float64 `json:"scores"`
		} `json:"shortlist"`
	}
	if err := json.Unmarshal(res.Output, &out); err != nil {
		t.Fatalf("output not JSON: %v", err)
	}
	// d_lowipsae fails MinIPSAE 0.5; d_lowplddt fails MinPLDDT 80.
	if len(out.Shortlist) != 2 {
		t.Fatalf("shortlist size = %d, want 2: %+v", len(out.Shortlist), out.Shortlist)
	}
	// Ranked by ipSAE descending.
	if out.Shortlist[0].ID != "d_hi" || out.Shortlist[1].ID != "d_mid" {
		t.Errorf("ranking wrong: %s then %s", out.Shortlist[0].ID, out.Shortlist[1].ID)
	}
}

func TestFilterCustomThresholds(t *testing.T) {
	st := openStore(t)
	putDesign(t, st, "d1", map[string]float64{"ipsae": 0.65, "plddt_mean": 90})
	res, err := NewFilterTool(st).Execute(context.Background(),
		json.RawMessage(`{"filters":{"min_ipsae":0.7}}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Display, "0 ") && !strings.Contains(res.Display, "shortlist: 0") {
		t.Logf("display: %q", res.Display)
	}
	var out struct {
		Shortlist []json.RawMessage `json:"shortlist"`
	}
	_ = json.Unmarshal(res.Output, &out)
	if len(out.Shortlist) != 0 {
		t.Errorf("min_ipsae 0.7 should reject ipsae 0.65; got %d", len(out.Shortlist))
	}
}
