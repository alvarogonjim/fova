package local

import (
	"context"
	"os/exec"
	"strings"
	"testing"
)

// TestRFdiffusion2RecipeIsContainerMode pins the v0.7 container-schema
// migration: the rfdiffusion2 recipe in tools.toml must declare image_tag,
// containerfile, entrypoint, GPU, weights_paths, a non-zero timeout, and a
// smoke_test command — and must NOT carry the legacy install_steps/run_command.
func TestRFdiffusion2RecipeIsContainerMode(t *testing.T) {
	reg, err := LoadRegistry("/home/u/fova")
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	rec, ok := reg.Tool("rfdiffusion2")
	if !ok {
		t.Fatal("rfdiffusion2 missing from registry")
	}
	if rec.ImageTag == "" {
		t.Error("image_tag empty — recipe still in legacy shape")
	}
	if rec.Containerfile != "rfdiffusion2.Containerfile" {
		t.Errorf("containerfile = %q, want rfdiffusion2.Containerfile", rec.Containerfile)
	}
	if !strings.Contains(rec.Entrypoint, "pipeline.py") {
		t.Errorf("entrypoint = %q, expected the upstream-documented pipeline.py", rec.Entrypoint)
	}
	if !rec.GPU {
		t.Error("gpu = false, but RFdiffusion2 inference needs CUDA")
	}
	if len(rec.WeightsPaths) == 0 {
		t.Error("weights_paths empty — model checkpoints live under /models/rfdiffusion2/")
	}
	if rec.TimeoutSeconds == 0 {
		t.Error("timeout_seconds = 0; pick a finite cap so a stuck job exits")
	}
	if rec.SmokeTest == "" {
		t.Error("smoke_test empty; container-mode tools must declare a smoke command")
	}
	// Legacy fields MUST be cleared during the migration so the installer
	// branches into installContainer (not installLegacy).
	if len(rec.InstallSteps) != 0 {
		t.Errorf("install_steps = %v, expected empty after container migration", rec.InstallSteps)
	}
	if rec.RunCommand != "" {
		t.Errorf("run_command = %q, expected empty after container migration", rec.RunCommand)
	}
}

// TestRFdiffusion2WeightsAssetDeclared makes sure the per-tool weights cache
// has somewhere to fetch from. The four URLs mirror upstream setup.py's
// model_weights / third_party_model_weights manifest.
func TestRFdiffusion2WeightsAssetDeclared(t *testing.T) {
	reg, _ := LoadRegistry("/home/u/fova")
	a, ok := reg.DataAsset("rfdiffusion2_weights")
	if !ok {
		t.Fatal("rfdiffusion2_weights data asset missing")
	}
	if len(a.URLs) < 2 {
		t.Errorf("URLs = %v, expected at least the two RFD checkpoints", a.URLs)
	}
	var sawRFD173, sawRFD140 bool
	for _, u := range a.URLs {
		if strings.Contains(u, "RFD_173.pt") {
			sawRFD173 = true
		}
		if strings.Contains(u, "RFD_140.pt") {
			sawRFD140 = true
		}
		if !strings.Contains(u, "files.ipd.uw.edu/pub/rfdiffusion2/") {
			t.Errorf("URL %q is not under upstream's BASE_URL", u)
		}
	}
	if !sawRFD173 || !sawRFD140 {
		t.Errorf("missing required weight files in %v", a.URLs)
	}
}

// TestRFdiffusion2ContainerfileIsEmbeddedAndVerifiesGPU is the smoke test
// the v0.7 Phase 2 task calls out: the Containerfile must be embedded in the
// installer's FS, FROM the shared BaseImage, clone upstream, and run the
// shared verify_gpu.py assertion before the image build completes.
func TestRFdiffusion2ContainerfileIsEmbeddedAndVerifiesGPU(t *testing.T) {
	body, err := loadContainerfile("rfdiffusion2.Containerfile")
	if err != nil {
		t.Fatalf("loadContainerfile: %v", err)
	}
	text := string(body)
	if !strings.Contains(text, "FROM "+BaseImage) {
		t.Errorf("Containerfile does not FROM the shared BaseImage %q", BaseImage)
	}
	if !strings.Contains(text, "github.com/RosettaCommons/RFdiffusion2") {
		t.Error("Containerfile does not clone upstream RFdiffusion2 repo")
	}
	if !strings.Contains(text, "requirements_cuda124.txt") {
		t.Error("Containerfile does not install the upstream-declared pip requirements")
	}
	// The smoke fragment from _base/verify_gpu.py — either COPYed in or, as
	// the Phase-1 installer's build context can only hold the Containerfile,
	// inlined as a `python -c` step. Either is acceptable; both end up
	// running `assert torch.cuda.is_available()` at build time.
	if !strings.Contains(text, "torch.cuda.is_available") {
		t.Error("Containerfile does not exercise the verify_gpu CUDA assertion")
	}
	if !strings.Contains(text, "ENTRYPOINT") {
		t.Error("Containerfile is missing an ENTRYPOINT line — the runner can't launch it")
	}
}

// TestRFdiffusion2InstallBuildsContainerImage exercises the full install path
// using the real embedded Containerfile and the runtime test seams: no real
// podman call is made, but the recorded argv confirms the runtime is asked
// to pull BaseImage and build the rfdiffusion2 tag declared in tools.toml.
func TestRFdiffusion2InstallBuildsContainerImage(t *testing.T) {
	home := t.TempDir()
	reg, err := LoadRegistry(home)
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
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
	imageStates := map[string]bool{}
	runCmdOutput = func(cmd *exec.Cmd) ([]byte, error) {
		image := cmd.Args[len(cmd.Args)-1]
		if imageStates[image] {
			return []byte("[{}]\n"), nil
		}
		return nil, &exec.ExitError{}
	}

	if err := inst.Install(context.Background(), "rfdiffusion2"); err != nil {
		t.Fatalf("Install: %v", err)
	}

	rec, _ := reg.Tool("rfdiffusion2")
	var sawPull, sawBuild bool
	for _, a := range calls {
		joined := strings.Join(a, " ")
		if strings.Contains(joined, "pull "+BaseImage) {
			sawPull = true
		}
		if strings.Contains(joined, "build -t "+rec.ImageTag) {
			sawBuild = true
		}
	}
	if !sawPull {
		t.Errorf("expected pull of BaseImage %q, got calls: %v", BaseImage, calls)
	}
	if !sawBuild {
		t.Errorf("expected build of %q, got calls: %v", rec.ImageTag, calls)
	}

	// Status reflects image-present once the runtime stub claims the tag exists.
	imageStates[rec.ImageTag] = true
	st := inst.Status("rfdiffusion2")
	if !st.Installed {
		t.Error("Status should report rfdiffusion2 installed when its image is present")
	}
	if st.Image != rec.ImageTag {
		t.Errorf("Status.Image = %q, want %q", st.Image, rec.ImageTag)
	}
}
