// Package safety implements the v0.5 content-filter guard: a small embedded
// list of restricted target sequences plus a substring screen that the agent
// loop consults before executing any design.* or lab.submit_experiment tool
// call.
//
// The list ships embedded in the binary (//go:embed) so the guard cannot be
// silently disabled by tampering with a config file or losing a network
// fetch. The match is a deliberately conservative case-insensitive substring
// screen; a hit returns a Refusal with the entry's id and human-readable
// reason. The reason is surfaced verbatim to the user — loud and plain
// (SPECS §20 #3).
//
// The shipped list is a placeholder (see restricted_targets.toml). Operators
// who deploy fova in regulated settings replace the embedded file with
// their organisation's real screening list at build time.
package safety

import (
	_ "embed"
	"fmt"
	"strings"

	"github.com/BurntSushi/toml"
)

// defaultTOML is the embedded list. See restricted_targets.toml for schema.
//
//go:embed restricted_targets.toml
var defaultTOML []byte

// Entry is one entry in the restricted-target list.
type Entry struct {
	ID         string   `toml:"id"`
	Reason     string   `toml:"reason"`
	Signatures []string `toml:"signatures"`
}

// Refusal is what Check returns on a hit.
type Refusal struct {
	ID     string // the matched Entry.ID
	Reason string // the matched Entry.Reason, shown verbatim to the user
}

// Table is a loaded restricted-target list.
type Table struct {
	entries []Entry
}

// LoadDefaultTable parses the embedded restricted_targets.toml. Called once
// at startup by cmd/fova/main.go.
func LoadDefaultTable() (*Table, error) {
	return parseTable(defaultTOML)
}

// parseTable unmarshals a TOML byte slice into a *Table. Exposed only for
// tests; production callers use LoadDefaultTable.
func parseTable(data []byte) (*Table, error) {
	var doc struct {
		Entry []Entry `toml:"entry"`
	}
	if err := toml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("safety: parse restricted_targets.toml: %w", err)
	}
	return &Table{entries: doc.Entry}, nil
}

// Entries returns the parsed entries. Read-only — callers must not mutate.
func (t *Table) Entries() []Entry {
	if t == nil {
		return nil
	}
	return t.entries
}

// Check reports whether seq contains any signature from any entry. The match
// is case-insensitive after stripping whitespace from both sides. On a hit
// the first matching entry's Refusal is returned. Blank signatures are
// skipped (strings.Contains(x, "") is true, which would refuse everything).
// A nil receiver is a no-op — it refuses nothing.
func (t *Table) Check(seq string) (Refusal, bool) {
	if t == nil {
		return Refusal{}, false
	}
	norm := normalise(seq)
	if norm == "" {
		return Refusal{}, false
	}
	for _, e := range t.entries {
		for _, sig := range e.Signatures {
			n := normalise(sig)
			if n == "" {
				continue
			}
			if strings.Contains(norm, n) {
				return Refusal{ID: e.ID, Reason: e.Reason}, true
			}
		}
	}
	return Refusal{}, false
}

// normalise upper-cases s and removes every whitespace rune. It is applied to
// both the input sequence and each signature so FASTA-style line wrapping,
// mixed case, and stray tabs cannot mask a match.
func normalise(s string) string {
	return strings.ToUpper(strings.Join(strings.Fields(s), ""))
}
