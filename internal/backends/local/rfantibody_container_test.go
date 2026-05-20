package local

import (
	"context"
	"io"
	"os/exec"
	"strings"
	"testing"
)

// TestRFantibodyRecipeIsContainerMode locks the v0.7 migration of rfantibody
// from legacy uv-venv install to container mode (Bug 18). All container schema
// fields must be present; legacy install_steps / run_command must be gone.
func TestRFantibodyRecipeIsContainerMode(t *testing.T) {
	reg, err := LoadRegistry(t.TempDir())
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	rec, ok := reg.Tool("rfantibody")
	if !ok {
		t.Fatal("rfantibody recipe missing from tools.toml")
	}
	if rec.ImageTag == "" {
		t.Error("ImageTag is empty — rfantibody must be container-mode in v0.7")
	}
	if rec.Containerfile != "rfantibody.Containerfile" {
		t.Errorf("Containerfile = %q, want rfantibody.Containerfile", rec.Containerfile)
	}
	if !rec.GPU {
		t.Error("GPU must be true — RFantibody requires CUDA")
	}
	if len(rec.WeightsPaths) == 0 {
		t.Error("WeightsPaths must declare /models — RFantibody needs ~5 GB of weights")
	}
	if rec.SmokeTest == "" {
		t.Error("SmokeTest is empty — the v0.7 plan requires every container tool to ship a smoke")
	}
	if !strings.Contains(rec.SmokeTest, "verify_gpu.py") {
		t.Errorf("SmokeTest must exercise the shared verify_gpu.py fragment, got: %q", rec.SmokeTest)
	}
	// Legacy fields must be cleared so the installer takes the container path.
	if len(rec.InstallSteps) != 0 {
		t.Errorf("InstallSteps should be empty after migration, got: %v", rec.InstallSteps)
	}
	if rec.RunCommand != "" {
		t.Errorf("RunCommand should be empty after migration, got: %q", rec.RunCommand)
	}
	// Pin SHA must show up via git_ref so future drift is easy to spot.
	if !strings.HasPrefix(rec.GitRef, "8fe31141") {
		t.Errorf("git_ref = %q, want pinned RFantibody SHA (so the vendored SE(3)-transformer subtree is fixed)", rec.GitRef)
	}
}

// TestRFantibodyContainerfileEmbedded confirms the Containerfile is bundled
// via go:embed and FROMs the shared BaseImage. Without this the installer
// would silently fall through to "Containerfile not found" on /install.
func TestRFantibodyContainerfileEmbedded(t *testing.T) {
	body, err := loadContainerfile("rfantibody.Containerfile")
	if err != nil {
		t.Fatalf("loadContainerfile: %v", err)
	}
	s := string(body)
	if !strings.Contains(s, "FROM "+BaseImage) {
		t.Errorf("Containerfile must FROM %q for sm_121 PyTorch parity, got first 200 chars:\n%s",
			BaseImage, s[:min(200, len(s))])
	}
	if !strings.Contains(s, "git clone https://github.com/RosettaCommons/RFantibody") {
		t.Error("Containerfile must clone the RFantibody repo")
	}
	if !strings.Contains(s, "uv sync") {
		t.Error("Containerfile must invoke `uv sync` — upstream's only tested install path")
	}
	if !strings.Contains(s, "ARG RFANTIBODY_SHA=8fe31141") {
		t.Error("Containerfile must pin RFANTIBODY_SHA so the vendored SE(3)-transformer subtree is fixed")
	}
	if !strings.Contains(s, "verify_gpu.py") {
		t.Error("Containerfile must stage the shared verify_gpu.py for the smoke test")
	}
}

// TestRFantibodyInstallBuildsImageViaRuntime drives the container-mode install
// path through the runtime exec seam: no podman is actually invoked, but the
// argv that would have been is asserted to mention rfantibody.Containerfile +
// the rfantibody image tag. This is the stub-driven build verification Bug 18
// asks for (GB10 podman build is the maintainer's Phase 3 step).
func TestRFantibodyInstallBuildsImageViaRuntime(t *testing.T) {
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
	runCmdOutput = func(cmd *exec.Cmd) ([]byte, error) {
		// Pretend the BaseImage is cached so we exercise only the build path.
		image := cmd.Args[len(cmd.Args)-1]
		if image == BaseImage {
			return []byte("[{}]\n"), nil
		}
		return nil, &exec.ExitError{}
	}

	if err := inst.InstallLogged(context.Background(), "rfantibody", io.Discard); err != nil {
		t.Fatalf("InstallLogged: %v", err)
	}

	var sawBuild bool
	for _, a := range calls {
		joined := strings.Join(a, " ")
		if strings.Contains(joined, "build -t fova/rfantibody:1.0.0") &&
			strings.Contains(joined, "rfantibody.Containerfile") {
			sawBuild = true
		}
	}
	if !sawBuild {
		t.Errorf("expected `podman build -t fova/rfantibody:1.0.0 ... rfantibody.Containerfile`; got calls:\n%v", calls)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
