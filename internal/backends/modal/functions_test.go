package modal

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestFunctionsPyEmbedded(t *testing.T) {
	if !strings.Contains(FunctionsPy, "modal.App(\"proteus-tools\")") {
		t.Fatal("functions.py does not look like the Modal app")
	}
	for _, marker := range []string{"run_rfdiffusion", "run_bindcraft", "run_ipsae", "fastapi_endpoint"} {
		if !strings.Contains(FunctionsPy, marker) {
			t.Errorf("functions.py missing %q", marker)
		}
	}
}

func TestFunctionsPyCompiles(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 not available; skipping syntax check")
	}
	path := filepath.Join(t.TempDir(), "functions.py")
	if err := os.WriteFile(path, []byte(FunctionsPy), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(python, "-m", "py_compile", path).CombinedOutput()
	if err != nil {
		t.Fatalf("functions.py is not valid Python:\n%s", out)
	}
}
