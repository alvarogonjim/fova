package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/alvarogonjim/fova/internal/domain"
	"github.com/alvarogonjim/fova/pkg/proteinio"
)

// --- fs.read_structure ---

type fsReadStructure struct{ root string }

func (fsReadStructure) Name() string { return "fs.read_structure" }
func (fsReadStructure) Description() string {
	return "Read a protein structure file (.pdb, .cif, .mmcif) within the workspace and return its per-chain amino-acid sequences."
}
func (fsReadStructure) InputSchema() map[string]any {
	return objectSchema(map[string]any{
		"path": strProp("Path within the workspace to a .pdb, .cif or .mmcif file"),
	}, "path")
}
func (fsReadStructure) RequiresConfirmation(json.RawMessage) bool       { return false }
func (fsReadStructure) EstimatedCostUSD(json.RawMessage) float64        { return 0 }
func (fsReadStructure) EstimatedDuration(json.RawMessage) time.Duration { return 50 * time.Millisecond }

func (t fsReadStructure) Execute(_ context.Context, input json.RawMessage) (Result, error) {
	var in struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return Result{}, err
	}
	abs, err := SafeJoin(t.root, in.Path)
	if err != nil {
		return Result{}, err
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return Result{}, err
	}
	var chains map[string]string
	switch ext := strings.ToLower(filepath.Ext(in.Path)); ext {
	case ".pdb":
		chains, err = proteinio.ChainsFromPDB(bytes.NewReader(data))
	case ".cif", ".mmcif":
		chains, err = proteinio.ChainsFromMMCIF(bytes.NewReader(data))
	default:
		return Result{}, fmt.Errorf("fs.read_structure: unsupported extension %q (want .pdb, .cif or .mmcif)", ext)
	}
	if err != nil {
		return Result{}, err
	}
	out, err := json.Marshal(map[string]any{
		"chains":      chains,
		"chain_count": len(chains),
	})
	if err != nil {
		return Result{}, err
	}
	ids := make([]string, 0, len(chains))
	for id := range chains {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var b strings.Builder
	fmt.Fprintf(&b, "%s: %d chain(s)", in.Path, len(chains))
	for _, id := range ids {
		fmt.Fprintf(&b, "\n  %s: %d residues", id, len(chains[id]))
	}
	return Result{
		Output:     out,
		Display:    b.String(),
		Provenance: domain.NewToolCallRef("fs.read_structure", input),
	}, nil
}
