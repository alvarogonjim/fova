# fova rebrand — design spec

**Date:** 2026-05-20
**Branch:** `rebrand/fova`
**Scope:** rename `proteus → fova` (binary, module path, all references); apply
the visual identity from `docs/DESIGN.md` and the three TUI mockups in
`fova_tui_screens_v2.html` (kept out of the repo; treated as reference).

This spec is intentionally narrow: it does NOT add features. Every change is
either a rename or a re-skin of an existing surface.

---

## 1. Approach

One feature branch, `rebrand/fova`, off `master`. Three logical commits:

1. **`chore: rename proteus → fova`** — mechanical. `go.mod`, all Go imports,
   `cmd/proteus/` directory, `Makefile`, `install.sh`, `homebrew-tap/`,
   `.goreleaser.yaml`, READMEs, narrative package comments. Verification gate:
   `go build ./... && go test ./...` must pass before moving on.
2. **`feat(tui): fova palette and design tokens`** — swap `internal/tui/theme.go`
   to the new palette (moss + saffron + sand on dark forest). Adds one new
   semantic role (`Primary`). All consumer styles auto-pick up new colors
   because they read from the palette.
3. **`feat(tui): fova header, status markers, modal style`** — folded-F header,
   `▸ ⠿ ✓ ○` status glyphs, saffron-bordered confirmation modal, designs-table
   highlighting, input-border + prompt re-coloring.

Why this order: every Phase-2 style edit is written against the new module
path. No rework. CI stays green between commits.

---

## 2. Design tokens

Source of truth: the three screens in `fova_tui_screens_v2.html`. The HTML
references six colors; the README badges in `docs/DESIGN.md` add a printed-brand
variant `#3B6D11` we use for badges and the README header, but not in the TUI.

| Role | New value | Mockup source |
|---|---|---|
| `Bg` (new — explicit) | `#0d1f15` | `.tui` background |
| `Fg` (sand) | `#d4cfc0` | `.sand` |
| `FgMuted` (dim) | `#6b7a6b` | `.dim` |
| `FgSubtle` (moss-dim) | `#4a7e2a` | `.moss-dim` |
| `Primary` (moss) — NEW role | `#7fc14a` | `.moss` |
| `Accent` (saffron) | `#EF9F27` | `.saffron` |
| `Border` (idle input) | `#4a7e2a` | input box border |
| `Queued` | `#6b7a6b` | `○` color |
| `Running` | `#7fc14a` | `⠿` color |
| `Succeeded` | `#7fc14a` | `✓` color |
| `Failed` | `#F87171` (unchanged) | not depicted; sensible default kept |
| `Warning` | `#EF9F27` | `▸ awaiting confirm` |

**Why a new `Primary` role:** today `Accent` is overloaded — it carries both
brand identity (logo, user-label bold) and warning/focus state. The mockup
separates moss (brand, agent, success) from saffron (attention, focus, cost,
modal border). Two roles are unavoidable.

**Light/dark adaptivity:** dropped for brand colors. The mockup is dark-only;
forcing a light variant would be invented, not derived. `Failed` keeps its
adaptive shape because it's not in the mockup.

**Backward-compat:** code that currently reads `Accent` for the agent/brand
flavor (e.g. `StatusBar`, `UserText`) is reassigned to `Primary` during the
commit; no consumer keeps a dangling reference.

---

## 3. Components

Seven surfaces changed. Anything not listed stays as-is.

### 3.1 Header (new file `internal/tui/header.go`)

Replaces the current minimal title strip. Layout:

```
fova · /home/alvaro/fova/projects/<name>           ← line 0: full path, dim
 ┌─╮     fova 0.5.0-dev                            ← line 1: mark + version
 │ ╰──●  qwen3.5-35b-a3b-fp8                       ← line 2: model, ● saffron
 ├─╮    ~/Projects/fova/<name>                     ← line 3: short cwd, dim
 │ ╰─                                              ← line 4: mark continued
 │                                                 ← line 5: mark closer
```

The folded-F mark is moss (`#7fc14a`); the endpoint dot on line 2 is saffron
(`#EF9F27`). Version + model in sand; paths in dim.

### 3.2 Logo asset (new file `internal/tui/logo.go`)

Six-line string constant + a `RenderLogo(p Palette) string` helper. Used by
the header and by `/about` / splash. Centralizing prevents drift.

### 3.3 Section rules (existing `chat.go` + side panels)

Right-side panel labels render as `<name> ─────────` in dim. When the panel
has pending attention (e.g. wet-lab awaiting confirm) prepend a saffron `▸`.

### 3.4 Status markers (jobs panel — `joblog.go` + `jobs.go`)

Four glyphs, each a `lipgloss.Style` on the `Theme`:

- `✓` moss — succeeded
- `⠿` moss — running (delegated to existing spinner frames)
- `○` dim — queued
- `▸` saffron — active / awaiting user

### 3.5 Designs table (`designs.go`)

Top-N rows (where N = the count selected for wet-lab) render the ID in moss
and the ipSAE score in saffron. Other rows render in sand. Overflow row
`+ <k> more` stays dim.

### 3.6 Input border + prompt (`editor.go` + `commandbar.go`)

All three mockup screens use the same moss-dim rounded border on the message
input — the box itself never recolours. Differentiation lives in the **prompt
glyph and cursor**:

- Border: moss-dim rounded in every state.
- `›` prompt — moss when idle / running; **saffron when a confirmation modal
  is open** (this is the only "awaiting user" signal in the input row).
- Cursor block `█` — saffron only during modal-confirm; otherwise default.

We keep the three existing `InputBorder*` style fields for compatibility but
collapse them to a single colour; the per-state distinction shifts to a new
`PromptIdle` / `PromptAwaiting` pair on `Theme`.

### 3.7 Confirmation modal (`modal.go`, `labmodal.go`)

Wet-lab and other irreversible flows get a saffron-bordered modal box. Key-row
pattern: `Submit? [y] yes  [n] no  [r] review  [s] save for later` — letter in
saffron, label in sand. Implemented as a small helper `keyRow(...keys)` so
other modals can adopt it without copy-paste.

---

## 4. Rename mechanics (Phase 1)

### 4.1 Module + imports

- `go.mod`: `module github.com/alvarogonjim/proteus` → `.../fova`.
- All Go files: `sed -i 's|alvarogonjim/proteus|alvarogonjim/fova|g'` over
  `**/*.go`. `gofmt -w` after.

### 4.2 Binary + entrypoint

- Directory `cmd/proteus/` → `cmd/fova/` (single Go file inside).
- `Makefile`: build target writes `./bin/fova`.
- `.goreleaser.yaml`: `binary: fova`, archive name `fova_<ver>_<os>_<arch>`.

### 4.3 Distribution

- `install.sh`: every reference to `proteus` becomes `fova`. Install URL
  placeholder is `https://fova.dev/install` (matches `docs/DESIGN.md`).
- `homebrew-tap/Formula/proteus.rb` → `fova.rb`. Update `desc`, `homepage`,
  `url`, `bin.install` line.

### 4.4 Narrative comments

`s|Proteus|fova|` is applied to package docstrings and READMEs, then a manual
sweep is needed for mid-sentence prose where capitalisation rules differ
(e.g. "Proteus is a..." → "fova is a..."). About 70 markdown / package-doc
hits.

### 4.5 Docs

- `README.md` — rewritten body matches the structure of `docs/DESIGN.md`.
- `docs/SPECS.md` header + first paragraph.
- `docs/RELEASING.md` — `proteus_<ver>` artifact names.
- `docs/CODE-REVIEW-*.md` — left untouched (historical artifacts).
- `docs/DESIGN.md` — source of truth, unchanged.

### 4.6 Out of scope

- The on-disk project directory `/home/gonjim/Projects/proteus/` stays where
  it is; renaming it is a user follow-up after the branch merges.
- The GitHub remote is left for the user to rename on github.com after the PR
  is created against the new repo name.
- `.claude/worktrees/` — throw-away; not touched.
- Auto-memory `MEMORY.md` index — its slug `proteus-v03-plan-from-target` stays
  as a history pointer; the body of that memory file gets a one-line "renamed
  to fova on 2026-05-20" note.

### 4.7 Verification gate

After Phase 1 and before Phase 2 starts:

```
go build ./...
go test ./...
./bin/fova --version    # must print "fova <current version>"
```

The version string itself is **not** bumped as part of this rebrand. The
mockup's `fova 0.5.0-dev` is illustrative — whatever `internal/buildinfo`
currently exposes (today `v0.4.0-dev`) flows through unchanged. A version
bump is a separate decision.

If any of those fail, Phase 2 is blocked.

---

## 5. Out of scope for this spec

- New features. Plan/jobs/designs/wet-lab behavior is unchanged.
- Light-theme fallback for the new palette.
- Changing the v0.5 milestone scope.
- Renaming the on-disk working directory.
- Slash commands, keybindings, chat scroll, replay.
- The remote GitHub repo rename (a user action).
