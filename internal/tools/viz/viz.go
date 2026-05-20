// Package viz holds the four v0.5 SP-C visualisation tools:
//
//	viz.metric_plot    — PNG histogram of score distributions.
//	viz.contact_map    — PNG heatmap of inter-chain Cα–Cα distances.
//	viz.ascii_structure — per-chain DSSP-lite secondary-structure string.
//	viz.pymol_render   — PNG render of a structure via the PyMOL CLI.
//
// Each tool writes its output under <workspace>/designs/<tool>_<id>.<ext> and
// returns the absolute path in Result.Output. None require user confirmation
// and none cost anything to run (PyMOL is local-CPU).
package viz

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// random8 returns 8 lowercase hex characters drawn from crypto/rand. Used as
// the short suffix that disambiguates outputs from concurrent tool calls.
func random8() string {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		// Fall back to a time-derived id — never observed in practice but
		// avoids a panic in the rare /dev/urandom failure case.
		return fmt.Sprintf("%08x", time.Now().UnixNano()&0xffffffff)
	}
	return hex.EncodeToString(b[:])
}

// OutputPath returns <workspace>/designs/<tool>_<random8>.<ext>, creating the
// designs directory if it does not exist. ext is appended verbatim — pass
// "png", not ".png".
func OutputPath(workspace, tool, ext string) (string, error) {
	dir := filepath.Join(workspace, "designs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("viz: create designs dir: %w", err)
	}
	name := fmt.Sprintf("%s_%s.%s", tool, random8(), ext)
	return filepath.Join(dir, name), nil
}

// noopMeta is the cost/duration/confirmation boilerplate every viz tool
// embeds — none requires confirmation, none costs anything, all complete in
// well under a second except viz.pymol_render which overrides EstimatedDuration.
type noopMeta struct{}

func (noopMeta) RequiresConfirmation(json.RawMessage) bool       { return false }
func (noopMeta) EstimatedCostUSD(json.RawMessage) float64        { return 0 }
func (noopMeta) EstimatedDuration(json.RawMessage) time.Duration { return 500 * time.Millisecond }
