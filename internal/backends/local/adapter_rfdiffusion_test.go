package local

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alvarogonjim/fova/internal/domain"
)

func TestParseRFdiffusionOutput(t *testing.T) {
	outDir := t.TempDir()
	for i := 0; i < 2; i++ {
		if err := os.WriteFile(filepath.Join(outDir, fmt.Sprintf("out_%d.pdb", i)),
			[]byte("ATOM\nEND\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	designs, err := parseRFdiffusionOutput(outDir)
	if err != nil {
		t.Fatalf("parseRFdiffusionOutput: %v", err)
	}
	if len(designs) != 2 {
		t.Fatalf("want 2 designs, got %d", len(designs))
	}
	if designs[0].StructureFile == "" {
		t.Error("structure_file must be set")
	}
	if len(designs[0].Sequence) != 0 {
		t.Error("RFdiffusion designs carry no sequence")
	}
}

func TestParseRFdiffusionOutputEmptyErrors(t *testing.T) {
	if _, err := parseRFdiffusionOutput(t.TempDir()); err == nil {
		t.Fatal("expected an error when no backbone PDBs are present")
	}
}

// rfdiffStubRunner records commands and, on the run_inference call, drops two
// backbone PDBs into the directory named by inference.output_prefix. It scans
// the raw command (not strings.Fields) so a contig with embedded spaces does
// not break output-prefix detection.
func rfdiffStubRunner(ran *[]string) CmdRunner {
	return func(ctx context.Context, dir, cmd string, log io.Writer) (string, error) {
		*ran = append(*ran, cmd)
		if log != nil {
			_, _ = log.Write([]byte("stub: " + cmd + "\n"))
		}
		if _, after, ok := strings.Cut(cmd, "inference.output_prefix="); ok {
			prefix, _, _ := strings.Cut(after, " ")
			outDir := filepath.Dir(prefix)
			for i := 0; i < 2; i++ {
				p := filepath.Join(outDir, fmt.Sprintf("out_%d.pdb", i))
				if err := os.WriteFile(p, []byte("ATOM\nEND\n"), 0o644); err != nil {
					return "", err
				}
			}
		}
		return "ok", nil
	}
}

// rfdiffTestEnv builds an AdapterEnv with an installed-looking recipe and a
// registry whose rfdiffusion_weights directory exists on disk. logBuf and
// progress (when non-nil) capture the adapter's log writes and progress ticks.
func rfdiffTestEnv(t *testing.T, ran *[]string, logBuf *bytes.Buffer, progress *[]float64) AdapterEnv {
	t.Helper()
	home := t.TempDir()
	reg, err := LoadRegistry(home)
	if err != nil {
		t.Fatal(err)
	}
	asset, ok := reg.DataAsset("rfdiffusion_weights")
	if !ok {
		t.Fatal("rfdiffusion_weights data asset is not registered")
	}
	if err := os.MkdirAll(asset.TargetDir, 0o755); err != nil {
		t.Fatal(err)
	}
	env := AdapterEnv{
		Recipe:   ToolRecipe{Name: "rfdiffusion", InstallDir: t.TempDir(), VenvDir: t.TempDir()},
		Run:      rfdiffStubRunner(ran),
		WorkDir:  t.TempDir(),
		Registry: reg,
	}
	if logBuf != nil {
		env.Log = logBuf
	}
	if progress != nil {
		env.Progress = func(f float64) { *progress = append(*progress, f) }
	}
	return env
}

func TestRFdiffusionAdapterInvoke(t *testing.T) {
	var ran []string
	var logBuf bytes.Buffer
	var progress []float64
	env := rfdiffTestEnv(t, &ran, &logBuf, &progress)

	out, err := rfdiffusionAdapter{}.Invoke(context.Background(), env,
		[]byte(`{"contigs":"50-70","num_designs":2}`))
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	var env2 designsEnvelope
	if err := json.Unmarshal(out, &env2); err != nil {
		t.Fatalf("output is not valid designs JSON: %v", err)
	}
	if len(env2.Designs) != 2 {
		t.Fatalf("want 2 designs, got %d", len(env2.Designs))
	}
	if env2.Designs[0].StructureFile == "" {
		t.Error("design structure_file must be set")
	}
	if !strings.HasPrefix(env2.Designs[0].StructureFile, env.Registry.Home()) {
		t.Errorf("structure_file %q must be under FOVA_HOME %q (outlives the temp WorkDir)",
			env2.Designs[0].StructureFile, env.Registry.Home())
	}
	if len(ran) != 1 {
		t.Fatalf("want 1 command, got %d: %v", len(ran), ran)
	}
	if !strings.Contains(ran[0], "contigmap.contigs=[50-70]") {
		t.Errorf("command must carry the contig map: %s", ran[0])
	}
	if !strings.Contains(ran[0], "inference.num_designs=2") {
		t.Errorf("command must carry num_designs: %s", ran[0])
	}
	if !strings.Contains(ran[0], "Base_ckpt.pt") {
		t.Errorf("no target → Base_ckpt.pt expected: %s", ran[0])
	}
	// Bug 2: log must be written and progress must be ticked.
	if logBuf.Len() == 0 {
		t.Error("env.Log should receive the stubbed run_inference.py output")
	}
	if !strings.Contains(logBuf.String(), "run_inference.py") {
		t.Errorf("env.Log should carry the run_inference.py invocation, got: %q", logBuf.String())
	}
	if len(progress) < 2 {
		t.Errorf("env.Progress should have been called at least twice, got %v", progress)
	}
}

func TestRFdiffusionAdapterInvokeComplexCheckpoint(t *testing.T) {
	var ran []string
	env := rfdiffTestEnv(t, &ran, nil, nil)
	target := filepath.Join(t.TempDir(), "t.pdb")
	if err := os.WriteFile(target, []byte("ATOM\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := rfdiffusionAdapter{}.Invoke(context.Background(), env,
		[]byte(`{"contigs":"A1-50/0 50-70","target":"`+target+`","hotspots":"A30,A33"}`))
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if !strings.Contains(ran[0], "Complex_base_ckpt.pt") {
		t.Errorf("a target → Complex_base_ckpt.pt expected: %s", ran[0])
	}
	if !strings.Contains(ran[0], "inference.input_pdb="+target) {
		t.Errorf("command must carry the target pdb: %s", ran[0])
	}
	if !strings.Contains(ran[0], "ppi.hotspot_res=[A30,A33]") {
		t.Errorf("command must carry the hotspots: %s", ran[0])
	}
}

func TestRFdiffusionAdapterInvokeMissingContigs(t *testing.T) {
	var ran []string
	env := rfdiffTestEnv(t, &ran, nil, nil)
	if _, err := (rfdiffusionAdapter{}).Invoke(context.Background(), env, []byte(`{"num_designs":1}`)); err == nil {
		t.Fatal("expected an error when contigs is missing")
	}
}

func TestRFdiffusionAdapterInvokeWeightsMissing(t *testing.T) {
	home := t.TempDir()
	reg, err := LoadRegistry(home)
	if err != nil {
		t.Fatal(err)
	}
	// rfdiffusion_weights directory is deliberately NOT created.
	env := AdapterEnv{
		Recipe:   ToolRecipe{Name: "rfdiffusion", InstallDir: t.TempDir(), VenvDir: t.TempDir()},
		WorkDir:  t.TempDir(),
		Registry: reg,
	}
	_, err = rfdiffusionAdapter{}.Invoke(context.Background(), env, []byte(`{"contigs":"50-70"}`))
	if err == nil {
		t.Fatal("expected an error when the weights directory is absent")
	}
}

func TestRFdiffusionAdapterInvokeNotInstalled(t *testing.T) {
	reg, err := LoadRegistry(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	env := AdapterEnv{
		Recipe:   ToolRecipe{InstallDir: filepath.Join(t.TempDir(), "gone"), VenvDir: t.TempDir()},
		WorkDir:  t.TempDir(),
		Registry: reg,
	}
	_, err = rfdiffusionAdapter{}.Invoke(context.Background(), env, []byte(`{"contigs":"50-70"}`))
	if err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Fatalf("want a 'not installed' error, got: %v", err)
	}
}

func TestRFdiffusionAdapterInvokeBadTarget(t *testing.T) {
	var ran []string
	env := rfdiffTestEnv(t, &ran, nil, nil)

	// A target that is not a .pdb file.
	notPDB := filepath.Join(t.TempDir(), "target.txt")
	if err := os.WriteFile(notPDB, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := (rfdiffusionAdapter{}).Invoke(context.Background(), env,
		[]byte(`{"contigs":"50-70","target":"`+notPDB+`"}`)); err == nil {
		t.Error("expected an error when target is not a .pdb file")
	}

	// A .pdb target path that does not exist.
	_, err := rfdiffusionAdapter{}.Invoke(context.Background(), env,
		[]byte(`{"contigs":"50-70","target":"/no/such/file.pdb"}`))
	if err == nil {
		t.Error("expected an error when the target file does not exist")
	} else if !strings.Contains(err.Error(), "fs.read_structure") {
		// Bug 4: error should steer the agent at fs.read_structure.
		t.Errorf("error %q should point at fs.read_structure", err)
	}
	if len(ran) != 0 {
		t.Errorf("a bad target must not run any command, got %d", len(ran))
	}
}

func TestRFdiffusionArgs(t *testing.T) {
	det := true
	sym := true
	gs := 5.0
	got := strings.Join(rfdiffusionArgs(domain.RFdiffusionParams{
		Target: "/work/t.pdb", Hotspots: "A30,A33", Contigs: "50-100",
		NumDesigns: 8, Deterministic: &det,
		Symmetric: &sym, SymmetryKind: "cyclic", NChains: 4,
		PartialT: 12, GuidingPotentials: []string{"binder_ROG"}, GuideScale: &gs,
	}, "/models/Base_ckpt.pt"), " ")
	for _, want := range []string{
		"inference.input_pdb=/work/t.pdb",
		"inference.num_designs=8",
		"'ppi.hotspot_res=[A30,A33]'",
		"'contigmap.contigs=[50-100]'",
		"inference.ckpt_override_path=/models/Base_ckpt.pt",
		"inference.deterministic=true",
		"inference.symmetric=true",
		"symmetry.symmetry_kind=cyclic",
		"symmetry.n_chains=4",
		"diffuser.partial_T=12",
		"'potentials.guiding_potentials=[binder_ROG]'",
		"potentials.guide_scale=5",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("args missing %q in %q", want, got)
		}
	}
	// Unset optionals omit their overrides.
	if strings.Contains(strings.Join(rfdiffusionArgs(domain.RFdiffusionParams{Contigs: "X"}, "/m/c.pt"), " "), "noise_scale") {
		t.Error("unset noise_scale must omit the override")
	}
}

func TestRunDesignRFdiffusionIsRegistered(t *testing.T) {
	reg, err := LoadRegistry(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// Missing contigs makes Invoke fail fast — which still proves
	// design.rfdiffusion is registered and dispatched.
	_, err = RunDesign(context.Background(), reg, "design.rfdiffusion", []byte(`{"num_designs":1}`), io.Discard, nil)
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), "no local adapter") {
		t.Fatalf("design.rfdiffusion must be registered, got: %v", err)
	}
}
