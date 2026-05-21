package local

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// boltzGenCheckStubRuntime stubs lookPath + runCmd + runCmdOutput so the check
// adapter's container path runs without podman/docker. It records every argv
// in ranArgs. When checkOutput is non-empty it is written to the container's
// log sink (cmd.Stdout) so the adapter can parse it; when failExit is true the
// stub returns a non-zero-exit error, mimicking `boltzgen check` rejecting a
// spec. vizName, when non-empty, is the .cif the stub drops into the workdir to
// mimic the visualization BoltzGen renders.
func boltzGenCheckStubRuntime(t *testing.T, ranArgs *[][]string, checkOutput string, failExit bool, workDir, vizName string) func() {
	t.Helper()
	oldLook := lookPath
	oldRun := runCmd
	oldOut := runCmdOutput
	lookPath = func(bin string) (string, error) {
		if bin == "podman" {
			return "/usr/bin/podman", nil
		}
		return "", errors.New("not found")
	}
	runCmd = func(cmd *exec.Cmd) error {
		*ranArgs = append(*ranArgs, append([]string(nil), cmd.Args...))
		if checkOutput != "" && cmd.Stdout != nil {
			_, _ = cmd.Stdout.Write([]byte(checkOutput))
		}
		if vizName != "" {
			_ = os.WriteFile(filepath.Join(workDir, vizName), []byte("data_viz\n"), 0o644)
		}
		if failExit {
			// A non-zero container exit surfaces as an *exec.ExitError in
			// production; any error is enough to drive the adapter's
			// invalid-spec path here.
			return errors.New("exit status 1")
		}
		return nil
	}
	runCmdOutput = func(cmd *exec.Cmd) ([]byte, error) {
		return []byte("[{}]\n"), nil // image inspect → exists
	}
	return func() {
		lookPath = oldLook
		runCmd = oldRun
		runCmdOutput = oldOut
	}
}

func TestBoltzGenCheckAdapterRegistered(t *testing.T) {
	a, ok := adapterRegistry["design.boltzgen_check"]
	if !ok {
		t.Fatal("design.boltzgen_check adapter is not registered")
	}
	if a.Recipe() != "boltzgen" {
		t.Errorf("Recipe() = %q, want boltzgen", a.Recipe())
	}
	if a.AgentTool() != "design.boltzgen_check" {
		t.Errorf("AgentTool() = %q, want design.boltzgen_check", a.AgentTool())
	}
}

func TestBoltzGenCheckAdapterValidSpec(t *testing.T) {
	env, _ := boltzGenTestEnv(t)
	var logBuf bytes.Buffer
	env.Log = &logBuf

	specDir := t.TempDir()
	specPath, targetName := boltzGenWriteSpec(t, specDir)

	var ran [][]string
	restore := boltzGenCheckStubRuntime(t, &ran, "Spec OK: 2 entities parsed\n", false, env.WorkDir, "in_visualization.cif")
	defer restore()

	req, _ := json.Marshal(map[string]any{"spec_path": specPath})
	out, err := boltzGenCheckAdapter{}.Invoke(context.Background(), env, req)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	var result boltzGenCheckOutput
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatalf("output is not valid check JSON: %v", err)
	}
	if !result.Valid {
		t.Errorf("a clean `boltzgen check` (exit 0) must yield valid:true, got %+v", result)
	}
	if len(result.Errors) != 0 {
		t.Errorf("a valid spec must carry no errors, got %v", result.Errors)
	}
	if result.VisualizationPath == "" {
		t.Error("visualization_path must be set when boltzgen check renders an mmCIF")
	}

	// The argv must invoke `boltzgen check /work/in.yaml` on the boltzgen image.
	if len(ran) != 1 {
		t.Fatalf("want 1 runCmd invocation, got %d: %v", len(ran), ran)
	}
	argv := strings.Join(ran[0], " ")
	if !strings.Contains(argv, "fova/boltzgen:v0.3.2") {
		t.Errorf("argv must reference the boltzgen image tag, got: %s", argv)
	}
	if !strings.Contains(argv, "check /work/in.yaml") {
		t.Errorf("argv must call `boltzgen check /work/in.yaml`, got: %s", argv)
	}
	if !strings.Contains(argv, env.WorkDir+":/work") {
		t.Errorf("argv must bind-mount env.WorkDir at /work, got: %s", argv)
	}
	// boltzgen check needs no GPU — the argv must not request one.
	if strings.Contains(argv, "nvidia.com/gpu") || strings.Contains(argv, "--gpus") {
		t.Errorf("boltzgen check must not request a GPU, got: %s", argv)
	}

	// The spec + its referenced structure file must have been staged.
	if _, err := os.Stat(filepath.Join(env.WorkDir, "in.yaml")); err != nil {
		t.Errorf("spec must be staged to in.yaml: %v", err)
	}
	if _, err := os.Stat(filepath.Join(env.WorkDir, targetName)); err != nil {
		t.Errorf("the referenced structure file must be staged into WorkDir: %v", err)
	}
}

func TestBoltzGenCheckAdapterInvalidSpec(t *testing.T) {
	env, _ := boltzGenTestEnv(t)
	specDir := t.TempDir()
	specPath, _ := boltzGenWriteSpec(t, specDir)

	var ran [][]string
	restore := boltzGenCheckStubRuntime(t, &ran,
		"Validating spec...\nError: entity B references unknown chain\nValidationError: 1 problem\n",
		true, env.WorkDir, "")
	defer restore()

	req, _ := json.Marshal(map[string]any{"spec_path": specPath})
	out, err := boltzGenCheckAdapter{}.Invoke(context.Background(), env, req)
	if err != nil {
		t.Fatalf("Invoke must not error for an invalid spec — it reports valid:false: %v", err)
	}
	var result boltzGenCheckOutput
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatalf("output is not valid check JSON: %v", err)
	}
	if result.Valid {
		t.Error("a non-zero `boltzgen check` exit must yield valid:false")
	}
	if len(result.Errors) == 0 {
		t.Fatal("an invalid spec must populate errors")
	}
	joined := strings.Join(result.Errors, " | ")
	if !strings.Contains(joined, "entity B references unknown chain") {
		t.Errorf("errors must carry the parsed check message, got: %v", result.Errors)
	}
}

func TestBoltzGenCheckAdapterMissingSpecPath(t *testing.T) {
	env, _ := boltzGenTestEnv(t)
	_, err := boltzGenCheckAdapter{}.Invoke(context.Background(), env, []byte(`{}`))
	if err == nil {
		t.Fatal("expected an error when spec_path is missing")
	}
	if !strings.Contains(err.Error(), "spec_path") {
		t.Errorf("error %q should mention spec_path", err)
	}
}

func TestBoltzGenCheckAdapterSpecNotFound(t *testing.T) {
	env, _ := boltzGenTestEnv(t)
	_, err := boltzGenCheckAdapter{}.Invoke(context.Background(), env,
		[]byte(`{"spec_path":"/no/such/spec.yaml"}`))
	if err == nil {
		t.Fatal("expected an error when the spec file does not exist")
	}
	if !strings.Contains(err.Error(), "boltzgen-spec") {
		t.Errorf("error %q should point at the boltzgen-spec skill", err)
	}
}

func TestBoltzGenCheckAdapterNotInstalled(t *testing.T) {
	specDir := t.TempDir()
	specPath, _ := boltzGenWriteSpec(t, specDir)
	reg, err := LoadRegistry(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	env := AdapterEnv{
		Recipe:   ToolRecipe{Name: "boltzgen"}, // no ImageTag
		WorkDir:  t.TempDir(),
		Registry: reg,
	}
	_, err = boltzGenCheckAdapter{}.Invoke(context.Background(), env,
		[]byte(`{"spec_path":"`+specPath+`"}`))
	if err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Fatalf("want a 'not installed' error, got: %v", err)
	}
}

func TestParseBoltzGenCheckErrors(t *testing.T) {
	cases := []struct {
		name string
		out  string
		want int // number of error lines expected
	}{
		{"clean output yields no errors", "Spec OK\n2 entities parsed\nDone.\n", 0},
		{"error line is collected", "Validating...\nError: bad chain\n", 1},
		{"traceback is collected", "Traceback (most recent call last):\n  File ...\n", 1},
		{"empty output yields no errors", "", 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := parseBoltzGenCheckErrors(c.out)
			if len(got) != c.want {
				t.Errorf("parseBoltzGenCheckErrors(%q) = %v, want %d lines", c.out, got, c.want)
			}
		})
	}
}
