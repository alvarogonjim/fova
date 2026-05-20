package safety

import (
	"encoding/json"
	"strings"
)

// Guard is consulted by the agent loop before executing a tool call. Inspect
// returns a non-zero Refusal and true when the call must be blocked. The
// agent surfaces Refusal.Reason verbatim to the user.
type Guard interface {
	Inspect(toolName string, input json.RawMessage) (Refusal, bool)
}

// NewGuard returns a Guard backed by tbl. A nil tbl produces a guard that
// never refuses (used only in tests; production wiring always supplies a
// loaded table from LoadDefaultTable).
func NewGuard(tbl *Table) Guard { return &guard{tbl: tbl} }

// guard is the concrete Guard.
type guard struct{ tbl *Table }

// Inspect dispatches by tool name. Tools not in the inspected set are passed
// through. Malformed input JSON is treated as "nothing to inspect" — the
// tool itself will surface the parse error in its own error path.
func (g *guard) Inspect(toolName string, input json.RawMessage) (Refusal, bool) {
	if g == nil || g.tbl == nil {
		return Refusal{}, false
	}
	if !inspected(toolName) {
		return Refusal{}, false
	}
	for _, seq := range extractSequences(toolName, input) {
		if r, refused := g.tbl.Check(seq); refused {
			return r, true
		}
	}
	return Refusal{}, false
}

// inspected reports whether toolName falls into a guarded family. The guard
// inspects every design.* tool and lab.submit_experiment. Other lab.* tools
// (cost_estimate, results, etc.) are not inspected: they cannot start a
// real-world experiment by themselves.
func inspected(toolName string) bool {
	if toolName == "lab.submit_experiment" {
		return true
	}
	return strings.HasPrefix(toolName, "design.")
}

// extractSequences pulls every plausibly target-bearing string out of the
// tool's input JSON. It checks three shapes and returns each non-empty
// sequence it finds. Order does not matter; Check short-circuits on the
// first match.
//
//   - target_sequence (string)             — flat top-level key
//   - target.sequence (object -> string)   — nested form
//   - sequences[].sequence (array)         — used by lab.submit_experiment
//     and any design.* tool that takes pre-existing sequences (e.g. as a
//     starting pool for binder design).
func extractSequences(toolName string, input json.RawMessage) []string {
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(input, &doc); err != nil {
		return nil
	}
	var out []string
	if raw, ok := doc["target_sequence"]; ok {
		var s string
		if json.Unmarshal(raw, &s) == nil && s != "" {
			out = append(out, s)
		}
	}
	if raw, ok := doc["target"]; ok {
		var nested struct {
			Sequence string `json:"sequence"`
		}
		if json.Unmarshal(raw, &nested) == nil && nested.Sequence != "" {
			out = append(out, nested.Sequence)
		}
	}
	if raw, ok := doc["sequences"]; ok {
		var seqs []struct {
			Sequence string `json:"sequence"`
		}
		if json.Unmarshal(raw, &seqs) == nil {
			for _, s := range seqs {
				if s.Sequence != "" {
					out = append(out, s.Sequence)
				}
			}
		}
	}
	_ = toolName // reserved for future per-tool-shape branching
	return out
}
