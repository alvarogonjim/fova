package viz

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"

	"github.com/alvarogonjim/fova/internal/domain"
	"github.com/alvarogonjim/fova/internal/tools"
)

// contactThresholdAngstroms is the visual saturation point — anything closer
// than this maps to black, anything farther fades to white. ~8 Å is the usual
// inter-chain contact cutoff in CA-based maps.
const contactThresholdAngstroms = 8.0

// pixelsPerResidue scales the heatmap up so a 30×30 contact matrix renders to
// a 300×300 PNG — large enough to read in the TUI inline-graphics layer.
const pixelsPerResidue = 10

// ContactMap implements viz.contact_map: render an inter-chain Cα–Cα distance
// heatmap as a greyscale PNG.
type ContactMap struct {
	noopMeta
	workspace string
}

// NewContactMap builds the viz.contact_map tool.
func NewContactMap(workspace string) *ContactMap {
	return &ContactMap{workspace: workspace}
}

func (*ContactMap) Name() string { return "viz.contact_map" }
func (*ContactMap) Description() string {
	return "Render an inter-chain Cα–Cα distance heatmap from a PDB as a greyscale PNG."
}
func (*ContactMap) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"pdb":     map[string]any{"type": "string", "description": "Path to a PDB file."},
			"chain_a": map[string]any{"type": "string", "description": "Chain ID for the rows (default: first chain in the PDB)."},
			"chain_b": map[string]any{"type": "string", "description": "Chain ID for the columns (default: second chain in the PDB)."},
		},
		"required": []string{"pdb"},
	}
}

func (t *ContactMap) Execute(_ context.Context, input json.RawMessage) (tools.Result, error) {
	var in struct {
		PDB    string `json:"pdb"`
		ChainA string `json:"chain_a"`
		ChainB string `json:"chain_b"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return tools.Result{}, fmt.Errorf("viz.contact_map: parse input: %w", err)
	}
	if in.PDB == "" {
		return tools.Result{}, fmt.Errorf("viz.contact_map: pdb is required")
	}
	chains, err := parsePDBAtoms(in.PDB)
	if err != nil {
		return tools.Result{}, fmt.Errorf("viz.contact_map: %w", err)
	}
	order := chainOrder(chains)
	if in.ChainA == "" {
		in.ChainA = order[0]
	}
	if in.ChainB == "" {
		if len(order) < 2 {
			return tools.Result{}, fmt.Errorf("viz.contact_map: PDB has only one chain (%s); pass chain_a and chain_b explicitly", order[0])
		}
		in.ChainB = order[1]
	}
	a, okA := chains[in.ChainA]
	b, okB := chains[in.ChainB]
	if !okA || !okB {
		return tools.Result{}, fmt.Errorf("viz.contact_map: chain %s or %s not found in PDB", in.ChainA, in.ChainB)
	}
	if len(a.CA) == 0 || len(b.CA) == 0 {
		return tools.Result{}, fmt.Errorf("viz.contact_map: chain %s or %s has no Cα atoms", in.ChainA, in.ChainB)
	}

	outPath, err := OutputPath(t.workspace, "contact_map", "png")
	if err != nil {
		return tools.Result{}, fmt.Errorf("viz.contact_map: %w", err)
	}
	img := renderContactMap(a.CA, b.CA)
	if err := writePNG(outPath, img); err != nil {
		return tools.Result{}, fmt.Errorf("viz.contact_map: write png: %w", err)
	}
	body, _ := json.Marshal(map[string]any{
		"path":    outPath,
		"chain_a": in.ChainA,
		"chain_b": in.ChainB,
		"width":   img.Bounds().Dx(),
		"height":  img.Bounds().Dy(),
	})
	return tools.Result{
		Output:     body,
		Display:    fmt.Sprintf("contact_map: chain %s × chain %s (%d × %d residues) → %s", in.ChainA, in.ChainB, len(a.CA), len(b.CA), outPath),
		Provenance: domain.NewToolCallRef("viz.contact_map", input),
	}, nil
}

// renderContactMap returns a greyscale image whose pixel (i, j) intensity is
// inversely proportional to the Cα–Cα distance between residue i of chainA and
// residue j of chainB: ≤0 Å → black, ≥contactThreshold Å → white.
func renderContactMap(chainA, chainB []vec3) *image.Gray {
	rows, cols := len(chainA), len(chainB)
	w, h := cols*pixelsPerResidue, rows*pixelsPerResidue
	img := image.NewGray(image.Rect(0, 0, w, h))
	for i, ca := range chainA {
		for j, cb := range chainB {
			d := distance(ca, cb)
			// 0 → 0 (black), contactThreshold → 255 (white), beyond → 255.
			g := d / contactThresholdAngstroms
			if g < 0 {
				g = 0
			}
			if g > 1 {
				g = 1
			}
			shade := color.Gray{Y: uint8(g * 255)}
			// Paint a pixelsPerResidue × pixelsPerResidue block.
			for px := 0; px < pixelsPerResidue; px++ {
				for py := 0; py < pixelsPerResidue; py++ {
					img.SetGray(j*pixelsPerResidue+px, i*pixelsPerResidue+py, shade)
				}
			}
		}
	}
	return img
}

// writePNG encodes img to path.
func writePNG(path string, img image.Image) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}
