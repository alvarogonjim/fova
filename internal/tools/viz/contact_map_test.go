package viz

import (
	"bytes"
	"context"
	"encoding/json"
	"image/png"
	"os"
	"strings"
	"testing"
)

func TestContactMapProducesPNG(t *testing.T) {
	ws := t.TempDir()
	pdbPath := writePDB(t, twoChainPDB)
	tool := NewContactMap(ws)
	in, _ := json.Marshal(map[string]any{"pdb": pdbPath})
	res, err := tool.Execute(context.Background(), in)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var out struct {
		Path   string `json:"path"`
		ChainA string `json:"chain_a"`
		ChainB string `json:"chain_b"`
		Width  int    `json:"width"`
		Height int    `json:"height"`
	}
	if err := json.Unmarshal(res.Output, &out); err != nil {
		t.Fatalf("Output is not JSON: %v", err)
	}
	if out.ChainA != "A" || out.ChainB != "B" {
		t.Errorf("default chains = %s/%s, want A/B", out.ChainA, out.ChainB)
	}
	if !strings.HasSuffix(out.Path, ".png") {
		t.Errorf("path %q is not a .png", out.Path)
	}
	body, err := os.ReadFile(out.Path)
	if err != nil {
		t.Fatalf("read png: %v", err)
	}
	if !bytes.HasPrefix(body, []byte("\x89PNG\r\n\x1a\n")) {
		t.Fatalf("file is not a PNG (header = %q)", body[:8])
	}
	// Verify the image decoder is happy and the dimensions match what we
	// reported in Output.
	img, err := png.Decode(bytes.NewReader(body))
	if err != nil {
		t.Fatalf("png.Decode: %v", err)
	}
	if img.Bounds().Dx() != out.Width || img.Bounds().Dy() != out.Height {
		t.Errorf("decoded %dx%d, reported %dx%d", img.Bounds().Dx(), img.Bounds().Dy(), out.Width, out.Height)
	}
}

func TestContactMapMissingPDB(t *testing.T) {
	tool := NewContactMap(t.TempDir())
	if _, err := tool.Execute(context.Background(), []byte(`{"pdb":"/no/such.pdb"}`)); err == nil {
		t.Fatal("expected an error when the pdb does not exist")
	}
}

func TestContactMapExplicitChains(t *testing.T) {
	tool := NewContactMap(t.TempDir())
	in, _ := json.Marshal(map[string]any{
		"pdb": writePDB(t, twoChainPDB), "chain_a": "B", "chain_b": "A",
	})
	res, err := tool.Execute(context.Background(), in)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var out struct {
		ChainA string `json:"chain_a"`
		ChainB string `json:"chain_b"`
	}
	_ = json.Unmarshal(res.Output, &out)
	if out.ChainA != "B" || out.ChainB != "A" {
		t.Errorf("chains = %s/%s, want B/A", out.ChainA, out.ChainB)
	}
}

func TestContactMapNotEnoughChainsErrors(t *testing.T) {
	singleChain := `ATOM      1  CA  ALA A   1       0.000   0.000   0.000
ATOM      2  CA  ALA A   2       3.800   0.000   0.000
END
`
	tool := NewContactMap(t.TempDir())
	in, _ := json.Marshal(map[string]any{"pdb": writePDB(t, singleChain)})
	if _, err := tool.Execute(context.Background(), in); err == nil {
		t.Fatal("expected an error when the PDB has only one chain")
	}
}
