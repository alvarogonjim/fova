package local

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestInstallerDryRun(t *testing.T) {
	reg, _ := LoadRegistry(t.TempDir())
	inst := NewInstaller(reg)
	steps, err := inst.DryRun("ipsae")
	if err != nil {
		t.Fatalf("DryRun: %v", err)
	}
	if len(steps) == 0 {
		t.Fatal("DryRun returned no steps")
	}
	if !strings.Contains(steps[0], "git clone") {
		t.Errorf("first step = %q", steps[0])
	}
}

func TestInstallerInstallRunsStepsAndWritesLock(t *testing.T) {
	home := t.TempDir()
	reg, _ := LoadRegistry(home)
	inst := NewInstaller(reg)

	var ran []string
	inst.run = func(ctx context.Context, dir, command string) (string, error) {
		ran = append(ran, command)
		return "ok", nil
	}
	inst.ensureUV = func(ctx context.Context) error { return nil } // skip real uv

	if err := inst.Install(context.Background(), "ipsae"); err != nil {
		t.Fatalf("Install: %v", err)
	}
	if len(ran) != 3 {
		t.Fatalf("expected 3 steps run, got %d: %v", len(ran), ran)
	}
	st := inst.Status("ipsae")
	if !st.Installed {
		t.Error("Status should report ipsae installed after Install")
	}
	if st.Version != "1.0.0" {
		t.Errorf("Status version = %q", st.Version)
	}
}

func TestInstallerInstallFailureNamesStep(t *testing.T) {
	home := t.TempDir()
	reg, _ := LoadRegistry(home)
	inst := NewInstaller(reg)
	inst.ensureUV = func(ctx context.Context) error { return nil }
	inst.run = func(ctx context.Context, dir, command string) (string, error) {
		if strings.Contains(command, "uv venv") {
			return "boom", errors.New("venv failed")
		}
		return "ok", nil
	}
	err := inst.Install(context.Background(), "ipsae")
	if err == nil {
		t.Fatal("expected install to fail")
	}
	if !strings.Contains(err.Error(), "step 2") {
		t.Errorf("error should name the failing step, got: %v", err)
	}
	if inst.Status("ipsae").Installed {
		t.Error("a failed install must not be marked installed")
	}
}

func TestInstallerRemove(t *testing.T) {
	home := t.TempDir()
	reg, _ := LoadRegistry(home)
	inst := NewInstaller(reg)
	inst.ensureUV = func(ctx context.Context) error { return nil }
	inst.run = func(ctx context.Context, dir, command string) (string, error) { return "", nil }
	if err := inst.Install(context.Background(), "ipsae"); err != nil {
		t.Fatal(err)
	}
	if err := inst.Remove(context.Background(), "ipsae"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if inst.Status("ipsae").Installed {
		t.Error("Status should report ipsae not installed after Remove")
	}
}

func TestUVPath(t *testing.T) {
	// UVPath must not panic and returns ok=false cleanly when uv is absent.
	_, _ = UVPath()
}
