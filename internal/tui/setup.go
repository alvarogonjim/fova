// Setup slash commands: /doctor, /tools, /install, /uninstall, /modal deploy.
package tui

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/alvarogonjim/proteus/internal/backends/local"
	"github.com/alvarogonjim/proteus/internal/backends/modal"
	"github.com/alvarogonjim/proteus/internal/domain"
	jobmgr "github.com/alvarogonjim/proteus/internal/jobs"
)

// cmdDoctor renders the local-environment diagnostic inline in chat.
func (m *Model) cmdDoctor() (tea.Model, tea.Cmd) {
	if m.localReg == nil {
		m.chat.appendError("tool registry unavailable")
		return m, nil
	}
	rep := local.Diagnose(m.localReg, local.NewInstaller(m.localReg))
	m.chat.appendAgentDeltaBlock(rep.String())
	return m, nil
}

// cmdTools lists installable tools and their install status inline in chat.
func (m *Model) cmdTools() (tea.Model, tea.Cmd) {
	if m.localReg == nil {
		m.chat.appendError("tool registry unavailable")
		return m, nil
	}
	inst := local.NewInstaller(m.localReg)
	var b strings.Builder
	b.WriteString("Installable tools:\n")
	for _, rec := range m.localReg.Tools() {
		mark := "--"
		if inst.Status(rec.Name).Installed {
			mark = "ok"
		}
		gpu := ""
		if rec.RequiresGPU {
			gpu = " (GPU)"
		}
		fmt.Fprintf(&b, "  %s  %-14s %.1f GB%s\n", mark, rec.Name, rec.DiskGB, gpu)
	}
	m.chat.appendAgentDeltaBlock(strings.TrimRight(b.String(), "\n"))
	return m, nil
}

// setupAvailable reports whether the dependencies the install / uninstall /
// deploy commands need are wired. When they are not, it posts an error so the
// command degrades gracefully instead of panicking (Deps allows nil here).
func (m *Model) setupAvailable() bool {
	if m.localReg == nil || m.jobMgr == nil {
		m.chat.appendError("setup commands are unavailable in this session")
		return false
	}
	return true
}

// parseInstallArgs splits a /install argument line into a tool name and flags.
func parseInstallArgs(arg string) (tool string, all, force, dryRun bool) {
	for _, tok := range strings.Fields(arg) {
		switch tok {
		case "--all":
			all = true
		case "--force":
			force = true
		case "--dry-run":
			dryRun = true
		default:
			tool = tok
		}
	}
	return
}

// cmdInstall installs one tool, every tool (--all), or prints the steps
// (--dry-run). Real installs run as JobSetup jobs visible in the jobs panel.
func (m *Model) cmdInstall(arg string) (tea.Model, tea.Cmd) {
	if !m.setupAvailable() {
		return m, nil
	}
	tool, all, force, dryRun := parseInstallArgs(arg)
	if !all && tool == "" {
		m.chat.appendError("usage: /install <tool> [--force] [--dry-run]  |  /install --all")
		return m, nil
	}
	inst := local.NewInstaller(m.localReg)

	var targets []string
	if all {
		for _, rec := range m.localReg.Tools() {
			targets = append(targets, rec.Name)
		}
	} else {
		if _, ok := m.localReg.Tool(tool); !ok {
			m.chat.appendError("unknown tool: " + tool)
			return m, nil
		}
		targets = []string{tool}
	}

	if dryRun {
		var b strings.Builder
		for _, name := range targets {
			steps, err := inst.DryRun(name)
			if err != nil {
				m.chat.appendError(err.Error())
				return m, nil
			}
			fmt.Fprintf(&b, "install %s:\n", name)
			for i, s := range steps {
				fmt.Fprintf(&b, "  %d. %s\n", i+1, s)
			}
		}
		m.chat.appendAgentDeltaBlock(strings.TrimRight(b.String(), "\n"))
		return m, nil
	}

	for _, name := range targets {
		if err := m.submitInstall(name, force); err != nil {
			m.chat.appendError(err.Error())
		}
	}
	return m, nil
}

// submitInstall submits one install job and posts its start line to chat.
func (m *Model) submitInstall(name string, force bool) error {
	installFn, localReg := m.installFn, m.localReg
	id, err := m.jobMgr.Submit(jobmgr.Spec{
		Kind: domain.JobSetup,
		Tool: "install:" + name,
		Run: func(ctx context.Context, progress func(float64), log io.Writer) ([]byte, error) {
			if force {
				_ = local.NewInstaller(localReg).Remove(ctx, name)
			}
			if err := installFn(ctx, name, log); err != nil {
				return nil, err
			}
			return []byte("installed " + name), nil
		},
	})
	if err != nil {
		return err
	}
	m.chat.appendAgentDeltaBlock(fmt.Sprintf(
		"started install job %s for %s — watch the jobs panel", id, name))
	return nil
}

// cmdUninstall removes an installed tool as a JobSetup job.
func (m *Model) cmdUninstall(arg string) (tea.Model, tea.Cmd) {
	if !m.setupAvailable() {
		return m, nil
	}
	name := strings.TrimSpace(arg)
	if name == "" {
		m.chat.appendError("usage: /uninstall <tool>")
		return m, nil
	}
	if _, ok := m.localReg.Tool(name); !ok {
		m.chat.appendError("unknown tool: " + name)
		return m, nil
	}
	localReg := m.localReg
	id, err := m.jobMgr.Submit(jobmgr.Spec{
		Kind: domain.JobSetup,
		Tool: "uninstall:" + name,
		Run: func(ctx context.Context, progress func(float64), log io.Writer) ([]byte, error) {
			fmt.Fprintf(log, "removing %s\n", name)
			if err := local.NewInstaller(localReg).Remove(ctx, name); err != nil {
				return nil, err
			}
			fmt.Fprintf(log, "removed %s\n", name)
			return []byte("removed " + name), nil
		},
	})
	if err != nil {
		m.chat.appendError(err.Error())
		return m, nil
	}
	m.chat.appendAgentDeltaBlock(fmt.Sprintf("started uninstall job %s for %s", id, name))
	return m, nil
}

// cmdModalDeploy writes functions.py and deploys it via the Modal CLI as a
// JobSetup job. If the Modal CLI is absent it posts a hint and submits nothing.
func (m *Model) cmdModalDeploy(arg string) (tea.Model, tea.Cmd) {
	if !m.setupAvailable() {
		return m, nil
	}
	if strings.TrimSpace(arg) != "deploy" {
		m.chat.appendError("usage: /modal deploy")
		return m, nil
	}
	if _, err := exec.LookPath("modal"); err != nil {
		m.chat.appendAgentDeltaBlock("The Modal CLI is not installed. Run `pip install modal` " +
			"and `modal token new`, then retry /modal deploy.")
		return m, nil
	}
	home := m.proteusHome
	id, err := m.jobMgr.Submit(jobmgr.Spec{
		Kind: domain.JobSetup,
		Tool: "modal:deploy",
		Run: func(ctx context.Context, progress func(float64), log io.Writer) ([]byte, error) {
			dir := filepath.Join(home, "modal")
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return nil, err
			}
			path := filepath.Join(dir, "functions.py")
			if err := os.WriteFile(path, []byte(modal.FunctionsPy), 0o644); err != nil {
				return nil, err
			}
			fmt.Fprintf(log, "$ modal deploy %s\n", path)
			cmd := exec.CommandContext(ctx, "modal", "deploy", path)
			cmd.Stdout, cmd.Stderr = log, log
			if err := cmd.Run(); err != nil {
				return nil, fmt.Errorf("modal deploy failed: %w", err)
			}
			return []byte("deployed " + path), nil
		},
	})
	if err != nil {
		m.chat.appendError(err.Error())
		return m, nil
	}
	m.chat.appendAgentDeltaBlock(fmt.Sprintf(
		"started Modal deploy job %s — watch the jobs panel", id))
	return m, nil
}
