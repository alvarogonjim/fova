// Package replay defines the on-disk JSON event format used by
// `fova export` and `fova replay`. The schema (see the SP-F design)
// is the v0.5 stable contract: kind, ts, plus kind-specific payload fields.
package replay

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// Kind is the discriminator for a replay event.
type Kind string

const (
	KindUserMsg    Kind = "user_msg"
	KindAgentText  Kind = "agent_text"
	KindToolStart  Kind = "tool_start"
	KindToolResult Kind = "tool_result"
	KindTurnDone   Kind = "turn_done"
)

// Event is one entry in a replay document. The kind-specific fields are
// `omitempty` so an exported document only carries what each event needs.
type Event struct {
	Kind    Kind            `json:"kind"`
	TS      time.Time       `json:"ts"`
	Text    string          `json:"text,omitempty"`
	Name    string          `json:"name,omitempty"`
	Input   json.RawMessage `json:"input,omitempty"`
	Display string          `json:"display,omitempty"`
	Err     string          `json:"err,omitempty"`
}

// Document is one recorded fova session, normalised into events ready
// for replay. SessionID, Started, and Model carry the original session's
// identity; Events is the ordered event stream.
type Document struct {
	SessionID string    `json:"session_id"`
	Started   time.Time `json:"started"`
	Model     string    `json:"model"`
	Events    []Event   `json:"events"`
}

// LoadDocument reads and validates a replay document from disk.
func LoadDocument(path string) (*Document, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read replay document: %w", err)
	}
	var d Document
	if err := json.Unmarshal(body, &d); err != nil {
		return nil, fmt.Errorf("parse replay document: %w", err)
	}
	for i, ev := range d.Events {
		if !validKind(ev.Kind) {
			return nil, fmt.Errorf("event %d: unknown kind %q", i, ev.Kind)
		}
	}
	return &d, nil
}

// Write marshals the document as JSON to path. Plain Marshal is used (not
// MarshalIndent) so embedded json.RawMessage tool inputs round-trip byte-for-
// byte: MarshalIndent reformats nested JSON objects and would lose fidelity.
func (d *Document) Write(path string) error {
	body, err := json.Marshal(d)
	if err != nil {
		return fmt.Errorf("marshal replay document: %w", err)
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		return fmt.Errorf("write replay document: %w", err)
	}
	return nil
}

// validKind reports whether k is one of the five v0.5 event kinds.
func validKind(k Kind) bool {
	switch k {
	case KindUserMsg, KindAgentText, KindToolStart, KindToolResult, KindTurnDone:
		return true
	}
	return false
}
