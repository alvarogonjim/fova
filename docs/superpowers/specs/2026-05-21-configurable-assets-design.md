# fova — Unified configurable assets (`internal/assets`) — Design

**Date:** 2026-05-21
**Status:** Approved, ready for implementation planning
**Branch:** `feat/configurable-assets` (worktree, branched off `main` @ `c543aba`)
**Milestone:** Fulfils and extends SPECS §8.1 (user skills) and §11 (`/skills` command); unifies SPECS §14 configuration.
**Approach:** Approach B — one `internal/assets` package owns every materializable, user-editable asset.

## 1. Goal & scope

fova ships four kinds of "things the binary contains that a user may want to
change", and today each is handled differently:

| Asset | Today | Owner |
|---|---|---|
| `config.toml` | embedded + materialize-on-first-run + validate | `internal/config` |
| `models.toml` | embedded + materialize-on-first-run + validate | `internal/config` |
| skills (7 markdown files) | embedded `go:embed`, **never** written to disk | `internal/skills` |
| system prompt | embedded `go:embed`, **never** written to disk | `internal/agent` |

The user cannot see, edit, or extend the skills or the system prompt without
recompiling. This design makes **all four asset kinds** discoverable, editable,
extensible, validated, and resettable through one consistent mechanism and one
consistent UI surface.

**Defining choice (Approach B):** a new package `internal/assets` becomes the
single owner of all four. `internal/config` is absorbed into it and deleted.
The embedded skills (`internal/skills/builtin/`) and system prompt
(`internal/agent/prompts/system.md`) move into `internal/assets/embed/`. This
is a real refactor — every importer of `internal/config` is updated — and is
accepted as the cost of one unified model.

**Out of scope:** a skill marketplace / remote skill installation (SPECS §19
"documented skill marketplace" remains future work); per-project skill
overrides under `~/fova/projects/<name>/`; reworking how `config.toml` /
`models.toml` are *validated* (validation logic is preserved verbatim).

### 1.1 Decisions locked during brainstorming

- **System prompt — full override.** A user may replace the entire system
  prompt. fova validates the replacement; it does not restrict it to an
  append-only appendix.
- **Materialize on first run.** All built-in assets (config, models, system
  prompt, the 7 skills) are written to `~/.config/fova/` on first launch.
  After that, disk is authoritative. Accepted trade-off: edits made to an
  *existing* built-in file in a future fova version do not auto-propagate to a
  user who already has that file (mitigated — see §4.3 and §7).
- **Skill format — optional YAML frontmatter.** Skills are markdown with an
  optional `---` frontmatter block carrying `name` + `description`. Files
  without frontmatter remain valid.

## 2. Directory layout

```
~/.config/fova/                    # $FOVA_CONFIG_DIR overrides this root
├── config.toml                    # materialized, validated (TOML)
├── models.toml                    # materialized, validated (TOML)
├── system.md                      # materialized, validated (markdown + marker)
└── skills/
    ├── design-binder.md            # built-in, materialized
    ├── design-antibody.md
    ├── design-enzyme.md
    ├── filter-thresholds.md
    ├── plan-from-target.md
    ├── submit-to-adaptyv.md
    ├── close-the-loop.md
    └── <user-skill>.md             # user-authored — fova never writes or resets these
```

`system.md` is materialized (not override-on-demand) so that a user opening
`~/.config/fova/` sees every editable asset in one place — one consistent
"everything here is yours to edit" model.

## 3. The `internal/assets` package

### 3.1 Unifying insight

Materialization is **mirroring an embedded file tree onto disk, skipping any
file that already exists.** One embedded tree, one mirror function, all four
asset kinds covered. A future fova version that adds
`embed/skills/redesign-stability.md` ships it for free: the file is "missing"
on every existing user's disk, so it materializes on next launch. Only edits
to *already-materialized* built-ins do not auto-propagate.

### 3.2 Package layout

```
internal/assets/
├── embed/                          # the single embedded source-of-truth tree
│   ├── config.toml
│   ├── models.toml
│   ├── system.md
│   └── skills/
│       └── *.md                    # the 7 built-in skills
├── assets.go                       # Dir(), Bundle, Load(), Reset(), Export(), Path()
├── materialize.go                  # mirror embed.FS → Dir(), skip-if-exists
├── config.go                       # config.toml — Config type + validation (from internal/config)
├── models.go                       # models.toml — Catalog type + validation (from internal/config)
├── skill.go                        # Skill type, frontmatter parsing, per-skill validation
├── system.go                       # system.md load + validation
├── report.go                       # Report — unified validation result
└── *_test.go
```

### 3.3 Public API

```go
package assets

// Dir returns the assets directory: $FOVA_CONFIG_DIR, else ~/.config/fova.
func Dir() string

// Bundle is the entire on-disk config state, loaded once at startup.
type Bundle struct {
    Config       Config   // config.toml
    Models       Catalog  // models.toml
    Skills       []Skill  // skills/*.md, sorted by name
    SystemPrompt string   // raw system.md template — still contains {{COMMAND_CATALOGUE}}
    Report       Report   // non-fatal warnings + per-skill-file errors
}

// Load materializes any missing asset, then parses and validates everything.
// A malformed config.toml or models.toml is a returned error (fail-hard, see
// §5). A malformed system.md or skill file is degraded to a Report entry and
// the embedded default / a skipped file is used instead.
func Load() (*Bundle, error)

// Reset restores one asset from the embedded default, overwriting the on-disk
// copy. name is an asset key: "config", "models", "system", or
// "skills/<skill-name>". A user-authored skill (no embedded counterpart) is
// rejected. Returns an error if name is unknown.
func Reset(name string) error

// Export ensures an asset exists on disk (materializing it if missing) and
// returns its absolute path. Used by the edit commands.
func Export(name string) (path string, err error)

// Path returns an asset's absolute on-disk path without touching the file.
func Path(name string) string

// DefaultSystemPrompt returns the embedded system.md template (no disk
// access) — the fallback used when the on-disk system.md is invalid.
func DefaultSystemPrompt() string
```

`Config`, `Catalog`, `Provider`, `Model` move verbatim from `internal/config`
into `internal/assets` (renamed `assets.Config`, etc.). Their TOML tags,
validation, and the `SaveConfig` atomic-write helper are preserved unchanged.

## 4. Asset types & validation

### 4.1 Skills

```go
type Source int
const (
    SourceUser            Source = iota // no embedded counterpart
    SourceBuiltin                       // on-disk bytes identical to the embedded copy
    SourceBuiltinModified               // embedded counterpart exists, on-disk bytes differ
)

type Skill struct {
    Name        string // frontmatter name, else filename stem
    Description string // frontmatter description, else ""
    Body        string // markdown after the frontmatter block
    Path        string // absolute on-disk path
    Source      Source
}
```

`Source` is computed by comparing the on-disk file to the embedded
`embed/skills/<file>`: absent → `SourceUser`; byte-identical → `SourceBuiltin`;
present but different → `SourceBuiltinModified`.

**Per-skill validation:**

- File extension is `.md`; content is valid UTF-8.
- Body (after frontmatter) is non-empty.
- Frontmatter, if present, is a well-formed YAML mapping containing only the
  keys `name` and `description`. An unknown key is a **warning**, not an error.
- `name`, if present, matches `^[a-z0-9]+(-[a-z0-9]+)*$` **and equals the
  filename stem**. A mismatch is an **error** (keeps the `skills.read{name}` ↔
  file mapping unambiguous).
- `description`, if present, is a non-empty single line; longer than 120
  characters is a **warning**.
- Two loaded skills resolving to the same `Name` is an **error** on the second.

A skill file that produces an **error** is skipped (see §5); its error is
recorded in the `Report`. The 7 built-ins are updated to carry frontmatter.

### 4.2 System prompt

`system.md` validation:

- Content is non-empty and valid UTF-8.
- Contains **exactly one** `{{COMMAND_CATALOGUE}}` marker. Zero or more than
  one is an **error** (the catalogue could not be rendered correctly).
- Heuristic **warning** if it contains no "Refusals" and no "Tone" section
  (recommended, not required).

An invalid `system.md` is a `Report` entry; `Bundle.SystemPrompt` falls back
to `DefaultSystemPrompt()` so the agent always has a working prompt.

### 4.3 config.toml / models.toml

Validation logic is moved unchanged from `internal/config`. A malformed or
schema-invalid file is a **hard error** returned by `Load()` (see §5).

### 4.4 Frontmatter format

YAML frontmatter delimited by `---` fences at the very start of the file,
parsed with `gopkg.in/yaml.v3` (already a transitive dependency — promoted to
a direct one in `go.mod`). `/skills new` scaffolds:

```markdown
---
name: my-skill
description: One-line summary shown in skills.list and /skills list.
---
# Skill: <title>

## When to use
...
```

A file without frontmatter is valid: `Name` = filename stem, `Description` = "".

## 5. Failure policy

One framework, a policy that differs by asset kind **by design** — stated here
so the difference is a decision, not an inconsistency:

| Asset | Malformed / invalid → |
|---|---|
| `config.toml`, `models.toml` | **Hard error** from `Load()`. Structured machine-config (budget guards, provider kinds) must stop the user and be fixed, never silently revert. Unchanged from today. |
| `system.md` | **Warning** in `Report`; `SystemPrompt` falls back to the embedded default. The agent must always have a working prompt. |
| a `skills/*.md` file | **Warning** in `Report`; that one file is skipped, the rest load. |

If the config directory itself is unwritable, `Load()` returns a `Bundle`
built entirely from embedded defaults plus a `Report` warning — fova never
crashes over a config-directory problem.

`Report` is surfaced as a one-line startup banner when non-empty, and in full
by `/skills validate` and `/config validate`.

```go
type Report struct {
    Errors   []AssetIssue // a skill file skipped, system.md fell back, ...
    Warnings []AssetIssue // unknown frontmatter key, missing Refusals section, ...
}
type AssetIssue struct {
    Asset   string // "skills/foo.md", "system.md", ...
    Message string
}
```

## 6. TUI surface

Two slash commands plus an extension of the existing `/reload`. Both new
commands are registered in `internal/tui/commands.go` so the
`{{COMMAND_CATALOGUE}}` rendered into the system prompt stays ground-truth.

### 6.1 `/skills` (SPECS §11)

| Invocation | Behaviour |
|---|---|
| `/skills` or `/skills list` | Table: name · description · source (`built-in`, `built-in*` for modified, `user`) · any load warning. |
| `/skills show <name>` | Print the skill's full markdown. |
| `/skills new <name>` | Scaffold `~/.config/fova/skills/<name>.md` with the frontmatter template, open `$EDITOR`, then re-validate + reload. Rejects an existing name. |
| `/skills edit <name>` | Open the skill file in `$EDITOR`, then re-validate + reload. |
| `/skills reset <name>` | Restore a built-in skill from the embedded copy. Confirms first if the file is `built-in*` (modified). Rejects user skills. |
| `/skills reset --all` | Reset every built-in skill (single confirm). |
| `/skills validate` | Print the skill portion of the `Report`. |
| `/skills path` | Print `~/.config/fova/skills/`. |

### 6.2 `/config` (new)

Editing surface for the singleton assets, including the system prompt.

| Invocation | Behaviour |
|---|---|
| `/config edit <config\|models\|system>` | Open the file in `$EDITOR`, then re-validate + reload. |
| `/config reset <config\|models\|system>` | Restore the file from the embedded default (confirm first). |
| `/config validate` | Print the config/models/system portion of the `Report`. |
| `/config path` | Print `~/.config/fova/`. |

### 6.3 `/reload`

Extended from "reload `config.toml` + `models.toml`" to reload the whole
`Bundle`: re-apply theming, swap the model registry, swap the skill set, and
**hot-swap the running agent's system prompt**. All edit commands above end by
invoking the same reload path.

### 6.4 Editor handoff

`/skills new`, `/skills edit`, and `/config edit` reuse the existing
`$EDITOR` integration in `internal/tui/editor.go` (`tea.ExecProcess` →
`$VISUAL` → `$EDITOR` → `vi`). On editor exit the asset is re-validated and
the `Bundle` reloaded; validation errors are shown inline in the chat.

## 7. Migration — what moves, what is deleted

**Deleted:**

- `internal/config/` — absorbed into `internal/assets`.
- `internal/skills/builtin/` — the 7 markdown files move to `internal/assets/embed/skills/`.
- `internal/agent/prompts/system.md` — moves to `internal/assets/embed/system.md`.

**Renamed (mechanical, across ~10–15 importers):** `config.Config` →
`assets.Config`, `config.Catalog` → `assets.Catalog`, `config.Provider`,
`config.Model`, `config.LoadConfig`, `config.LoadModels`, `config.ConfigDir`,
`config.SaveConfig`, etc.

**Modified:**

- `internal/skills/loader.go` — `skills.list` / `skills.read` are constructed
  from a loaded `[]assets.Skill` (or a small `SkillSet`) instead of embedding
  their own `embed.FS`. `internal/skills` imports `internal/assets`;
  `internal/assets` does **not** import `internal/skills` — no import cycle.
  `skills.list` output gains the description column.
- `internal/agent/prompts.go` — `BuildSystemPrompt(cat, template string)`
  takes the template string as a parameter instead of embedding `system.md`.
  The package-level `agent.SystemPrompt` var is removed; callers and tests
  obtain the rendered prompt via `BuildSystemPrompt` with
  `assets.DefaultSystemPrompt()` as the template. `internal/agent` tests may
  import `internal/assets`; `internal/assets` does not import `internal/agent`
  — no cycle.
- `cmd/fova/main.go` — calls `assets.Load()` once at startup, threads the
  `Bundle` into `buildRegistry`, the TUI model, and the agent session.
- `internal/tui/` — new `/skills` and `/config` command handlers; `/reload`
  extended; `commands.go` catalogue updated.

**Decision held:** the two `tools.Tool` wrappers (`skills.list`, `skills.read`)
stay in `internal/skills`. Moving them to `internal/tools/skills/` would be
tidier but adds churn; the loading logic — the part being unified — lives in
`internal/assets` regardless.

## 8. Error handling & edge cases

- Config directory unwritable → all-embedded `Bundle` + `Report` warning, no crash.
- Skill file with frontmatter `name` ≠ filename stem → error, file skipped.
- User deletes `system.md` → treated as missing → re-materialized on next `Load()`.
- `/skills reset` / `/config reset` on a modified file → confirmation prompt before overwrite.
- `/reload` while a job is in flight → config/skills/prompt reload; in-flight jobs keep the settings they started with.
- `/skills new <name>` where `<name>` is not kebab-case → rejected with the rule shown.
- Two skill files whose frontmatter resolves to the same name → the second is an error and skipped.

## 9. Testing strategy

- **Materialization:** first run writes the full tree; second run is a no-op;
  a newly-added embedded file materializes while an existing user file is left
  untouched; an unwritable dir degrades to embedded defaults.
- **Frontmatter parser:** with frontmatter, without, `name`/filename mismatch,
  malformed YAML, unknown key.
- **Validation:** every rule in §4.1 and §4.2, each asserted independently.
- **System prompt:** missing marker and duplicate marker both fall back to the
  embedded default and record a `Report` entry.
- **`Reset` / `Export` / `Path`:** known keys, unknown keys, user-skill rejection.
- **Failure policy:** malformed `config.toml` is a hard error; one malformed
  skill file is skipped without losing the rest.
- **TUI:** `/skills` and `/config` subcommand handlers; `/reload` hot-swap of
  the system prompt.
- **Migration:** the existing test suite compiles and passes after the
  `internal/config` → `internal/assets` rename.

## 10. Implementation parallelization

Designed for the worktree + parallel-Opus execution model. **Phase 1
establishes the package interfaces; Phase 2 work units run concurrently**, each
in its own worktree-isolated agent.

| Unit | Scope | Depends on |
|---|---|---|
| **1. Core** | `internal/assets`: `embed/` tree, `materialize.go`, `Bundle`, `Load`/`Reset`/`Export`/`Path`, `report.go`. Moves `config.go` / `models.go` in verbatim. | — |
| **2. Skills** | `Skill` type, frontmatter parser, per-skill validation; rewire `internal/skills` tools onto `[]assets.Skill`; add frontmatter to the 7 built-ins. | Unit 1 interfaces |
| **3. System prompt** | `system.go` load + validation; rewire `internal/agent/prompts.go` to take the template string. | Unit 1 interfaces |
| **4. TUI** | `/skills`, `/config` command handlers; `/reload` extension; `commands.go` catalogue. | Units 1–3 interfaces |
| **5. Migration** | Mechanical `internal/config` → `internal/assets` import/rename sweep across all other importers; `cmd/fova/main.go` wiring. | Unit 1 interfaces |

`cmd/fova/main.go` wiring — calling `assets.Load()` and threading the
`Bundle` into `buildRegistry`, the TUI model, and the agent session — is
owned solely by unit 5, so it is not a contention point. Remaining shared-file
contention is `internal/tui` (units 4 and 5 both touch it) and `go.mod`
(unit 2 promotes `yaml.v3`); resolved by merging units in order
1 → 2 → 3 → 5 → 4. The implementation plan (written next, via the
writing-plans skill) turns this into phased tasks with review checkpoints.

## 11. Acceptance criteria

- `~/.config/fova/` contains `config.toml`, `models.toml`, `system.md`, and
  `skills/` with all 7 built-ins after first launch.
- Editing a skill file or `system.md` and running `/reload` (or finishing
  `/skills edit` / `/config edit`) changes agent behaviour without a rebuild.
- A new file dropped in `~/.config/fova/skills/` appears in `skills.list`,
  `/skills list`, and is readable via `skills.read`.
- A malformed skill file is skipped with a clear `Report` message; the other
  skills still load; the agent still runs.
- A `system.md` missing the `{{COMMAND_CATALOGUE}}` marker falls back to the
  embedded prompt with a clear `Report` message.
- `/skills reset` and `/config reset` restore embedded defaults.
- `internal/config` no longer exists; the full build and test suite pass.
