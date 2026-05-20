# Proteus — Setup commands move into the TUI — Design

**Date:** 2026-05-18
**Status:** Approved, ready for planning
**Predecessor:** v0.2 "Real designs" — complete (tag `v0.2.0`).
**Source of truth:** `docs/SPECS.md`; the v0.2 design (`2026-05-16-proteus-v0.2-design.md`).

## 1. Goal

The environment-setup operations — installing local tools, removing them,
listing them, diagnosing the environment, and deploying the Modal backend — are
currently CLI subcommands under `cmd/proteus/` and run *outside* the TUI. Move
them *inside* the TUI as slash commands. After this change the TUI is the single
entry point for setup; the corresponding CLI subcommands are removed.

## 2. Scope

**In scope — five operations move from CLI subcommand to TUI slash command:**

| Removed CLI subcommand | New TUI slash command |
|---|---|
| `proteus install <tool>` | `/install <tool>` |
| `proteus uninstall <tool>` | `/uninstall <tool>` |
| `proteus list tools` | `/tools` |
| `proteus doctor` | `/doctor` |
| `proteus modal deploy` | `/modal deploy` |

**Retained CLI surface:** bare `proteus` (launches the TUI), `proteus tui`, and
`proteus version`. `version` stays a subcommand — scripts and bug reports rely
on a no-TUI way to read the build version.

**Out of scope:** no change to the underlying install/doctor/Modal logic in
`internal/backends/local` and `internal/backends/modal`; no new agent tools (the
agent does not gain the ability to install tools — setup stays user-driven);
no change to the existing `/model`, `/provider`, `/clear`, `/help`, `/quit`
commands.

## 3. Key decisions

1. **TUI-only — CLI subcommands removed, not kept alongside.** `cmd/proteus/`
   loses `install.go`, `modal.go`, and their tests. Bootstrapping is unaffected:
   `runTUI` requires no installed tools, so a fresh machine launches `proteus`,
   lands in the TUI, and runs `/install` from there.
2. **Long-running commands (`/install`, `/modal deploy`) run as jobs.** They
   submit a `jobs.Spec` to the existing `jobs.Manager`, appear in the SP6 jobs
   panel with live status, and stream full subprocess output to a log file.
   The chat shows only a one-line start and a one-line finish/failure.
3. **Fast commands (`/doctor`, `/tools`) render inline in chat**, as a formatted
   block via `appendAgentDeltaBlock`, exactly like `/help`. They are read-only
   and instant — no job, no panel, no overlay.
4. **The setup logic is not duplicated.** It already lives, tested, in
   `internal/backends/local` and `internal/backends/modal`. The TUI-side
   handlers are thin wrappers; they go in a new `internal/tui/setup.go` so
   `app.go` does not bloat.
5. **A new `JobSetup` job kind** is added so the jobs panel can label and glyph
   install/deploy jobs distinctly from `JobCompute` design jobs.

## 4. Components

### 4.1 `internal/domain/types.go` — new job kind

Add one constant alongside `JobCompute`/`JobLab`:

```go
JobSetup JobKind = "setup"   // install / uninstall / modal deploy
```

### 4.2 `internal/backends/local` — logged install

`Installer.Install` currently captures each step's combined output in memory and
surfaces it only inside an error. The TUI install job needs that output in a log
file. Add one method, leaving `Install` untouched:

```go
// InstallLogged runs an install, writing each step's command line and combined
// output to log as the step completes. Same semantics as Install otherwise.
func (i *Installer) InstallLogged(ctx context.Context, name string, log io.Writer) error
```

It reuses the existing per-step loop and the stubbable `CmdRunner`, so it stays
unit-testable without a real subprocess.

### 4.3 `internal/tui/setup.go` — new file, TUI-side handlers

Holds the slash-command handlers and the job-spec builders. Each handler is a
method on `*Model` and returns the same `(tea.Model, tea.Cmd)` shape
`runSlashCommand` already uses.

- `cmdDoctor()` — calls `local.Diagnose`, posts the report via
  `appendAgentDeltaBlock`.
- `cmdTools()` — calls `Registry.Tools()` + `Installer.Status`, posts a status
  table inline.
- `cmdInstall(arg)` / `cmdUninstall(arg)` — resolve the tool name; build a
  `jobs.Spec{Kind: JobSetup, ...}` whose `Run` closure opens a log file and
  calls `InstallLogged` / `Remove`; `mgr.Submit` it; post a one-line start.
- `cmdModalDeploy()` — build a `jobs.Spec{Kind: JobSetup, ...}` whose `Run`
  closure writes `functions.py` and shells out to `modal deploy`, streaming to a
  log file; post a one-line start. If the `modal` CLI is absent the handler
  posts the existing pip/token hint inline and submits nothing.
- Argument parsing: handlers recognize trailing `--all`, `--force`, `--dry-run`
  tokens in `arg` (the slash bar has no flag parser). `/install --dry-run <tool>`
  posts the step list inline instead of submitting a job.

Log files live at `$PROTEUS_HOME/logs/<op>-<tool>-<timestamp>.log`.

### 4.4 `internal/tui/app.go` — wiring and dispatch

- `runSlashCommand` gains cases `install`, `uninstall`, `tools`, `doctor`,
  `modal`, delegating to the `setup.go` handlers. These names are removed from
  the "arrives in a later milestone" stub list.
- The TUI needs the `jobs.Manager`, the `local.Registry`/`Installer`, and
  `proteusHome` to run setup commands. `tui.New` currently takes four positional
  args and never sees the manager or backend. Replace the signature with a
  `tui.Deps` struct:

  ```go
  type Deps struct {
      Registry     *tools.Registry
      Models       *llm.ModelRegistry
      SystemPrompt string
      Store        *store.Store
      Jobs         *jobs.Manager
      Local        *local.Registry
      ProteusHome  string
  }
  func New(d Deps) *Model
  ```

### 4.5 `internal/tui/commandbar.go` — hint line

`slashCommandHints` gains the new commands, e.g.:
` /install  /tools  /doctor  /modal  /model  /clear  /help  /quit `

### 4.6 `cmd/proteus/main.go` — root command and wiring

- Delete `install.go`, `modal.go`, `install_test.go`, `modal_test.go`.
- `newRootCmd` drops `newInstallCmd`, `newUninstallCmd`, `newListCmd`,
  `newDoctorCmd`, `newModalCmd`; keeps `tui` and `version`.
- `buildRegistry` already constructs the `jobs.Manager` and selects a backend.
  `runTUI` now also builds the `local.Registry` and passes the manager, local
  registry, and `proteusHome` into `tui.New` via `tui.Deps`.

## 5. Data flow — `/install bindcraft`

1. `parseSlashCommand("/install bindcraft")` → `runSlashCommand("install", "bindcraft")`.
2. `cmdInstall("bindcraft")` resolves the name against `local.Registry`. Unknown
   name → inline chat error, nothing submitted.
3. The handler builds `jobs.Spec{Kind: JobSetup, Tool: "install:bindcraft",
   Run: closure}`. The closure opens `$PROTEUS_HOME/logs/install-bindcraft-<ts>.log`
   and calls `Installer.InstallLogged(ctx, "bindcraft", logFile)`.
4. `mgr.Submit(spec)` returns a `JobID`. The job appears in the jobs panel via
   the manager's existing `onUpdate` bus hook.
5. Chat posts one line: `started install job j_… — watch the jobs panel · log: <path>`.
6. On completion the manager's bus update refreshes the panel; the TUI posts a
   one-line success, or a one-line failure that quotes the log path.

`/doctor` and `/tools` run synchronously inside `runSlashCommand` (instant) and
post their report block immediately — no job, no `tea.Cmd`.

## 6. Error handling

- Unknown tool name (`/install`, `/uninstall`) → inline chat error; no job.
- Install/deploy failure → job ends `failed`; chat posts a failure line quoting
  the log-file path. The full subprocess output is in the log, not the chat.
- `/modal deploy` with the `modal` CLI absent → inline hint (`pip install modal`,
  `modal token new`); no job submitted.
- `/install` with no argument and no `--all` → inline usage error.

## 7. Testing

All tests stay offline and deterministic, consistent with the v0.1/v0.2 pattern.

**Automated:**
- `internal/backends/local`: `InstallLogged` writes each step's output to the
  log writer — tested with the existing stubbed `CmdRunner`.
- `internal/tui/setup_test.go` (new): `cmdDoctor`/`cmdTools` post a non-empty
  report block; `cmdInstall` with a known tool submits exactly one `JobSetup`
  job and posts a start line; `cmdInstall` with an unknown tool submits nothing
  and posts an error; `--dry-run` posts the step list without submitting.
- `internal/tui/app_test.go`: extend so `runSlashCommand` routes the five new
  commands and no longer treats them as "later milestone" stubs.
- `cmd/proteus/main_test.go`: update `TestRootCommandHasSubcommands` —
  `install`/`doctor`/`modal` are gone; `tui` and `version` remain.

**Removed:** `cmd/proteus/install_test.go`, `cmd/proteus/modal_test.go`.

**Manual (unchanged from v0.2):** a real `/install bindcraft` end-to-end and a
real `/modal deploy` still need a GPU box / Modal account. Moving the trigger
from a CLI subcommand to a slash command does not change that.

## 8. Acceptance criteria

1. The `install`, `uninstall`, `list`, `doctor`, and `modal` CLI subcommands no
   longer exist; `proteus install …` exits with an unknown-command error.
2. `proteus version` and bare `proteus` still work.
3. Inside the TUI, `/doctor` and `/tools` post a report block in chat.
4. Inside the TUI, `/install <tool>` submits a `JobSetup` job visible in the
   jobs panel with live status, and writes a log file.
5. `/install --dry-run <tool>` posts the install steps inline without submitting.
6. `/modal deploy` submits a deploy job (or posts the modal-CLI-absent hint).
7. `go test ./...` passes; `go vet ./...` is clean.

## 9. Execution approach

Invoke `writing-plans` to produce a task-level implementation plan, then build
it via subagent-driven development. The change touches a shared core
(`cmd/proteus/main.go`, `internal/tui/app.go`) and is therefore largely
sequential; tasks that genuinely do not share files (the `domain` constant, the
`InstallLogged` method) may be done independently, but per-file edits are
serialized to avoid conflicts.
