# TUI Setup Commands Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Move the `install`, `uninstall`, `list tools`, `doctor`, and `modal deploy` operations from CLI subcommands into TUI slash commands, and remove the CLI subcommands.

**Architecture:** The setup logic already lives, tested, in `internal/backends/local` and `internal/backends/modal`. New TUI-side handlers in `internal/tui/setup.go` are thin wrappers: `/doctor` and `/tools` render inline in chat; `/install`, `/uninstall`, and `/modal deploy` submit `JobSetup` jobs to the existing `jobs.Manager` and stream output to a log file. `tui.New` is converted to a `tui.Deps` struct so the TUI receives the job manager and local registry.

**Tech Stack:** Go, Bubble Tea (`charmbracelet/bubbletea`), Cobra (`spf13/cobra`).

**Spec:** `docs/superpowers/specs/2026-05-18-proteus-tui-setup-commands-design.md`

**Branch:** `feat/tui-setup-commands` (already created).

---

## File map

| File | Change | Task |
|---|---|---|
| `internal/domain/types.go` | add `JobSetup` constant | 1 |
| `internal/domain/types_test.go` | test the constant | 1 |
| `internal/backends/local/installer.go` | add `InstallLogged`, route `Install` through it | 2 |
| `internal/backends/local/installer_test.go` | test `InstallLogged` | 2 |
| `internal/tui/app.go` | add `Deps` struct, new `New`, Model fields | 3, 5 |
| `cmd/proteus/main.go` | thread manager + local registry into `tui.New` | 3 |
| `cmd/proteus/main_test.go` | update `buildRegistry` calls | 3 |
| `internal/tui/app_test.go` | update `New` callers | 3 |
| `internal/tui/setup.go` | **new** — slash-command handlers | 4, 5 |
| `internal/tui/setup_test.go` | **new** — handler tests | 4, 5 |
| `internal/tui/commandbar.go` | update slash-command hint line | 4 |
| `cmd/proteus/install.go`, `modal.go` | **delete** | 6 |
| `cmd/proteus/install_test.go`, `modal_test.go` | **delete** | 6 |

**Parallelism:** Task 1 and Task 2 are independent (different packages, no shared files) and may run concurrently. Tasks 3 → 4 → 5 → 6 share `internal/tui/app.go` / `cmd/proteus/main.go` and **must run sequentially in order**.

---

## Task 1: Add the `JobSetup` job kind

**Files:**
- Modify: `internal/domain/types.go` (the `JobKind` const block, ~line 40-43)
- Test: `internal/domain/types_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/domain/types_test.go`:

```go
func TestJobSetupKind(t *testing.T) {
	if JobSetup != "setup" {
		t.Fatalf("JobSetup = %q, want \"setup\"", JobSetup)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/domain/ -run TestJobSetupKind`
Expected: FAIL — `undefined: JobSetup`

- [ ] **Step 3: Add the constant**

In `internal/domain/types.go`, the current block is:

```go
const (
	JobCompute JobKind = "compute"
	JobLab     JobKind = "lab"
)
```

Change it to:

```go
const (
	JobCompute JobKind = "compute"
	JobLab     JobKind = "lab"
	JobSetup   JobKind = "setup" // install / uninstall / modal deploy
)
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/domain/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/domain/types.go internal/domain/types_test.go
git commit -m "$(printf 'feat: add the JobSetup job kind\n\nCo-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>')"
```

---

## Task 2: Add `Installer.InstallLogged`

`Installer.Install` captures each install step's output only inside an error. The TUI install job needs that output in a log file as steps complete. Add `InstallLogged(ctx, name, log)` and route `Install` through it with `io.Discard` (DRY — no duplicated step loop).

**Files:**
- Modify: `internal/backends/local/installer.go`
- Test: `internal/backends/local/installer_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/backends/local/installer_test.go`:

```go
func TestInstallerInstallLoggedWritesStepOutput(t *testing.T) {
	home := t.TempDir()
	reg, _ := LoadRegistry(home)
	inst := NewInstaller(reg)
	inst.ensureUV = func(ctx context.Context) error { return nil }
	inst.run = func(ctx context.Context, dir, command string) (string, error) {
		return "step-output-here", nil
	}

	var log bytes.Buffer
	if err := inst.InstallLogged(context.Background(), "ipsae", &log); err != nil {
		t.Fatalf("InstallLogged: %v", err)
	}
	if !strings.Contains(log.String(), "step-output-here") {
		t.Errorf("log missing step output, got: %q", log.String())
	}
	if !inst.Status("ipsae").Installed {
		t.Error("InstallLogged should mark the tool installed")
	}
}
```

Add `"bytes"` to the test file's import block (`"context"` and `"strings"` are already imported).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/backends/local/ -run TestInstallerInstallLogged`
Expected: FAIL — `inst.InstallLogged undefined`

- [ ] **Step 3: Implement `InstallLogged` and route `Install` through it**

In `internal/backends/local/installer.go`, add `"io"` to the import block. Replace the current `Install` method:

```go
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
```

with:

```go
// Install runs a tool's install_steps in order, then writes the lock marker.
// A failure leaves the partial install directory in place and names the step
// that failed. Re-install after wiping with Remove (or `--force`).
func (i *Installer) Install(ctx context.Context, name string) error {
	return i.InstallLogged(ctx, name, io.Discard)
}

// InstallLogged behaves like Install, additionally writing each install step's
// command line and combined output to log as the step completes.
func (i *Installer) InstallLogged(ctx context.Context, name string, log io.Writer) error {
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
		fmt.Fprintf(log, "$ %s\n", step)
		out, err := i.run(ctx, rec.InstallDir, step)
		fmt.Fprintf(log, "%s\n", out)
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/backends/local/`
Expected: PASS (the existing `TestInstallerInstall*` tests still pass — `Install` is unchanged in behaviour)

- [ ] **Step 5: Commit**

```bash
git add internal/backends/local/installer.go internal/backends/local/installer_test.go
git commit -m "$(printf 'feat: add Installer.InstallLogged for log-file capture\n\nCo-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>')"
```

---

## Task 3: Convert `tui.New` to a `tui.Deps` struct

The TUI needs the `jobs.Manager`, the `local.Registry`, and `$PROTEUS_HOME` to run setup commands. `tui.New` currently takes four positional args and never sees the manager or backend. Replace it with a `Deps` struct and thread the dependencies through from `cmd/proteus/main.go`.

**Files:**
- Modify: `internal/tui/app.go`
- Modify: `cmd/proteus/main.go`
- Modify: `cmd/proteus/main_test.go`
- Modify: `internal/tui/app_test.go`

- [ ] **Step 1: Update the test callers (these are the failing tests)**

In `internal/tui/app_test.go`, replace the `newTestApp` helper (currently around line 18):

```go
func newTestApp() *Model {
	return New(tools.NewRegistry(), llm.NewModelRegistry(), agent.SystemPrompt, nil)
}
```

with:

```go
func newTestApp() *Model {
	return New(Deps{
		Registry:     tools.NewRegistry(),
		Models:       llm.NewModelRegistry(),
		SystemPrompt: agent.SystemPrompt,
	})
}
```

In the same file there are two more `New(...)` calls (in `TestAppPersistsSessionAndMessages` around line 29, and around line 127). Each currently reads:

```go
m := New(tools.NewRegistry(), llm.NewModelRegistry(), agent.SystemPrompt, st)
```

Replace **both** with:

```go
m := New(Deps{
	Registry:     tools.NewRegistry(),
	Models:       llm.NewModelRegistry(),
	SystemPrompt: agent.SystemPrompt,
	Store:        st,
})
```

In `cmd/proteus/main_test.go`, add this import to the import block:

```go
jobmgr "github.com/alvarogonjim/proteus/internal/jobs"
```

Then replace the two `buildRegistry(t.TempDir(), st)` calls (in `TestRunTUIWiresJobTools` and `TestRunTUIWiresDesignAndScoreTools`) with:

```go
reg := buildRegistry(t.TempDir(), st, jobmgr.NewManager(st, nil))
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go build ./...`
Expected: FAIL — `too many arguments in call to New` / `too many arguments in call to buildRegistry` / `undefined: Deps`

- [ ] **Step 3: Add the `Deps` struct and new `New` in `app.go`**

In `internal/tui/app.go`, add two imports to the import block:

```go
"github.com/alvarogonjim/proteus/internal/backends/local"
jobmgr "github.com/alvarogonjim/proteus/internal/jobs"
```

In the `Model` struct, add three fields after the existing `store`/`sessionID` fields:

```go
	jobMgr      *jobmgr.Manager // async job manager (install / deploy / design jobs)
	localReg    *local.Registry // installable-tool registry
	proteusHome string          // $PROTEUS_HOME, used for setup log-file paths
```

Replace the entire `New` function:

```go
// New builds the root model. st may be nil (persistence disabled).
func New(reg *tools.Registry, models *llm.ModelRegistry, systemPrompt string, st *store.Store) *Model {
	th := NewTheme()
	m := &Model{
		theme:        th,
		chat:         newChatModel(th, 80, 20),
		status:       newStatusBarModel(th),
		cmdbar:       newCommandBarModel(th, 80),
		registry:     reg,
		models:       models,
		systemPrompt: systemPrompt,
		session:      agent.NewSession(systemPrompt),
		store:        st,
		bus:          make(chan tea.Msg, 256),
		confirmCh:    make(chan bool, 1),
	}
	m.jobs = newJobsModel(th)
	m.designs = newDesignsModel(th)
	m.status.model = models.ActiveModel()
	m.status.provider = models.ActiveProviderName()
	m.beginPersistedSession()
	return m
}
```

with:

```go
// Deps are the dependencies the root model needs. Store, Jobs, and Local may
// be nil to disable persistence / job submission / setup commands respectively.
type Deps struct {
	Registry     *tools.Registry
	Models       *llm.ModelRegistry
	SystemPrompt string
	Store        *store.Store
	Jobs         *jobmgr.Manager
	Local        *local.Registry
	ProteusHome  string
}

// New builds the root model from its dependencies.
func New(d Deps) *Model {
	th := NewTheme()
	m := &Model{
		theme:        th,
		chat:         newChatModel(th, 80, 20),
		status:       newStatusBarModel(th),
		cmdbar:       newCommandBarModel(th, 80),
		registry:     d.Registry,
		models:       d.Models,
		systemPrompt: d.SystemPrompt,
		session:      agent.NewSession(d.SystemPrompt),
		store:        d.Store,
		jobMgr:       d.Jobs,
		localReg:     d.Local,
		proteusHome:  d.ProteusHome,
		bus:          make(chan tea.Msg, 256),
		confirmCh:    make(chan bool, 1),
	}
	m.jobs = newJobsModel(th)
	m.designs = newDesignsModel(th)
	m.status.model = d.Models.ActiveModel()
	m.status.provider = d.Models.ActiveProviderName()
	m.beginPersistedSession()
	return m
}
```

- [ ] **Step 4: Update `cmd/proteus/main.go`**

Add this import to the import block:

```go
"github.com/alvarogonjim/proteus/internal/backends/local"
```

Replace the `runTUI` function:

```go
// runTUI builds the registry, model registry, store, and starts the app.
func runTUI() error {
	workspace, err := defaultWorkspace()
	if err != nil {
		return err
	}

	st, err := store.Open(filepath.Join(workspace, "workspace.db"))
	if err != nil {
		return err
	}
	defer st.Close()
	if err := st.MarkRunningJobsInterrupted(); err != nil {
		return err
	}

	mgr := jobmgr.NewManager(st, nil)
	registry := buildRegistry(workspace, st, mgr)

	localReg, err := local.LoadRegistry(proteusHome())
	if err != nil {
		return err
	}

	models := llm.NewModelRegistry()
	app := tui.New(tui.Deps{
		Registry:     registry,
		Models:       models,
		SystemPrompt: agent.SystemPrompt,
		Store:        st,
		Jobs:         mgr,
		Local:        localReg,
		ProteusHome:  proteusHome(),
	})

	p := tea.NewProgram(app, tea.WithAltScreen())
	_, err = p.Run()
	return err
}
```

In `buildRegistry`, change the signature and remove the internal `mgr` creation. Replace the function header line:

```go
func buildRegistry(workspace string, st *store.Store) *tools.Registry {
```

with:

```go
func buildRegistry(workspace string, st *store.Store, mgr *jobmgr.Manager) *tools.Registry {
```

and delete the line inside the function that reads `mgr := jobmgr.NewManager(st, nil)` (the `mgr` parameter now supplies it; the rest of the function is unchanged).

- [ ] **Step 5: Run the full build and test suite**

Run: `go build ./... && go test ./...`
Expected: PASS — all packages

- [ ] **Step 6: Commit**

```bash
git add internal/tui/app.go internal/tui/app_test.go cmd/proteus/main.go cmd/proteus/main_test.go
git commit -m "$(printf 'refactor: pass TUI dependencies through a tui.Deps struct\n\nCo-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>')"
```

---

## Task 4: Add `/doctor` and `/tools` slash commands

These are read-only and instant — they render a report block inline in chat, no job.

**Files:**
- Create: `internal/tui/setup.go`
- Create: `internal/tui/setup_test.go`
- Modify: `internal/tui/app.go` (`runSlashCommand`)
- Modify: `internal/tui/commandbar.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/tui/setup_test.go`:

```go
package tui

import (
	"path/filepath"
	"testing"

	"github.com/alvarogonjim/proteus/internal/agent"
	"github.com/alvarogonjim/proteus/internal/backends/local"
	jobmgr "github.com/alvarogonjim/proteus/internal/jobs"
	"github.com/alvarogonjim/proteus/internal/llm"
	"github.com/alvarogonjim/proteus/internal/store"
	"github.com/alvarogonjim/proteus/internal/tools"
)

// newSetupTestModel builds a Model wired with a local registry and a job
// manager backed by a temp store — enough to exercise the setup commands.
func newSetupTestModel(t *testing.T) *Model {
	t.Helper()
	home := t.TempDir()
	reg, err := local.LoadRegistry(home)
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	st, err := store.Open(filepath.Join(t.TempDir(), "workspace.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return New(Deps{
		Registry:     tools.NewRegistry(),
		Models:       llm.NewModelRegistry(),
		SystemPrompt: agent.SystemPrompt,
		Store:        st,
		Jobs:         jobmgr.NewManager(st, nil),
		Local:        reg,
		ProteusHome:  home,
	})
}

func TestCmdDoctorPostsReport(t *testing.T) {
	m := newSetupTestModel(t)
	before := len(m.chat.entries)
	m.cmdDoctor()
	if len(m.chat.entries) <= before {
		t.Fatal("cmdDoctor posted nothing to chat")
	}
	if m.chat.entries[len(m.chat.entries)-1].kind != entryAgent {
		t.Error("doctor report should be an agent entry")
	}
}

func TestCmdToolsListsTools(t *testing.T) {
	m := newSetupTestModel(t)
	m.cmdTools()
	last := m.chat.entries[len(m.chat.entries)-1]
	if last.kind != entryAgent || last.text == "" {
		t.Fatalf("cmdTools should post a non-empty agent entry, got %+v", last)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/ -run 'TestCmdDoctor|TestCmdTools'`
Expected: FAIL — `m.cmdDoctor undefined`, `m.cmdTools undefined`

- [ ] **Step 3: Create `internal/tui/setup.go`**

```go
// Setup slash commands: /doctor, /tools, /install, /uninstall, /modal deploy.
package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/alvarogonjim/proteus/internal/backends/local"
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
```

- [ ] **Step 4: Route the commands in `runSlashCommand`**

In `internal/tui/app.go`, in the `runSlashCommand` switch, add two cases immediately before the existing `case "jobs", "designs", ...` stub case:

```go
	case "doctor":
		return m.cmdDoctor()
	case "tools":
		return m.cmdTools()
```

- [ ] **Step 5: Update the command-bar hint line**

In `internal/tui/commandbar.go`, replace:

```go
const slashCommandHints = " /model  /provider  /clear  /help  /quit "
```

with:

```go
const slashCommandHints = " /install  /tools  /doctor  /modal  /model  /provider  /clear  /help  /quit "
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/tui/`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/tui/setup.go internal/tui/setup_test.go internal/tui/app.go internal/tui/commandbar.go
git commit -m "$(printf 'feat: add /doctor and /tools TUI slash commands\n\nCo-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>')"
```

---

## Task 5: Add `/install`, `/uninstall`, and `/modal deploy` slash commands

These are long-running: they submit `JobSetup` jobs to the job manager and stream output to a log file under `$PROTEUS_HOME/logs/`. The install runner is held in a `Model.installFn` field so tests can stub it (the real installer runs `uv` and downloads gigabytes).

**Files:**
- Modify: `internal/tui/app.go` (Model field + `New` default + `runSlashCommand`)
- Modify: `internal/tui/setup.go` (append handlers)
- Modify: `internal/tui/setup_test.go` (append tests)

- [ ] **Step 1: Write the failing tests**

Append to `internal/tui/setup_test.go` the following. Add these imports to its import block: `"context"`, `"io"`, `"os/exec"`, `"time"`, and `"github.com/alvarogonjim/proteus/internal/domain"`.

```go
// waitJobDone blocks until a job reaches a terminal state, so the job's
// background goroutine cannot write to the store after the test closes it.
func waitJobDone(t *testing.T, mgr *jobmgr.Manager, id domain.JobID) {
	t.Helper()
	for i := 0; i < 200; i++ {
		j, err := mgr.Status(id)
		if err != nil {
			t.Fatalf("Status: %v", err)
		}
		switch j.Status {
		case domain.JobSucceeded, domain.JobFailed, domain.JobCancelled:
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("job did not finish in time")
}

func TestCmdInstallSubmitsJob(t *testing.T) {
	m := newSetupTestModel(t)
	m.installFn = func(ctx context.Context, name string, log io.Writer) error {
		io.WriteString(log, "stub install\n")
		return nil
	}
	m.cmdInstall("ipsae")
	jobs, err := m.jobMgr.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 {
		t.Fatalf("expected one job, got %d", len(jobs))
	}
	if jobs[0].Kind != domain.JobSetup || jobs[0].Tool != "install:ipsae" {
		t.Errorf("job = kind %q tool %q, want setup / install:ipsae", jobs[0].Kind, jobs[0].Tool)
	}
	waitJobDone(t, m.jobMgr, jobs[0].ID)
}

func TestCmdInstallUnknownToolSubmitsNothing(t *testing.T) {
	m := newSetupTestModel(t)
	m.cmdInstall("nonesuch")
	jobs, _ := m.jobMgr.List()
	if len(jobs) != 0 {
		t.Fatalf("unknown tool must not submit a job, got %d", len(jobs))
	}
	if m.chat.entries[len(m.chat.entries)-1].kind != entryError {
		t.Error("unknown tool should post an error entry")
	}
}

func TestCmdInstallDryRunSubmitsNothing(t *testing.T) {
	m := newSetupTestModel(t)
	m.cmdInstall("--dry-run ipsae")
	jobs, _ := m.jobMgr.List()
	if len(jobs) != 0 {
		t.Fatalf("--dry-run must not submit a job, got %d", len(jobs))
	}
	if m.chat.entries[len(m.chat.entries)-1].kind != entryAgent {
		t.Error("--dry-run should post an agent block with the steps")
	}
}

func TestCmdModalDeployWithoutCLIPostsHint(t *testing.T) {
	// This test covers the absent-CLI path. Skip it when `modal` is installed,
	// so it never triggers a real deploy on a developer's machine.
	if _, err := exec.LookPath("modal"); err == nil {
		t.Skip("modal CLI is installed; skipping the absent-CLI path test")
	}
	m := newSetupTestModel(t)
	m.cmdModalDeploy("deploy")
	jobs, _ := m.jobMgr.List()
	if len(jobs) != 0 {
		t.Fatalf("modal deploy without the CLI must submit no job, got %d", len(jobs))
	}
	if m.chat.entries[len(m.chat.entries)-1].text == "" {
		t.Error("modal deploy should post a hint")
	}
}
```

Add `"github.com/alvarogonjim/proteus/internal/domain"` to the `setup_test.go` import block.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/ -run 'TestCmdInstall|TestCmdModalDeploy'`
Expected: FAIL — `m.installFn undefined`, `m.cmdInstall undefined`, `m.cmdModalDeploy undefined`

- [ ] **Step 3: Add the `installFn` field and its default**

In `internal/tui/app.go`, add `"context"` and `"io"` to the import block if not already present (`"context"` is already imported; add `"io"`).

In the `Model` struct, add one field after `proteusHome`:

```go
	// installFn runs a tool install, writing progress to log. Defaults to the
	// real local installer; tests override it.
	installFn func(ctx context.Context, name string, log io.Writer) error
```

In `New`, immediately before `m.beginPersistedSession()`, add:

```go
	if d.Local != nil {
		m.installFn = local.NewInstaller(d.Local).InstallLogged
	}
```

- [ ] **Step 4: Append the handlers to `internal/tui/setup.go`**

Add these imports to `setup.go`'s import block: `"context"`, `"os"`, `"os/exec"`, `"path/filepath"`, `"time"`, `"github.com/alvarogonjim/proteus/internal/backends/modal"`, `"github.com/alvarogonjim/proteus/internal/domain"`, `jobmgr "github.com/alvarogonjim/proteus/internal/jobs"`.

Append to `setup.go`:

```go
// setupLogPath returns a fresh timestamped log-file path under
// $PROTEUS_HOME/logs, creating the directory.
func (m *Model) setupLogPath(label string) (string, error) {
	dir := filepath.Join(m.proteusHome, "logs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(dir, fmt.Sprintf("%s-%d.log", label, time.Now().Unix())), nil
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
	logPath, err := m.setupLogPath("install-" + name)
	if err != nil {
		return err
	}
	installFn, localReg := m.installFn, m.localReg
	id, err := m.jobMgr.Submit(jobmgr.Spec{
		Kind: domain.JobSetup,
		Tool: "install:" + name,
		Run: func(ctx context.Context, progress func(float64)) ([]byte, error) {
			f, err := os.Create(logPath)
			if err != nil {
				return nil, err
			}
			defer f.Close()
			if force {
				_ = local.NewInstaller(localReg).Remove(ctx, name)
			}
			if err := installFn(ctx, name, f); err != nil {
				return nil, err
			}
			return []byte("installed " + name), nil
		},
	})
	if err != nil {
		return err
	}
	m.chat.appendAgentDeltaBlock(fmt.Sprintf(
		"started install job %s for %s — watch the jobs panel · log: %s", id, name, logPath))
	return nil
}

// cmdUninstall removes an installed tool as a JobSetup job.
func (m *Model) cmdUninstall(arg string) (tea.Model, tea.Cmd) {
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
		Run: func(ctx context.Context, progress func(float64)) ([]byte, error) {
			if err := local.NewInstaller(localReg).Remove(ctx, name); err != nil {
				return nil, err
			}
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
	if strings.TrimSpace(arg) != "deploy" {
		m.chat.appendError("usage: /modal deploy")
		return m, nil
	}
	if _, err := exec.LookPath("modal"); err != nil {
		m.chat.appendAgentDeltaBlock("The Modal CLI is not installed. Run `pip install modal` " +
			"and `modal token new`, then retry /modal deploy.")
		return m, nil
	}
	logPath, err := m.setupLogPath("modal-deploy")
	if err != nil {
		m.chat.appendError(err.Error())
		return m, nil
	}
	home := m.proteusHome
	id, err := m.jobMgr.Submit(jobmgr.Spec{
		Kind: domain.JobSetup,
		Tool: "modal:deploy",
		Run: func(ctx context.Context, progress func(float64)) ([]byte, error) {
			dir := filepath.Join(home, "modal")
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return nil, err
			}
			path := filepath.Join(dir, "functions.py")
			if err := os.WriteFile(path, []byte(modal.FunctionsPy), 0o644); err != nil {
				return nil, err
			}
			f, err := os.Create(logPath)
			if err != nil {
				return nil, err
			}
			defer f.Close()
			cmd := exec.CommandContext(ctx, "modal", "deploy", path)
			cmd.Stdout, cmd.Stderr = f, f
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
		"started Modal deploy job %s — watch the jobs panel · log: %s", id, logPath))
	return m, nil
}
```

- [ ] **Step 5: Route the commands in `runSlashCommand`**

In `internal/tui/app.go`, in the `runSlashCommand` switch, add three cases next to the `doctor`/`tools` cases added in Task 4:

```go
	case "install":
		return m.cmdInstall(arg)
	case "uninstall":
		return m.cmdUninstall(arg)
	case "modal":
		return m.cmdModalDeploy(arg)
```

- [ ] **Step 6: Run the build and test suite**

Run: `go build ./... && go test ./internal/tui/`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/tui/app.go internal/tui/setup.go internal/tui/setup_test.go
git commit -m "$(printf 'feat: add /install, /uninstall, /modal deploy TUI commands\n\nCo-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>')"
```

---

## Task 6: Remove the CLI setup subcommands

The five operations now exist inside the TUI. Delete the CLI subcommands and their tests; keep `version` and `tui`.

**Files:**
- Delete: `cmd/proteus/install.go`, `cmd/proteus/modal.go`, `cmd/proteus/install_test.go`, `cmd/proteus/modal_test.go`
- Modify: `cmd/proteus/main.go` (`newRootCmd`)
- Modify: `cmd/proteus/main_test.go`

- [ ] **Step 1: Update the subcommand test**

In `cmd/proteus/main_test.go`, replace `TestRootCommandHasSubcommands`:

```go
func TestRootCommandHasSubcommands(t *testing.T) {
	root := newRootCmd()
	var names []string
	for _, c := range root.Commands() {
		names = append(names, c.Name())
	}
	have := map[string]bool{}
	for _, n := range names {
		have[n] = true
	}
	if !have["version"] || !have["tui"] {
		t.Fatalf("missing version/tui subcommand; got %v", names)
	}
	for _, gone := range []string{"install", "uninstall", "list", "doctor", "modal"} {
		if have[gone] {
			t.Errorf("subcommand %q should have been removed; got %v", gone, names)
		}
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./cmd/proteus/ -run TestRootCommandHasSubcommands`
Expected: FAIL — `install`/`uninstall`/`list`/`doctor`/`modal` still registered

- [ ] **Step 3: Delete the CLI command files**

```bash
git rm cmd/proteus/install.go cmd/proteus/modal.go cmd/proteus/install_test.go cmd/proteus/modal_test.go
```

- [ ] **Step 4: Remove the registrations from `newRootCmd`**

In `cmd/proteus/main.go`, delete these five lines from `newRootCmd`:

```go
	root.AddCommand(newInstallCmd())
	root.AddCommand(newUninstallCmd())
	root.AddCommand(newListCmd())
	root.AddCommand(newDoctorCmd())
	root.AddCommand(newModalCmd())
```

(The `version` and `tui` `AddCommand` calls stay.)

- [ ] **Step 5: Run the full build and test suite**

Run: `go build ./... && go test ./... && go vet ./...`
Expected: PASS — all packages, vet clean

- [ ] **Step 6: Commit**

```bash
git add cmd/proteus/
git commit -m "$(printf 'refactor: remove the CLI setup subcommands (now in the TUI)\n\nCo-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>')"
```

---

## Final verification

- [ ] Run `go build -ldflags='-s -w' -o bin/proteus ./cmd/proteus` — builds clean.
- [ ] Run `./bin/proteus install foo` — exits with an unknown-command error (criterion 1).
- [ ] Run `./bin/proteus version` — prints the version (criterion 2).
- [ ] Run `./bin/proteus`, type `/doctor` — a report block appears in chat (criterion 3).
- [ ] In the TUI, type `/install ipsae --dry-run` — the install steps print inline, no job (criterion 5).
- [ ] In the TUI, type `/install ipsae` — a job appears in the jobs panel; a log file is written under `$PROTEUS_HOME/logs/` (criterion 4).
- [ ] Run `go test ./... && go vet ./...` — green (criterion 7).
- [ ] Update `README.md` if it references the removed CLI subcommands.
