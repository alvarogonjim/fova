package local

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// boltzGenStubRuntime swaps in lookPath + runCmd + runCmdOutput stubs so the
// adapter's container-mode path can run without invoking podman/docker.
// The returned ranArgs slice collects every argv the adapter dispatched
// through runCmd. fakeOutDir is the host path the stub seeds with a fake
// BoltzGen output tree when it sees a `run` subcommand.
func boltzGenStubRuntime(t *testing.T, ranArgs *[][]string, fakeOutDir string) func() {
	t.Helper()
	oldLook := lookPath
	oldRun := runCmd
	oldOut := runCmdOutput
	lookPath = func(bin string) (string, error) {
		if bin == "podman" {
			return "/usr/bin/podman", nil
		}
		return "", errors.New("not found")
	}
	runCmd = func(cmd *exec.Cmd) error {
		args := append([]string(nil), cmd.Args...)
		*ranArgs = append(*ranArgs, args)
		// Seed a fake BoltzGen final-set directory + a couple of CIFs so
		// the adapter's parseBoltzGenOutput step can succeed.
		for i, a := range args {
			if a == "--budget" && i+1 < len(args) {
				budget := args[i+1]
				if err := os.MkdirAll(filepath.Join(fakeOutDir,
					"final_ranked_designs", "final_"+budget+"_designs"), 0o755); err != nil {
					return err
				}
				for j := 0; j < 2; j++ {
					p := filepath.Join(fakeOutDir, "final_ranked_designs",
						"final_"+budget+"_designs", fmt.Sprintf("design_%d.cif", j))
					if err := os.WriteFile(p, []byte("data_design\n"), 0o644); err != nil {
						return err
					}
				}
			}
		}
		return nil
	}
	runCmdOutput = func(cmd *exec.Cmd) ([]byte, error) {
		// image inspect → success means "image exists" in ImageExists.
		return []byte("[{}]\n"), nil
	}
	return func() {
		lookPath = oldLook
		runCmd = oldRun
		runCmdOutput = oldOut
	}
}

// boltzGenTestEnv assembles an AdapterEnv with a container-mode boltzgen
// recipe, a registry whose ~/.fova/models/boltzgen cache exists on disk,
// and a target .cif file staged into a fresh workdir.
func boltzGenTestEnv(t *testing.T) (env AdapterEnv, targetPath string, fakeOutDir string) {
	t.Helper()
	home := t.TempDir()
	reg, err := LoadRegistry(home)
	if err != nil {
		t.Fatal(err)
	}
	// Pre-create the weights cache the adapter requires.
	weightsDir := ModelsRoot(home, "boltzgen")
	if err := os.MkdirAll(weightsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Stage a .cif target outside env.WorkDir so the adapter copies it in.
	targetDir := t.TempDir()
	targetPath = filepath.Join(targetDir, "target.cif")
	if err := os.WriteFile(targetPath, []byte("data_target\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	workDir := t.TempDir()
	fakeOutDir = filepath.Join(workDir, "out")
	env = AdapterEnv{
		Recipe: ToolRecipe{
			Name:     "boltzgen",
			ImageTag: "fova/boltzgen:v0.3.2",
			GPU:      true,
		},
		WorkDir:  workDir,
		Registry: reg,
	}
	return env, targetPath, fakeOutDir
}

func TestBoltzGenAdapterInvokeContainerMode(t *testing.T) {
	env, target, fakeOutDir := boltzGenTestEnv(t)
	var ran [][]string
	restore := boltzGenStubRuntime(t, &ran, fakeOutDir)
	defer restore()
	var logBuf bytes.Buffer
	env.Log = &logBuf

	out, err := boltzGenAdapter{}.Invoke(context.Background(), env,
		[]byte(`{"target":"`+target+`","hotspots":"A30,A33","num_designs":4}`))
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	var envOut designsEnvelope
	if err := json.Unmarshal(out, &envOut); err != nil {
		t.Fatalf("output is not valid designs JSON: %v", err)
	}
	if len(envOut.Designs) != 2 {
		t.Fatalf("want 2 designs (from the seeded final-set CIFs), got %d", len(envOut.Designs))
	}
	if envOut.Designs[0].StructureFile == "" {
		t.Error("structure_file must be set on each design")
	}
	if !strings.HasPrefix(envOut.Designs[0].StructureFile, env.Registry.Home()) {
		t.Errorf("structure_file %q must live under FOVA_HOME %q so it outlives the temp WorkDir",
			envOut.Designs[0].StructureFile, env.Registry.Home())
	}

	// The adapter should have called RunContainer exactly once (image inspect
	// is plumbed through runCmdOutput, not runCmd).
	if len(ran) != 1 {
		t.Fatalf("want 1 runCmd invocation, got %d: %v", len(ran), ran)
	}
	argv := strings.Join(ran[0], " ")
	if !strings.Contains(argv, "fova/boltzgen:v0.3.2") {
		t.Errorf("argv must reference the boltzgen image tag, got: %s", argv)
	}
	if !strings.Contains(argv, "--num_designs") || !strings.Contains(argv, " 4 ") &&
		!strings.HasSuffix(argv, " 4") {
		t.Errorf("argv must carry --num_designs 4, got: %s", argv)
	}
	if !strings.Contains(argv, "--budget") {
		t.Errorf("argv must carry --budget, got: %s", argv)
	}
	if !strings.Contains(argv, "--protocol protein-anything") {
		t.Errorf("argv must default to protocol=protein-anything, got: %s", argv)
	}
	if !strings.Contains(argv, env.WorkDir+":/work") {
		t.Errorf("argv must bind-mount env.WorkDir at /work, got: %s", argv)
	}
	if !strings.Contains(argv, ModelsRoot(env.Registry.Home(), "boltzgen")+":/models") {
		t.Errorf("argv must bind-mount the boltzgen weights cache at /models, got: %s", argv)
	}
	// run subcommand + yaml path are part of the container Cmd, not flags.
	if !strings.Contains(argv, "run /work/in.yaml") {
		t.Errorf("argv must call `boltzgen run /work/in.yaml`, got: %s", argv)
	}

	// The yaml spec must have been written with the staged target filename and
	// the normalized hotspot residues.
	yamlBody, err := os.ReadFile(filepath.Join(env.WorkDir, "in.yaml"))
	if err != nil {
		t.Fatalf("in.yaml: %v", err)
	}
	body := string(yamlBody)
	if !strings.Contains(body, "path: target.cif") {
		t.Errorf("yaml must reference the staged target filename, got:\n%s", body)
	}
	if !strings.Contains(body, "binding: 30,33") {
		t.Errorf("yaml must normalize hotspots into binding indices, got:\n%s", body)
	}

	// Log + progress wiring (parallels the rfdiffusion test's assertions).
	// We don't tee a stub message here because runCmd is captured before
	// exec.Cmd actually runs — but the request must have completed.
	_ = logBuf
}

func TestBoltzGenAdapterInvokeBudgetCapped(t *testing.T) {
	env, target, fakeOutDir := boltzGenTestEnv(t)
	var ran [][]string
	restore := boltzGenStubRuntime(t, &ran, fakeOutDir)
	defer restore()

	// num_designs well above the cap: budget should clamp to boltzGenMaxBudget.
	_, err := boltzGenAdapter{}.Invoke(context.Background(), env,
		[]byte(`{"target":"`+target+`","num_designs":100}`))
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if len(ran) != 1 {
		t.Fatalf("want 1 runCmd invocation, got %d", len(ran))
	}
	argv := strings.Join(ran[0], " ")
	if !strings.Contains(argv, "--num_designs 100") {
		t.Errorf("argv must keep --num_designs 100, got: %s", argv)
	}
	if !strings.Contains(argv, fmt.Sprintf("--budget %d", boltzGenMaxBudget)) {
		t.Errorf("argv must cap --budget at %d, got: %s", boltzGenMaxBudget, argv)
	}
}

func TestBoltzGenAdapterInvokeMissingTarget(t *testing.T) {
	env, _, _ := boltzGenTestEnv(t)
	_, err := boltzGenAdapter{}.Invoke(context.Background(), env, []byte(`{"num_designs":1}`))
	if err == nil {
		t.Fatal("expected an error when target is missing")
	}
}

func TestBoltzGenAdapterInvokeNotFoundIncludesHint(t *testing.T) {
	env, _, _ := boltzGenTestEnv(t)
	_, err := boltzGenAdapter{}.Invoke(context.Background(), env,
		[]byte(`{"target":"/no/such/file.cif"}`))
	if err == nil {
		t.Fatal("expected an error when target file does not exist")
	}
	if !strings.Contains(err.Error(), "fs.read_structure") {
		t.Errorf("error %q should point at fs.read_structure", err)
	}
}

func TestBoltzGenAdapterInvokeBadExtension(t *testing.T) {
	env, _, _ := boltzGenTestEnv(t)
	notStruct := filepath.Join(t.TempDir(), "target.txt")
	if err := os.WriteFile(notStruct, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := boltzGenAdapter{}.Invoke(context.Background(), env,
		[]byte(`{"target":"`+notStruct+`"}`))
	if err == nil {
		t.Fatal("expected an error when target is not a .pdb/.cif file")
	}
}

func TestBoltzGenAdapterInvokeNotInstalled(t *testing.T) {
	// No ImageTag => container-only adapter must complain that boltzgen is
	// not installed.
	target := filepath.Join(t.TempDir(), "t.cif")
	if err := os.WriteFile(target, []byte("data_x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	reg, err := LoadRegistry(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	env := AdapterEnv{
		Recipe:   ToolRecipe{Name: "boltzgen"}, // no ImageTag
		WorkDir:  t.TempDir(),
		Registry: reg,
	}
	_, err = boltzGenAdapter{}.Invoke(context.Background(), env,
		[]byte(`{"target":"`+target+`"}`))
	if err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Fatalf("want a 'not installed' error, got: %v", err)
	}
}

func TestBoltzGenAdapterInvokeCreatesWeightsCache(t *testing.T) {
	// BoltzGen downloads its weights at container runtime, so a missing
	// weights cache must be created on demand, not rejected.
	home := t.TempDir()
	reg, err := LoadRegistry(home)
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "t.cif")
	if err := os.WriteFile(target, []byte("data_x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	env := AdapterEnv{
		Recipe: ToolRecipe{
			Name:     "boltzgen",
			ImageTag: "fova/boltzgen:v0.3.2",
			GPU:      true,
		},
		WorkDir:  t.TempDir(),
		Registry: reg,
	}
	// Stub runtime so Detect succeeds and ImageExists returns true.
	var ran [][]string
	restore := boltzGenStubRuntime(t, &ran, filepath.Join(env.WorkDir, "out"))
	defer restore()
	_, err = boltzGenAdapter{}.Invoke(context.Background(), env,
		[]byte(`{"target":"`+target+`"}`))
	if err != nil && strings.Contains(err.Error(), "weights cache") {
		t.Fatalf("a missing weights cache must not error, got: %v", err)
	}
	cache := ModelsRoot(reg.Home(), "boltzgen")
	if info, statErr := os.Stat(cache); statErr != nil || !info.IsDir() {
		t.Fatalf("Invoke must create the weights cache %s; stat err = %v", cache, statErr)
	}
}

func TestBoltzGenRecipeIsRegistered(t *testing.T) {
	reg, err := LoadRegistry(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	rec, ok := reg.Tool("boltzgen")
	if !ok {
		t.Fatal("boltzgen missing from registry")
	}
	if rec.ImageTag == "" {
		t.Error("boltzgen must be a container-mode recipe (ImageTag is empty)")
	}
}

func TestRunDesignBoltzGenIsRegistered(t *testing.T) {
	reg, err := LoadRegistry(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// A bogus target makes Invoke fail fast — which still proves
	// design.boltzgen is registered and dispatched through RunDesign.
	_, err = RunDesign(context.Background(), reg, "design.boltzgen",
		[]byte(`{"target":"/no/such/file.cif"}`), io.Discard, nil)
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), "no local adapter") {
		t.Fatalf("design.boltzgen must be registered, got: %v", err)
	}
}

func TestNormalizeBoltzGenHotspots(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", ""},
		{"A30,A33", "30,33"},
		{"30, 33, 251", "30,33,251"},
		{"A30, B33,12", "30,33,12"},
		{"A30-35,42", "42"}, // ranges don't parse → only 42 survives
		{"chain_A", ""},     // no digits → skipped
	}
	for _, c := range cases {
		got := normalizeBoltzGenHotspots(c.in)
		if got != c.want {
			t.Errorf("normalizeBoltzGenHotspots(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
