package safety

import (
	"encoding/json"
	"strings"
	"testing"
)

// banSig is a single signature used by every test in this file.
const banSig = "MPFVNKQFNYKDPVNGV"

// testTable builds a one-entry Table whose signature is banSig.
func testTable(t *testing.T) *Table {
	t.Helper()
	return &Table{entries: []Entry{
		{ID: "test-entry", Reason: "fixture; banned",
			Signatures: []string{banSig}},
	}}
}

func TestGuardSkipsUnrelatedTools(t *testing.T) {
	g := NewGuard(testTable(t))
	// score.* / jobs.* / etc. are never inspected, even when the input
	// happens to contain a banned signature in some other field.
	if r, refused := g.Inspect("jobs.status", json.RawMessage(`{"id":"`+banSig+`"}`)); refused {
		t.Fatalf("jobs.status must not be inspected, got refusal %+v", r)
	}
	if _, refused := g.Inspect("score.metrics", json.RawMessage(`{}`)); refused {
		t.Fatal("score.metrics must not be inspected")
	}
}

func TestGuardInspectsAllDesignTools(t *testing.T) {
	g := NewGuard(testTable(t))
	for _, name := range []string{
		"design.proteinmpnn",
		"design.rfdiffusion",
		"design.rfdiffusion2",
		"design.bindcraft",
		"design.ligandmpnn",
		"design.rfantibody",
		"design.chai2",
	} {
		in := json.RawMessage(`{"target_sequence":"GSHM` + banSig + `AAA"}`)
		r, refused := g.Inspect(name, in)
		if !refused {
			t.Errorf("%s with a banned target_sequence was NOT refused", name)
			continue
		}
		if r.ID != "test-entry" {
			t.Errorf("%s refusal.ID = %q, want test-entry", name, r.ID)
		}
	}
}

func TestGuardExtractsFlatTargetSequence(t *testing.T) {
	g := NewGuard(testTable(t))
	in := json.RawMessage(`{"target_sequence":"GSHM` + banSig + `AAA","num_designs":4}`)
	if _, refused := g.Inspect("design.proteinmpnn", in); !refused {
		t.Fatal("flat target_sequence shape was not refused")
	}
}

func TestGuardExtractsNestedTargetSequence(t *testing.T) {
	g := NewGuard(testTable(t))
	in := json.RawMessage(`{"target":{"sequence":"GSHM` + banSig + `AAA"},"num_designs":4}`)
	if _, refused := g.Inspect("design.proteinmpnn", in); !refused {
		t.Fatal("nested target.sequence shape was not refused")
	}
}

func TestGuardExtractsSequencesArray(t *testing.T) {
	g := NewGuard(testTable(t))
	in := json.RawMessage(`{"sequences":[{"name":"d1","sequence":"SAFE"},` +
		`{"name":"d2","sequence":"GSHM` + banSig + `AAA"}]}`)
	if _, refused := g.Inspect("design.bindcraft", in); !refused {
		t.Fatal("sequences[].sequence shape was not refused")
	}
}

func TestGuardInspectsLabSubmitExperiment(t *testing.T) {
	g := NewGuard(testTable(t))
	in := json.RawMessage(`{"target_id":"t1","assay_type":"binding",` +
		`"sequences":[{"name":"d","sequence":"` + banSig + `"}]}`)
	r, refused := g.Inspect("lab.submit_experiment", in)
	if !refused {
		t.Fatal("lab.submit_experiment with a banned sequence was not refused")
	}
	if !strings.Contains(r.Reason, "banned") {
		t.Errorf("refusal.Reason = %q, want it to include the entry's reason", r.Reason)
	}
}

func TestGuardLabSubmitWithSafeSequencesPasses(t *testing.T) {
	g := NewGuard(testTable(t))
	in := json.RawMessage(`{"target_id":"t1","assay_type":"binding",` +
		`"sequences":[{"name":"d","sequence":"MKLVAAGGSS"}]}`)
	if r, refused := g.Inspect("lab.submit_experiment", in); refused {
		t.Fatalf("safe sequences were refused: %+v", r)
	}
}

func TestGuardIgnoresMalformedInputJSON(t *testing.T) {
	g := NewGuard(testTable(t))
	// Malformed JSON must NOT crash the guard. It returns "not refused" so
	// the tool itself reports the parse error in its usual path.
	if _, refused := g.Inspect("design.proteinmpnn", json.RawMessage(`{not json`)); refused {
		t.Fatal("malformed JSON should fall through to the tool, not be refused")
	}
}

func TestGuardNilTableNeverRefuses(t *testing.T) {
	g := NewGuard(nil)
	in := json.RawMessage(`{"target_sequence":"` + banSig + `"}`)
	if _, refused := g.Inspect("design.proteinmpnn", in); refused {
		t.Fatal("a nil-table guard must never refuse (fail-open ONLY in tests)")
	}
}
