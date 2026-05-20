package local

import (
	"context"
	"io"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunnerRendersAndRuns(t *testing.T) {
	reg, _ := LoadRegistry(t.TempDir())
	runner := NewRunner(reg)

	var got string
	runner.run = func(ctx context.Context, dir, command string, log io.Writer) (string, error) {
		got = command
		return "tool output", nil
	}
	// rfantibody is still a legacy-mode recipe (Bug 14 only migrated
	// proteinmpnn to container-mode). Use it to verify run_command placeholder
	// rendering for the legacy path.
	out, err := runner.Run(context.Background(), "legacy_fixture", map[string]string{
		"args_file": "/tmp/req.json",
	}, io.Discard)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out != "tool output" {
		t.Errorf("out = %q", out)
	}
	// The runtime placeholder was filled.
	if !strings.Contains(got, "/tmp/req.json") {
		t.Errorf("command did not get the input path: %q", got)
	}
	if strings.Contains(got, "{{") {
		t.Errorf("command still has an unfilled placeholder: %q", got)
	}
}

func TestRunnerUnknownTool(t *testing.T) {
	reg, _ := LoadRegistry(t.TempDir())
	runner := NewRunner(reg)
	if _, err := runner.Run(context.Background(), "no-such-tool", nil, io.Discard); err == nil {
		t.Error("running an unknown tool should error")
	}
}

func TestRunnerContainerModeInvokesRuntime(t *testing.T) {
	home := t.TempDir()
	reg, _ := LoadRegistry(home)
	// Overlay a container-mode recipe.
	reg.tools["mpnnc"] = ToolRecipe{
		Name:       "mpnnc",
		ImageTag:   "fova/proteinmpnn:v1.0.1",
		Entrypoint: "python /opt/proteinmpnn/run.py @{{ args_file }}",
		GPU:        true,
	}
	runner := NewRunner(reg)
	runner.runtime = Runtime{Bin: "/usr/bin/podman", Kind: RuntimePodman}

	var got []string
	old := runCmd
	defer func() { runCmd = old }()
	runCmd = func(cmd *exec.Cmd) error {
		got = append([]string(nil), cmd.Args[1:]...)
		return nil
	}

	_, err := runner.Run(context.Background(), "mpnnc", map[string]string{
		"args_file": "/work/args.txt",
		"job_id":    "j_test",
	}, io.Discard)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	joined := strings.Join(got, " ")
	for _, want := range []string{
		"run", "--rm",
		"--name j_test",
		"--device nvidia.com/gpu=all",
		"fova/proteinmpnn:v1.0.1",
		"python /opt/proteinmpnn/run.py @/work/args.txt",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("argv missing %q in: %s", want, joined)
		}
	}
}

func TestRunnerContainerModeUsesPlaceholdersForJobID(t *testing.T) {
	// Without an explicit job_id placeholder, the runner generates one so the
	// container name is unique and cancellation has a target to kill.
	home := t.TempDir()
	reg, _ := LoadRegistry(home)
	reg.tools["t"] = ToolRecipe{
		Name:       "t",
		ImageTag:   "fova/t:v1",
		Entrypoint: "x",
	}
	runner := NewRunner(reg)
	runner.runtime = Runtime{Bin: "/usr/bin/podman", Kind: RuntimePodman}

	var captured []string
	old := runCmd
	defer func() { runCmd = old }()
	runCmd = func(cmd *exec.Cmd) error {
		captured = append([]string(nil), cmd.Args[1:]...)
		return nil
	}
	if _, err := runner.Run(context.Background(), "t", nil, io.Discard); err != nil {
		t.Fatalf("Run: %v", err)
	}
	joined := strings.Join(captured, " ")
	if !strings.Contains(joined, "--name ") {
		t.Errorf("expected an auto-generated container name in: %s", joined)
	}
}

func TestRunnerContainerCancellationCallsKill(t *testing.T) {
	home := t.TempDir()
	reg, _ := LoadRegistry(home)
	reg.tools["t"] = ToolRecipe{
		Name:       "t",
		ImageTag:   "fova/t:v1",
		Entrypoint: "x",
	}
	runner := NewRunner(reg)
	runner.runtime = Runtime{Bin: "/usr/bin/podman", Kind: RuntimePodman}

	ctx, cancel := context.WithCancel(context.Background())

	var killed atomic.Bool
	started := make(chan struct{})
	var wg sync.WaitGroup
	old := runCmd
	defer func() { runCmd = old }()
	runCmd = func(cmd *exec.Cmd) error {
		// `kill <name>` is the cancellation path; everything else is Run.
		if len(cmd.Args) >= 2 && cmd.Args[1] == "kill" {
			killed.Store(true)
			return nil
		}
		// The "run" call blocks until ctx is done so the runner has time to
		// observe ctx.Done() and fire Kill.
		started <- struct{}{}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Second):
			return nil
		}
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		_, _ = runner.Run(ctx, "t", map[string]string{"job_id": "j_kill"}, io.Discard)
	}()

	<-started
	cancel()
	wg.Wait()

	if !killed.Load() {
		t.Error("expected Runtime.Kill to be invoked on context cancellation")
	}
}

func TestRunnerContainerModeMountsWorkspaceAndModels(t *testing.T) {
	home := t.TempDir()
	reg, _ := LoadRegistry(home)
	reg.tools["wt"] = ToolRecipe{
		Name:         "wt",
		ImageTag:     "fova/wt:v1",
		Entrypoint:   "python /opt/wt/run.py",
		WeightsPaths: []string{"/models"},
	}
	runner := NewRunner(reg)
	runner.runtime = Runtime{Bin: "/usr/bin/podman", Kind: RuntimePodman}

	var got []string
	old := runCmd
	defer func() { runCmd = old }()
	runCmd = func(cmd *exec.Cmd) error {
		got = append([]string(nil), cmd.Args[1:]...)
		return nil
	}

	_, err := runner.Run(context.Background(), "wt", map[string]string{
		"workspace": "/tmp/ws",
	}, io.Discard)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	joined := strings.Join(got, " ")
	if !strings.Contains(joined, "-v /tmp/ws:/work") {
		t.Errorf("workspace mount missing in: %s", joined)
	}
	if !strings.Contains(joined, ":/models") {
		t.Errorf("models mount missing in: %s", joined)
	}
}
