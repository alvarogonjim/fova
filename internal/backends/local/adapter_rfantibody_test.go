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

// rfantibodyTestEnv builds an AdapterEnv with the container-mode rfantibody
// recipe and a registry whose ~/.fova/models/rfantibody directory exists on
// disk. Modelled on boltz2TestEnv.
func rfantibodyTestEnv(t *testing.T) AdapterEnv {
	t.Helper()
	home := t.TempDir()
	reg, err := LoadRegistry(home)
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	if err := os.MkdirAll(ModelsRoot(home, "rfantibody"), 0o755); err != nil {
		t.Fatal(err)
	}
	rec, ok := reg.Tool("rfantibody")
	if !ok {
		t.Fatal("rfantibody missing from registry")
	}
	return AdapterEnv{
		Recipe:   rec,
		WorkDir:  t.TempDir(),
		Registry: reg,
	}
}

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

func TestBuildRFantibodyDriver(t *testing.T) {
	tmp := 0.2
	script := buildRFantibodyDriver(domain.RFantibodyParams{
		NumDesigns: 20, Hotspots: "T305,T456", DesignLoops: "H3:5-13",
		SeqsPerStruct: 4, Temperature: &tmp,
	}, "/work/target.pdb", "/work/framework.pdb")
	for _, want := range []string{
		"uv run --project /opt/rfantibody rfdiffusion",
		"-t /work/target.pdb", "-f /work/framework.pdb",
		"-h T305,T456", "-n 20", "-l H3:5-13",
		"uv run --project /opt/rfantibody proteinmpnn",
		"-n 4", "-t 0.2",
		"uv run --project /opt/rfantibody rf2",
		"qvextract /work/predictions.qv", "qvscorefile /work/predictions.qv",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("driver missing %q in:\n%s", want, script)
		}
	}
}

func TestRFantibodyAdapterInvoke(t *testing.T) {
	env := rfantibodyTestEnv(t) // helper modelled on boltz2TestEnv
	target := filepath.Join(t.TempDir(), "ag.pdb")
	if err := os.WriteFile(target, []byte("ATOM\nEND\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stubContainerRuntime(t, func(args []string) error {
		if len(args) < 2 || args[1] != "run" {
			return nil
		}
		out := filepath.Join(env.WorkDir, "out")
		if err := os.MkdirAll(out, 0o755); err != nil {
			return err
		}
		_ = os.WriteFile(filepath.Join(out, "scores.tsv"),
			[]byte("tag\tplddt\tpae\nab_0\t80.0\t8.0\n"), 0o644)
		return os.WriteFile(filepath.Join(out, "ab_0.pdb"), []byte("ATOM\nEND\n"), 0o644)
	})
	body := []byte(`{"target":"` + target + `","hotspots":"T10","framework":"nanobody"}`)
	out, err := rfantibodyAdapter{}.Invoke(context.Background(), env, body)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	var resp designsEnvelope
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("not a designs envelope: %v", err)
	}
	if len(resp.Designs) != 1 || resp.Designs[0].Scores["plddt"] != 80.0 {
		t.Fatalf("want 1 scored design, got %+v", resp.Designs)
	}
}

func TestRFantibodyAdapterInvokeRejectsMissingTarget(t *testing.T) {
	env := rfantibodyTestEnv(t)
	if _, err := (rfantibodyAdapter{}).Invoke(context.Background(), env, []byte(`{"hotspots":"T10"}`)); err == nil {
		t.Fatal("expected an error when target is missing")
	}
}
