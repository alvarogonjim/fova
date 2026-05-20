package viz

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"github.com/alvarogonjim/fova/internal/domain"
	"github.com/alvarogonjim/fova/internal/tools"
)

// AsciiStructure implements viz.ascii_structure: emit a DSSP-lite secondary-
// structure string (HHHHEEEE---) per chain, with the matching one-letter
// sequence beneath, computed from the φ/ψ dihedrals of the backbone.
type AsciiStructure struct {
	noopMeta
	workspace string
}

// NewAsciiStructure builds the viz.ascii_structure tool. The workspace is
// only used for the Provenance lineage record — this tool returns its output
// inline and does not write a file.
func NewAsciiStructure(workspace string) *AsciiStructure {
	return &AsciiStructure{workspace: workspace}
}

func (*AsciiStructure) Name() string { return "viz.ascii_structure" }
func (*AsciiStructure) Description() string {
	return "Emit a DSSP-lite secondary-structure string (HHHEEE---) per chain from a PDB, with the aligned sequence."
}
func (*AsciiStructure) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"pdb": map[string]any{"type": "string", "description": "Path to a PDB file."},
		},
		"required": []string{"pdb"},
	}
}

func (t *AsciiStructure) Execute(_ context.Context, input json.RawMessage) (tools.Result, error) {
	var in struct {
		PDB string `json:"pdb"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return tools.Result{}, fmt.Errorf("viz.ascii_structure: parse input: %w", err)
	}
	if in.PDB == "" {
		return tools.Result{}, fmt.Errorf("viz.ascii_structure: pdb is required")
	}
	chains, err := parsePDBAtoms(in.PDB)
	if err != nil {
		return tools.Result{}, fmt.Errorf("viz.ascii_structure: %w", err)
	}
	type chainOut struct {
		SS  string `json:"ss"`
		Seq string `json:"seq"`
	}
	outChains := map[string]chainOut{}
	var displayLines []string
	for _, id := range chainOrder(chains) {
		ch := chains[id]
		ss := classifySS(ch)
		seq := seqOneLetter(ch.ResName)
		outChains[id] = chainOut{SS: ss, Seq: seq}
		displayLines = append(displayLines,
			fmt.Sprintf("chain %s SS  %s", id, ss),
			fmt.Sprintf("        SEQ %s", seq),
		)
	}
	body, _ := json.Marshal(map[string]any{"chains": outChains})
	return tools.Result{
		Output:     body,
		Display:    strings.Join(displayLines, "\n"),
		Provenance: domain.NewToolCallRef("viz.ascii_structure", input),
	}, nil
}

// classifySS runs the DSSP-lite pipeline:
//
//  1. Compute φ (C[i-1]–N[i]–CA[i]–C[i]) and ψ (N[i]–CA[i]–C[i]–N[i+1]) for
//     every i with full context. Terminal residues and any residue missing
//     an N or C atom are marked coil.
//  2. Helix when φ ∈ [−90°, −30°] AND ψ ∈ [−75°, −15°].
//  3. Strand when φ ∈ [−160°, −80°] AND ψ ∈ [90°, 170°].
//  4. Smooth out runs shorter than 4 (H) / 3 (E) residues to coil so noisy
//     single hits don't pollute the printout.
func classifySS(ch *pdbChain) string {
	n := len(ch.CA)
	if n == 0 {
		return ""
	}
	// Backbone N/C must align with CA for the dihedral calculation. Without
	// matching counts (CA-only PDBs, broken parsers) every residue is coil.
	hasBackbone := len(ch.N) == n && len(ch.C) == n
	raw := make([]byte, n)
	for i := range raw {
		raw[i] = '-' // coil
	}
	if hasBackbone {
		for i := 1; i < n-1; i++ {
			phi := dihedral(ch.C[i-1], ch.N[i], ch.CA[i], ch.C[i])
			psi := dihedral(ch.N[i], ch.CA[i], ch.C[i], ch.N[i+1])
			switch {
			case phi >= -90 && phi <= -30 && psi >= -75 && psi <= -15:
				raw[i] = 'H'
			case phi >= -160 && phi <= -80 && psi >= 90 && psi <= 170:
				raw[i] = 'E'
			}
		}
	}
	return smoothSS(raw)
}

// smoothSS replaces runs shorter than the minimum length (4 for H, 3 for E)
// with coil dashes so only meaningful regions appear in the printout.
func smoothSS(raw []byte) string {
	const minH, minE = 4, 3
	out := append([]byte(nil), raw...)
	i := 0
	for i < len(out) {
		ch := out[i]
		if ch != 'H' && ch != 'E' {
			i++
			continue
		}
		j := i
		for j < len(out) && out[j] == ch {
			j++
		}
		run := j - i
		min := minH
		if ch == 'E' {
			min = minE
		}
		if run < min {
			for k := i; k < j; k++ {
				out[k] = '-'
			}
		}
		i = j
	}
	return string(out)
}

// dihedral returns the φ-style dihedral angle (in degrees) defined by the
// four points a-b-c-d using the standard atan2 formulation.
func dihedral(a, b, c, d vec3) float64 {
	b1 := sub(b, a)
	b2 := sub(c, b)
	b3 := sub(d, c)
	n1 := cross(b1, b2)
	n2 := cross(b2, b3)
	x := dot(n1, n2)
	y := dot(cross(n1, n2), norm(b2))
	return math.Atan2(y, x) * 180 / math.Pi
}

func sub(a, b vec3) vec3    { return vec3{a.X - b.X, a.Y - b.Y, a.Z - b.Z} }
func dot(a, b vec3) float64 { return a.X*b.X + a.Y*b.Y + a.Z*b.Z }
func cross(a, b vec3) vec3  { return vec3{a.Y*b.Z - a.Z*b.Y, a.Z*b.X - a.X*b.Z, a.X*b.Y - a.Y*b.X} }
func norm(v vec3) vec3 {
	l := math.Sqrt(v.X*v.X + v.Y*v.Y + v.Z*v.Z)
	if l == 0 {
		return v
	}
	return vec3{v.X / l, v.Y / l, v.Z / l}
}

// seqOneLetter maps three-letter residue names to the canonical one-letter
// codes. Anything unrecognised becomes 'X' so the SEQ line always aligns with
// the SS line one-to-one.
func seqOneLetter(resNames []string) string {
	tbl := map[string]byte{
		"ALA": 'A', "ARG": 'R', "ASN": 'N', "ASP": 'D', "CYS": 'C',
		"GLN": 'Q', "GLU": 'E', "GLY": 'G', "HIS": 'H', "ILE": 'I',
		"LEU": 'L', "LYS": 'K', "MET": 'M', "PHE": 'F', "PRO": 'P',
		"SER": 'S', "THR": 'T', "TRP": 'W', "TYR": 'Y', "VAL": 'V',
	}
	out := make([]byte, len(resNames))
	for i, r := range resNames {
		if c, ok := tbl[r]; ok {
			out[i] = c
		} else {
			out[i] = 'X'
		}
	}
	return string(out)
}
