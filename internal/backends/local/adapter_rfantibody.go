package local

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/alvarogonjim/fova/internal/domain"
)

// parseRFantibodyOutput reads the RF2 prediction PDBs and the qvscorefile TSV
// under outDir and returns one designOut per prediction.
//
// outDir/scores.tsv, when present, is a TSV whose first row is the header. For
// each data row the "tag" column keys the row; every other column that parses
// as a float64 becomes a score — header names "plddt"/"pLDDT" map to "plddt"
// and "pae"/"pAE" to "pae", any other numeric column is carried through under
// its header name. A missing or unreadable scores.tsv is not an error: those
// designs simply get an empty Scores map.
//
// The design set is every outDir/*.pdb, sorted; each file's tag is its stem
// and links it to a score row. An empty PDB set is an error — a 3-stage run
// that produced no predictions has failed.
func parseRFantibodyOutput(outDir string) ([]designOut, error) {
	scores := readRFantibodyScores(filepath.Join(outDir, "scores.tsv"))

	pdbs, err := filepath.Glob(filepath.Join(outDir, "*.pdb"))
	if err != nil {
		return nil, err
	}
	sort.Strings(pdbs)
	if len(pdbs) == 0 {
		return nil, fmt.Errorf("design.rfantibody: no prediction PDBs found in %s", outDir)
	}

	designs := make([]designOut, 0, len(pdbs))
	for _, pdb := range pdbs {
		tag := strings.TrimSuffix(filepath.Base(pdb), filepath.Ext(pdb))
		row := scores[tag]
		if row == nil {
			row = map[string]float64{}
		}
		designs = append(designs, designOut{
			Sequence:      map[string]string{},
			StructureFile: pdb,
			Scores:        row,
		})
	}
	return designs, nil
}

// readRFantibodyScores parses a qvscorefile TSV into tag -> score map. The
// first row is the header; the "tag" column keys each data row. Numeric
// columns become scores, with plddt/pae case-folded to canonical keys. A
// missing or unreadable file yields an empty map — never an error, because a
// dropped score must not fail an otherwise-successful design run.
func readRFantibodyScores(tsvPath string) map[string]map[string]float64 {
	out := map[string]map[string]float64{}
	body, err := os.ReadFile(tsvPath)
	if err != nil {
		return out
	}
	lines := strings.Split(strings.TrimRight(string(body), "\n"), "\n")
	if len(lines) < 2 {
		return out
	}
	header := strings.Split(lines[0], "\t")
	tagCol := -1
	for i, col := range header {
		if strings.EqualFold(strings.TrimSpace(col), "tag") {
			tagCol = i
			break
		}
	}
	if tagCol < 0 {
		return out
	}
	for _, line := range lines[1:] {
		if strings.TrimSpace(line) == "" {
			continue
		}
		cells := strings.Split(line, "\t")
		if tagCol >= len(cells) {
			continue
		}
		tag := strings.TrimSpace(cells[tagCol])
		if tag == "" {
			continue
		}
		row := map[string]float64{}
		for i, col := range header {
			if i == tagCol || i >= len(cells) {
				continue
			}
			v, err := strconv.ParseFloat(strings.TrimSpace(cells[i]), 64)
			if err != nil {
				continue
			}
			row[scoreKeyFor(strings.TrimSpace(col))] = v
		}
		out[tag] = row
	}
	return out
}

// scoreKeyFor folds the qvscorefile plddt/pae columns to their canonical
// lower-case keys; any other column is carried through unchanged.
func scoreKeyFor(col string) string {
	switch strings.ToLower(col) {
	case "plddt":
		return "plddt"
	case "pae":
		return "pae"
	default:
		return col
	}
}

// buildRFantibodyDriver renders the bash script that drives RFantibody's
// 3-stage pipeline inside the tool image. Every stage runs via
// `uv run --project /opt/rfantibody <stage>`; targetPath and frameworkPath are
// container-side paths (under /work for staged files, under /opt for the
// bundled framework PDBs). A flag is omitted when its pointer is nil, its
// string is empty, or its int is <= 0. The final two commands extract the RF2
// predictions to /work/out and write the qvscorefile TSV alongside them.
func buildRFantibodyDriver(p domain.RFantibodyParams, targetPath, frameworkPath string) string {
	var b strings.Builder
	b.WriteString("#!/bin/bash\n")
	b.WriteString("set -euo pipefail\n")

	// Stage 1 — RFdiffusion: diffuse antibody backbones against the target.
	b.WriteString("uv run --project /opt/rfantibody rfdiffusion")
	b.WriteString(" -t " + targetPath)
	b.WriteString(" -f " + frameworkPath)
	b.WriteString(" -q /work/designs.qv")
	b.WriteString(" -h " + p.Hotspots)
	if p.NumDesigns > 0 {
		b.WriteString(" -n " + strconv.Itoa(p.NumDesigns))
	}
	if p.DesignLoops != "" {
		b.WriteString(" -l " + p.DesignLoops)
	}
	if p.Deterministic != nil && *p.Deterministic {
		b.WriteString(" --deterministic")
	}
	b.WriteString("\n")

	// Stage 2 — ProteinMPNN: design sequences onto the diffused backbones.
	b.WriteString("uv run --project /opt/rfantibody proteinmpnn")
	b.WriteString(" -q /work/designs.qv")
	b.WriteString(" --output-quiver /work/sequences.qv")
	if p.SeqsPerStruct > 0 {
		b.WriteString(" -n " + strconv.Itoa(p.SeqsPerStruct))
	}
	if p.Temperature != nil {
		b.WriteString(" -t " + strconv.FormatFloat(*p.Temperature, 'g', -1, 64))
	}
	if p.Deterministic != nil && *p.Deterministic {
		b.WriteString(" --deterministic")
	}
	b.WriteString("\n")

	// Stage 3 — RF2: predict and score the designed complexes.
	b.WriteString("uv run --project /opt/rfantibody rf2")
	b.WriteString(" -q /work/sequences.qv")
	b.WriteString(" --output-quiver /work/predictions.qv")
	if p.NumRecycles != nil {
		b.WriteString(" -r " + strconv.Itoa(*p.NumRecycles))
	}
	if p.Seed != nil {
		b.WriteString(" -s " + strconv.Itoa(*p.Seed))
	}
	if p.HotspotShowProp != nil {
		b.WriteString(" --hotspot-show-prop " + strconv.FormatFloat(*p.HotspotShowProp, 'g', -1, 64))
	}
	b.WriteString("\n")

	// Extract the predictions and write the qvscorefile TSV alongside them.
	b.WriteString("mkdir -p /work/out\n")
	b.WriteString("uv run --project /opt/rfantibody qvextract /work/predictions.qv -o /work/out\n")
	b.WriteString("uv run --project /opt/rfantibody qvscorefile /work/predictions.qv > /work/out/scores.tsv\n")
	return b.String()
}
