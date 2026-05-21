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

// TestChai1RecipeIsContainerMode pins the Bug 21 schema: the registry must
// surface chai1 as a container-mode tool with the NGC PyTorch base, a GPU
// requirement, a /models weights mount, and the full list of weight specs.
func TestChai1RecipeIsContainerMode(t *testing.T) {
	r, err := LoadRegistry(t.TempDir())
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	rec, ok := r.Tool("chai1")
	if !ok {
		t.Fatal("chai1 missing from registry")
	}
	if rec.ImageTag == "" {
		t.Error("chai1 must declare image_tag (container mode)")
	}
	if rec.Containerfile != "chai1.Containerfile" {
		t.Errorf("Containerfile = %q, want chai1.Containerfile", rec.Containerfile)
	}
	if !rec.GPU {
		t.Error("chai1 must declare gpu = true")
	}
	if !rec.RequiresGPU {
		t.Error("chai1 must declare requires_gpu = true (legacy mirror flag)")
	}
	wantMount := false
	for _, p := range rec.WeightsPaths {
		if p == "/models" {
			wantMount = true
		}
	}
	if !wantMount {
		t.Errorf("weights_paths must include /models, got %v", rec.WeightsPaths)
	}
	if !strings.Contains(rec.Entrypoint, "chai-lab fold") {
		t.Errorf("entrypoint = %q, want it to invoke `chai-lab fold`", rec.Entrypoint)
	}
	// Smoke test MUST exercise verify_gpu.py before the tool-specific call.
	if !strings.Contains(rec.SmokeTest, "verify_gpu.py") {
		t.Errorf("smoke_test must exercise verify_gpu.py, got %q", rec.SmokeTest)
	}
	if !strings.Contains(rec.SmokeTest, "chai-lab fold") {
		t.Errorf("smoke_test must include chai-lab fold, got %q", rec.SmokeTest)
	}
	if len(rec.Weights) < 7 {
		t.Errorf("expected at least 7 weight specs (6 model components + conformers), got %d",
			len(rec.Weights))
	}
	// Every weight URL is a chaiassets.com URL (no HF auth) and has a SHA256.
	for _, w := range rec.Weights {
		if !strings.HasPrefix(w.URL, "https://chaiassets.com/") {
			t.Errorf("unexpected weight URL host: %q", w.URL)
		}
		if w.SHA256 == "" {
			t.Errorf("weight %q missing sha256", w.Path)
		}
		if w.Path == "" {
			t.Errorf("weight %q missing relative path", w.URL)
		}
	}
}

// TestChai1ContainerfileEmbedded confirms the embedded containerfilesFS
// carries the chai1 Containerfile and the shared verify_gpu.py fragment.
func TestChai1ContainerfileEmbedded(t *testing.T) {
	body, err := containerfilesFS.ReadFile(
		filepath.Join("containerfiles", "chai1.Containerfile"))
	if err != nil {
		t.Fatalf("read embedded chai1.Containerfile: %v", err)
	}
	s := string(body)
	for _, want := range []string{
		"FROM nvcr.io/nvidia/pytorch:",
		"chai_lab==0.6.1",
		"CHAI_DOWNLOADS_DIR=/models",
		"COPY _base/verify_gpu.py",
		`ENTRYPOINT ["chai-lab"]`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("chai1.Containerfile missing %q", want)
		}
	}
	// HF_TOKEN is declared so it can be forwarded at run time, but it must
	// NEVER carry a default value baked into the image.
	for _, banned := range []string{`HF_TOKEN="hf_`, `HF_TOKEN=hf_`} {
		if strings.Contains(s, banned) {
			t.Errorf("chai1.Containerfile leaks an HF token: matched %q", banned)
		}
	}
	// The verify_gpu.py fragment is bundled separately and gets staged into
	// the build context by stageContainerBaseTree.
	if _, err := containerfilesFS.ReadFile(
		filepath.Join("containerfiles", "_base", "verify_gpu.py")); err != nil {
		t.Fatalf("verify_gpu.py not embedded: %v", err)
	}
}

// TestInstallerChai1BuildsAndDownloadsWeights exercises the Bug 21 install
// path end-to-end through the stubbed runtime exec seam:
//   - the embedded chai1.Containerfile is staged into a build context,
//   - `<runtime> build` is invoked with the correct tag,
//   - and the post-build EnsureWeights hook fires with the tool's weight specs.
//
// No real podman/network is required: runCmd captures argv, and
// ensureWeightsHook is stubbed.
func TestInstallerChai1BuildsAndDownloadsWeights(t *testing.T) {
	home := t.TempDir()
	reg, err := LoadRegistry(home)
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	rec, ok := reg.Tool("chai1")
	if !ok {
		t.Fatal("chai1 missing")
	}
	inst := NewInstaller(reg)
	inst.runtime = Runtime{Bin: "/usr/bin/podman", Kind: RuntimePodman}

	var calls [][]string
	oldRun := runCmd
	defer func() { runCmd = oldRun }()
	runCmd = func(cmd *exec.Cmd) error {
		calls = append(calls, append([]string(nil), cmd.Args...))
		return nil
	}
	oldOut := runCmdOutput
	defer func() { runCmdOutput = oldOut }()
	runCmdOutput = func(cmd *exec.Cmd) ([]byte, error) {
		// Pretend the base image is already cached so we skip the pull.
		if cmd.Args[len(cmd.Args)-1] == BaseImage {
			return []byte("[{}]\n"), nil
		}
		return nil, &exec.ExitError{}
	}

	type hookCall struct {
		home, name string
		specs      []WeightSpec
	}
	var hookCalls []hookCall
	oldHook := inst.ensureWeights
	defer func() { inst.ensureWeights = oldHook }()
	inst.ensureWeights = func(ctx context.Context, h, name string, specs []WeightSpec) (string, error) {
		hookCalls = append(hookCalls, hookCall{h, name, specs})
		return filepath.Join(h, ".fova", "models", name), nil
	}

	// Capture what gets written into the build context to prove the
	// embedded chai1.Containerfile + _base/verify_gpu.py both made it
	// before the temp dir is cleaned up. We inspect from inside the seam
	// because installContainer wipes its build dir on return.
	type stagedSnapshot struct {
		hasVerifyGPU  bool
		hasContainerf bool
	}
	var staged []stagedSnapshot
	oldStage := stageContainerBaseTree
	defer func() { stageContainerBaseTree = oldStage }()
	stageContainerBaseTree = func(buildDir string) error {
		if err := oldStage(buildDir); err != nil {
			return err
		}
		snap := stagedSnapshot{}
		if _, err := os.Stat(filepath.Join(buildDir, "_base", "verify_gpu.py")); err == nil {
			snap.hasVerifyGPU = true
		}
		if _, err := os.Stat(filepath.Join(buildDir, "chai1.Containerfile")); err == nil {
			snap.hasContainerf = true
		}
		staged = append(staged, snap)
		return nil
	}

	if err := inst.Install(context.Background(), "chai1"); err != nil {
		t.Fatalf("Install: %v", err)
	}

	// Build call invoked with the correct tag.
	var sawBuild bool
	for _, a := range calls {
		joined := strings.Join(a, " ")
		if strings.Contains(joined, "build -t "+rec.ImageTag) {
			sawBuild = true
		}
	}
	if !sawBuild {
		t.Errorf("expected build of %s, got calls: %v", rec.ImageTag, calls)
	}

	// EnsureWeights fired exactly once, for the chai1 tool, with every spec
	// the recipe declares.
	if len(hookCalls) != 1 {
		t.Fatalf("ensureWeightsHook called %d times, want 1", len(hookCalls))
	}
	if hookCalls[0].name != "chai1" {
		t.Errorf("ensureWeightsHook tool = %q, want chai1", hookCalls[0].name)
	}
	if hookCalls[0].home != home {
		t.Errorf("ensureWeightsHook home = %q, want %q", hookCalls[0].home, home)
	}
	if len(hookCalls[0].specs) != len(rec.Weights) {
		t.Errorf("hook got %d specs, recipe declares %d",
			len(hookCalls[0].specs), len(rec.Weights))
	}

	// stageContainerBaseTree ran exactly once, and the build context it populated
	// holds both the Containerfile and the verify_gpu.py smoke fragment.
	if len(staged) != 1 {
		t.Fatalf("stageContainerBaseTree called %d times, want 1", len(staged))
	}
	if !staged[0].hasVerifyGPU {
		t.Error("verify_gpu.py not staged into build context")
	}
	if !staged[0].hasContainerf {
		t.Error("chai1.Containerfile not staged into build context")
	}
}

// TestInstallerChai1WeightsHookFailureFailsInstall proves the contract: when
// EnsureWeights returns an error after a successful build, the install as a
// whole fails (so /install chai1 doesn't lie about partial state).
func TestInstallerChai1WeightsHookFailureFailsInstall(t *testing.T) {
	home := t.TempDir()
	reg, err := LoadRegistry(home)
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	inst := NewInstaller(reg)
	inst.runtime = Runtime{Bin: "/usr/bin/podman", Kind: RuntimePodman}

	oldRun := runCmd
	defer func() { runCmd = oldRun }()
	runCmd = func(cmd *exec.Cmd) error { return nil }
	oldOut := runCmdOutput
	defer func() { runCmdOutput = oldOut }()
	runCmdOutput = func(cmd *exec.Cmd) ([]byte, error) {
		if cmd.Args[len(cmd.Args)-1] == BaseImage {
			return []byte("[{}]\n"), nil
		}
		return nil, &exec.ExitError{}
	}

	oldHook := inst.ensureWeights
	defer func() { inst.ensureWeights = oldHook }()
	inst.ensureWeights = func(ctx context.Context, h, name string, specs []WeightSpec) (string, error) {
		return "", io.ErrUnexpectedEOF // any error
	}

	if err := inst.Install(context.Background(), "chai1"); err == nil {
		t.Fatal("Install must propagate weight-download failures")
	}
}

// chai1TestEnv builds an AdapterEnv with the registered chai1 recipe and a
// pre-existing ~/.fova/models/chai1 weights cache directory.
func chai1TestEnv(t *testing.T, logBuf *bytes.Buffer, progress *[]float64) AdapterEnv {
	t.Helper()
	home := t.TempDir()
	reg, err := LoadRegistry(home)
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	if err := os.MkdirAll(ModelsRoot(home, "chai1"), 0o755); err != nil {
		t.Fatal(err)
	}
	rec, ok := reg.Tool("chai1")
	if !ok {
		t.Fatal("chai1 missing from registry")
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

func TestBuildChai1FASTA(t *testing.T) {
	req := chai1Request{Entities: []chai1Entity{
		{Type: "protein", ID: "A", Sequence: "MKQ"},
		{Type: "ligand", ID: "L", SMILES: "CCO"},
		{Type: "rna", ID: "R", Sequence: "ACGU"},
		{Type: "glycan", ID: "G", Glycan: "NAG"},
	}}
	got := buildChai1FASTA(req)
	want := ">protein|name=A\nMKQ\n" +
		">ligand|name=L\nCCO\n" +
		">rna|name=R\nACGU\n" +
		">glycan|name=G\nNAG\n"
	if got != want {
		t.Errorf("fasta mismatch\n got:\n%s\nwant:\n%s", got, want)
	}
}

func TestParseChai1Output(t *testing.T) {
	outDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(outDir, "pred.model_0.cif"),
		[]byte("data_pred\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	designs, err := parseChai1Output(outDir)
	if err != nil {
		t.Fatalf("parseChai1Output: %v", err)
	}
	if len(designs) != 1 {
		t.Fatalf("want 1 design, got %d", len(designs))
	}
	if !strings.HasSuffix(designs[0].StructureFile, ".cif") {
		t.Errorf("structure_file = %q, want a .cif", designs[0].StructureFile)
	}
}

func TestParseChai1OutputEmptyErrors(t *testing.T) {
	if _, err := parseChai1Output(t.TempDir()); err == nil {
		t.Fatal("expected an error when no structures are present")
	}
}

func TestChai1AdapterInvoke(t *testing.T) {
	var logBuf bytes.Buffer
	var progress []float64
	env := chai1TestEnv(t, &logBuf, &progress)

	calls := stubContainerRuntime(t, func(args []string) error {
		if len(args) < 2 || args[1] != "run" {
			return nil
		}
		// Drop a stub CIF where chai-lab would have written one.
		out := filepath.Join(env.WorkDir, "out")
		if err := os.MkdirAll(out, 0o755); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(out, "pred.model_0.cif"),
			[]byte("data_pred\n"), 0o644)
	})

	saveAs := filepath.Join(t.TempDir(), "designs", "predicted.cif")
	body := []byte(`{"sequences":{"A":"MKQHKAMIVAL","B":"GGGGSGGGGS"},"save_as":"` + saveAs + `"}`)

	out, err := chai1Adapter{}.Invoke(context.Background(), env, body)
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

	fasta, err := os.ReadFile(filepath.Join(env.WorkDir, "in.fasta"))
	if err != nil {
		t.Fatalf("read in.fasta: %v", err)
	}
	fastaStr := string(fasta)
	for _, want := range []string{
		">protein|name=chain_A", "MKQHKAMIVAL",
		">protein|name=chain_B", "GGGGSGGGGS",
	} {
		if !strings.Contains(fastaStr, want) {
			t.Errorf("FASTA missing %q in:\n%s", want, fastaStr)
		}
	}
	if strings.Index(fastaStr, "chain_A") > strings.Index(fastaStr, "chain_B") {
		t.Errorf("FASTA chains must be alphabetically ordered, got:\n%s", fastaStr)
	}

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
		"--device nvidia.com/gpu=all",
		"-v " + env.WorkDir + ":/work",
		"-v " + ModelsRoot(env.Registry.Home(), "chai1") + ":/models",
		"-w /work",
		env.Recipe.ImageTag,
		// Cmd: subcommand `fold` + FASTA + output dir.
		"fold /work/in.fasta /work/out",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("argv missing %q in:\n%s", want, joined)
		}
	}
	if len(progress) < 2 {
		t.Errorf("env.Progress should have ticked at least twice, got %v", progress)
	}
}

func TestChai1AdapterInvokeMissingSequences(t *testing.T) {
	env := chai1TestEnv(t, nil, nil)
	if _, err := (chai1Adapter{}).Invoke(context.Background(), env, []byte(`{}`)); err == nil {
		t.Fatal("expected an error when sequences is missing")
	}
}

func TestChai1AdapterInvokeEmptyChainErrors(t *testing.T) {
	env := chai1TestEnv(t, nil, nil)
	_, err := chai1Adapter{}.Invoke(context.Background(), env,
		[]byte(`{"sequences":{"A":""}}`))
	if err == nil || !strings.Contains(err.Error(), "empty sequence") {
		t.Fatalf("expected an empty-sequence error, got: %v", err)
	}
}

func TestChai1AdapterInvokeCreatesWeightsCache(t *testing.T) {
	home := t.TempDir()
	reg, err := LoadRegistry(home)
	if err != nil {
		t.Fatal(err)
	}
	rec, _ := reg.Tool("chai1")
	env := AdapterEnv{Recipe: rec, WorkDir: t.TempDir(), Registry: reg}
	_ = stubContainerRuntime(t, nil)

	// Chai-1 downloads its weights at container runtime, so a missing
	// weights cache must be created on demand, not rejected.
	_, err = chai1Adapter{}.Invoke(context.Background(), env,
		[]byte(`{"sequences":{"A":"MKQ"}}`))
	if err != nil && strings.Contains(err.Error(), "weights cache") {
		t.Fatalf("a missing weights cache must not error, got: %v", err)
	}
	cache := ModelsRoot(reg.Home(), "chai1")
	if info, statErr := os.Stat(cache); statErr != nil || !info.IsDir() {
		t.Fatalf("Invoke must create the weights cache %s; stat err = %v", cache, statErr)
	}
}

func TestRunDesignChai1IsRegistered(t *testing.T) {
	reg, err := LoadRegistry(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_, err = RunDesign(context.Background(), reg, "fold.chai1", []byte(`{}`), io.Discard, nil)
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), "no local adapter") {
		t.Fatalf("fold.chai1 must be registered, got: %v", err)
	}
}
