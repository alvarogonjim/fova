// Package domain holds the core data types shared across Proteus.
// v0.1 implements only the minimal subset needed by the tool registry.
package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/alvarogonjim/proteus/internal/version"
	"github.com/google/uuid"
)

// Identifiers.
type DesignID string
type JobID string

// validResidues lists the 20 standard amino-acid one-letter codes.
const validResidues = "ACDEFGHIKLMNPQRSTVWY"

// ValidAA reports whether s is a non-empty string of standard amino-acid codes.
func ValidAA(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !strings.ContainsRune(validResidues, r) {
			return false
		}
	}
	return true
}

// Sequence is one or more amino-acid chains keyed by chain ID.
type Sequence struct {
	Chains map[string]string `json:"chains"`
}

// Validate checks every chain holds a valid amino-acid sequence.
func (s Sequence) Validate() error {
	if len(s.Chains) == 0 {
		return errors.New("sequence has no chains")
	}
	for id, seq := range s.Chains {
		if !ValidAA(seq) {
			return errors.New("chain " + id + " is not a valid amino-acid sequence")
		}
	}
	return nil
}

// ToolCallRef records the provenance of one tool invocation.
type ToolCallRef struct {
	CallID    string    `json:"call_id"`
	Tool      string    `json:"tool"`
	InputHash string    `json:"input_hash"`
	Version   string    `json:"version"`
	Timestamp time.Time `json:"timestamp"`
}

// NewToolCallRef builds a provenance record for a tool call with the given input.
func NewToolCallRef(tool string, input []byte) ToolCallRef {
	sum := sha256.Sum256(input)
	return ToolCallRef{
		CallID:    uuid.NewString(),
		Tool:      tool,
		InputHash: hex.EncodeToString(sum[:]),
		Version:   version.String(),
		Timestamp: time.Now().UTC(),
	}
}

// Design is a designed or predicted protein. v0.1 populates only the fields
// produced by fold.esmfold; the full SPECS §4 struct arrives with v0.2.
type Design struct {
	ID            DesignID           `json:"id"`
	Created       time.Time          `json:"created"`
	Sequence      Sequence           `json:"sequence"`
	StructureFile string             `json:"structure_file,omitempty"`
	Scores        map[string]float64 `json:"scores"`
	Provenance    []ToolCallRef      `json:"provenance"`
}
