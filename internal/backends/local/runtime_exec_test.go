package local

import (
	"context"
	"errors"
	"io"
	"os/exec"
	"reflect"
	"strings"
	"testing"
)

// capturedCmd records what runCmd was asked to execute.
type capturedCmd struct {
	bin  string
	args []string
}

// withCapturedRunCmd swaps runCmd for a stub that records the argv and
// returns the supplied error. It restores the original on cleanup.
func withCapturedRunCmd(t *testing.T, retErr error) *capturedCmd {
	t.Helper()
	cap := &capturedCmd{}
	old := runCmd
	runCmd = func(cmd *exec.Cmd) error {
		cap.bin = cmd.Path
		cap.args = append([]string(nil), cmd.Args[1:]...)
		return retErr
	}
	t.Cleanup(func() { runCmd = old })
	return cap
}

func TestRuntimePullBuildsArgv(t *testing.T) {
	cap := withCapturedRunCmd(t, nil)
	r := Runtime{Bin: "/usr/bin/podman", Kind: RuntimePodman}
	if err := r.Pull(context.Background(), BaseImage, io.Discard); err != nil {
		t.Fatalf("Pull: %v", err)
	}
	if cap.bin != "/usr/bin/podman" {
		t.Errorf("bin = %q", cap.bin)
	}
	want := []string{"pull", BaseImage}
	if !reflect.DeepEqual(cap.args, want) {
		t.Errorf("args = %v, want %v", cap.args, want)
	}
}

func TestRuntimeBuildBuildsArgv(t *testing.T) {
	cap := withCapturedRunCmd(t, nil)
	r := Runtime{Bin: "/usr/bin/docker", Kind: RuntimeDocker}
	err := r.Build(context.Background(), "fova/proteinmpnn:v1.0.1",
		"/tmp/build/proteinmpnn.Containerfile", "/tmp/build", io.Discard)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	want := []string{"build", "-t", "fova/proteinmpnn:v1.0.1",
		"-f", "/tmp/build/proteinmpnn.Containerfile", "/tmp/build"}
	if !reflect.DeepEqual(cap.args, want) {
		t.Errorf("args = %v, want %v", cap.args, want)
	}
}

func TestRuntimeRunContainerComposesArgvPodmanGPU(t *testing.T) {
	cap := withCapturedRunCmd(t, nil)
	r := Runtime{Bin: "/usr/bin/podman", Kind: RuntimePodman}
	_, err := r.RunContainer(context.Background(), ContainerRunArgs{
		Name:    "j_abc",
		Image:   "fova/proteinmpnn:v1.0.1",
		Cmd:     []string{"python", "/opt/proteinmpnn/run.py", "@/work/args"},
		GPU:     true,
		Workdir: "/work",
		Mounts: []Mount{
			{HostPath: "/tmp/ws", ContainerPath: "/work"},
			{HostPath: "/home/u/.fova/models/proteinmpnn", ContainerPath: "/models", ReadOnly: true},
		},
		Env: map[string]string{"FOVA_TOOL": "proteinmpnn"},
		Log: io.Discard,
	})
	if err != nil {
		t.Fatalf("RunContainer: %v", err)
	}
	joined := strings.Join(cap.args, " ")
	for _, want := range []string{
		"run", "--rm",
		"--name j_abc",
		"--device nvidia.com/gpu=all",
		"-v /tmp/ws:/work",
		"-v /home/u/.fova/models/proteinmpnn:/models:ro",
		"-w /work",
		"-e FOVA_TOOL=proteinmpnn",
		"fova/proteinmpnn:v1.0.1",
		"python /opt/proteinmpnn/run.py @/work/args",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("argv missing %q in: %s", want, joined)
		}
	}
}

func TestRuntimeRunContainerComposesArgvDockerGPU(t *testing.T) {
	cap := withCapturedRunCmd(t, nil)
	r := Runtime{Bin: "/usr/bin/docker", Kind: RuntimeDocker}
	_, err := r.RunContainer(context.Background(), ContainerRunArgs{
		Name:  "j_xyz",
		Image: "fova/bindcraft:v1.0.0",
		Cmd:   []string{"python", "/opt/bindcraft/run.py"},
		GPU:   true,
		Log:   io.Discard,
	})
	if err != nil {
		t.Fatalf("RunContainer: %v", err)
	}
	joined := strings.Join(cap.args, " ")
	if !strings.Contains(joined, "--gpus all") {
		t.Errorf("docker GPU flag missing in: %s", joined)
	}
	if strings.Contains(joined, "nvidia.com/gpu=all") {
		t.Errorf("docker should not use Podman CDI syntax: %s", joined)
	}
}

func TestRuntimeRunContainerNoGPUOmitsFlag(t *testing.T) {
	cap := withCapturedRunCmd(t, nil)
	r := Runtime{Bin: "/usr/bin/podman", Kind: RuntimePodman}
	_, err := r.RunContainer(context.Background(), ContainerRunArgs{
		Name:  "j_cpu",
		Image: "fova/ipsae:v1.0.0",
		Cmd:   []string{"python", "/opt/ipsae/ipsae.py"},
		Log:   io.Discard,
	})
	if err != nil {
		t.Fatalf("RunContainer: %v", err)
	}
	joined := strings.Join(cap.args, " ")
	if strings.Contains(joined, "--gpus") || strings.Contains(joined, "nvidia.com/gpu") {
		t.Errorf("CPU-only run should not request GPU: %s", joined)
	}
}

func TestRuntimeKillBuildsArgv(t *testing.T) {
	cap := withCapturedRunCmd(t, nil)
	r := Runtime{Bin: "/usr/bin/podman", Kind: RuntimePodman}
	if err := r.Kill("j_abc"); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	want := []string{"kill", "j_abc"}
	if !reflect.DeepEqual(cap.args, want) {
		t.Errorf("args = %v, want %v", cap.args, want)
	}
}

func TestRuntimeImageExistsTrue(t *testing.T) {
	old := runCmdOutput
	defer func() { runCmdOutput = old }()
	runCmdOutput = func(cmd *exec.Cmd) ([]byte, error) {
		// Simulate `<bin> image inspect <image>` succeeding.
		return []byte("[{}]\n"), nil
	}
	r := Runtime{Bin: "/usr/bin/podman", Kind: RuntimePodman}
	ok, err := r.ImageExists("fova/proteinmpnn:v1.0.1")
	if err != nil {
		t.Fatalf("ImageExists: %v", err)
	}
	if !ok {
		t.Error("ok = false, want true")
	}
}

func TestRuntimeImageExistsFalse(t *testing.T) {
	old := runCmdOutput
	defer func() { runCmdOutput = old }()
	runCmdOutput = func(cmd *exec.Cmd) ([]byte, error) {
		// `image inspect` exits non-zero when the image is absent.
		return nil, &exec.ExitError{}
	}
	r := Runtime{Bin: "/usr/bin/podman", Kind: RuntimePodman}
	ok, err := r.ImageExists("fova/nope:v0")
	if err != nil {
		t.Fatalf("ImageExists: %v", err)
	}
	if ok {
		t.Error("ok = true, want false")
	}
}

func TestRuntimeNoneBinFails(t *testing.T) {
	r := Runtime{Kind: RuntimeNone}
	if err := r.Pull(context.Background(), BaseImage, io.Discard); err == nil {
		t.Error("Pull on RuntimeNone should error")
	}
	if err := r.Build(context.Background(), "x", "/tmp/cf", "/tmp", io.Discard); err == nil {
		t.Error("Build on RuntimeNone should error")
	}
	if _, err := r.RunContainer(context.Background(), ContainerRunArgs{}); err == nil {
		t.Error("RunContainer on RuntimeNone should error")
	}
}

func TestRuntimePullPropagatesError(t *testing.T) {
	_ = withCapturedRunCmd(t, errors.New("network down"))
	r := Runtime{Bin: "/usr/bin/podman", Kind: RuntimePodman}
	if err := r.Pull(context.Background(), BaseImage, io.Discard); err == nil {
		t.Error("expected error from failed pull")
	}
}

func TestRunContainerEntrypointOverride(t *testing.T) {
	calls := stubContainerRuntime(t, nil)
	rt := Detect()
	_, _ = rt.RunContainer(context.Background(), ContainerRunArgs{
		Image: "fova/x:1", Entrypoint: "bash", Cmd: []string{"/work/run.sh"},
	})
	joined := strings.Join((*calls)[0], " ")
	if !strings.Contains(joined, "--entrypoint bash") {
		t.Errorf("argv missing --entrypoint bash: %s", joined)
	}
	if strings.Index(joined, "--entrypoint") > strings.Index(joined, "fova/x:1") {
		t.Error("--entrypoint must precede the image")
	}
}
