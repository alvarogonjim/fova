package domain

import (
	"encoding/json"
	"testing"
	"time"
)

func TestValidAA(t *testing.T) {
	cases := []struct {
		seq  string
		want bool
	}{
		{"MAQVQLVESGGG", true},
		{"", false},
		{"ACDEFGHIKLMNPQRSTVWY", true},
		{"MAQVB", false}, // B is not a standard residue
		{"maqv", false},  // lowercase rejected
		{"MAQ ", false},  // whitespace rejected
	}
	for _, c := range cases {
		if got := ValidAA(c.seq); got != c.want {
			t.Errorf("ValidAA(%q) = %v, want %v", c.seq, got, c.want)
		}
	}
}

func TestSequenceValidate(t *testing.T) {
	ok := Sequence{Chains: map[string]string{"A": "MAQVQL"}}
	if err := ok.Validate(); err != nil {
		t.Errorf("valid sequence rejected: %v", err)
	}
	empty := Sequence{Chains: map[string]string{}}
	if err := empty.Validate(); err == nil {
		t.Error("empty sequence accepted")
	}
	bad := Sequence{Chains: map[string]string{"A": "MAQXB"}}
	if err := bad.Validate(); err == nil {
		t.Error("invalid residues accepted")
	}
}

func TestNewToolCallRef(t *testing.T) {
	a := NewToolCallRef("fold.esmfold", []byte(`{"sequence":"MAQ"}`))
	b := NewToolCallRef("fold.esmfold", []byte(`{"sequence":"MAQ"}`))
	if a.InputHash != b.InputHash {
		t.Error("same input produced different hashes")
	}
	if a.Tool != "fold.esmfold" || a.InputHash == "" || a.CallID == "" {
		t.Errorf("ToolCallRef not fully populated: %+v", a)
	}
}

func TestDesignJSONRoundTrip(t *testing.T) {
	d := Design{
		ID:          "d_0001",
		ProjectID:   "default",
		Created:     time.Date(2026, 5, 16, 12, 0, 0, 0, time.UTC),
		Origin:      OriginBindCraft,
		Application: AppBinder,
		Sequence:    Sequence{Chains: map[string]string{"A": "MAQ"}},
		Scores:      map[string]float64{"ipsae": 0.7},
		Provenance:  []ToolCallRef{{Tool: "design.bindcraft"}},
	}
	raw, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	var got Design
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.ID != d.ID || got.Origin != OriginBindCraft || got.Scores["ipsae"] != 0.7 {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
}

func TestJobAndPlanCompile(t *testing.T) {
	_ = Job{ID: "j1", Kind: JobCompute, Status: JobQueued, Input: []byte(`{}`)}
	_ = DesignPlan{ID: "p1", Application: AppEnzyme, Filters: FilterConfig{MinIPSAE: 0.5}}
	_ = Experiment{ID: "e1", Designs: []DesignID{"d_0001"}}
	_ = Message{ID: "m1", Role: "user", ToolCalls: []ToolCall{{ID: "tc1", Input: json.RawMessage(`{}`)}}}
	_ = Session{ID: "s1", ProjectID: "default"}
}

func TestJobSetupKind(t *testing.T) {
	if JobSetup != "setup" {
		t.Fatalf("JobSetup = %q, want \"setup\"", JobSetup)
	}
}
