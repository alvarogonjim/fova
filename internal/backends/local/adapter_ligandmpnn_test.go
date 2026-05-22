package local

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseLigandMPNNOutput(t *testing.T) {
	out := t.TempDir()
	seqs := filepath.Join(out, "seqs")
	if err := os.MkdirAll(seqs, 0o755); err != nil {
		t.Fatal(err)
	}
	// Record 0 = native (skipped); records 1-2 = designs.
	fa := ">1BC8, native\nMKQTAA\n" +
		">1BC8, id=1, overall_confidence=0.62, ligand_confidence=0.55, sequence_recovery=0.38\nMKDTAA\n" +
		">1BC8, id=2, overall_confidence=0.71, ligand_confidence=0.0, sequence_recovery=0.41\nMRDTAA\n"
	if err := os.WriteFile(filepath.Join(seqs, "1BC8.fa"), []byte(fa), 0o644); err != nil {
		t.Fatal(err)
	}
	designs, err := parseLigandMPNNOutput(out)
	if err != nil {
		t.Fatalf("parseLigandMPNNOutput: %v", err)
	}
	if len(designs) != 2 {
		t.Fatalf("want 2 designs (native skipped), got %d", len(designs))
	}
	if designs[0].Scores["overall_confidence"] != 0.62 ||
		designs[0].Scores["sequence_recovery"] != 0.38 {
		t.Errorf("design 0 scores wrong: %v", designs[0].Scores)
	}
	if designs[0].Sequence["A"] != "MKDTAA" {
		t.Errorf("design 0 sequence wrong: %v", designs[0].Sequence)
	}
}

func TestParseLigandMPNNOutputEmptyErrors(t *testing.T) {
	if _, err := parseLigandMPNNOutput(t.TempDir()); err == nil {
		t.Fatal("expected an error when no seqs/*.fa are present")
	}
}
