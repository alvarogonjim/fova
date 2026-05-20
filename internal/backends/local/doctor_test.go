package local

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDoctorReportsToolStatus(t *testing.T) {
	home := t.TempDir()
	reg, _ := LoadRegistry(home)
	inst := NewInstaller(reg)
	inst.ensureUV = func(ctx context.Context) error { return nil }
	inst.run = func(ctx context.Context, dir, command string, log io.Writer) (string, error) {
		return "", nil
	}
	// rfantibody is still a legacy-shape recipe in this branch — installing it
	// via the bash CmdRunner is what the doctor report should reflect.
	if err := inst.Install(context.Background(), "legacy_fixture"); err != nil {
		t.Fatal(err)
	}

	rep := Diagnose(reg, inst)
	text := rep.String()
	if !strings.Contains(text, "legacy_fixture") {
		t.Errorf("report missing rfantibody:\n%s", text)
	}
	// rfantibody is installed; bindcraft is not — the report must distinguish them.
	if !rep.toolInstalled("legacy_fixture") {
		t.Error("rfantibody should be reported installed")
	}
	if rep.toolInstalled("bindcraft") {
		t.Error("bindcraft should be reported not installed")
	}
	if !strings.Contains(text, "uv") {
		t.Errorf("report missing the uv line:\n%s", text)
	}
}

// stubRuntimeEnv swaps the runtime detection seams for a known-good Podman
// install with the NGC base image cached and nvidia-container-toolkit present.
func stubRuntimeEnv(t *testing.T, podmanPresent, baseCached, nvidia bool) {
	t.Helper()
	oldLookPath := lookPath
	oldInfoOut := runCmdOutput
	t.Cleanup(func() {
		lookPath = oldLookPath
		runCmdOutput = oldInfoOut
	})
	lookPath = func(bin string) (string, error) {
		if podmanPresent && bin == "podman" {
			return "/usr/bin/podman", nil
		}
		return "", exec.ErrNotFound
	}
	runCmdOutput = func(cmd *exec.Cmd) ([]byte, error) {
		// `image inspect <image>` for base-cached check.
		if len(cmd.Args) >= 3 && cmd.Args[1] == "image" && cmd.Args[2] == "inspect" {
			if baseCached {
				return []byte("[{}]\n"), nil
			}
			return nil, &exec.ExitError{}
		}
		// `info` for the nvidia probe.
		if len(cmd.Args) >= 2 && cmd.Args[1] == "info" {
			if nvidia {
				return []byte("runtimes:\n  nvidia\n"), nil
			}
			return []byte("runtimes:\n  crun\n"), nil
		}
		return nil, &exec.ExitError{}
	}
}

func TestDoctorReportsContainerRuntimeSection(t *testing.T) {
	stubRuntimeEnv(t, true, true, true)
	home := t.TempDir()
	reg, _ := LoadRegistry(home)
	inst := NewInstaller(reg)
	inst.runtime = Detect()

	rep := Diagnose(reg, inst)
	text := rep.String()

	for _, want := range []string{
		"Container runtime",
		"podman",
		"nvidia-container-toolkit",
		BaseImage,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("report missing %q:\n%s", want, text)
		}
	}
	// Container runtime section comes before System.
	if i := strings.Index(text, "Container runtime"); i < 0 || i > strings.Index(text, "System") {
		t.Errorf("Container runtime section must come before System:\n%s", text)
	}
}

func TestDoctorReportsMissingRuntime(t *testing.T) {
	stubRuntimeEnv(t, false, false, false)
	home := t.TempDir()
	reg, _ := LoadRegistry(home)
	inst := NewInstaller(reg)
	inst.runtime = Detect()

	rep := Diagnose(reg, inst)
	text := rep.String()
	if !strings.Contains(text, "no container runtime") {
		t.Errorf("expected `no container runtime` line:\n%s", text)
	}
}

func TestDoctorReportsMissingNvidiaToolkit(t *testing.T) {
	stubRuntimeEnv(t, true, true, false)
	home := t.TempDir()
	reg, _ := LoadRegistry(home)
	inst := NewInstaller(reg)
	inst.runtime = Detect()

	rep := Diagnose(reg, inst)
	text := rep.String()
	if !strings.Contains(text, "nvidia-container-toolkit not detected") {
		t.Errorf("expected nvidia-container-toolkit warning:\n%s", text)
	}
}

func TestDoctorReportsLegacyVenvMigrationHint(t *testing.T) {
	// A tool that has a container recipe AND a legacy install_dir on disk
	// (no built image) gets a migration hint.
	stubRuntimeEnv(t, true, false, true)
	home := t.TempDir()
	reg, _ := LoadRegistry(home)
	reg.tools["legacytool"] = ToolRecipe{
		Name:          "legacytool",
		ImageTag:      "fova/legacytool:v1",
		Containerfile: "legacytool.Containerfile",
		Entrypoint:    "x",
		InstallDir:    filepath.Join(home, "tools", "legacytool"),
	}
	// Pre-create the legacy venv directory.
	if err := os.MkdirAll(filepath.Join(home, "tools", "legacytool", ".venv"), 0o755); err != nil {
		t.Fatal(err)
	}
	inst := NewInstaller(reg)
	inst.runtime = Detect()

	rep := Diagnose(reg, inst)
	text := rep.String()
	if !strings.Contains(text, "legacy venv install detected") {
		t.Errorf("expected legacy-venv migration hint:\n%s", text)
	}
	if !strings.Contains(text, "/install legacytool") {
		t.Errorf("migration hint should suggest the /install command:\n%s", text)
	}
}
