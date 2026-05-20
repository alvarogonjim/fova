package local

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
)

// Runner invokes an installed tool's run_command with runtime placeholders
// (e.g. {{ input_json }}, {{ args_file }}, {{ out_dir }}) filled in. For
// container-mode recipes (ImageTag != "") it runs the tool inside the runtime;
// for legacy recipes it falls through to the bash CmdRunner.
type Runner struct {
	registry *Registry
	run      CmdRunner
	runtime  Runtime
}

// NewRunner builds a runner using the production command runner and the
// auto-detected container runtime.
func NewRunner(reg *Registry) *Runner {
	return &Runner{registry: reg, run: bashRunner, runtime: Detect()}
}

// Run dispatches on the recipe shape: container-mode tools run inside the
// container runtime, legacy tools run via the bash CmdRunner.
func (r *Runner) Run(ctx context.Context, name string, placeholders map[string]string, log io.Writer) (string, error) {
	rec, ok := r.registry.Tool(name)
	if !ok {
		return "", fmt.Errorf("unknown tool %q", name)
	}
	if log == nil {
		log = io.Discard
	}
	if rec.ImageTag != "" {
		return r.runContainer(ctx, rec, placeholders, log)
	}
	return r.runLegacy(ctx, rec, placeholders, log)
}

// runLegacy fills the recipe's run_command with placeholders and executes it
// in the tool's install directory.
func (r *Runner) runLegacy(ctx context.Context, rec ToolRecipe, placeholders map[string]string, log io.Writer) (string, error) {
	command := expandPlaceholders(rec.RunCommand, placeholders)
	if i := strings.Index(command, "{{"); i >= 0 {
		return "", fmt.Errorf("%s run_command has an unfilled placeholder: %s",
			rec.Name, command[i:])
	}
	return r.run(ctx, rec.InstallDir, command, log)
}

// runContainer launches the recipe's entrypoint inside its image. The
// workspace placeholder mounts at /work; per-tool weights (if declared) mount
// at /models. Container name = placeholders["job_id"] or an auto-generated id.
// Cancellation fires `<runtime> kill <name>` so the container tree exits.
func (r *Runner) runContainer(ctx context.Context, rec ToolRecipe, placeholders map[string]string, log io.Writer) (string, error) {
	if !r.runtime.Available() {
		return "", fmt.Errorf("%s: no container runtime — install podman or docker", rec.Name)
	}
	entrypoint := expandPlaceholders(rec.Entrypoint, placeholders)
	if i := strings.Index(entrypoint, "{{"); i >= 0 {
		return "", fmt.Errorf("%s entrypoint has an unfilled placeholder: %s",
			rec.Name, entrypoint[i:])
	}
	cmd := splitArgs(entrypoint)
	jobID := placeholders["job_id"]
	if jobID == "" {
		jobID = generateContainerName(rec.Name)
	}
	mounts := []Mount{}
	if ws := placeholders["workspace"]; ws != "" {
		mounts = append(mounts, Mount{HostPath: ws, ContainerPath: "/work"})
	}
	if len(rec.WeightsPaths) > 0 {
		mounts = append(mounts, Mount{
			HostPath:      ModelsRoot(r.registry.Home(), rec.Name),
			ContainerPath: "/models",
		})
	}
	args := ContainerRunArgs{
		Name:    jobID,
		Image:   rec.ImageTag,
		Cmd:     cmd,
		Mounts:  mounts,
		GPU:     rec.GPU,
		Workdir: "/work",
		Log:     log,
	}

	// Cancellation: when ctx is done, kill the container by name so the whole
	// process tree exits promptly. The runtime exposes its own SIGKILL path,
	// which supersedes v0.6's process-group kill for container-mode tools.
	// We start the run in a goroutine so the cancellation watcher always
	// observes ctx.Done() (a deterministic order between RunContainer return
	// and ctx cancellation isn't otherwise guaranteed).
	type runResult struct {
		out string
		err error
	}
	resultCh := make(chan runResult, 1)
	go func() {
		o, e := r.runtime.RunContainer(ctx, args)
		resultCh <- runResult{out: o, err: e}
	}()
	select {
	case <-ctx.Done():
		_ = r.runtime.Kill(jobID)
		res := <-resultCh
		if res.err == nil {
			res.err = ctx.Err()
		}
		return res.out, res.err
	case res := <-resultCh:
		return res.out, res.err
	}
}

// generateContainerName returns a unique container name for a one-off tool
// invocation. Used when the caller didn't supply a job_id placeholder.
func generateContainerName(tool string) string {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return "fova_" + tool + "_" + hex.EncodeToString(b[:])
}

// splitArgs tokenises a shell-ish entrypoint into argv. It does NOT honour
// quotes; container entrypoints in tools.toml are simple `python /path/x.py
// @{{ args_file }}` lines, and the args_file file holds anything fancier.
func splitArgs(s string) []string {
	parts := strings.Fields(s)
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
