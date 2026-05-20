package local

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

//go:embed all:containerfiles
var containerfilesFS embed.FS

// loadContainerfile is the seam tests stub to avoid relying on real embedded
// Containerfiles (Phase 1 ships only the shared verify_gpu.py; per-tool
// Containerfiles arrive in Phase 2).
var loadContainerfile = func(name string) ([]byte, error) {
	return containerfilesFS.ReadFile(filepath.Join("containerfiles", name))
}

// stageContainerBaseTree copies the embedded `containerfiles/_base/` tree into
// dst so per-tool Containerfiles can `COPY _base/<file> …` from the build
// context. Tests can override via the seam below.
var stageContainerBaseTree = func(dst string) error {
	const root = "containerfiles/_base"
	entries, err := containerfilesFS.ReadDir(root)
	if err != nil {
		// _base/ absent (e.g. test-time stubbing): not fatal — per-tool
		// Containerfiles that don't reference _base/ continue to build.
		return nil
	}
	baseOut := filepath.Join(dst, "_base")
	if err := os.MkdirAll(baseOut, 0o755); err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue // _base/ is flat in v0.7; skip any future sub-trees.
		}
		body, err := containerfilesFS.ReadFile(filepath.Join(root, e.Name()))
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(baseOut, e.Name()), body, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// CmdRunner runs a shell command in the given directory and returns its
// combined stdout+stderr as a string. An empty dir means the process's
// current directory. The runner also tees the live stdout+stderr to log,
// so callers (e.g. the job log file) can observe progress while the
// process runs. A nil log is treated as io.Discard.
type CmdRunner func(ctx context.Context, dir, command string, log io.Writer) (string, error)

// bashRunner is the production CmdRunner: it executes `bash -c <command>`
// and tees the process's stdout+stderr to both an in-memory buffer (for
// the returned string) and log (for live job-log streaming).
//
// On context cancellation, bashRunner sends SIGTERM to the entire process
// group it created (not just the bash leader), then SIGKILLs the group if
// any process is still alive after a 5 s grace period. Without this, a
// cancelled `bash -c "python tool.py"` would leave the python child
// reparented to PID 1 and chewing GPU time after the user thought the
// job was gone. See Bug 7 in
// docs/superpowers/specs/2026-05-20-v0.6-design-path-resolution.md.
func bashRunner(ctx context.Context, dir, command string, log io.Writer) (string, error) {
	cmd := exec.CommandContext(ctx, "bash", "-c", command)
	if dir != "" {
		cmd.Dir = dir
	}
	// Put the child in a new process group so the whole tree can be
	// signalled at once on cancellation.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		// Negative PID => signal the entire process group.
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
	}
	// If the group hasn't exited within the grace period, exec.Cmd will
	// SIGKILL the bash leader and Wait will return. The kernel reaps the
	// rest of the group via the SIGTERM we sent above (or, for stubborn
	// children, SIGKILL on group-leader exit + setsid behaviour). 5 s
	// matches the kill heuristic used elsewhere in the codebase.
	cmd.WaitDelay = 5 * time.Second
	if log == nil {
		log = io.Discard
	}
	var buf bytes.Buffer
	w := io.MultiWriter(&buf, log)
	cmd.Stdout = w
	cmd.Stderr = w
	err := cmd.Run()
	return buf.String(), err
}

// lockFile is the .fova.lock marker written after a successful install.
type lockFile struct {
	Version     string    `json:"version"`
	InstalledAt time.Time `json:"installed_at"`
}

// ToolStatus reports whether a tool is installed and, if so, its details.
// For container-mode tools, Image is the image tag and Installed reflects
// whether that image exists in the local runtime.
type ToolStatus struct {
	Name        string
	Installed   bool
	Version     string
	InstalledAt time.Time
	InstallDir  string
	Image       string
}

// Installer installs, removes, and inspects local tools.
type Installer struct {
	registry      *Registry
	run           CmdRunner
	ensureUV      func(ctx context.Context) error
	runtime       Runtime
	ensureWeights func(ctx context.Context, home, toolName string, specs []WeightSpec) (string, error)
}

// NewInstaller builds an installer using the production command runner and
// a detected container runtime (may be RuntimeNone — only container-mode
// installs will fail; legacy tools still work).
func NewInstaller(reg *Registry) *Installer {
	inst := &Installer{
		registry:      reg,
		run:           bashRunner,
		runtime:       Detect(),
		ensureWeights: EnsureWeights,
	}
	inst.ensureUV = func(ctx context.Context) error { return ensureUV(ctx, inst.run) }
	return inst
}

// DryRun returns the shell commands Install would execute, without running them.
func (i *Installer) DryRun(name string) ([]string, error) {
	rec, ok := i.registry.Tool(name)
	if !ok {
		return nil, fmt.Errorf("unknown tool %q", name)
	}
	return append([]string(nil), rec.InstallSteps...), nil
}

// Install runs a tool's install_steps in order, then writes the lock marker.
// A failure leaves the partial install directory in place and names the step
// that failed. Re-install after wiping with Remove (or `--force`).
func (i *Installer) Install(ctx context.Context, name string) error {
	return i.InstallLogged(ctx, name, io.Discard)
}

// InstallLogged behaves like Install, additionally writing each install step's
// command line and combined output to log as the step completes. Branches on
// the recipe shape: Containerfile mode calls the container runtime, legacy
// mode runs the bash install_steps.
func (i *Installer) InstallLogged(ctx context.Context, name string, log io.Writer) error {
	rec, ok := i.registry.Tool(name)
	if !ok {
		return fmt.Errorf("unknown tool %q", name)
	}
	if rec.Containerfile != "" {
		return i.installContainer(ctx, rec, log)
	}
	return i.installLegacy(ctx, rec, log)
}

// installContainer pulls the NGC base (if missing), extracts the embedded
// Containerfile to a temp dir, and runs `<runtime> build`.
func (i *Installer) installContainer(ctx context.Context, rec ToolRecipe, log io.Writer) error {
	// Tag every error with `<tool>: <step>:` so the user-facing Job.Error
	// names the failing step. Each fallible step writes a breadcrumb to log
	// BEFORE attempting it, so the per-job log file shows the last
	// attempted action even if the wrapped error is later truncated.
	step := func(tag string) { fmt.Fprintf(log, "[install %s] %s\n", rec.Name, tag) }
	wrap := func(tag string, err error) error {
		return fmt.Errorf("%s: %s: %w", rec.Name, tag, err)
	}

	step("probe runtime")
	if !i.runtime.Available() {
		return fmt.Errorf("%s: probe runtime: no container runtime — install podman or docker", rec.Name)
	}

	step("probe base image " + BaseImage)
	exists, err := i.runtime.ImageExists(BaseImage)
	if err != nil {
		return wrap("probe base image", err)
	}
	if !exists {
		step("pull base image " + BaseImage)
		fmt.Fprintf(log, "$ %s pull %s\n", i.runtime.Bin, BaseImage)
		if err := i.runtime.Pull(ctx, BaseImage, log); err != nil {
			return wrap("pull base image", err)
		}
	}

	step("load Containerfile " + rec.Containerfile)
	body, err := loadContainerfile(rec.Containerfile)
	if err != nil {
		return wrap(fmt.Sprintf("load Containerfile %q", rec.Containerfile), err)
	}

	step("create build dir")
	buildDir, err := os.MkdirTemp("", "fova-build-*")
	if err != nil {
		return wrap("create build dir", err)
	}
	defer os.RemoveAll(buildDir)
	cfPath := filepath.Join(buildDir, filepath.Base(rec.Containerfile))

	step("write Containerfile to " + cfPath)
	if err := os.WriteFile(cfPath, body, 0o644); err != nil {
		return wrap("write Containerfile", err)
	}

	// Stage the shared _base/ tree into the build context so per-tool
	// Containerfiles can `COPY _base/verify_gpu.py …` (the smoke fragment).
	step("stage build context")
	if err := stageContainerBaseTree(buildDir); err != nil {
		return wrap("stage build context", err)
	}

	step("build image " + rec.ImageTag)
	fmt.Fprintf(log, "$ %s build -t %s -f %s %s\n", i.runtime.Bin, rec.ImageTag, cfPath, buildDir)
	if err := i.runtime.Build(ctx, rec.ImageTag, cfPath, buildDir, log); err != nil {
		return wrap("build image", err)
	}

	// Post-install weights hook: download any declared model weights into the
	// per-tool host cache so RunContainer can bind-mount them at /models.
	// Weights live outside the image so re-builds don't refetch them.
	if len(rec.Weights) > 0 {
		step(fmt.Sprintf("fetch %d weight file(s) into %s",
			len(rec.Weights), ModelsRoot(i.registry.Home(), rec.Name)))
		fmt.Fprintf(log, "fetching %d weight file(s) into %s\n",
			len(rec.Weights), ModelsRoot(i.registry.Home(), rec.Name))
		if _, err := i.ensureWeights(ctx, i.registry.Home(), rec.Name, rec.Weights); err != nil {
			return wrap("fetch weights", err)
		}
	}
	return nil
}

// installLegacy runs the bash install_steps in the per-tool install_dir and
// writes a lock file on success. Preserves the v0.6 behaviour byte-for-byte.
func (i *Installer) installLegacy(ctx context.Context, rec ToolRecipe, log io.Writer) error {
	if err := i.ensureUV(ctx); err != nil {
		return err
	}
	if err := os.MkdirAll(rec.InstallDir, 0o755); err != nil {
		return fmt.Errorf("create install dir: %w", err)
	}
	for idx, step := range rec.InstallSteps {
		fmt.Fprintf(log, "$ %s\n", step)
		out, err := i.run(ctx, rec.InstallDir, step, log)
		if !endsWithNewline(out) {
			fmt.Fprintln(log)
		}
		if err != nil {
			return fmt.Errorf("%s install step %d (%q) failed: %w\n%s",
				rec.Name, idx+1, step, err, out)
		}
	}
	lock := lockFile{Version: rec.Version, InstalledAt: time.Now().UTC()}
	body, _ := json.MarshalIndent(lock, "", "  ")
	if err := os.WriteFile(i.lockPath(rec), body, 0o644); err != nil {
		return fmt.Errorf("write lock file: %w", err)
	}
	return nil
}

// Remove deletes a tool's install directory (legacy mode only).
func (i *Installer) Remove(ctx context.Context, name string) error {
	rec, ok := i.registry.Tool(name)
	if !ok {
		return fmt.Errorf("unknown tool %q", name)
	}
	return os.RemoveAll(rec.InstallDir)
}

// Uninstall removes the on-disk artefacts for a tool. For container-mode tools
// the image is removed (best-effort) but the per-tool weights cache under
// ~/.fova/models/<tool>/ is preserved — weights are expensive to refetch.
// Legacy-mode tools fall back to Remove.
func (i *Installer) Uninstall(ctx context.Context, name string) error {
	rec, ok := i.registry.Tool(name)
	if !ok {
		return fmt.Errorf("unknown tool %q", name)
	}
	if rec.ImageTag == "" {
		return os.RemoveAll(rec.InstallDir)
	}
	if i.runtime.Available() {
		// Best-effort: log a warning but never propagate the failure — the
		// user may want to /install again even if rmi raced with another job.
		_ = i.runtime.RemoveImage(rec.ImageTag)
	}
	_ = os.Remove(i.lockPath(rec))
	return nil
}

// Status reports whether a tool is installed. For legacy tools the answer
// comes from the lock file; for container tools, the runtime is queried for
// the recipe's image tag.
func (i *Installer) Status(name string) ToolStatus {
	st := ToolStatus{Name: name}
	rec, ok := i.registry.Tool(name)
	if !ok {
		return st
	}
	st.InstallDir = rec.InstallDir
	if rec.ImageTag != "" {
		st.Image = rec.ImageTag
		if i.runtime.Available() {
			ok, _ := i.runtime.ImageExists(rec.ImageTag)
			st.Installed = ok
			if ok {
				st.Version = rec.Version
			}
		}
		return st
	}
	body, err := os.ReadFile(i.lockPath(rec))
	if err != nil {
		return st
	}
	var lock lockFile
	if err := json.Unmarshal(body, &lock); err != nil {
		return st
	}
	st.Installed = true
	st.Version = lock.Version
	st.InstalledAt = lock.InstalledAt
	return st
}

func (i *Installer) lockPath(rec ToolRecipe) string {
	return filepath.Join(rec.InstallDir, ".fova.lock")
}

// endsWithNewline reports whether s ends in '\n'.
func endsWithNewline(s string) bool {
	return len(s) > 0 && s[len(s)-1] == '\n'
}
