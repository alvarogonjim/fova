package viz

import (
	"os"
	"path/filepath"
	"testing"
)

// twoChainPDB is a 3+3-residue toy PDB with CA, N, C records for chains A and B.
// Column-faithful: cols 13–16 atom name, 22 chain id, 31–54 X/Y/Z (8 each).
const twoChainPDB = `ATOM      1  N   ALA A   1       0.000   0.000   0.000  1.00 80.00           N
ATOM      2  CA  ALA A   1       1.500   0.000   0.000  1.00 80.00           C
ATOM      3  C   ALA A   1       2.300   1.200   0.000  1.00 80.00           C
ATOM      4  N   ALA A   2       3.500   1.000   0.000  1.00 80.00           N
ATOM      5  CA  ALA A   2       4.800   1.500   0.000  1.00 80.00           C
ATOM      6  C   ALA A   2       5.800   2.500   0.000  1.00 80.00           C
ATOM      7  N   ALA A   3       6.900   2.000   0.000  1.00 80.00           N
ATOM      8  CA  ALA A   3       8.000   2.500   0.000  1.00 80.00           C
ATOM      9  C   ALA A   3       9.100   3.500   0.000  1.00 80.00           C
ATOM     10  N   GLY B   1      20.000   0.000   0.000  1.00 80.00           N
ATOM     11  CA  GLY B   1      21.500   0.000   0.000  1.00 80.00           C
ATOM     12  C   GLY B   1      22.300   1.200   0.000  1.00 80.00           C
ATOM     13  N   GLY B   2      23.500   1.000   0.000  1.00 80.00           N
ATOM     14  CA  GLY B   2      24.800   1.500   0.000  1.00 80.00           C
ATOM     15  C   GLY B   2      25.800   2.500   0.000  1.00 80.00           C
ATOM     16  N   GLY B   3      26.900   2.000   0.000  1.00 80.00           N
ATOM     17  CA  GLY B   3      28.000   2.500   0.000  1.00 80.00           C
ATOM     18  C   GLY B   3      29.100   3.500   0.000  1.00 80.00           C
END
`

func writePDB(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "fixture.pdb")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestParsePDBTwoChainsCAOnly(t *testing.T) {
	chains, err := parsePDBAtoms(writePDB(t, twoChainPDB))
	if err != nil {
		t.Fatalf("parsePDBAtoms: %v", err)
	}
	if len(chains) != 2 {
		t.Fatalf("want 2 chains, got %d", len(chains))
	}
	a := chains["A"]
	if len(a.CA) != 3 {
		t.Errorf("chain A has %d CA atoms, want 3", len(a.CA))
	}
	if a.CA[0].X != 1.5 {
		t.Errorf("chain A CA[0].X = %v, want 1.5", a.CA[0].X)
	}
	if len(a.N) != 3 || len(a.C) != 3 {
		t.Errorf("chain A has %d N / %d C atoms, want 3 / 3", len(a.N), len(a.C))
	}
}

func TestParsePDBIgnoresHeaderAndHetatm(t *testing.T) {
	pdb := "HEADER stuff\nHETATM   1  ZN  ZN  A   1       0.000   0.000   0.000\n" + twoChainPDB
	chains, err := parsePDBAtoms(writePDB(t, pdb))
	if err != nil {
		t.Fatalf("parsePDBAtoms: %v", err)
	}
	if len(chains["A"].CA) != 3 {
		t.Errorf("expected 3 CA in chain A, got %d", len(chains["A"].CA))
	}
}

func TestParsePDBOrderedChains(t *testing.T) {
	chains, err := parsePDBAtoms(writePDB(t, twoChainPDB))
	if err != nil {
		t.Fatalf("parsePDBAtoms: %v", err)
	}
	order := chainOrder(chains)
	if len(order) != 2 || order[0] != "A" || order[1] != "B" {
		t.Errorf("chainOrder = %v, want [A B]", order)
	}
}
