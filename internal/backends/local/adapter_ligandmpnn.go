package local

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/alvarogonjim/fova/pkg/proteinio"
)

// ligandMPNNScores pulls the LigandMPNN per-design confidence keys out of a
// FASTA header like "1BC8, id=1, overall_confidence=0.62, ligand_confidence=0.55,
// sequence_recovery=0.38". A missing key is simply absent from the map — never
// an error (FASTA header format may drift; a dropped score must not fail a run).
func ligandMPNNScores(header string) map[string]float64 {
	scores := map[string]float64{}
	for _, field := range strings.Split(header, ",") {
		kv := strings.SplitN(strings.TrimSpace(field), "=", 2)
		if len(kv) != 2 {
			continue
		}
		switch kv[0] {
		case "overall_confidence", "ligand_confidence", "sequence_recovery":
			if v, err := strconv.ParseFloat(strings.TrimSpace(kv[1]), 64); err == nil {
				scores[kv[0]] = v
			}
		}
	}
	return scores
}

// parseLigandMPNNOutput reads every seqs/*.fa under outDir and returns one
// designOut per designed sequence. Record 0 in each FASTA is the native input
// (skipped). Each design record's Sequence is split on '/' into chain ids
// (A, B, ...); its Scores come from the header's key=value tokens; its
// StructureFile is the matching backbones/<stem>_<i>.pdb if present, else the
// packed/<stem>_<i>_1.pdb if present, else "".
func parseLigandMPNNOutput(outDir string) ([]designOut, error) {
	files, err := filepath.Glob(filepath.Join(outDir, "seqs", "*.fa"))
	if err != nil {
		return nil, err
	}
	var designs []designOut
	for _, f := range files {
		body, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		recs, err := proteinio.ParseFASTA(bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("design.ligandmpnn: parse %s: %w", f, err)
		}
		stem := strings.TrimSuffix(filepath.Base(f), filepath.Ext(f))
		for i, rec := range recs {
			if i == 0 {
				continue // native input sequence
			}
			if strings.TrimSpace(rec.Sequence) == "" {
				continue // malformed record — header with no sequence
			}
			designs = append(designs, designOut{
				Sequence:      splitChains(rec.Sequence),
				StructureFile: ligandMPNNStructureFile(outDir, stem, i),
				Scores:        ligandMPNNScores(rec.Header),
			})
		}
	}
	if len(designs) == 0 {
		return nil, fmt.Errorf("design.ligandmpnn: no designed sequences found in %s", outDir)
	}
	return designs, nil
}

// ligandMPNNStructureFile resolves the structure file for design record i of
// FASTA stem: the backbone PDB if present, else the first packed PDB if
// present, else "" (a designed sequence with no PDB is still a valid design).
func ligandMPNNStructureFile(outDir, stem string, i int) string {
	backbone := filepath.Join(outDir, "backbones", fmt.Sprintf("%s_%d.pdb", stem, i))
	if info, err := os.Stat(backbone); err == nil && !info.IsDir() {
		return backbone
	}
	packed := filepath.Join(outDir, "packed", fmt.Sprintf("%s_%d_1.pdb", stem, i))
	if info, err := os.Stat(packed); err == nil && !info.IsDir() {
		return packed
	}
	return ""
}
