package local

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sort"
)

// ContainerRunArgs is the set of options Runtime.RunContainer consumes. The
// caller is responsible for filling Cmd with the templated entrypoint+args.
type ContainerRunArgs struct {
	Name       string            // job/container name, used for cancellation
	Image      string            // full image tag, e.g. fova/proteinmpnn:v1.0.1
	Cmd        []string          // entrypoint + args, already templated
	Mounts     []Mount           // bind mounts
	GPU        bool              // request all GPUs
	Env        map[string]string // extra environment variables for the container
	Workdir    string            // working directory inside the container
	Entrypoint string            // override the image ENTRYPOINT; empty = keep the image default
	Log        io.Writer         // stream stdout+stderr here; nil = io.Discard
}

// Mount describes one bind mount into the container.
type Mount struct {
	HostPath      string
	ContainerPath string
	ReadOnly      bool
}

// runCmd is the seam tests override to capture argv instead of executing.
// Production wires it to exec.Cmd.Run.
var runCmd = func(cmd *exec.Cmd) error { return cmd.Run() }

// runCmdOutput is the seam for commands whose stdout we need (ImageExists).
var runCmdOutput = func(cmd *exec.Cmd) ([]byte, error) { return cmd.Output() }

// errNoRuntime is returned when a Runtime method is called on a zero Runtime.
var errNoRuntime = errors.New("no container runtime configured")

// Pull fetches an image from the remote registry. Streams runtime output to log.
func (r Runtime) Pull(ctx context.Context, image string, log io.Writer) error {
	if r.Bin == "" {
		return errNoRuntime
	}
	cmd := exec.CommandContext(ctx, r.Bin, "pull", image)
	attachLog(cmd, log)
	return runCmd(cmd)
}

// Build runs `<bin> build -t tag -f containerfile ctxDir`. Streams output to log.
func (r Runtime) Build(ctx context.Context, tag, containerfile, ctxDir string, log io.Writer) error {
	if r.Bin == "" {
		return errNoRuntime
	}
	cmd := exec.CommandContext(ctx, r.Bin, "build", "-t", tag, "-f", containerfile, ctxDir)
	attachLog(cmd, log)
	return runCmd(cmd)
}

// RunContainer assembles `<bin> run --rm --name <a.Name> [GPU] [mounts] [env]
// [workdir] <image> <cmd...>` and runs it, streaming stdout+stderr to a.Log.
// Returns no captured string in this iteration — adapters that need the output
// should tee it via a.Log.
func (r Runtime) RunContainer(ctx context.Context, a ContainerRunArgs) (string, error) {
	if r.Bin == "" {
		return "", errNoRuntime
	}
	args := []string{"run", "--rm"}
	if a.Name != "" {
		args = append(args, "--name", a.Name)
	}
	if a.GPU {
		// Docker uses --gpus all; Podman uses CDI device syntax.
		if r.Kind == RuntimeDocker {
			args = append(args, "--gpus", "all")
		} else {
			args = append(args, "--device", "nvidia.com/gpu=all")
		}
	}
	for _, m := range a.Mounts {
		v := m.HostPath + ":" + m.ContainerPath
		if m.ReadOnly {
			v += ":ro"
		}
		args = append(args, "-v", v)
	}
	// Deterministic env order so argv is testable.
	envKeys := make([]string, 0, len(a.Env))
	for k := range a.Env {
		envKeys = append(envKeys, k)
	}
	sort.Strings(envKeys)
	for _, k := range envKeys {
		args = append(args, "-e", k+"="+a.Env[k])
	}
	if a.Workdir != "" {
		args = append(args, "-w", a.Workdir)
	}
	if a.Entrypoint != "" {
		args = append(args, "--entrypoint", a.Entrypoint)
	}
	args = append(args, a.Image)
	args = append(args, a.Cmd...)
	cmd := exec.CommandContext(ctx, r.Bin, args...)
	attachLog(cmd, a.Log)
	if err := runCmd(cmd); err != nil {
		return "", err
	}
	return "", nil
}

// Kill terminates a running container by name. Best-effort: errors are
// returned so the caller can log them; cancellation paths typically ignore.
func (r Runtime) Kill(name string) error {
	if r.Bin == "" {
		return errNoRuntime
	}
	cmd := exec.Command(r.Bin, "kill", name)
	return runCmd(cmd)
}

// ImageExists reports whether the runtime has the named image locally.
// Implemented via `<bin> image inspect <image>` — non-zero exit ⇒ absent.
func (r Runtime) ImageExists(image string) (bool, error) {
	if r.Bin == "" {
		return false, errNoRuntime
	}
	cmd := exec.Command(r.Bin, "image", "inspect", image)
	if _, err := runCmdOutput(cmd); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return false, nil
		}
		// LookPath/exec startup errors propagate so callers see real failures.
		return false, fmt.Errorf("image inspect %s: %w", image, err)
	}
	return true, nil
}

// RemoveImage deletes a local image. Used by Installer.Uninstall.
func (r Runtime) RemoveImage(image string) error {
	if r.Bin == "" {
		return errNoRuntime
	}
	cmd := exec.Command(r.Bin, "rmi", image)
	return runCmd(cmd)
}

// Info returns the output of `<bin> info` so /doctor can probe for
// nvidia-container-toolkit. Uses runCmdOutput so tests can stub.
func (r Runtime) Info() (string, error) {
	if r.Bin == "" {
		return "", errNoRuntime
	}
	cmd := exec.Command(r.Bin, "info")
	out, err := runCmdOutput(cmd)
	if err != nil {
		return string(out), err
	}
	return string(out), nil
}

// attachLog wires cmd's stdout+stderr to log, using io.Discard when log is nil.
func attachLog(cmd *exec.Cmd, log io.Writer) {
	if log == nil {
		log = io.Discard
	}
	cmd.Stdout = log
	cmd.Stderr = log
}
