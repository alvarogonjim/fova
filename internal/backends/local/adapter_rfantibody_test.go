package local

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseRFantibodyOutput(t *testing.T) {
	outDir := t.TempDir()
	for _, name := range []string{"ab_0.pdb", "ab_1.pdb"} {
		if err := os.WriteFile(filepath.Join(outDir, name), []byte("ATOM\nEND\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	tsv := "tag\tplddt\tpae\n" +
		"ab_0\t82.5\t7.1\n" +
		"ab_1\t74.0\t11.8\n"
	if err := os.WriteFile(filepath.Join(outDir, "scores.tsv"), []byte(tsv), 0o644); err != nil {
		t.Fatal(err)
	}
	designs, err := parseRFantibodyOutput(outDir)
	if err != nil {
		t.Fatalf("parseRFantibodyOutput: %v", err)
	}
	if len(designs) != 2 {
		t.Fatalf("want 2 designs, got %d", len(designs))
	}
	// designs are sorted by tag; ab_0 first.
	if designs[0].Scores["plddt"] != 82.5 || designs[0].Scores["pae"] != 7.1 {
		t.Errorf("ab_0 scores wrong: %v", designs[0].Scores)
	}
	if designs[0].StructureFile == "" {
		t.Error("ab_0 structure_file must be set")
	}
}

func TestParseRFantibodyOutputEmptyErrors(t *testing.T) {
	if _, err := parseRFantibodyOutput(t.TempDir()); err == nil {
		t.Fatal("expected an error when no prediction PDBs are present")
	}
}
