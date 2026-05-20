package viz

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// idealHelixPDB is an excerpt of the α1 helix of ubiquitin (PDB 1UBQ,
// residues 23–30) — eight consecutive helical residues with φ ≈ −60,
// ψ ≈ −40 dihedrals. Verified manually before commit: the inner six
// residues all classify as helix.
const idealHelixPDB = `ATOM    171  N   ILE A  23      31.113  20.863  15.860  1.00  8.32           N
ATOM    172  CA  ILE A  23      31.288  22.201  16.417  1.00  9.92           C
ATOM    173  C   ILE A  23      32.776  22.519  16.577  1.00 10.01           C
ATOM    179  N   GLU A  24      33.548  21.526  16.950  1.00  9.54           N
ATOM    180  CA  GLU A  24      35.031  21.722  17.069  1.00 11.81           C
ATOM    181  C   GLU A  24      35.615  22.190  15.759  1.00 11.14           C
ATOM    188  N   ASN A  25      35.139  21.624  14.662  1.00  9.43           N
ATOM    189  CA  ASN A  25      35.590  21.945  13.302  1.00 10.96           C
ATOM    190  C   ASN A  25      35.238  23.382  12.920  1.00  9.68           C
ATOM    196  N   VAL A  26      34.007  23.745  13.250  1.00  6.52           N
ATOM    197  CA  VAL A  26      33.533  25.097  12.978  1.00  5.53           C
ATOM    198  C   VAL A  26      34.441  26.099  13.684  1.00  4.42           C
ATOM    203  N   LYS A  27      34.734  25.822  14.949  1.00  2.64           N
ATOM    204  CA  LYS A  27      35.596  26.715  15.736  1.00  4.14           C
ATOM    205  C   LYS A  27      36.975  26.826  15.107  1.00  5.58           C
ATOM    212  N   ALA A  28      37.499  25.743  14.571  1.00  6.61           N
ATOM    213  CA  ALA A  28      38.794  25.761  13.880  1.00  7.74           C
ATOM    214  C   ALA A  28      38.728  26.591  12.611  1.00  9.17           C
ATOM    217  N   LYS A  29      37.633  26.543  11.867  1.00  8.96           N
ATOM    218  CA  LYS A  29      37.471  27.391  10.668  1.00  7.90           C
ATOM    219  C   LYS A  29      37.441  28.882  11.052  1.00  6.92           C
ATOM    226  N   ILE A  30      36.811  29.170  12.192  1.00  4.57           N
ATOM    227  CA  ILE A  30      36.731  30.570  12.645  1.00  5.58           C
ATOM    228  C   ILE A  30      38.148  30.981  13.069  1.00  7.26           C
END
`

func TestAsciiStructureHelixStartsWithH(t *testing.T) {
	tool := NewAsciiStructure(t.TempDir())
	in, _ := json.Marshal(map[string]any{"pdb": writePDB(t, idealHelixPDB)})
	res, err := tool.Execute(context.Background(), in)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var out struct {
		Chains map[string]struct {
			SS  string `json:"ss"`
			Seq string `json:"seq"`
		} `json:"chains"`
	}
	if err := json.Unmarshal(res.Output, &out); err != nil {
		t.Fatalf("Output is not JSON: %v", err)
	}
	a, ok := out.Chains["A"]
	if !ok {
		t.Fatal("chain A missing in output")
	}
	// The middle of a 6-residue helix must classify as helix; the first and
	// last residues lack a full dihedral context.
	if !strings.Contains(a.SS, "HHHH") {
		t.Errorf("expected at least 4 H in a row, got SS = %q", a.SS)
	}
	if len(a.Seq) != len(a.SS) {
		t.Errorf("SS length %d != SEQ length %d", len(a.SS), len(a.Seq))
	}
}

func TestAsciiStructureCoilWhenNoBackbone(t *testing.T) {
	caOnly := `ATOM      1  CA  ALA A   1       0.000   0.000   0.000
ATOM      2  CA  ALA A   2       3.800   0.000   0.000
ATOM      3  CA  ALA A   3       7.600   0.000   0.000
END
`
	tool := NewAsciiStructure(t.TempDir())
	in, _ := json.Marshal(map[string]any{"pdb": writePDB(t, caOnly)})
	res, err := tool.Execute(context.Background(), in)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var out struct {
		Chains map[string]struct {
			SS string `json:"ss"`
		} `json:"chains"`
	}
	_ = json.Unmarshal(res.Output, &out)
	// No N/C → φ/ψ are undefined → every residue is coil.
	if strings.ContainsAny(out.Chains["A"].SS, "HE") {
		t.Errorf("SS without backbone N/C must be all coil, got %q", out.Chains["A"].SS)
	}
}

func TestAsciiStructureDisplayHasChainHeader(t *testing.T) {
	tool := NewAsciiStructure(t.TempDir())
	in, _ := json.Marshal(map[string]any{"pdb": writePDB(t, idealHelixPDB)})
	res, err := tool.Execute(context.Background(), in)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(res.Display, "chain A") {
		t.Errorf("Display %q must mention chain A", res.Display)
	}
}
