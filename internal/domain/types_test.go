package domain

import "testing"

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
