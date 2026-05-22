# fova — First-Run Onboarding Wizard

**Spec date:** 2026-05-22
**Status:** Implementation-ready
**Author:** Alvaro (brainstormed with Claude Code)
**Scope:** `internal/tui`, `internal/llm`, the config package, `cmd/fova`

## 1. Summary

The first time someone runs `fova`, nothing guides them. `config.toml` and
`models.toml` are silently materialized from embedded defaults into
`~/.config/fova/`, the data folder is whatever `~/fova` resolves to, and a
paid LLM provider only works if the user already exported the right API-key
environment variable. For a non-technical user this is opaque.

This spec adds a **full-screen in-TUI onboarding wizard** that runs once on
first launch. It walks the user through: where fova keeps its data, which LLM
provider to use (storing a pasted API key in the OS keychain), the colour
theme, the compute backend, the knowledge email, and the session budget. It
writes the result to `~/.config/fova/config.toml`. The wizard is skippable and
re-runnable via an `/onboarding` slash command.

It is the second of two specs requested together; the first (TUI navigation
polish) is complete.

## 2. Current behaviour and gaps

- **No first-run flow.** `runTUI` (`cmd/fova/main.go`) calls
  `config.LoadConfig()` / `config.LoadModels()`, which auto-materialize the
  embedded defaults the first time the files are missing. There is no prompt.
- **API keys are environment-only.** LLM provider construction
  (`internal/llm/modelregistry.go`) reads keys exclusively via
  `os.Getenv(provider.APIKeyEnv)`. The OS keychain (`99designs/keyring`) is
  used only for the Adaptyv lab token (`internal/tools/lab/auth.go`). SPECS
  §14.3 specifies "read from env first, then OS keychain" — the keychain half
  is unimplemented for LLM providers.
- **The data folder is environment-only.** `fovaHome()` resolves
  `$FOVA_HOME`, else `~/fova`. There is no persisted, user-chosen location —
  a non-technical user cannot move it without setting an environment variable.

## 3. Goals / non-goals

**Goals**
- A guided, skippable first-run wizard that feels like part of the fova TUI.
- The wizard configures: data-folder location, LLM provider (+ API key via the
  keychain), theme, compute backend, knowledge email, session budget.
- A pasted API key is stored in the OS keychain and is actually used —
  LLM key resolution becomes env → keychain (SPECS §14.3).
- The chosen data folder persists across launches without an environment
  variable.
- Re-runnable from inside the TUI via `/onboarding`.

**Non-goals**
- Editing custom local-provider base URLs (e.g. a non-default Ollama host) —
  v1 uses the providers as defined in the embedded `models.toml`; a custom URL
  is a hand-edit of `models.toml`.
- Choosing a specific model — the wizard sets the *provider*; the model stays
  the provider default (`[defaults].model = ""`).
- Installing local tools or deploying Modal — those remain `/install`,
  `/doctor`, `/modal deploy`. The wizard only records the compute *preference*.
- Multiple config profiles.
- Migrating an existing data folder when the location is changed on re-run.

## 4. The wizard component

A single self-contained Bubble Tea component, `wizardModel`, drives the whole
flow. It is used by **two harnesses** (§6, §7) without change.

### 4.1 `wizardModel`
`wizardModel` implements `tea.Model` (`Init` / `Update` / `View`) so it can be
the root model of a standalone program *and* be embedded as an overlay. It holds:

- the ordered list of active steps (the API-key step is included only when
  needed — §5.4),
- the current step index,
- a `WizardResult` accumulating the user's choices,
- per-step transient input state (text-input buffer, list cursor),
- `finished bool` / `skipped bool`.

When the user finishes the summary step or skips, `Update` emits a
`wizardDoneMsg{Result WizardResult, Skipped bool}` (it does **not** itself
return `tea.Quit` — each harness decides what "done" means).

### 4.2 `WizardResult`
The collected choices, all optional-with-defaults:

```
WizardResult{
    DataDir        string  // chosen fova home; "" = leave default
    Provider       string  // provider name from models.toml; "" = auto
    APIKeyProvider string  // which paid provider the key belongs to; "" = none
    APIKey         string  // pasted key (kept in memory only until applied)
    Theme          string  // auto | light | dark
    ComputeBackend string  // local | modal
    KnowledgeEmail string
    BudgetUSD      float64
}
```

`Skipped` results carry an empty `WizardResult` — applying it is a no-op.

### 4.3 Step model
Each step is one of three kinds, so the component stays small:

- **info** — text only, advanced with Enter (Welcome).
- **pick** — a single-select list, ↑/↓ + Enter (provider, theme, compute).
- **input** — a single bordered text field, typed + Enter (data folder, API
  key, knowledge email, budget). Each input step has a validator.

A step declares: its title, a one-line counter visibility flag, explanatory
body text, its kind, and how it reads/writes its `WizardResult` field.

## 5. The steps

Welcome is an unnumbered intro screen; the rest carry a `step N of M` counter
and progress dots, where `M` is 8 when the conditional API-key step is shown
(§5.4) and 7 when it is not.

### 5.1 Welcome (info)
Explains what the setup does, that it takes ~a minute, and that fova is free by
default (a local model needs no account). `Enter` begins; `Esc` skips.

### 5.2 Data folder (input)
Prompt: "Where should fova keep its data?" Default value `~/fova` (the current
`fovaHome()` result, tilde-collapsed). The user may edit it. Validation: the
path's parent must exist or be creatable; `~` is expanded. If `$FOVA_HOME` is
already set in the environment, this step is shown read-only with a note that
the environment variable wins — the wizard does not fight an explicit override.

### 5.3 LLM provider (pick)
A list of the providers fova supports, sourced from the embedded
`models.toml`: Ollama (local), Anthropic, OpenAI, Google. Each row shows a
free/paid tag. On entering this step the wizard probes the local Ollama
endpoint (`GET http://localhost:11434/api/tags`, ~300 ms timeout); if it
responds, the Ollama row is tagged "detected". The probe never blocks or
errors the wizard — a non-response just means no "detected" tag.

### 5.4 API key (input, conditional)
Included **only** when the chosen provider is paid *and* its API-key
environment variable is not already set. Prompt names the provider; the field
masks input. Body text explains the key goes into the OS keychain, never a
plain file, and shows the provider's key-issuance URL. `Enter` stores & moves
on; `Ctrl+S` defers ("set it up later" — recorded as no key). When the chosen
provider is local, or its env var is already set, this step is absent and the
counter's `M` is 7.

### 5.5 Theme (pick)
`auto` / `light` / `dark`. Selecting a value applies it live so the user sees
the effect immediately (`ApplyTheme`, already used by `/theme`).

### 5.6 Compute backend (pick)
`local` / `modal`. Body text notes that `local` needs tools installed
(`/install`, `/doctor`) and `modal` needs the Modal CLI + token and a
`/modal deploy` — the wizard records the preference only.

### 5.7 Knowledge email (input)
Optional. Prompt explains it is used for the OpenAlex "polite pool" and
improves literature-API rate limits. Empty is allowed. If non-empty, it is
validated as a syntactically plausible email.

### 5.8 Budget (input)
Prompt: the per-session soft USD limit that triggers a warning. Default `5.0`.
Validation: parses as a number ≥ 0.

### 5.9 Summary & finish (info)
Lists every chosen value. `Enter` finishes — emits `wizardDoneMsg` with the
result. `←` / `Backspace` goes back to any step to change a value.

### 5.10 Navigation & skip
`Enter` advances (after validation); `←` / `Backspace` goes back; `Esc` skips
the whole wizard from any step (emits `wizardDoneMsg{Skipped: true}`). Invalid
input on an `input` step blocks `Enter` and shows an inline error; it never
crashes.

## 6. First-run detection and the standalone harness

### 6.1 Detection
`runTUI` (`cmd/fova/main.go`), **before** it builds the workspace, store, or
registries, calls a new `firstRun()` check: the wizard runs when
`config.toml` does not yet exist in the config directory (`config.ConfigDir()`)
**and** stdin/stdout is an interactive terminal. A non-TTY (piped input, CI,
tests) never shows the wizard.

This ordering matters: `config.LoadConfig()` auto-materializes `config.toml`,
so the absence check must happen before any config load. After a first run —
whether the wizard was completed or skipped — `config.toml` exists (the wizard
writes it on completion; on skip the subsequent `LoadConfig` materializes the
default), so the wizard never re-triggers on its own.

### 6.2 Standalone harness
On first run, `cmd/fova` runs the wizard as its own Bubble Tea program:

```
result, ok := runOnboarding()   // tea.NewProgram(newWizardModel()).Run()
if ok { applyWizardResult(result) }
// then runTUI continues: LoadConfig now reads what the wizard wrote
```

A thin wrapper translates the wizard's `wizardDoneMsg` into `tea.Quit`;
`runOnboarding` reads the final model's `result` / `skipped`. Because this
happens before the store opens, a data-folder change takes effect for the very
first session — no restart needed on first run.

## 7. Re-run: the `/onboarding` overlay

A new `/onboarding` slash command (added to the slash-command catalogue, so it
appears in the autocomplete and `/help`) re-opens the wizard inside the running
TUI as a new full-screen overlay, `overlayWizard`, following the existing
overlay pattern (`overlayConfirm`, `overlayKeys`, the picker, …). The root
`Model` embeds a `*wizardModel`; while `overlayWizard` is active, `handleKey`
forwards keys to it and `View` renders it full-screen.

When the embedded wizard emits `wizardDoneMsg`:
- `applyWizardResult` writes `config.toml` and stores any API key (§8);
- the existing config-reload path (`cmdReload`) re-reads `config.toml` /
  `models.toml` so theme, provider, budget, and knowledge settings apply live;
- if the data folder was changed, a chat note states the change is saved and
  takes effect on the next launch (the store is already open at the old path —
  no live migration, per §3 non-goals);
- the overlay closes.

A skipped re-run closes the overlay and changes nothing.

## 8. Applying the result

`applyWizardResult(WizardResult)` is the single apply path, shared by both
harnesses:

1. **Data folder** — if `DataDir` is non-empty and differs from the default,
   create the directory and persist the path to config (§9). Skipped/empty →
   untouched.
2. **config.toml** — load the current config (embedded default on first run),
   set `[ui].theme`, `[defaults].provider`, `[defaults].compute_backend`,
   `[knowledge].mailto`, `[budget].session_soft_limit_usd`, and the data-folder
   path field, then `config.SaveConfig`. Fields the user left at default are
   written as their default — the file is always complete and valid.
3. **API key** — if `APIKey` is non-empty, store it in the OS keychain under a
   stable key derived from `APIKeyProvider` (§9). A keychain write failure is
   surfaced as a clear message and the key is also set in the process
   environment so the current session still works; the wizard does not abort.
4. `models.toml` is **not** written — the embedded default already defines
   every provider; the choice lives in `config.toml`'s `[defaults].provider`.

## 9. Supporting changes

These are required for the wizard's choices to actually take effect.

### 9.1 LLM API-key resolution: env → keychain
`internal/llm` provider construction (currently `os.Getenv(APIKeyEnv)`) is
changed to resolve a key as: the environment variable if set, otherwise the OS
keychain. This implements SPECS §14.3 and makes a wizard-stored key work on
every subsequent launch. The `availability` check (`modelregistry.go:62`,
"provider is usable") uses the same resolution.

### 9.2 Shared keychain helper
The keychain access pattern in `internal/tools/lab/auth.go` (a single
`99designs/keyring` service) is generalized into a small shared helper exposing
`Get(name) (string, error)` / `Set(name, value) error`, used by both the wizard
(to store LLM keys) and `internal/llm` (to read them). The Adaptyv token keeps
working through the same service. LLM keys use stable names such as
`anthropic-api-key`, `openai-api-key`, `google-api-key`.

### 9.3 Persisted data-folder path
A new optional path field is added to `config.toml` (e.g. under `[defaults]`)
holding the fova data folder. `fovaHome()` resolution becomes:
`$FOVA_HOME` (environment override) → the config value → `~/fova` (default).
This lets the wizard persist the user's folder choice without an environment
variable, while an explicit `$FOVA_HOME` still wins.

> **Note on the config package.** A `config` → `assets` package refactor is in
> flight on `dev`. This spec describes the changes behaviourally; the
> implementation plan will pin exact file paths and the config struct's field
> names against whichever layout has landed when the plan is executed.

## 10. Edge cases & error handling

- **Non-interactive run** (piped stdin, CI, tests) — the wizard never shows;
  defaults materialize as today.
- **`$FOVA_HOME` set** — the data-folder step is read-only; the environment
  override wins; the wizard records nothing for it.
- **`$FOVA_CONFIG_DIR` set** — the wizard reads and writes `config.toml` there.
- **Keychain unavailable** (headless Linux, no secret service) — a key store
  failure is reported inline; the key is applied to the process environment for
  the session; the wizard continues. The user can re-run later.
- **Ollama probe fails / times out** — no error; the row simply lacks the
  "detected" tag. The user may still select Ollama (it may be started later).
- **Invalid input** — `input` steps block `Enter` and show an inline message;
  budget must parse, email (if given) must be plausible, the folder path's
  parent must be creatable.
- **Skip** — at any step `Esc` ends the wizard applying nothing; fova starts on
  defaults; `config.toml` still ends up materialized so the wizard does not
  re-trigger.
- **Re-run data-folder change** — saved to config, takes effect next launch;
  the running session keeps using the already-open workspace.

## 11. Testing

`wizardModel` (`internal/tui/wizard_test.go`)
- Step navigation: `Enter` advances, `←` goes back, the API-key step is present
  for a paid provider with no env var and absent for a local provider / when
  the env var is set; the `M` counter reflects 7 vs 8.
- Each `input` validator: budget rejects non-numbers and negatives; email
  rejects implausible values and accepts empty; the folder step expands `~`.
- `Esc` from any step emits `wizardDoneMsg{Skipped: true}` with an empty result.
- Finishing the summary emits `wizardDoneMsg` with the accumulated result.
- Theme selection applies live.
- The Ollama probe tags the row when a stub server responds and does not when
  it does not.

Apply path (`applyWizardResult`)
- Produces a complete, valid `config.toml` with the chosen fields and defaults
  for the rest.
- Stores the API key under the expected keychain name; a simulated keychain
  failure degrades to the process environment without aborting.
- A non-empty `DataDir` is persisted and the directory created; an empty one
  changes nothing.

First-run detection
- `firstRun()` is true when `config.toml` is absent, false when present.
- A non-TTY context never launches the wizard.

LLM key resolution (`internal/llm`)
- The environment variable wins when set; the keychain value is used when the
  environment variable is empty; neither present → provider unavailable.

`/onboarding`
- The command opens `overlayWizard`; finishing applies the result and closes
  the overlay; skipping closes it with no change.

## 12. Files (behavioural — the plan pins exact paths)

**New**
- `internal/tui/wizard.go` — `wizardModel`, the step definitions, `WizardResult`,
  `wizardDoneMsg`, the wizard `View`.
- `internal/tui/wizard_test.go`.
- A small shared keychain helper (new package, or generalized from
  `internal/tools/lab/auth.go`).
- `cmd/fova` onboarding harness (`firstRun()`, `runOnboarding()`,
  `applyWizardResult()`) — a new file in `cmd/fova` or `internal/tui`.

**Modified**
- `cmd/fova/main.go` — first-run detection + wizard invocation before the
  workspace/registry are built.
- `internal/tui/app.go` — `overlayWizard`, the embedded `*wizardModel`,
  `/onboarding` handling, key forwarding and `View` rendering for the overlay.
- The slash-command catalogue — register `/onboarding`.
- `internal/llm/modelregistry.go` — API-key resolution env → keychain.
- The config package — the persisted data-folder field; `fovaHome()` resolution.

## 13. Out of scope

- Custom local-provider base URLs; model (not provider) selection.
- Installing tools / deploying Modal from the wizard.
- Live data-folder migration on re-run.
- Multiple config profiles; importing settings from another machine.
