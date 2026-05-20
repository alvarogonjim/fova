package local

import (
	"context"
	"os/exec"
	"strings"
	"testing"
)

// TestRFdiffusionRecipeIsContainerMode asserts that the [tools.rfdiffusion]
// block in tools.toml decodes into the container-mode schema (image_tag,
// containerfile, entrypoint, gpu, weights_paths, smoke_test, weights). The
// legacy install_steps/run_command fields must be absent so the platform
// branches into installContainer.
func TestRFdiffusionRecipeIsContainerMode(t *testing.T) {
	reg, err := LoadRegistry(t.TempDir())
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	rec, ok := reg.Tool("rfdiffusion")
	if !ok {
		t.Fatal("rfdiffusion missing from registry")
	}
	if rec.ImageTag == "" {
		t.Error("ImageTag must be set for container-mode rfdiffusion")
	}
	if rec.Containerfile != "rfdiffusion.Containerfile" {
		t.Errorf("Containerfile = %q, want rfdiffusion.Containerfile", rec.Containerfile)
	}
	if !strings.Contains(rec.Entrypoint, "run_inference.py") {
		t.Errorf("Entrypoint must invoke run_inference.py, got %q", rec.Entrypoint)
	}
	if !rec.GPU {
		t.Error("rfdiffusion is GPU-bound; GPU field must be true")
	}
	if len(rec.WeightsPaths) == 0 {
		t.Error("WeightsPaths must declare at least /models so the runner mounts the cache")
	}
	if rec.SmokeTest == "" {
		t.Error("smoke_test must be set")
	}
	// The shared verify_gpu.py fragment must be exercised by the smoke step
	// (verbatim or as an equivalent inline torch.cuda.is_available() assertion).
	if !strings.Contains(rec.SmokeTest, "torch.cuda.is_available()") {
		t.Errorf("smoke_test must assert torch.cuda.is_available(), got %q", rec.SmokeTest)
	}
	if len(rec.InstallSteps) != 0 {
		t.Errorf("install_steps must be removed for container-mode recipe, got %v", rec.InstallSteps)
	}
	if rec.RunCommand != "" {
		t.Errorf("run_command must be removed for container-mode recipe, got %q", rec.RunCommand)
	}
}

// TestRFdiffusionRecipeDeclaresAllWeightURLs asserts every weight URL listed
// in the RFdiffusion README is wired into the recipe. Missing one would mean
// the post-install hook leaves a checkpoint absent and the smoke test (or a
// later /install run) would fail to load it.
func TestRFdiffusionRecipeDeclaresAllWeightURLs(t *testing.T) {
	reg, _ := LoadRegistry(t.TempDir())
	rec, _ := reg.Tool("rfdiffusion")
	if len(rec.Weights) == 0 {
		t.Fatal("rfdiffusion.weights must be populated for the post-install hook to fetch them")
	}
	wantFiles := []string{
		"Base_ckpt.pt",
		"Complex_base_ckpt.pt",
		"Complex_Fold_base_ckpt.pt",
		"InpaintSeq_ckpt.pt",
		"InpaintSeq_Fold_ckpt.pt",
		"ActiveSite_ckpt.pt",
		"Base_epoch8_ckpt.pt",
		"Complex_beta_ckpt.pt",
	}
	gotPaths := map[string]bool{}
	for _, w := range rec.Weights {
		gotPaths[w.Path] = true
		if !strings.HasPrefix(w.URL, "http://files.ipd.uw.edu/pub/RFdiffusion/") {
			t.Errorf("weight URL %q is not on the Baker lab CDN", w.URL)
		}
	}
	for _, f := range wantFiles {
		if !gotPaths[f] {
			t.Errorf("missing weight file %q in rfdiffusion.weights", f)
		}
	}
}

// TestRFdiffusionContainerfileEmbedded confirms the //go:embed directive on
// containerfilesFS picks up the per-tool Containerfile. Without this, the
// Installer would fail at install time with "no such file".
func TestRFdiffusionContainerfileEmbedded(t *testing.T) {
	body, err := loadContainerfile("rfdiffusion.Containerfile")
	if err != nil {
		t.Fatalf("loadContainerfile: %v", err)
	}
	src := string(body)
	if !strings.HasPrefix(src, "# RFdiffusion") {
		t.Errorf("Containerfile body unexpected first line: %q",
			strings.SplitN(src, "\n", 2)[0])
	}
	if !strings.Contains(src, "FROM "+BaseImage) {
		t.Errorf("Containerfile must FROM the BaseImage constant; got: %q", src)
	}
	if !strings.Contains(src, "RosettaCommons/RFdiffusion") {
		t.Error("Containerfile must clone RosettaCommons/RFdiffusion")
	}
	if !strings.Contains(src, "hydra-core") || !strings.Contains(src, "omegaconf") || !strings.Contains(src, "e3nn") {
		t.Error("Containerfile must pip-install hydra-core, omegaconf, and e3nn")
	}
	if !strings.Contains(src, `ENTRYPOINT ["python", "/opt/rfdiffusion/scripts/run_inference.py"]`) {
		t.Error("Containerfile ENTRYPOINT must invoke scripts/run_inference.py via python")
	}
	if !strings.Contains(src, "torch.cuda.is_available()") {
		t.Error("Containerfile must inline the _base/verify_gpu.py fragment so the build sanity-checks cuda")
	}
}

// TestInstallerContainerModeCallsEnsureWeights asserts that on a successful
// image build of a recipe declaring [[tools.<name>.weights]], the installer
// dispatches to EnsureWeights with the exact (URL, path, sha256) tuples from
// the recipe. Verifies the post-install weights hook wiring end-to-end through
// the stubbed runtime exec seam + injected ensureWeights.
func TestInstallerContainerModeCallsEnsureWeights(t *testing.T) {
	weights := []WeightSpec{
		{URL: "http://example.test/Base_ckpt.pt", Path: "Base_ckpt.pt"},
		{URL: "http://example.test/Complex.pt", Path: "Complex.pt", SHA256: "deadbeef"},
	}
	rec := ToolRecipe{
		ImageTag:      "fova/rfdiffusion:v1.1.0",
		Containerfile: "rfdiffusion.Containerfile",
		Entrypoint:    "python /opt/rfdiffusion/scripts/run_inference.py",
		GPU:           true,
		WeightsPaths:  []string{"/models"},
		Weights:       weights,
	}
	inst := installerWithContainerRecipe(t, "rfdiffusion-c", rec)

	// Stub the embedded Containerfile loader so the test does not depend on
	// the on-disk fixture.
	oldLoad := loadContainerfile
	defer func() { loadContainerfile = oldLoad }()
	loadContainerfile = func(name string) ([]byte, error) {
		return []byte("FROM " + BaseImage + "\n"), nil
	}

	// Stub the container runtime exec seam — same shape as
	// TestInstallerContainerModeBuildsImage uses.
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
		// Base image already cached so we skip Pull.
		image := cmd.Args[len(cmd.Args)-1]
		if image == BaseImage {
			return []byte("[{}]\n"), nil
		}
		return nil, &exec.ExitError{}
	}

	// Capture EnsureWeights invocation.
	var gotTool string
	var gotSpecs []WeightSpec
	var gotHome string
	oldEW := inst.ensureWeights
	defer func() { inst.ensureWeights = oldEW }()
	inst.ensureWeights = func(ctx context.Context, home, toolName string, specs []WeightSpec) (string, error) {
		gotHome = home
		gotTool = toolName
		gotSpecs = specs
		return ModelsRoot(home, toolName), nil
	}

	if err := inst.Install(context.Background(), "rfdiffusion-c"); err != nil {
		t.Fatalf("Install: %v", err)
	}

	// Build must have happened (runtime exec captured).
	var sawBuild bool
	for _, a := range calls {
		if strings.Contains(strings.Join(a, " "), "build -t fova/rfdiffusion:v1.1.0") {
			sawBuild = true
		}
	}
	if !sawBuild {
		t.Errorf("expected the runtime to run `build -t fova/rfdiffusion:v1.1.0 …`, got: %v", calls)
	}

	// EnsureWeights must have been called with the recipe's weights.
	if gotTool != "rfdiffusion-c" {
		t.Errorf("EnsureWeights tool name = %q, want %q", gotTool, "rfdiffusion-c")
	}
	if gotHome != inst.registry.Home() {
		t.Errorf("EnsureWeights home = %q, want %q", gotHome, inst.registry.Home())
	}
	if len(gotSpecs) != len(weights) {
		t.Fatalf("EnsureWeights got %d specs, want %d", len(gotSpecs), len(weights))
	}
	for i, w := range weights {
		if gotSpecs[i] != w {
			t.Errorf("spec[%d] = %+v, want %+v", i, gotSpecs[i], w)
		}
	}
}

// TestInstallerContainerModeSkipsWeightsWhenNoneDeclared confirms a container
// recipe without [[tools.<name>.weights]] does not invoke EnsureWeights. This
// matters for tools like ipsae (CPU-only, no model weights) that share the
// container schema but ship nothing to fetch.
func TestInstallerContainerModeSkipsWeightsWhenNoneDeclared(t *testing.T) {
	rec := ToolRecipe{
		ImageTag:      "fova/noweights:v1",
		Containerfile: "noop.Containerfile",
		Entrypoint:    "python",
		// Deliberately no Weights — should skip the hook.
	}
	inst := installerWithContainerRecipe(t, "noweights", rec)

	oldLoad := loadContainerfile
	defer func() { loadContainerfile = oldLoad }()
	loadContainerfile = func(name string) ([]byte, error) {
		return []byte("FROM " + BaseImage + "\n"), nil
	}
	oldRun := runCmd
	defer func() { runCmd = oldRun }()
	runCmd = func(cmd *exec.Cmd) error { return nil }
	oldOut := runCmdOutput
	defer func() { runCmdOutput = oldOut }()
	runCmdOutput = func(cmd *exec.Cmd) ([]byte, error) {
		// Pretend the base image is cached so Pull is skipped.
		image := cmd.Args[len(cmd.Args)-1]
		if image == BaseImage {
			return []byte("[{}]\n"), nil
		}
		return nil, &exec.ExitError{}
	}

	var called bool
	oldEW := inst.ensureWeights
	defer func() { inst.ensureWeights = oldEW }()
	inst.ensureWeights = func(ctx context.Context, home, toolName string, specs []WeightSpec) (string, error) {
		called = true
		return "", nil
	}

	if err := inst.Install(context.Background(), "noweights"); err != nil {
		t.Fatalf("Install: %v", err)
	}
	if called {
		t.Error("EnsureWeights must not be called when the recipe has no [[tools.x.weights]]")
	}
}
