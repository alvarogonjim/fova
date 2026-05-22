package local

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
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
