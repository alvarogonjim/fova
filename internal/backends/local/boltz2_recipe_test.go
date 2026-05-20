package local

import (
	"context"
	"os/exec"
	"strings"
	"testing"
)

// TestBoltz2RecipeShape locks in the [tools.boltz2] schema declared in
// tools.toml: it must be a container-mode recipe (image_tag + Containerfile
// + entrypoint), GPU-required, weights bind-mounted at /models, and the
// weights table must list every file the in-image `boltz predict` expects
// to find under --cache (ccd.pkl, mols.tar, boltz2_conf.ckpt,
// boltz2_aff.ckpt). The smoke_test must run the shared verify_gpu.py
// fragment before exercising the fold path.
func TestBoltz2RecipeShape(t *testing.T) {
	reg, err := LoadRegistry(t.TempDir())
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	rec, ok := reg.Tool("boltz2")
	if !ok {
		t.Fatal("boltz2 missing from tools.toml")
	}
	if rec.ImageTag == "" {
		t.Error("ImageTag empty — boltz2 must be a container-mode recipe")
	}
	if rec.Containerfile != "boltz2.Containerfile" {
		t.Errorf("Containerfile = %q, want %q", rec.Containerfile, "boltz2.Containerfile")
	}
	if !strings.Contains(rec.Entrypoint, "boltz predict") {
		t.Errorf("Entrypoint = %q, want it to call `boltz predict`", rec.Entrypoint)
	}
	if !rec.GPU {
		t.Error("GPU = false — boltz2 requires CUDA")
	}
	if !rec.RequiresGPU {
		t.Error("RequiresGPU = false — boltz2 must surface its GPU dependency in /doctor")
	}
	if len(rec.WeightsPaths) == 0 {
		t.Error("WeightsPaths empty — boltz2 expects /models to be bind-mounted")
	}
	if rec.TimeoutSeconds <= 0 {
		t.Errorf("TimeoutSeconds = %d, want > 0", rec.TimeoutSeconds)
	}

	// The shared GPU smoke fragment must precede the tool-specific call.
	if !strings.Contains(rec.SmokeTest, "torch.cuda.is_available") {
		t.Errorf("SmokeTest does not exercise verify_gpu.py: %q", rec.SmokeTest)
	}
	// The smoke must actually run `boltz predict` against the mounted
	// weights cache, not just print "ok".
	if !strings.Contains(rec.SmokeTest, "boltz predict") {
		t.Errorf("SmokeTest does not invoke `boltz predict`: %q", rec.SmokeTest)
	}
	if !strings.Contains(rec.SmokeTest, "/models/boltz2") {
		t.Errorf("SmokeTest does not pass --cache /models/boltz2: %q", rec.SmokeTest)
	}

	// Every file `boltz predict` opens on cold start must appear in the
	// weights table — otherwise EnsureWeights leaves the image broken.
	required := []string{"ccd.pkl", "mols.tar", "boltz2_conf.ckpt", "boltz2_aff.ckpt"}
	have := map[string]bool{}
	for _, w := range rec.Weights {
		if w.URL == "" {
			t.Errorf("weight %q has empty URL", w.Path)
		}
		if w.Path == "" {
			t.Errorf("weight with URL %q has empty Path", w.URL)
		}
		have[w.Path] = true
	}
	for _, want := range required {
		if !have[want] {
			t.Errorf("weights table missing required file %q", want)
		}
	}
}

// TestBoltz2ContainerfileEmbedded asserts the Containerfile is in the
// `//go:embed all:containerfiles` set and contains the expected pinning.
// We do NOT shell out to podman here — that's the maintainer's Phase 3
// step. We only check the bytes that ship in the binary.
func TestBoltz2ContainerfileEmbedded(t *testing.T) {
	body, err := loadContainerfile("boltz2.Containerfile")
	if err != nil {
		t.Fatalf("loadContainerfile: %v", err)
	}
	s := string(body)
	if !strings.Contains(s, "FROM "+BaseImage) {
		t.Errorf("Containerfile does not FROM %s:\n%s", BaseImage, s)
	}
	if !strings.Contains(s, "git clone") || !strings.Contains(s, "jwohlwend/boltz") {
		t.Errorf("Containerfile does not clone upstream boltz repo:\n%s", s)
	}
	if !strings.Contains(s, `-e "/opt/boltz2[cuda]"`) {
		t.Errorf("Containerfile does not pip-install boltz[cuda] in editable mode:\n%s", s)
	}
	if !strings.Contains(s, `ENTRYPOINT ["boltz", "predict"]`) {
		t.Errorf("Containerfile does not set ENTRYPOINT to boltz predict:\n%s", s)
	}
	// GPU reachability is verified at run time via tools.toml smoke_test,
	// not at build time (no GPU under `podman build`). The Containerfile
	// must stage the shared verify_gpu.py fragment so the smoke can use it.
	if !strings.Contains(s, "COPY _base/verify_gpu.py") {
		t.Errorf("Containerfile does not stage _base/verify_gpu.py:\n%s", s)
	}
}

// TestVerifyGpuFragmentShipsAndIsUsable confirms the shared verify_gpu.py
// fragment is present in the embedded FS — every container-mode tool's
// smoke_test inlines its logic, so a missing file would break the
// invariant the spec asserts in `## Bug 20`.
func TestVerifyGpuFragmentShipsAndIsUsable(t *testing.T) {
	body, err := containerfilesFS.ReadFile("containerfiles/_base/verify_gpu.py")
	if err != nil {
		t.Fatalf("_base/verify_gpu.py missing: %v", err)
	}
	if !strings.Contains(string(body), "torch.cuda.is_available") {
		t.Errorf("verify_gpu.py does not assert torch.cuda.is_available(): %s", body)
	}
}

// TestBoltz2InstallTriggersBuildAndWeights wires up a stubbed runtime exec
// seam plus a stubbed ensureWeights hook and confirms that:
//   - Installer.installContainer calls `<runtime> build` for the boltz2
//     image (the stubbed exec seam never invokes a real podman/docker), and
//   - the post-build hook calls EnsureWeights with the four weight specs
//     from tools.toml, targeted at ~/.fova/models/boltz2/.
//
// This is the test the task brief asks for: "Test that the Containerfile
// builds via the stubbed runtime exec seam AND EnsureWeights is called."
func TestBoltz2InstallTriggersBuildAndWeights(t *testing.T) {
	home := t.TempDir()
	reg, err := LoadRegistry(home)
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	inst := NewInstaller(reg)
	inst.runtime = Runtime{Bin: "/usr/bin/podman", Kind: RuntimePodman}

	// Capture all exec.Cmd invocations the runtime would make.
	var calls [][]string
	oldRun := runCmd
	defer func() { runCmd = oldRun }()
	runCmd = func(cmd *exec.Cmd) error {
		calls = append(calls, append([]string(nil), cmd.Args...))
		return nil
	}
	// Base image is already cached: `image inspect <image>` succeeds.
	oldOut := runCmdOutput
	defer func() { runCmdOutput = oldOut }()
	runCmdOutput = func(cmd *exec.Cmd) ([]byte, error) {
		return []byte("[{}]\n"), nil
	}

	// Replace the weights hook with a capture so we don't hit HuggingFace.
	type ensureCall struct {
		home, tool string
		specs      []WeightSpec
	}
	var got []ensureCall
	inst.ensureWeights = func(ctx context.Context, h, name string, specs []WeightSpec) (string, error) {
		got = append(got, ensureCall{home: h, tool: name, specs: specs})
		return ModelsRoot(h, name), nil
	}

	if err := inst.Install(context.Background(), "boltz2"); err != nil {
		t.Fatalf("Install boltz2: %v", err)
	}

	// The runtime must have been asked to build the boltz2 image.
	var sawBuild bool
	for _, a := range calls {
		joined := strings.Join(a, " ")
		if strings.Contains(joined, "build -t fova/boltz2:") &&
			strings.Contains(joined, "boltz2.Containerfile") {
			sawBuild = true
		}
	}
	if !sawBuild {
		t.Errorf("expected `build -t fova/boltz2:* boltz2.Containerfile`, got:\n%v", calls)
	}

	// The post-build hook must have called EnsureWeights for the boltz2 tool
	// with all four weight specs from tools.toml.
	if len(got) != 1 {
		t.Fatalf("ensureWeights called %d times, want exactly 1: %#v", len(got), got)
	}
	if got[0].tool != "boltz2" {
		t.Errorf("ensureWeights tool = %q, want %q", got[0].tool, "boltz2")
	}
	if got[0].home != home {
		t.Errorf("ensureWeights home = %q, want %q", got[0].home, home)
	}
	if len(got[0].specs) != 4 {
		t.Errorf("ensureWeights got %d specs, want 4: %#v", len(got[0].specs), got[0].specs)
	}
	wantPaths := map[string]bool{
		"ccd.pkl":          false,
		"mols.tar":         false,
		"boltz2_conf.ckpt": false,
		"boltz2_aff.ckpt":  false,
	}
	for _, s := range got[0].specs {
		if _, expected := wantPaths[s.Path]; !expected {
			t.Errorf("unexpected weight path %q", s.Path)
			continue
		}
		wantPaths[s.Path] = true
		if !strings.HasPrefix(s.URL, "https://huggingface.co/boltz-community/") {
			t.Errorf("weight %q URL not pinned to HuggingFace: %q", s.Path, s.URL)
		}
	}
	for p, saw := range wantPaths {
		if !saw {
			t.Errorf("ensureWeights specs missing path %q", p)
		}
	}
}
