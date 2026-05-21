package local

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// boltz2TestEnv builds an AdapterEnv with a container-mode recipe and a
// registry whose ~/.fova/models/boltz2 directory exists on disk.
func boltz2TestEnv(t *testing.T, logBuf *bytes.Buffer, progress *[]float64) AdapterEnv {
	t.Helper()
	home := t.TempDir()
	reg, err := LoadRegistry(home)
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	if err := os.MkdirAll(ModelsRoot(home, "boltz2"), 0o755); err != nil {
		t.Fatal(err)
	}
	rec, ok := reg.Tool("boltz2")
	if !ok {
		t.Fatal("boltz2 missing from registry")
	}
	env := AdapterEnv{
		Recipe:   rec,
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

// stubContainerRuntime makes Detect() see /usr/bin/podman, ImageExists return
// true, and RunContainer drop drop into the runCmd seam where `onRun` may
// stage outputs and inspect argv. Returns a slice that captures every argv
// runCmd was asked to run.
func stubContainerRuntime(t *testing.T, onRun func(args []string) error) *[][]string {
	t.Helper()
	calls := &[][]string{}
	oldLook := lookPath
	t.Cleanup(func() { lookPath = oldLook })
	lookPath = func(bin string) (string, error) {
		if bin == "podman" {
			return "/usr/bin/podman", nil
		}
		return "", exec.ErrNotFound
	}
	oldRun := runCmd
	t.Cleanup(func() { runCmd = oldRun })
	runCmd = func(cmd *exec.Cmd) error {
		args := append([]string(nil), cmd.Args...)
		*calls = append(*calls, args)
		if onRun != nil {
			return onRun(args)
		}
		return nil
	}
	oldOut := runCmdOutput
	t.Cleanup(func() { runCmdOutput = oldOut })
	runCmdOutput = func(cmd *exec.Cmd) ([]byte, error) {
		// ImageExists: pretend the tool image is cached locally.
		return []byte("[{}]\n"), nil
	}
	return calls
}

func TestBuildBoltz2YAML(t *testing.T) {
	req := boltz2Request{Entities: []boltz2Entity{
		{Type: "protein", ID: chainIDs{"A"}, Sequence: "MKQ", MSA: "empty"},
		{Type: "protein", ID: chainIDs{"B", "C"}, Sequence: "AAA"},
		{Type: "ligand", ID: chainIDs{"L"}, SMILES: "CCO"},
		{Type: "rna", ID: chainIDs{"R"}, Sequence: "ACGU", Cyclic: true},
	}}
	got := buildBoltz2YAML(req)
	want := "version: 1\n" +
		"sequences:\n" +
		"  - protein:\n      id: A\n      sequence: MKQ\n      msa: empty\n" +
		"  - protein:\n      id: [B, C]\n      sequence: AAA\n" +
		"  - ligand:\n      id: L\n      smiles: CCO\n" +
		"  - rna:\n      id: R\n      sequence: ACGU\n      cyclic: true\n"
	if got != want {
		t.Errorf("yaml mismatch\n got:\n%s\nwant:\n%s", got, want)
	}
}

func TestBuildBoltz2YAMLServerMSAOmitsLine(t *testing.T) {
	req := boltz2Request{Entities: []boltz2Entity{
		{Type: "protein", ID: chainIDs{"A"}, Sequence: "MKQ", MSA: "server"}}}
	if strings.Contains(buildBoltz2YAML(req), "msa:") {
		t.Error("msa: server must omit the msa line so --use_msa_server fills it")
	}
}

func TestParseBoltz2Output(t *testing.T) {
	outDir := t.TempDir()
	// Boltz writes per-model PDBs under predictions/<stem>/...
	sub := filepath.Join(outDir, "predictions", "in")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "in_model_0.pdb"),
		[]byte("ATOM\nEND\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	designs, err := parseBoltz2Output(outDir)
	if err != nil {
		t.Fatalf("parseBoltz2Output: %v", err)
	}
	if len(designs) != 1 {
		t.Fatalf("want 1 design, got %d", len(designs))
	}
	if designs[0].StructureFile == "" {
		t.Error("structure_file must be set")
	}
}

func TestParseBoltz2OutputEmptyErrors(t *testing.T) {
	if _, err := parseBoltz2Output(t.TempDir()); err == nil {
		t.Fatal("expected an error when no PDBs are present")
	}
}

func TestBoltz2AdapterInvoke(t *testing.T) {
	var logBuf bytes.Buffer
	var progress []float64
	env := boltz2TestEnv(t, &logBuf, &progress)

	// Stage a stub PDB into env.WorkDir/out the moment the container's
	// `run` argv is presented; the adapter parses it after RunContainer
	// returns.
	calls := stubContainerRuntime(t, func(args []string) error {
		// Only the `run` invocation produces output; `image inspect` goes
		// through runCmdOutput, not runCmd.
		if len(args) < 2 || args[1] != "run" {
			return nil
		}
		sub := filepath.Join(env.WorkDir, "out", "predictions", "in")
		if err := os.MkdirAll(sub, 0o755); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(sub, "in_model_0.pdb"),
			[]byte("ATOM\nEND\n"), 0o644)
	})

	saveAs := filepath.Join(t.TempDir(), "designs", "predicted.pdb")
	body := []byte(`{"sequences":{"A":"MKQHKAMIVAL","B":"MKQHKAMIVAL"},"save_as":"` + saveAs + `"}`)

	out, err := boltz2Adapter{}.Invoke(context.Background(), env, body)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	var resp designsEnvelope
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("output is not a designs envelope: %v", err)
	}
	if len(resp.Designs) != 1 {
		t.Fatalf("want 1 design, got %d", len(resp.Designs))
	}
	if resp.Designs[0].StructureFile != saveAs {
		t.Errorf("structure_file = %q, want save_as path %q", resp.Designs[0].StructureFile, saveAs)
	}
	if _, err := os.Stat(saveAs); err != nil {
		t.Errorf("save_as file not present at %q: %v", saveAs, err)
	}

	// The YAML must have been written with both chains, alphabetically ordered.
	yaml, err := os.ReadFile(filepath.Join(env.WorkDir, "in.yaml"))
	if err != nil {
		t.Fatalf("read in.yaml: %v", err)
	}
	yamlStr := string(yaml)
	for _, want := range []string{
		"id: A", "id: B",
		"sequence: MKQHKAMIVAL",
		"msa: empty",
	} {
		if !strings.Contains(yamlStr, want) {
			t.Errorf("YAML missing %q in:\n%s", want, yamlStr)
		}
	}
	if strings.Index(yamlStr, "id: A") > strings.Index(yamlStr, "id: B") {
		t.Errorf("YAML chains must be alphabetically ordered, got:\n%s", yamlStr)
	}

	// Exactly one container run call (image inspect goes through runCmdOutput).
	var runCalls [][]string
	for _, c := range *calls {
		if len(c) >= 2 && c[1] == "run" {
			runCalls = append(runCalls, c)
		}
	}
	if len(runCalls) != 1 {
		t.Fatalf("want 1 `podman run` call, got %d: %v", len(runCalls), runCalls)
	}
	joined := strings.Join(runCalls[0], " ")
	for _, want := range []string{
		"/usr/bin/podman run",
		// boltz2 recipe declares gpu = true so the GPU flag must appear.
		"--device nvidia.com/gpu=all",
		"-v " + env.WorkDir + ":/work",
		"-v " + ModelsRoot(env.Registry.Home(), "boltz2") + ":/models",
		"-w /work",
		env.Recipe.ImageTag,
		// Cmd: YAML + flags (no "predict" — that's in the ENTRYPOINT).
		"/work/in.yaml",
		"--out_dir /work/out",
		"--cache /models",
		"--output_format pdb",
		"--no_kernels",
		"--override",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("argv missing %q in:\n%s", want, joined)
		}
	}
	// Critically: do NOT pass `predict` as the first Cmd arg — that's in
	// the image's ENTRYPOINT, so doubling it would call `boltz predict
	// predict /work/in.yaml`.
	if strings.Contains(joined, env.Recipe.ImageTag+" predict ") {
		t.Errorf("Cmd must not start with `predict` (ENTRYPOINT already includes it): %s", joined)
	}

	if logBuf.Len() == 0 {
		// runtime_exec.attachLog hooks cmd.Stdout/Stderr to env.Log, but
		// the stubbed runCmd never writes anything itself — so the log
		// being empty is acceptable. We only assert ticks here.
	}
	if len(progress) < 2 {
		t.Errorf("env.Progress should have ticked at least twice, got %v", progress)
	}
}

func TestBoltz2AdapterInvokeMissingSequences(t *testing.T) {
	env := boltz2TestEnv(t, nil, nil)
	if _, err := (boltz2Adapter{}).Invoke(context.Background(), env, []byte(`{}`)); err == nil {
		t.Fatal("expected an error when sequences is missing")
	}
}

func TestBoltz2AdapterInvokeEmptyChainErrors(t *testing.T) {
	env := boltz2TestEnv(t, nil, nil)
	_, err := boltz2Adapter{}.Invoke(context.Background(), env,
		[]byte(`{"sequences":{"A":""}}`))
	if err == nil || !strings.Contains(err.Error(), "empty sequence") {
		t.Fatalf("expected an empty-sequence error, got: %v", err)
	}
}

func TestBoltz2AdapterInvokeCreatesWeightsCache(t *testing.T) {
	home := t.TempDir()
	reg, err := LoadRegistry(home)
	if err != nil {
		t.Fatal(err)
	}
	rec, _ := reg.Tool("boltz2")
	// Models cache deliberately NOT created — the adapter must create it.
	env := AdapterEnv{Recipe: rec, WorkDir: t.TempDir(), Registry: reg}
	_ = stubContainerRuntime(t, nil)

	// Boltz-2 downloads its weights at container runtime, so a missing
	// weights cache must be created on demand, not rejected.
	_, err = boltz2Adapter{}.Invoke(context.Background(), env,
		[]byte(`{"sequences":{"A":"MKQ"}}`))
	if err != nil && strings.Contains(err.Error(), "weights cache") {
		t.Fatalf("a missing weights cache must not error, got: %v", err)
	}
	cache := ModelsRoot(reg.Home(), "boltz2")
	if info, statErr := os.Stat(cache); statErr != nil || !info.IsDir() {
		t.Fatalf("Invoke must create the weights cache %s; stat err = %v", cache, statErr)
	}
}

func TestRunDesignBoltz2IsRegistered(t *testing.T) {
	reg, err := LoadRegistry(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// Missing sequences makes Invoke fail fast — which still proves
	// fold.boltz2 is registered and dispatched.
	_, err = RunDesign(context.Background(), reg, "fold.boltz2", []byte(`{}`), io.Discard, nil)
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), "no local adapter") {
		t.Fatalf("fold.boltz2 must be registered, got: %v", err)
	}
}
