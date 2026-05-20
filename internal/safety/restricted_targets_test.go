package safety

import (
	"strings"
	"testing"
)

func TestLoadDefaultTableHasEntries(t *testing.T) {
	tbl, err := LoadDefaultTable()
	if err != nil {
		t.Fatalf("LoadDefaultTable: %v", err)
	}
	if len(tbl.Entries()) < 2 {
		t.Fatalf("embedded table has %d entries, want >= 2", len(tbl.Entries()))
	}
	for i, e := range tbl.Entries() {
		if strings.TrimSpace(e.ID) == "" {
			t.Errorf("entry %d has empty id", i)
		}
		if strings.TrimSpace(e.Reason) == "" {
			t.Errorf("entry %d (%s) has empty reason", i, e.ID)
		}
		if len(e.Signatures) == 0 {
			t.Errorf("entry %d (%s) has no signatures", i, e.ID)
		}
		for j, sig := range e.Signatures {
			if len(strings.Fields(sig)) == 0 {
				t.Errorf("entry %d (%s) signature %d is blank", i, e.ID, j)
			}
		}
	}
}

func TestTableCheckMatchesSignature(t *testing.T) {
	tbl := &Table{entries: []Entry{
		{ID: "test-entry", Reason: "fixture reason",
			Signatures: []string{"MPFVNKQFNYKDPVNGV"}},
	}}
	// Sequence contains the signature as a substring.
	seq := "GSHM" + "MPFVNKQFNYKDPVNGV" + "AAAAAA"
	r, refused := tbl.Check(seq)
	if !refused {
		t.Fatal("expected a refusal for a sequence carrying the signature")
	}
	if r.ID != "test-entry" {
		t.Errorf("refusal.ID = %q, want test-entry", r.ID)
	}
	if !strings.Contains(r.Reason, "fixture reason") {
		t.Errorf("refusal.Reason = %q, want it to include the entry's reason", r.Reason)
	}
}

func TestTableCheckNoMatchOnUnrelatedSequence(t *testing.T) {
	tbl := &Table{entries: []Entry{
		{ID: "test-entry", Reason: "irrelevant",
			Signatures: []string{"MPFVNKQFNYKDPVNGV"}},
	}}
	if r, refused := tbl.Check("MKLVAAGGSSHHEEQQ"); refused {
		t.Fatalf("unrelated sequence was refused: %+v", r)
	}
}

func TestTableCheckIsCaseInsensitive(t *testing.T) {
	tbl := &Table{entries: []Entry{
		{ID: "lower-sig", Reason: "lower-cased reason",
			Signatures: []string{"mpfvnkqfnykdpvngv"}},
	}}
	if _, refused := tbl.Check("MPFVNKQFNYKDPVNGV"); !refused {
		t.Fatal("uppercase input should match lowercase signature")
	}
	if _, refused := tbl.Check("mpfvnkqfnykdpvngv"); !refused {
		t.Fatal("lowercase input should match lowercase signature")
	}
}

func TestTableCheckStripsWhitespace(t *testing.T) {
	tbl := &Table{entries: []Entry{
		{ID: "sig", Reason: "r", Signatures: []string{"MPFVNKQFNYKDPVNGV"}},
	}}
	// Real-world inputs sometimes carry spaces or newlines (FASTA-style).
	seq := "GSHM\nMPF VNKQF\nNYKDPVNGV\nAAA"
	if _, refused := tbl.Check(seq); !refused {
		t.Fatal("whitespace inside the sequence must not hide a signature")
	}
}

func TestTableCheckEmptyInputIsSafe(t *testing.T) {
	tbl := &Table{entries: []Entry{
		{ID: "sig", Reason: "r", Signatures: []string{"MPFVNKQFNYKDPVNGV"}},
	}}
	if _, refused := tbl.Check(""); refused {
		t.Fatal("empty sequence must not be refused")
	}
	if _, refused := tbl.Check("   \t\n"); refused {
		t.Fatal("whitespace-only sequence must not be refused")
	}
}

func TestTableCheckSkipsBlankSignatures(t *testing.T) {
	// Defensive: a malformed TOML entry with a blank signature must not turn
	// every input into a refusal (strings.Contains(x, "") is true).
	tbl := &Table{entries: []Entry{
		{ID: "blank-sig", Reason: "r", Signatures: []string{"", "   "}},
	}}
	if _, refused := tbl.Check("MKLVAAGG"); refused {
		t.Fatal("blank signature must not match any sequence")
	}
}

func TestTableCheckNilReceiver(t *testing.T) {
	var tbl *Table
	if _, refused := tbl.Check("MKLVAAGG"); refused {
		t.Fatal("nil table must not refuse anything")
	}
}
