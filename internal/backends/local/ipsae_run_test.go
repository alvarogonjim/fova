package local

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestRunIPSAEStagesInputsAndBuildsArgv exercises the happy-path container
// invocation: the helper must stage both inputs into a temp dir, mount that
// dir at /work, and call the runtime with the 4-positional ipsae argv.
func TestRunIPSAEStagesInputsAndBuildsArgv(t *testing.T) {
	scores := filepath.Join(t.TempDir(), "scores.json")
	if err := os.WriteFile(scores, []byte(`{"pae":[[0]]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	structure := filepath.Join(t.TempDir(), "complex.pdb")
	if err := os.WriteFile(structure, []byte("ATOM      1  N\nEND\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Force Detect() to find a "podman" binary.
	oldLookPath := lookPath
	defer func() { lookPath = oldLookPath }()
	lookPath = func(bin string) (string, error) {
		if bin == "podman" {
			return "/usr/bin/podman", nil
		}
		return "", errors.New("not found")
	}

	// Capture argv and inspect the staged /work dir before the helper deletes it.
	var capturedArgs []string
	var stagedFiles []string
	oldRun := runCmd
	defer func() { runCmd = oldRun }()
	runCmd = func(cmd *exec.Cmd) error {
		capturedArgs = append([]string(nil), cmd.Args[1:]...)
		// Find the -v <host>:/work mount and snapshot its contents.
		for i := 0; i+1 < len(cmd.Args); i++ {
			if cmd.Args[i] == "-v" && strings.HasSuffix(cmd.Args[i+1], ":/work") {
				hostDir := strings.TrimSuffix(cmd.Args[i+1], ":/work")
				entries, err := os.ReadDir(hostDir)
				if err != nil {
					return err
				}
				for _, e := range entries {
					stagedFiles = append(stagedFiles, e.Name())
				}
			}
		}
		// Write some text to stdout so the helper has output to capture.
		if cmd.Stdout != nil {
			_, _ = cmd.Stdout.Write([]byte("ipSAE_max 0.42\n"))
		}
		return nil
	}

	var log bytes.Buffer
	rec := ToolRecipe{Name: "ipsae", ImageTag: "fova/ipsae:v1.0.0"}
	out, err := RunIPSAE(context.Background(), scores, structure, rec, &log)
	if err != nil {
		t.Fatalf("RunIPSAE: %v", err)
	}
	if !strings.Contains(out, "ipSAE_max 0.42") {
		t.Errorf("captured stdout = %q, want it to contain 'ipSAE_max 0.42'", out)
	}
	if !strings.Contains(log.String(), "ipSAE_max 0.42") {
		t.Errorf("log writer did not receive stdout (got %q)", log.String())
	}

	joined := strings.Join(capturedArgs, " ")
	for _, want := range []string{
		"run", "--rm",
		"--name fova_ipsae_",
		":/work",
		"-w /work",
		"fova/ipsae:v1.0.0",
		"python /opt/ipsae/ipsae.py /work/pae.json /work/structure.pdb 10 10",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("argv missing %q in: %s", want, joined)
		}
	}
	// CPU-only — no GPU flag may appear.
	if strings.Contains(joined, "--gpus") || strings.Contains(joined, "nvidia.com/gpu") {
		t.Errorf("ipsae must run CPU-only; argv has a GPU flag: %s", joined)
	}

	// Both files must have been staged into /work with the expected names.
	got := map[string]bool{}
	for _, f := range stagedFiles {
		got[f] = true
	}
	if !got["pae.json"] {
		t.Errorf("scores file was not staged as pae.json (staged: %v)", stagedFiles)
	}
	if !got["structure.pdb"] {
		t.Errorf("structure file was not staged as structure.pdb (staged: %v)", stagedFiles)
	}
}

// TestRunIPSAEPreservesNonPDBExt covers the AF3/Boltz1 case where the
// structure is a .cif and the scores file is a .npz: ipsae.py dispatches
// on the extension, so the staged names must keep the suffix.
func TestRunIPSAEPreservesNonPDBExt(t *testing.T) {
	scores := filepath.Join(t.TempDir(), "pae_AURKA.npz")
	if err := os.WriteFile(scores, []byte("PK\x03\x04"), 0o644); err != nil {
		t.Fatal(err)
	}
	structure := filepath.Join(t.TempDir(), "model.cif")
	if err := os.WriteFile(structure, []byte("data_model\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	oldLookPath := lookPath
	defer func() { lookPath = oldLookPath }()
	lookPath = func(string) (string, error) { return "/usr/bin/podman", nil }

	var capturedArgs []string
	oldRun := runCmd
	defer func() { runCmd = oldRun }()
	runCmd = func(cmd *exec.Cmd) error {
		capturedArgs = append([]string(nil), cmd.Args[1:]...)
		return nil
	}

	rec := ToolRecipe{Name: "ipsae", ImageTag: "fova/ipsae:v1.0.0"}
	if _, err := RunIPSAE(context.Background(), scores, structure, rec, io.Discard); err != nil {
		t.Fatalf("RunIPSAE: %v", err)
	}
	joined := strings.Join(capturedArgs, " ")
	if !strings.Contains(joined, "/work/pae.npz") {
		t.Errorf("argv must reference /work/pae.npz, got: %s", joined)
	}
	if !strings.Contains(joined, "/work/structure.cif") {
		t.Errorf("argv must reference /work/structure.cif, got: %s", joined)
	}
}

// TestRunIPSAEMissingImageErrors guards the "recipe loaded but no image_tag"
// case (e.g. legacy uv-mode recipe shape) — we surface a clear install hint
// instead of trying to launch a container.
func TestRunIPSAEMissingImageErrors(t *testing.T) {
	scores := filepath.Join(t.TempDir(), "s.json")
	_ = os.WriteFile(scores, []byte("{}"), 0o644)
	structure := filepath.Join(t.TempDir(), "c.pdb")
	_ = os.WriteFile(structure, []byte("ATOM\n"), 0o644)

	_, err := RunIPSAE(context.Background(), scores, structure,
		ToolRecipe{Name: "ipsae"}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "image_tag") {
		t.Errorf("want an 'image_tag' error, got %v", err)
	}
}

// TestRunIPSAENoRuntimeErrors covers the host with neither podman nor docker
// installed — RunIPSAE must refuse to proceed with a clear message.
func TestRunIPSAENoRuntimeErrors(t *testing.T) {
	scores := filepath.Join(t.TempDir(), "s.json")
	_ = os.WriteFile(scores, []byte("{}"), 0o644)
	structure := filepath.Join(t.TempDir(), "c.pdb")
	_ = os.WriteFile(structure, []byte("ATOM\n"), 0o644)

	oldLookPath := lookPath
	defer func() { lookPath = oldLookPath }()
	lookPath = func(string) (string, error) { return "", errors.New("not found") }

	_, err := RunIPSAE(context.Background(), scores, structure,
		ToolRecipe{Name: "ipsae", ImageTag: "fova/ipsae:v1.0.0"}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "container runtime") {
		t.Errorf("want a 'container runtime' error, got %v", err)
	}
}

// TestRunIPSAEPropagatesContainerError ensures a non-zero exit from the
// container surfaces as an error and the captured stdout-so-far is returned
// so the caller can include it in the failure log.
func TestRunIPSAEPropagatesContainerError(t *testing.T) {
	scores := filepath.Join(t.TempDir(), "s.json")
	_ = os.WriteFile(scores, []byte("{}"), 0o644)
	structure := filepath.Join(t.TempDir(), "c.pdb")
	_ = os.WriteFile(structure, []byte("ATOM\n"), 0o644)

	oldLookPath := lookPath
	defer func() { lookPath = oldLookPath }()
	lookPath = func(string) (string, error) { return "/usr/bin/podman", nil }

	oldRun := runCmd
	defer func() { runCmd = oldRun }()
	runCmd = func(cmd *exec.Cmd) error {
		if cmd.Stdout != nil {
			_, _ = cmd.Stdout.Write([]byte("traceback...\n"))
		}
		return errors.New("exit status 1")
	}

	out, err := RunIPSAE(context.Background(), scores, structure,
		ToolRecipe{Name: "ipsae", ImageTag: "fova/ipsae:v1.0.0"}, io.Discard)
	if err == nil {
		t.Fatal("expected an error from the failed container")
	}
	if !strings.Contains(out, "traceback") {
		t.Errorf("captured output should include the container's stdout, got %q", out)
	}
}
