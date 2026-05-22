package local

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alvarogonjim/fova/internal/domain"
)

// ligandMPNNTestEnv builds an AdapterEnv with the container-mode ligandmpnn
// recipe and a registry whose ~/.fova/models/ligandmpnn directory exists on
// disk (the install-time weights cache the adapter os.Stats).
func ligandMPNNTestEnv(t *testing.T) AdapterEnv {
	t.Helper()
	home := t.TempDir()
	reg, err := LoadRegistry(home)
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	if err := os.MkdirAll(ModelsRoot(home, "ligandmpnn"), 0o755); err != nil {
		t.Fatal(err)
	}
	rec, ok := reg.Tool("ligandmpnn")
	if !ok {
		t.Fatal("ligandmpnn missing from registry")
	}
	return AdapterEnv{
		Recipe:   rec,
		WorkDir:  t.TempDir(),
		Registry: reg,
	}
}

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

func TestLigandMPNNArgs(t *testing.T) {
	temp := 0.2
	got := ligandMPNNArgs(domain.LigandMPNNParams{
		ModelType: "ligand_mpnn", NumDesigns: 8, Temperature: &temp,
		RedesignedResidues: "A23 A24",
	})
	joined := strings.Join(got, " ")
	for _, want := range []string{
		"--model_type ligand_mpnn", "--number_of_batches 8",
		"--temperature 0.2", "--redesigned_residues A23 A24",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("args missing %q in %q", want, joined)
		}
	}
	// Unset optionals omit their flags.
	if strings.Contains(strings.Join(ligandMPNNArgs(domain.LigandMPNNParams{}), " "), "--seed") {
		t.Error("an unset seed must omit the flag")
	}
}

func TestCheckpointForModelType(t *testing.T) {
	if got := checkpointForModelType("ligand_mpnn"); got == "" {
		t.Error("ligand_mpnn must map to a checkpoint filename")
	}
	if got := checkpointForModelType(""); got != checkpointForModelType("ligand_mpnn") {
		t.Error("empty model_type must default to the ligand_mpnn checkpoint")
	}
}

func TestLigandMPNNAdapterInvoke(t *testing.T) {
	env := ligandMPNNTestEnv(t)
	pdb := filepath.Join(t.TempDir(), "bb.pdb")
	if err := os.WriteFile(pdb, []byte("ATOM\nEND\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stubContainerRuntime(t, func(args []string) error {
		if len(args) < 2 || args[1] != "run" {
			return nil
		}
		seqs := filepath.Join(env.WorkDir, "out", "seqs")
		if err := os.MkdirAll(seqs, 0o755); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(seqs, "bb.fa"),
			[]byte(">bb, native\nMKQ\n>bb, id=1, overall_confidence=0.7\nMRD\n"), 0o644)
	})
	body := []byte(`{"pdb":"` + pdb + `","model_type":"ligand_mpnn"}`)
	out, err := ligandMPNNAdapter{}.Invoke(context.Background(), env, body)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	var resp designsEnvelope
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("not a designs envelope: %v", err)
	}
	if len(resp.Designs) != 1 || resp.Designs[0].Scores["overall_confidence"] != 0.7 {
		t.Fatalf("want 1 scored design, got %+v", resp.Designs)
	}
}

func TestLigandMPNNAdapterInvokeRejectsMissingPDB(t *testing.T) {
	env := ligandMPNNTestEnv(t)
	if _, err := (ligandMPNNAdapter{}).Invoke(context.Background(), env, []byte(`{}`)); err == nil {
		t.Fatal("expected an error when pdb is missing")
	}
}
