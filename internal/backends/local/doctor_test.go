package local

import (
	"context"
	"strings"
	"testing"
)

func TestDoctorReportsToolStatus(t *testing.T) {
	home := t.TempDir()
	reg, _ := LoadRegistry(home)
	inst := NewInstaller(reg)
	inst.ensureUV = func(ctx context.Context) error { return nil }
	inst.run = func(ctx context.Context, dir, command string) (string, error) { return "", nil }
	if err := inst.Install(context.Background(), "ipsae"); err != nil {
		t.Fatal(err)
	}

	rep := Diagnose(reg, inst)
	text := rep.String()
	if !strings.Contains(text, "ipsae") {
		t.Errorf("report missing ipsae:\n%s", text)
	}
	// ipsae is installed; bindcraft is not — the report must distinguish them.
	if !rep.toolInstalled("ipsae") {
		t.Error("ipsae should be reported installed")
	}
	if rep.toolInstalled("bindcraft") {
		t.Error("bindcraft should be reported not installed")
	}
	if !strings.Contains(text, "uv") {
		t.Errorf("report missing the uv line:\n%s", text)
	}
}
