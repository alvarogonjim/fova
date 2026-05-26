// Package design provides the agent's de-novo protein design tools. Each runs
// as an async job on the selected compute backend and persists the designs it
// produces.
//
// Every design.* tool is its own bespoke type with a typed input schema,
// per-tool preflight, and a tool-specific adapter (umbrella spec
// docs/superpowers/specs/2026-05-21-tool-integration-umbrella-design.md §3).
// The only thing they share is the conventional JSON envelope a backend
// returns for a design job; that envelope's type lives here so the bespoke
// tools can decode it uniformly.
package design

// backendOutput is the conventional JSON a backend returns for a design tool.
// A response without a "designs" array (e.g. a tool error) yields zero
// designs. Each bespoke tool's Execute / persist path unmarshals into this,
// so the on-the-wire shape stays consistent across the whole design surface.
type backendOutput struct {
	Designs []struct {
		Sequence      map[string]string  `json:"sequence"`
		StructureFile string             `json:"structure_file"`
		Scores        map[string]float64 `json:"scores"`
	} `json:"designs"`
}
