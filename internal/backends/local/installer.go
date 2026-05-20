package local

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// CmdRunner runs a shell command in the given directory and returns its
// combined stdout+stderr. An empty dir means the process's current directory.
type CmdRunner func(ctx context.Context, dir, command string) (string, error)

// bashRunner is the production CmdRunner: it executes `bash -c <command>`.
func bashRunner(ctx context.Context, dir, command string) (string, error) {
	cmd := exec.CommandContext(ctx, "bash", "-c", command)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// lockFile is the .proteus.lock marker written after a successful install.
type lockFile struct {
	Version     string    `json:"version"`
	InstalledAt time.Time `json:"installed_at"`
}

// ToolStatus reports whether a tool is installed and, if so, its details.
type ToolStatus struct {
	Name        string
	Installed   bool
	Version     string
	InstalledAt time.Time
	InstallDir  string
}

// Installer installs, removes, and inspects local tools.
type Installer struct {
	registry *Registry
	run      CmdRunner
	ensureUV func(ctx context.Context) error
}

// NewInstaller builds an installer using the production command runner.
func NewInstaller(reg *Registry) *Installer {
	inst := &Installer{registry: reg, run: bashRunner}
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
	rec, ok := i.registry.Tool(name)
	if !ok {
		return fmt.Errorf("unknown tool %q", name)
	}
	if err := i.ensureUV(ctx); err != nil {
		return err
	}
	if err := os.MkdirAll(rec.InstallDir, 0o755); err != nil {
		return fmt.Errorf("create install dir: %w", err)
	}
	for idx, step := range rec.InstallSteps {
		out, err := i.run(ctx, rec.InstallDir, step)
		if err != nil {
			return fmt.Errorf("%s install step %d (%q) failed: %w\n%s",
				name, idx+1, step, err, out)
		}
	}
	lock := lockFile{Version: rec.Version, InstalledAt: time.Now().UTC()}
	body, _ := json.MarshalIndent(lock, "", "  ")
	if err := os.WriteFile(i.lockPath(rec), body, 0o644); err != nil {
		return fmt.Errorf("write lock file: %w", err)
	}
	return nil
}

// Remove deletes a tool's install directory.
func (i *Installer) Remove(ctx context.Context, name string) error {
	rec, ok := i.registry.Tool(name)
	if !ok {
		return fmt.Errorf("unknown tool %q", name)
	}
	return os.RemoveAll(rec.InstallDir)
}

// Status reports whether a tool is installed (by reading its lock file).
func (i *Installer) Status(name string) ToolStatus {
	st := ToolStatus{Name: name}
	rec, ok := i.registry.Tool(name)
	if !ok {
		return st
	}
	st.InstallDir = rec.InstallDir
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
	return filepath.Join(rec.InstallDir, ".proteus.lock")
}
