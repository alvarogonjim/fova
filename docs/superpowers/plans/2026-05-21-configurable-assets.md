# Configurable Assets Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make every asset fova ships — `config.toml`, `models.toml`, the system prompt, and skills — materialize into `~/.config/fova/`, be validated by one engine, and be editable / extensible / resettable through `/skills` and `/config`.

**Architecture:** A new `internal/assets` package becomes the single owner of all four asset kinds. `internal/config` is absorbed into it (via a temporary type-alias re-export shim that keeps the build green throughout, deleted in the final task). Embedded defaults move into `internal/assets/embed/`. Skills gain optional YAML frontmatter; the system prompt becomes a user-overridable on-disk file. New TUI commands `/skills` and `/config` plus an extended `/reload` expose the lot.

**Tech Stack:** Go 1.x, `embed.FS`, `github.com/BurntSushi/toml`, `gopkg.in/yaml.v3` (promoted from indirect to direct), `github.com/charmbracelet/bubbletea`.

**Spec:** `docs/superpowers/specs/2026-05-21-configurable-assets-design.md`

---

## Execution phasing

- **Phase 1 — Foundation (Tasks 1–9).** Sequential, critical path, one agent. Builds the whole `internal/assets` package behind the re-export shim. Every commit keeps `go build ./...` green.
- **Phase 2 — Consumers (Tasks 10–14).** Parallelizable: Task 10 (`internal/skills`), Task 11 (`internal/agent`), and Tasks 12–14 (`internal/tui`) touch disjoint packages and may run as three concurrent agents once Phase 1 is merged.
- **Phase 3 — Integration & cleanup (Tasks 15–16).** Sequential. Wires `cmd/fova/main.go`, then the mechanical `internal/config` → `internal/assets` migration that deletes the shim.

The worktree `/home/gonjim/Projects/proteus/.claude/worktrees/configurable-assets` on branch `feat/configurable-assets` already exists; all work happens there.

## File structure

**Created:**

- `internal/assets/embed/config.toml` — moved from `internal/config/config.toml`
- `internal/assets/embed/models.toml` — moved from `internal/config/models.toml`
- `internal/assets/embed/system.md` — moved from `internal/agent/prompts/system.md`
- `internal/assets/embed/skills/*.md` — moved from `internal/skills/builtin/*.md` (7 files)
- `internal/assets/assets.go` — `Dir()`, `Bundle`, `Load()`, `Reset()`, `Export()`, `Path()`, embedded FS
- `internal/assets/materialize.go` — mirror the embed tree onto disk, skip-if-exists
- `internal/assets/config.go` — `Config` + `LoadConfig`/`SaveConfig`/`DefaultConfig` (moved from `internal/config/config_toml.go`)
- `internal/assets/models.go` — `Catalog`/`Provider`/`Model` + `LoadModels`/`DefaultCatalog` (moved from `internal/config/config.go`)
- `internal/assets/skill.go` — `Skill`, `Source`, frontmatter parsing, skill loading + validation
- `internal/assets/system.go` — system-prompt loading + validation
- `internal/assets/report.go` — `Report`, `AssetIssue`
- `internal/assets/*_test.go` — one test file per source file above

**Modified:**

- `internal/config/config.go`, `internal/config/config_toml.go` — replaced by the shim (Task 1), deleted (Task 16)
- `internal/skills/loader.go` — tools rebuilt on `[]assets.Skill` (Task 10)
- `internal/agent/prompts.go` — `BuildSystemPrompt` takes the template string (Task 11)
- `internal/agent/session.go` — add `SetSystemPrompt` (Task 14)
- `internal/tui/commands.go` — register `/skills` and `/config` (Tasks 12–13)
- `internal/tui/app.go` — `/skills`, `/config` handlers; `/reload` extension (Tasks 12–14)
- `cmd/fova/main.go`, `cmd/fova/replay.go` — `assets.Load()` wiring (Task 15)
- `internal/llm/modelregistry.go` + all test files importing `internal/config` (Task 16)

**Deleted (Task 16):** `internal/config/`, `internal/skills/builtin/`, `internal/agent/prompts/`.

---

## Phase 1 — Foundation

### Task 1: Move `config`/`models` into `internal/assets`, add the shim

Establishes the new package while keeping every existing importer compiling, via a type-alias re-export shim.

**Files:**
- Create dir: `internal/assets/embed/`
- Move: `internal/config/config.toml` → `internal/assets/embed/config.toml`
- Move: `internal/config/models.toml` → `internal/assets/embed/models.toml`
- Create: `internal/assets/config.go` (from `internal/config/config_toml.go`)
- Create: `internal/assets/models.go` (from `internal/config/config.go`)
- Create: `internal/assets/assets.go` (just `Dir()` for now)
- Create: `internal/assets/config_test.go`, `internal/assets/models_test.go` (from the old test files)
- Modify: `internal/config/config.go`, `internal/config/config_toml.go` → the shim

- [ ] **Step 1: Move the embed files and source files.**

```bash
mkdir -p internal/assets/embed
git mv internal/config/config.toml   internal/assets/embed/config.toml
git mv internal/config/models.toml   internal/assets/embed/models.toml
git mv internal/config/config_toml.go internal/assets/config.go
git mv internal/config/config.go      internal/assets/models.go
git mv internal/config/config_toml_test.go internal/assets/config_test.go
git mv internal/config/config_test.go internal/assets/models_test.go
```

- [ ] **Step 2: Rewrite headers in the moved files.**

In `internal/assets/config.go` and `internal/assets/models.go`: change `package config` → `package assets`. Change the embed directives: `//go:embed config.toml` → `//go:embed embed/config.toml`, `//go:embed models.toml` → `//go:embed embed/models.toml`. In `internal/assets/models.go`, rename the function `ConfigDir` → `Dir` (keep the body identical) and update its doc comment to read `// Dir returns fova's config directory: $FOVA_CONFIG_DIR if set, otherwise ~/.config/fova.` Update every internal reference to `ConfigDir()` within these two files to `Dir()`. In the two test files: change `package config` → `package assets`, and any `ConfigDir(` → `Dir(`.

- [ ] **Step 3: Create `internal/assets/assets.go` with the package doc.**

```go
// Package assets owns every materializable, user-editable asset fova ships:
// config.toml, models.toml, the system prompt, and skills. All four
// materialize into Dir() on first run, are validated by one engine, and are
// editable and resettable through the /skills and /config TUI commands.
package assets
```

(`Dir()` lives in `models.go` after Step 2; `assets.go` grows in later tasks.)

- [ ] **Step 4: Replace `internal/config` with the shim.**

Delete `internal/config/config.go` and `internal/config/config_toml.go` content; create a single `internal/config/config.go`:

```go
// Package config is a backward-compatibility shim. Every symbol re-exports
// internal/assets; new code must import internal/assets directly. This
// package is deleted once all importers are migrated (configurable-assets
// plan, Task 16).
package config

import "github.com/alvarogonjim/fova/internal/assets"

type (
	Config          = assets.Config
	UIConfig        = assets.UIConfig
	DefaultsConfig  = assets.DefaultsConfig
	KnowledgeConfig = assets.KnowledgeConfig
	WebhookConfig   = assets.WebhookConfig
	BudgetConfig    = assets.BudgetConfig
	Catalog         = assets.Catalog
	Provider        = assets.Provider
	Model           = assets.Model
)

// ConfigDir is the pre-rename name of assets.Dir.
func ConfigDir() string { return assets.Dir() }

var (
	LoadConfig     = assets.LoadConfig
	SaveConfig     = assets.SaveConfig
	DefaultConfig  = assets.DefaultConfig
	LoadModels     = assets.LoadModels
	DefaultCatalog = assets.DefaultCatalog
)
```

- [ ] **Step 5: Verify the build and tests are green.**

Run: `go build ./... && go test ./internal/assets/... ./internal/config/...`
Expected: build PASS; the moved tests PASS unchanged (they exercise `Dir`, `LoadConfig`, `LoadModels`, etc. in the `assets` package).

- [ ] **Step 6: Verify every old importer still compiles through the shim.**

Run: `go build ./...`
Expected: PASS — `internal/llm`, `internal/tui`, `cmd/fova` still import `internal/config` and resolve through the aliases.

- [ ] **Step 7: Commit.**

```bash
git add -A
git commit -m "refactor(assets): move config/models into internal/assets behind a shim"
```

---

### Task 2: `Report` type

**Files:**
- Create: `internal/assets/report.go`
- Create: `internal/assets/report_test.go`

- [ ] **Step 1: Write the failing test.**

`internal/assets/report_test.go`:

```go
package assets

import "testing"

func TestReportEmptyByDefault(t *testing.T) {
	var r Report
	if !r.OK() {
		t.Fatal("zero Report should be OK")
	}
	if r.Summary() != "" {
		t.Fatalf("zero Report summary should be empty, got %q", r.Summary())
	}
}

func TestReportSummaryCountsIssues(t *testing.T) {
	r := Report{
		Errors:   []AssetIssue{{Asset: "skills/bad.md", Message: "boom"}},
		Warnings: []AssetIssue{{Asset: "system.md", Message: "no Refusals section"}},
	}
	if r.OK() {
		t.Fatal("Report with errors must not be OK")
	}
	got := r.Summary()
	want := "1 error, 1 warning in fova config — run /skills validate and /config validate"
	if got != want {
		t.Fatalf("Summary = %q, want %q", got, want)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails.**

Run: `go test ./internal/assets/ -run TestReport -v`
Expected: FAIL — `undefined: Report`.

- [ ] **Step 3: Implement `internal/assets/report.go`.**

```go
package assets

import "fmt"

// AssetIssue is one validation problem found while loading an asset.
type AssetIssue struct {
	Asset   string // relative asset name, e.g. "skills/foo.md", "system.md"
	Message string
}

// Report is the unified validation result for one Load(). Errors are problems
// that made an asset unusable (a skipped skill, a system.md that fell back to
// the embedded default); Warnings are advisory.
type Report struct {
	Errors   []AssetIssue
	Warnings []AssetIssue
}

// OK reports whether the Report carries no errors.
func (r Report) OK() bool { return len(r.Errors) == 0 }

// Summary is a one-line description for the startup banner, empty when the
// Report is clean.
func (r Report) Summary() string {
	if len(r.Errors) == 0 && len(r.Warnings) == 0 {
		return ""
	}
	return fmt.Sprintf("%s, %s in fova config — run /skills validate and /config validate",
		plural(len(r.Errors), "error"), plural(len(r.Warnings), "warning"))
}

func plural(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}
```

- [ ] **Step 4: Run the test to verify it passes.**

Run: `go test ./internal/assets/ -run TestReport -v`
Expected: PASS.

- [ ] **Step 5: Commit.**

```bash
git add internal/assets/report.go internal/assets/report_test.go
git commit -m "feat(assets): add the unified validation Report type"
```

---

### Task 3: Materialization engine

Mirrors the embedded `embed/` tree onto disk, never overwriting an existing file.

**Files:**
- Create: `internal/assets/materialize.go`
- Create: `internal/assets/materialize_test.go`

- [ ] **Step 1: Write the failing test.**

`internal/assets/materialize_test.go`:

```go
package assets

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMaterializeWritesMissingFiles(t *testing.T) {
	dir := t.TempDir()
	if err := materializeAssets(dir); err != nil {
		t.Fatalf("materializeAssets: %v", err)
	}
	for _, rel := range []string{"config.toml", "models.toml", "system.md", "skills/design-binder.md"} {
		if _, err := os.Stat(filepath.Join(dir, rel)); err != nil {
			t.Errorf("expected %s materialized: %v", rel, err)
		}
	}
}

func TestMaterializeNeverOverwrites(t *testing.T) {
	dir := t.TempDir()
	skills := filepath.Join(dir, "skills")
	if err := os.MkdirAll(skills, 0o755); err != nil {
		t.Fatal(err)
	}
	custom := filepath.Join(skills, "design-binder.md")
	if err := os.WriteFile(custom, []byte("EDITED BY USER"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := materializeAssets(dir); err != nil {
		t.Fatalf("materializeAssets: %v", err)
	}
	body, _ := os.ReadFile(custom)
	if string(body) != "EDITED BY USER" {
		t.Fatalf("materialize overwrote a user-edited file: %q", body)
	}
}

func TestMaterializeSecondRunIsNoop(t *testing.T) {
	dir := t.TempDir()
	if err := materializeAssets(dir); err != nil {
		t.Fatal(err)
	}
	if err := materializeAssets(dir); err != nil {
		t.Fatalf("second materializeAssets: %v", err)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails.**

Run: `go test ./internal/assets/ -run TestMaterialize -v`
Expected: FAIL — `undefined: materializeAssets`.

- [ ] **Step 3: Copy the skill and system-prompt defaults into the embed tree.**

Copy — do **not** move — these files. `internal/skills/builtin/` and `internal/agent/prompts/system.md` are still embedded by `internal/skills` and `internal/agent` until Phase 2, so the originals must stay until Tasks 10 and 11 delete them. Copying keeps every package compiling at every Phase 1 commit.

```bash
mkdir -p internal/assets/embed/skills
cp internal/skills/builtin/*.md internal/assets/embed/skills/
cp internal/agent/prompts/system.md internal/assets/embed/system.md
git add internal/assets/embed
```

- [ ] **Step 4: Implement `internal/assets/materialize.go`.**

```go
package assets

import (
	"embed"
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// embeddedFS is the single source-of-truth tree of default assets, mirrored
// onto disk by materializeAssets.
//
//go:embed embed
var embeddedFS embed.FS

// materializeAssets mirrors the embedded default tree into dir. A file that
// already exists on disk is never touched — only missing files are written,
// so user edits and user-authored skills always survive.
func materializeAssets(dir string) error {
	return fs.WalkDir(embeddedFS, "embed", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel := strings.TrimPrefix(strings.TrimPrefix(p, "embed"), "/")
		if rel == "" {
			return nil
		}
		dst := filepath.Join(dir, filepath.FromSlash(rel))
		if d.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		if _, statErr := os.Stat(dst); statErr == nil {
			return nil // already on disk — never overwrite
		} else if !errors.Is(statErr, fs.ErrNotExist) {
			return statErr
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		body, err := embeddedFS.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(dst, body, 0o644)
	})
}

// embeddedBytes returns the embedded default for an asset path relative to the
// embed root (e.g. "system.md", "skills/design-binder.md").
func embeddedBytes(rel string) ([]byte, bool) {
	b, err := embeddedFS.ReadFile(path.Join("embed", rel))
	if err != nil {
		return nil, false
	}
	return b, true
}
```

- [ ] **Step 5: Run the test to verify it passes.**

Run: `go test ./internal/assets/ -run TestMaterialize -v`
Expected: PASS.

- [ ] **Step 6: Commit.**

```bash
git add -A
git commit -m "feat(assets): add the embed-tree materialization engine"
```

---

### Task 4: `Skill` type and frontmatter parser

**Files:**
- Create: `internal/assets/skill.go`
- Create: `internal/assets/skill_test.go`
- Modify: `go.mod` (promote `gopkg.in/yaml.v3` to a direct dependency)

- [ ] **Step 1: Write the failing test.**

`internal/assets/skill_test.go`:

```go
package assets

import "testing"

func TestParseFrontmatterNone(t *testing.T) {
	name, desc, unknown, body, err := parseFrontmatter([]byte("# Skill: x\n\nhello"))
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if name != "" || desc != "" || len(unknown) != 0 {
		t.Fatalf("no-frontmatter file yielded meta: name=%q desc=%q unknown=%v", name, desc, unknown)
	}
	if body != "# Skill: x\n\nhello" {
		t.Fatalf("body = %q", body)
	}
}

func TestParseFrontmatterPresent(t *testing.T) {
	src := "---\nname: design-binder\ndescription: De novo binders\n---\n# Skill: x\nbody"
	name, desc, unknown, body, err := parseFrontmatter([]byte(src))
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if name != "design-binder" || desc != "De novo binders" {
		t.Fatalf("name=%q desc=%q", name, desc)
	}
	if len(unknown) != 0 {
		t.Fatalf("unexpected unknown keys: %v", unknown)
	}
	if body != "# Skill: x\nbody" {
		t.Fatalf("body = %q", body)
	}
}

func TestParseFrontmatterUnknownKey(t *testing.T) {
	src := "---\nname: x\nauthor: nobody\n---\nbody"
	_, _, unknown, _, err := parseFrontmatter([]byte(src))
	if err != nil {
		t.Fatalf("unknown key must be a warning, not an error: %v", err)
	}
	if len(unknown) != 1 || unknown[0] != "author" {
		t.Fatalf("unknown = %v, want [author]", unknown)
	}
}

func TestParseFrontmatterUnclosed(t *testing.T) {
	if _, _, _, _, err := parseFrontmatter([]byte("---\nname: x\nbody")); err == nil {
		t.Fatal("an unclosed frontmatter fence must be an error")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails.**

Run: `go test ./internal/assets/ -run TestParseFrontmatter -v`
Expected: FAIL — `undefined: parseFrontmatter`.

- [ ] **Step 3: Implement `internal/assets/skill.go`.**

```go
package assets

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// Source classifies a loaded skill by its relationship to the embedded set.
type Source int

const (
	SourceUser            Source = iota // no embedded counterpart
	SourceBuiltin                       // on-disk bytes match the embedded copy
	SourceBuiltinModified               // embedded counterpart exists, on-disk bytes differ
)

// String renders a Source for /skills list.
func (s Source) String() string {
	switch s {
	case SourceBuiltin:
		return "built-in"
	case SourceBuiltinModified:
		return "built-in*"
	default:
		return "user"
	}
}

// Skill is one loaded skill markdown file.
type Skill struct {
	Name        string // frontmatter name, else the filename stem
	Description string // frontmatter description, else ""
	Body        string // markdown after the frontmatter block
	Path        string // absolute on-disk path
	Source      Source
}

// parseFrontmatter splits a skill file into optional YAML frontmatter and the
// markdown body. A file not beginning with a "---" fence has no frontmatter.
// Unknown frontmatter keys are returned (for a warning), not an error.
func parseFrontmatter(src []byte) (name, description string, unknownKeys []string, body string, err error) {
	s := string(src)
	if !strings.HasPrefix(s, "---\n") && !strings.HasPrefix(s, "---\r\n") {
		return "", "", nil, s, nil
	}
	rest := s[strings.IndexByte(s, '\n')+1:]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return "", "", nil, "", fmt.Errorf("frontmatter opened with --- but was never closed")
	}
	yamlText := rest[:end]
	afterFence := rest[end+1:] // begins at the closing "---" line
	if nl := strings.IndexByte(afterFence, '\n'); nl >= 0 {
		body = afterFence[nl+1:]
	}
	var raw map[string]any
	if err := yaml.Unmarshal([]byte(yamlText), &raw); err != nil {
		return "", "", nil, "", fmt.Errorf("invalid frontmatter YAML: %w", err)
	}
	for k := range raw {
		if k != "name" && k != "description" {
			unknownKeys = append(unknownKeys, k)
		}
	}
	name, _ = raw["name"].(string)
	description, _ = raw["description"].(string)
	return name, description, unknownKeys, body, nil
}
```

- [ ] **Step 4: Run the test to verify it passes.**

Run: `go test ./internal/assets/ -run TestParseFrontmatter -v`
Expected: PASS.

- [ ] **Step 5: Promote `yaml.v3` to a direct dependency.**

Run: `go mod tidy`
Expected: in `go.mod`, the `gopkg.in/yaml.v3` line loses its `// indirect` comment (it is now imported directly by `internal/assets/skill.go`).

- [ ] **Step 6: Commit.**

```bash
git add internal/assets/skill.go internal/assets/skill_test.go go.mod go.sum
git commit -m "feat(assets): add the Skill type and YAML frontmatter parser"
```

---

### Task 5: Skill loading and validation

**Files:**
- Modify: `internal/assets/skill.go`
- Modify: `internal/assets/skill_test.go`

- [ ] **Step 1: Write the failing test (append to `skill_test.go`).**

```go
import (
	"os"
	"path/filepath"
)

func writeSkill(t *testing.T, dir, file, content string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, file), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadSkillsPlainMarkdown(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, dir, "filter.md", "# Skill: filtering\nbody")
	skills, rep := loadSkills(dir)
	if len(skills) != 1 {
		t.Fatalf("want 1 skill, got %d (report: %+v)", len(skills), rep)
	}
	if skills[0].Name != "filter" || skills[0].Description != "" {
		t.Fatalf("name=%q desc=%q", skills[0].Name, skills[0].Description)
	}
	if !rep.OK() {
		t.Fatalf("plain markdown should validate clean: %+v", rep)
	}
}

func TestLoadSkillsFrontmatterNameMustMatchFilename(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, dir, "alpha.md", "---\nname: beta\n---\nbody")
	skills, rep := loadSkills(dir)
	if len(skills) != 0 {
		t.Fatalf("a name/filename mismatch must skip the file, got %d skills", len(skills))
	}
	if rep.OK() {
		t.Fatal("expected a validation error for the name mismatch")
	}
}

func TestLoadSkillsRejectsNonKebabName(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, dir, "Bad_Name.md", "body")
	skills, rep := loadSkills(dir)
	if len(skills) != 0 || rep.OK() {
		t.Fatalf("a non-kebab filename stem must be an error; got %d skills, ok=%v", len(skills), rep.OK())
	}
}

func TestLoadSkillsEmptyBodyIsError(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, dir, "empty.md", "---\nname: empty\n---\n   \n")
	skills, rep := loadSkills(dir)
	if len(skills) != 0 || rep.OK() {
		t.Fatal("an empty body must be an error")
	}
}

func TestLoadSkillsUnknownKeyIsWarningNotError(t *testing.T) {
	dir := t.TempDir()
	writeSkill(t, dir, "ok.md", "---\nname: ok\nauthor: x\n---\nreal body")
	skills, rep := loadSkills(dir)
	if len(skills) != 1 {
		t.Fatalf("an unknown key must not skip the file; got %d skills", len(skills))
	}
	if !rep.OK() || len(rep.Warnings) != 1 {
		t.Fatalf("expected exactly one warning, report=%+v", rep)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails.**

Run: `go test ./internal/assets/ -run TestLoadSkills -v`
Expected: FAIL — `undefined: loadSkills`.

- [ ] **Step 3: Implement `loadSkills` (append to `internal/assets/skill.go`).**

Add `os`, `path/filepath`, `regexp`, `sort` to the imports.

```go
// kebabRE matches a valid skill name: lowercase words joined by single dashes.
var kebabRE = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// loadSkills reads every *.md file in skillsDir, parses and validates each,
// and returns the valid skills sorted by name. A file that fails validation
// is skipped and recorded as a Report error; advisory problems are warnings.
// A missing skillsDir yields no skills and no error.
func loadSkills(skillsDir string) ([]Skill, Report) {
	var (
		out  []Skill
		rep  Report
		seen = map[string]string{} // name -> file that claimed it
	)
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		if !os.IsNotExist(err) {
			rep.Errors = append(rep.Errors, AssetIssue{"skills/", err.Error()})
		}
		return nil, rep
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		file := e.Name()
		asset := "skills/" + file
		full := filepath.Join(skillsDir, file)
		raw, err := os.ReadFile(full)
		if err != nil {
			rep.Errors = append(rep.Errors, AssetIssue{asset, err.Error()})
			continue
		}
		if !utf8.Valid(raw) {
			rep.Errors = append(rep.Errors, AssetIssue{asset, "file is not valid UTF-8"})
			continue
		}
		stem := strings.TrimSuffix(file, ".md")
		if !kebabRE.MatchString(stem) {
			rep.Errors = append(rep.Errors, AssetIssue{asset,
				"filename stem must be kebab-case (lowercase, digits, single dashes)"})
			continue
		}
		fmName, fmDesc, unknown, body, err := parseFrontmatter(raw)
		if err != nil {
			rep.Errors = append(rep.Errors, AssetIssue{asset, err.Error()})
			continue
		}
		if fmName != "" && fmName != stem {
			rep.Errors = append(rep.Errors, AssetIssue{asset,
				fmt.Sprintf("frontmatter name %q must equal the filename stem %q", fmName, stem)})
			continue
		}
		if strings.TrimSpace(body) == "" {
			rep.Errors = append(rep.Errors, AssetIssue{asset, "skill body is empty"})
			continue
		}
		for _, k := range unknown {
			rep.Warnings = append(rep.Warnings, AssetIssue{asset, "unknown frontmatter key: " + k})
		}
		if strings.ContainsRune(fmDesc, '\n') {
			rep.Warnings = append(rep.Warnings, AssetIssue{asset, "description should be a single line"})
		}
		if len(fmDesc) > 120 {
			rep.Warnings = append(rep.Warnings, AssetIssue{asset, "description exceeds 120 characters"})
		}
		if prev, dup := seen[stem]; dup {
			rep.Errors = append(rep.Errors, AssetIssue{asset,
				fmt.Sprintf("duplicate skill name %q (also defined by %s)", stem, prev)})
			continue
		}
		seen[stem] = file
		out = append(out, Skill{
			Name:        stem,
			Description: fmDesc,
			Body:        body,
			Path:        full,
			Source:      skillSource(file, raw),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, rep
}

// skillSource compares an on-disk skill file to its embedded counterpart.
func skillSource(file string, onDisk []byte) Source {
	emb, ok := embeddedBytes("skills/" + file)
	if !ok {
		return SourceUser
	}
	if string(emb) == string(onDisk) {
		return SourceBuiltin
	}
	return SourceBuiltinModified
}
```

Add `"unicode/utf8"` to the imports.

- [ ] **Step 4: Run the test to verify it passes.**

Run: `go test ./internal/assets/ -run TestLoadSkills -v`
Expected: PASS.

- [ ] **Step 5: Commit.**

```bash
git add internal/assets/skill.go internal/assets/skill_test.go
git commit -m "feat(assets): load and validate skill files from disk"
```

---

### Task 6: System-prompt loading and validation

**Files:**
- Create: `internal/assets/system.go`
- Create: `internal/assets/system_test.go`

- [ ] **Step 1: Write the failing test.**

`internal/assets/system_test.go`:

```go
package assets

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultSystemPromptHasMarkerAndPreamble(t *testing.T) {
	p := DefaultSystemPrompt()
	if !strings.Contains(p, "{{COMMAND_CATALOGUE}}") {
		t.Error("embedded system.md is missing the {{COMMAND_CATALOGUE}} marker")
	}
	if !strings.Contains(p, "You are fova") {
		t.Error("embedded system.md is missing the preamble")
	}
	if !strings.Contains(p, "Do not invoke `jobs.cancel`") {
		t.Error("embedded system.md is missing the long-running-job rule")
	}
}

func TestLoadSystemPromptValidFile(t *testing.T) {
	dir := t.TempDir()
	good := "You are fova.\n## Refusals\nno\n## Tone\nbrief\n{{COMMAND_CATALOGUE}}\n"
	if err := os.WriteFile(filepath.Join(dir, "system.md"), []byte(good), 0o644); err != nil {
		t.Fatal(err)
	}
	prompt, rep := loadSystemPrompt(dir)
	if prompt != good {
		t.Fatalf("prompt = %q, want the on-disk file", prompt)
	}
	if !rep.OK() || len(rep.Warnings) != 0 {
		t.Fatalf("a valid system.md should produce a clean report: %+v", rep)
	}
}

func TestLoadSystemPromptMissingMarkerFallsBack(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "system.md"), []byte("no marker here"), 0o644); err != nil {
		t.Fatal(err)
	}
	prompt, rep := loadSystemPrompt(dir)
	if prompt != DefaultSystemPrompt() {
		t.Fatal("a system.md missing the marker must fall back to the embedded default")
	}
	if rep.OK() {
		t.Fatal("expected an error for the missing marker")
	}
}

func TestLoadSystemPromptMissingFileFallsBack(t *testing.T) {
	prompt, rep := loadSystemPrompt(t.TempDir())
	if prompt != DefaultSystemPrompt() {
		t.Fatal("a missing system.md must fall back to the embedded default")
	}
	if rep.OK() {
		t.Fatal("expected an error for the missing file")
	}
}

func TestLoadSystemPromptWarnsOnMissingRefusals(t *testing.T) {
	dir := t.TempDir()
	body := "You are fova.\n{{COMMAND_CATALOGUE}}\n"
	if err := os.WriteFile(filepath.Join(dir, "system.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	_, rep := loadSystemPrompt(dir)
	if !rep.OK() || len(rep.Warnings) == 0 {
		t.Fatalf("expected a warning (not an error) for the missing Refusals/Tone section: %+v", rep)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails.**

Run: `go test ./internal/assets/ -run "TestDefaultSystemPrompt|TestLoadSystemPrompt" -v`
Expected: FAIL — `undefined: DefaultSystemPrompt`, `undefined: loadSystemPrompt`.

- [ ] **Step 3: Implement `internal/assets/system.go`.**

```go
package assets

import (
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// DefaultSystemPrompt returns the embedded system.md template (no disk
// access). It is the fallback used when the on-disk system.md is invalid.
func DefaultSystemPrompt() string {
	b, ok := embeddedBytes("system.md")
	if !ok {
		panic("embedded system.md is missing from the binary")
	}
	return string(b)
}

// loadSystemPrompt reads <dir>/system.md and validates it. A missing or
// invalid file degrades to DefaultSystemPrompt() plus a Report error — the
// agent must always have a working prompt. A missing Refusals/Tone section
// is a warning, not an error.
func loadSystemPrompt(dir string) (string, Report) {
	var rep Report
	path := filepath.Join(dir, "system.md")
	raw, err := os.ReadFile(path)
	if err != nil {
		rep.Errors = append(rep.Errors, AssetIssue{"system.md",
			"could not read system.md, using the built-in prompt: " + err.Error()})
		return DefaultSystemPrompt(), rep
	}
	if !utf8.Valid(raw) {
		rep.Errors = append(rep.Errors, AssetIssue{"system.md",
			"system.md is not valid UTF-8, using the built-in prompt"})
		return DefaultSystemPrompt(), rep
	}
	text := string(raw)
	if strings.TrimSpace(text) == "" {
		rep.Errors = append(rep.Errors, AssetIssue{"system.md",
			"system.md is empty, using the built-in prompt"})
		return DefaultSystemPrompt(), rep
	}
	if n := strings.Count(text, "{{COMMAND_CATALOGUE}}"); n != 1 {
		rep.Errors = append(rep.Errors, AssetIssue{"system.md",
			"system.md must contain exactly one {{COMMAND_CATALOGUE}} marker, using the built-in prompt"})
		return DefaultSystemPrompt(), rep
	}
	if !strings.Contains(text, "Refus") && !strings.Contains(text, "Tone") {
		rep.Warnings = append(rep.Warnings, AssetIssue{"system.md",
			"system.md has no Refusals or Tone section — recommended for safe agent behaviour"})
	}
	return text, rep
}
```

- [ ] **Step 4: Run the test to verify it passes.**

Run: `go test ./internal/assets/ -run "TestDefaultSystemPrompt|TestLoadSystemPrompt" -v`
Expected: PASS.

- [ ] **Step 5: Commit.**

```bash
git add internal/assets/system.go internal/assets/system_test.go
git commit -m "feat(assets): load and validate the system prompt from disk"
```

---

### Task 7: `Bundle`, `Load`, `Reset`, `Export`, `Path`

**Files:**
- Modify: `internal/assets/assets.go`
- Create: `internal/assets/assets_test.go`

- [ ] **Step 1: Write the failing test.**

`internal/assets/assets_test.go`:

```go
package assets

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMaterializesAndParses(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("FOVA_CONFIG_DIR", dir)

	b, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(b.Models.Models) == 0 {
		t.Error("Bundle.Models is empty")
	}
	if len(b.Skills) != 7 {
		t.Errorf("want 7 built-in skills, got %d", len(b.Skills))
	}
	if b.SystemPrompt == "" {
		t.Error("Bundle.SystemPrompt is empty")
	}
	if !b.Report.OK() {
		t.Errorf("first-run Load should be clean: %+v", b.Report)
	}
	for _, rel := range []string{"config.toml", "models.toml", "system.md", "skills/design-binder.md"} {
		if _, err := os.Stat(filepath.Join(dir, rel)); err != nil {
			t.Errorf("%s not materialized: %v", rel, err)
		}
	}
}

func TestLoadReportsBadSkillButKeepsGoing(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("FOVA_CONFIG_DIR", dir)
	if _, err := Load(); err != nil { // first run materializes the 7 built-ins
		t.Fatal(err)
	}
	bad := filepath.Join(dir, "skills", "Bad Name.md")
	if err := os.WriteFile(bad, []byte("body"), 0o644); err != nil {
		t.Fatal(err)
	}
	b, err := Load()
	if err != nil {
		t.Fatalf("Load must not hard-fail on a bad skill: %v", err)
	}
	if len(b.Skills) != 7 {
		t.Errorf("the 7 good skills should still load, got %d", len(b.Skills))
	}
	if b.Report.OK() {
		t.Error("expected a Report error for the bad skill file")
	}
}

func TestResetRestoresEmbeddedSkill(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("FOVA_CONFIG_DIR", dir)
	if _, err := Load(); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "skills", "design-binder.md")
	if err := os.WriteFile(p, []byte("WRECKED"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Reset("skills/design-binder"); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	body, _ := os.ReadFile(p)
	if string(body) == "WRECKED" {
		t.Fatal("Reset did not restore the embedded skill")
	}
}

func TestResetRejectsUserSkill(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("FOVA_CONFIG_DIR", dir)
	if err := Reset("skills/my-custom-skill"); err == nil {
		t.Fatal("Reset must reject a skill with no embedded counterpart")
	}
}

func TestPathResolvesAssetKeys(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("FOVA_CONFIG_DIR", dir)
	cases := map[string]string{
		"config": filepath.Join(dir, "config.toml"),
		"models": filepath.Join(dir, "models.toml"),
		"system": filepath.Join(dir, "system.md"),
		"skills/foo": filepath.Join(dir, "skills", "foo.md"),
	}
	for key, want := range cases {
		if got := Path(key); got != want {
			t.Errorf("Path(%q) = %q, want %q", key, got, want)
		}
	}
}
```

- [ ] **Step 2: Run the test to verify it fails.**

Run: `go test ./internal/assets/ -run "TestLoad|TestReset|TestPath" -v`
Expected: FAIL — `undefined: Load`, `Bundle`, `Reset`, `Path`.

- [ ] **Step 3: Implement the `Bundle` API (append to `internal/assets/assets.go`).**

```go
import (
	"fmt"
	"path/filepath"
	"strings"
)

// Bundle is the entire on-disk asset state, loaded once at startup.
type Bundle struct {
	Config       Config
	Models       Catalog
	Skills       []Skill
	SystemPrompt string // raw system.md template — still contains {{COMMAND_CATALOGUE}}
	Report       Report
}

// Load materializes any missing asset into Dir(), then parses and validates
// every asset. A malformed config.toml or models.toml is a returned error
// (fail-hard); a malformed system.md or skill file is degraded to a Report
// entry while Load still succeeds.
func Load() (*Bundle, error) {
	dir := Dir()
	if err := materializeAssets(dir); err != nil {
		// Materialization failure degrades to all-embedded defaults.
		return embeddedBundle(err), nil
	}
	cfg, err := LoadConfig()
	if err != nil {
		return nil, err
	}
	cat, err := LoadModels()
	if err != nil {
		return nil, err
	}
	skills, skillRep := loadSkills(filepath.Join(dir, "skills"))
	prompt, sysRep := loadSystemPrompt(dir)
	return &Bundle{
		Config:       cfg,
		Models:       cat,
		Skills:       skills,
		SystemPrompt: prompt,
		Report:       mergeReports(skillRep, sysRep),
	}, nil
}

// embeddedBundle builds a Bundle entirely from embedded defaults, used when
// the config directory cannot be materialized.
func embeddedBundle(cause error) *Bundle {
	rep := Report{Errors: []AssetIssue{{"~/.config/fova",
		"could not materialize the config directory, using built-in defaults: " + cause.Error()}}}
	return &Bundle{
		Config:       DefaultConfig(),
		Models:       DefaultCatalog(),
		Skills:       embeddedSkills(),
		SystemPrompt: DefaultSystemPrompt(),
		Report:       rep,
	}
}

// embeddedSkills parses the 7 built-in skills straight from the embedded FS.
func embeddedSkills() []Skill {
	entries, _ := embeddedFS.ReadDir("embed/skills")
	out := make([]Skill, 0, len(entries))
	for _, e := range entries {
		raw, err := embeddedFS.ReadFile("embed/skills/" + e.Name())
		if err != nil {
			continue
		}
		name, desc, _, body, err := parseFrontmatter(raw)
		if err != nil {
			continue
		}
		stem := strings.TrimSuffix(e.Name(), ".md")
		if name == "" {
			name = stem
		}
		out = append(out, Skill{Name: name, Description: desc, Body: body, Source: SourceBuiltin})
	}
	return out
}

func mergeReports(a, b Report) Report {
	return Report{
		Errors:   append(append([]AssetIssue{}, a.Errors...), b.Errors...),
		Warnings: append(append([]AssetIssue{}, a.Warnings...), b.Warnings...),
	}
}

// assetRel maps an asset key ("config", "models", "system", "skills/<name>")
// to its path relative to Dir(), and ok=false for an unknown key.
func assetRel(name string) (rel string, ok bool) {
	switch name {
	case "config":
		return "config.toml", true
	case "models":
		return "models.toml", true
	case "system":
		return "system.md", true
	}
	if stem, found := strings.CutPrefix(name, "skills/"); found && stem != "" {
		return filepath.Join("skills", stem+".md"), true
	}
	return "", false
}

// Path returns an asset's absolute on-disk path without touching the file.
func Path(name string) string {
	rel, ok := assetRel(name)
	if !ok {
		return ""
	}
	return filepath.Join(Dir(), rel)
}

// Export ensures an asset exists on disk (materializing the whole tree if
// needed) and returns its absolute path.
func Export(name string) (string, error) {
	rel, ok := assetRel(name)
	if !ok {
		return "", fmt.Errorf("unknown asset %q", name)
	}
	if err := materializeAssets(Dir()); err != nil {
		return "", err
	}
	return filepath.Join(Dir(), rel), nil
}

// Reset restores one asset from its embedded default, overwriting the on-disk
// copy. A user-authored skill (no embedded counterpart) is rejected.
func Reset(name string) error {
	rel, ok := assetRel(name)
	if !ok {
		return fmt.Errorf("unknown asset %q", name)
	}
	emb, ok := embeddedBytes(filepath.ToSlash(rel))
	if !ok {
		return fmt.Errorf("%q has no built-in default to reset to", name)
	}
	dst := filepath.Join(Dir(), rel)
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dst, emb, 0o644)
}
```

Add `"os"` to the `assets.go` import block.

- [ ] **Step 4: Run the test to verify it passes.**

Run: `go test ./internal/assets/ -run "TestLoad|TestReset|TestPath" -v`
Expected: PASS.

- [ ] **Step 5: Run the whole `assets` package and the shim.**

Run: `go test ./internal/assets/... ./internal/config/...`
Expected: PASS.

- [ ] **Step 6: Commit.**

```bash
git add internal/assets/assets.go internal/assets/assets_test.go
git commit -m "feat(assets): add Bundle, Load, Reset, Export and Path"
```

---

### Task 8: Add frontmatter to the 7 built-in skills

**Files:**
- Modify: `internal/assets/embed/skills/design-binder.md`, `design-antibody.md`, `design-enzyme.md`, `filter-thresholds.md`, `plan-from-target.md`, `submit-to-adaptyv.md`, `close-the-loop.md`

- [ ] **Step 1: Prepend frontmatter to each file.**

For each file, insert these four lines at the very top, leaving the existing markdown body untouched below:

`design-binder.md`:
```markdown
---
name: design-binder
description: De novo protein binder design against non-antibody targets
---
```

`design-antibody.md`:
```markdown
---
name: design-antibody
description: De novo antibody design and humanisation
---
```

`design-enzyme.md`:
```markdown
---
name: design-enzyme
description: De novo enzyme design for a target reaction
---
```

`filter-thresholds.md`:
```markdown
---
name: filter-thresholds
description: Standard score cutoffs for shortlisting designs
---
```

`plan-from-target.md`:
```markdown
---
name: plan-from-target
description: Turn a target into a structured DesignPlan before running tools
---
```

`submit-to-adaptyv.md`:
```markdown
---
name: submit-to-adaptyv
description: Submit a design shortlist to the Adaptyv wet lab
---
```

`close-the-loop.md`:
```markdown
---
name: close-the-loop
description: Fold wet-lab results back into the next design round
---
```

- [ ] **Step 2: Add a regression test (append to `internal/assets/assets_test.go`).**

```go
func TestEmbeddedSkillsAllHaveDescriptions(t *testing.T) {
	for _, s := range embeddedSkills() {
		if s.Description == "" {
			t.Errorf("built-in skill %q has no frontmatter description", s.Name)
		}
	}
}
```

- [ ] **Step 3: Run the test to verify it passes.**

Run: `go test ./internal/assets/ -run TestEmbeddedSkills -v`
Expected: PASS — all 7 skills carry a description.

- [ ] **Step 4: Commit.**

```bash
git add internal/assets/embed/skills/ internal/assets/assets_test.go
git commit -m "feat(assets): add YAML frontmatter to the built-in skills"
```

---

### Task 9: Phase 1 gate — verify the foundation

**Files:** none changed.

- [ ] **Step 1: Build and test the whole module.**

Run: `go build ./... && go test ./...`
Expected: PASS. Because Task 3 copied (not moved) the skill/system defaults, every package — `internal/skills`, `internal/agent`, `internal/tui`, `cmd/fova` — still compiles and its existing tests still pass. The new `internal/assets` behaviour is exercised by its own suite.

- [ ] **Step 2: No commit (verification only).** Phase 1 is complete. Phase 2 (Task 10, Task 11, Tasks 12–14) may now proceed as three parallel agents — they touch the disjoint packages `internal/skills`, `internal/agent`, and `internal/tui`.

---

## Phase 2 — Consumers (parallelizable)

### Task 10: Rewire `internal/skills` onto `assets.Skill`

**Files:**
- Modify: `internal/skills/loader.go`
- Modify: `internal/skills/loader_test.go`

- [ ] **Step 1: Rewrite `internal/skills/loader.go`.**

Replace the whole file:

```go
// Package skills exposes loaded skills as the skills.list and skills.read
// agent tools. The skills themselves are loaded by internal/assets.
package skills

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/alvarogonjim/fova/internal/assets"
	"github.com/alvarogonjim/fova/internal/domain"
	"github.com/alvarogonjim/fova/internal/tools"
)

// Loader holds the loaded skills, keyed by name.
type Loader struct {
	skills map[string]assets.Skill
}

// NewLoader wraps an already-loaded skill set (from assets.Bundle.Skills).
func NewLoader(skills []assets.Skill) *Loader {
	m := make(map[string]assets.Skill, len(skills))
	for _, s := range skills {
		m[s.Name] = s
	}
	return &Loader{skills: m}
}

// Names returns the loaded skill names, sorted.
func (l *Loader) Names() []string {
	names := make([]string, 0, len(l.skills))
	for n := range l.skills {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// ListTool returns the skills.list tool.
func (l *Loader) ListTool() tools.Tool { return skillsList{l} }

// ReadTool returns the skills.read tool.
func (l *Loader) ReadTool() tools.Tool { return skillsRead{l} }

// --- skills.list ---

type skillsList struct{ l *Loader }

func (skillsList) Name() string        { return "skills.list" }
func (skillsList) Description() string { return "List available fova skills." }
func (skillsList) InputSchema() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{}}
}
func (skillsList) RequiresConfirmation(json.RawMessage) bool       { return false }
func (skillsList) EstimatedCostUSD(json.RawMessage) float64        { return 0 }
func (skillsList) EstimatedDuration(json.RawMessage) time.Duration { return time.Millisecond }
func (t skillsList) Execute(_ context.Context, input json.RawMessage) (tools.Result, error) {
	var b strings.Builder
	for _, n := range t.l.Names() {
		if d := t.l.skills[n].Description; d != "" {
			fmt.Fprintf(&b, "- %s — %s\n", n, d)
		} else {
			fmt.Fprintf(&b, "- %s\n", n)
		}
	}
	return tools.Result{
		Display:    b.String(),
		Provenance: domain.NewToolCallRef("skills.list", input),
	}, nil
}

// --- skills.read ---

type skillsRead struct{ l *Loader }

func (skillsRead) Name() string        { return "skills.read" }
func (skillsRead) Description() string { return "Read the full markdown of one skill by name." }
func (skillsRead) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{"type": "string", "description": "Skill name"},
		},
		"required": []string{"name"},
	}
}
func (skillsRead) RequiresConfirmation(json.RawMessage) bool       { return false }
func (skillsRead) EstimatedCostUSD(json.RawMessage) float64        { return 0 }
func (skillsRead) EstimatedDuration(json.RawMessage) time.Duration { return time.Millisecond }
func (t skillsRead) Execute(_ context.Context, input json.RawMessage) (tools.Result, error) {
	var in struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return tools.Result{}, err
	}
	s, ok := t.l.skills[in.Name]
	if !ok {
		return tools.Result{}, fmt.Errorf("unknown skill %q", in.Name)
	}
	return tools.Result{
		Display:    s.Body,
		Provenance: domain.NewToolCallRef("skills.read", input),
	}, nil
}
```

- [ ] **Step 2: Rewrite `internal/skills/loader_test.go`.**

```go
package skills

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/alvarogonjim/fova/internal/assets"
)

func testSkills() []assets.Skill {
	return []assets.Skill{
		{Name: "filter-thresholds", Description: "Standard score cutoffs", Body: "rank by ipSAE"},
		{Name: "design-binder", Description: "Binder design", Body: "use design.boltzgen"},
	}
}

func TestLoaderListsSkill(t *testing.T) {
	l := NewLoader(testSkills())
	found := false
	for _, n := range l.Names() {
		if n == "filter-thresholds" {
			found = true
		}
	}
	if !found {
		t.Fatalf("filter-thresholds not loaded; got %v", l.Names())
	}
}

func TestSkillsListToolShowsDescriptions(t *testing.T) {
	l := NewLoader(testSkills())
	res, err := l.ListTool().Execute(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Display, "filter-thresholds — Standard score cutoffs") {
		t.Fatalf("skills.list missing the description column: %q", res.Display)
	}
}

func TestSkillsReadToolReturnsBody(t *testing.T) {
	l := NewLoader(testSkills())
	res, err := l.ReadTool().Execute(context.Background(),
		json.RawMessage(`{"name":"filter-thresholds"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Display, "ipSAE") {
		t.Fatalf("skills.read returned wrong content: %q", res.Display)
	}
	if _, err := l.ReadTool().Execute(context.Background(),
		json.RawMessage(`{"name":"does-not-exist"}`)); err == nil {
		t.Fatal("reading an unknown skill should error")
	}
}
```

- [ ] **Step 3: Delete the orphaned built-in skill directory.**

The rewritten `loader.go` no longer embeds anything, so `internal/skills/builtin/` is now dead code.

```bash
git rm -r internal/skills/builtin
```

- [ ] **Step 4: Run the tests.**

Run: `go build ./... && go test ./internal/skills/...`
Expected: PASS.

- [ ] **Step 5: Commit.**

```bash
git add internal/skills/
git commit -m "feat(skills): build skills.list/read from assets.Skill set"
```

---

### Task 11: Rewire `internal/agent` system prompt

**Files:**
- Modify: `internal/agent/prompts.go`
- Modify: `internal/agent/prompts_test.go`
- Modify: `internal/agent/session_test.go`, `internal/agent/smoke_test.go`
- Delete: `internal/agent/prompts/` (the `system.md` already moved in Task 3)

- [ ] **Step 1: Change `BuildSystemPrompt` to take the template (`internal/agent/prompts.go`).**

Remove the `_ "embed"` import, the `//go:embed prompts/system.md` directive, the `systemPromptTemplate` var, and the `SystemPrompt` var. Change the builder:

```go
// BuildSystemPrompt renders template with cat substituted for the
// {{COMMAND_CATALOGUE}} marker. template is the system.md source loaded by
// internal/assets; a nil or empty cat yields an empty catalogue block.
func BuildSystemPrompt(cat []SlashCommand, template string) string {
	return strings.Replace(template, catalogueMarker, renderCatalogue(cat), 1)
}
```

Keep `SlashCommand`, `SlashSubcommand`, `catalogueMarker`, and `renderCatalogue` exactly as they are.

- [ ] **Step 2: Update `internal/agent/prompts_test.go`.**

These tests exercise the templating mechanism only; give them a local fake template (the system.md *content* assertions now live in `internal/assets/system_test.go`, Task 6). Replace `prompts_test.go`:

```go
package agent

import (
	"strings"
	"testing"
)

// fakeTemplate is a minimal system.md stand-in carrying the marker plus the
// grounding clause, enough to exercise BuildSystemPrompt's templating.
const fakeTemplate = "You are fova.\n" +
	"{{COMMAND_CATALOGUE}}\n" +
	"When suggesting next steps, refer to these commands literally. " +
	"Never invent a slash command. If a needed verb doesn't exist as a command, " +
	"tell the user to describe the change in plain English instead.\n"

func testCatalogue() []SlashCommand {
	return []SlashCommand{
		{Name: "model", Description: "switch the model (and its provider)"},
		{
			Name:        "plan",
			Description: "show or act on the current design plan",
			Subcommands: []SlashSubcommand{
				{Name: "approve", Description: "approve and commit the current design plan"},
				{Name: "cancel", Description: "discard the current design plan"},
			},
		},
		{Name: "doctor", Description: "diagnose the local tool environment"},
		{Name: "install", Description: "install a local design tool"},
		{Name: "auth", Description: "store an API token, e.g. /auth adaptyv <token>"},
	}
}

func TestSystemPromptContainsSlashCatalogue(t *testing.T) {
	prompt := BuildSystemPrompt(testCatalogue(), fakeTemplate)
	for _, want := range []string{"/model", "/plan", "/plan approve", "/plan cancel", "/doctor", "/install", "/auth"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("system prompt missing command %q; have:\n%s", want, prompt)
		}
	}
}

func TestSystemPromptRendersSubcommandDescriptions(t *testing.T) {
	prompt := BuildSystemPrompt(testCatalogue(), fakeTemplate)
	for _, want := range []string{"approve and commit the current design plan", "discard the current design plan"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("system prompt missing sub-command description %q", want)
		}
	}
}

func TestSystemPromptOmitsDynamicArgumentValues(t *testing.T) {
	prompt := BuildSystemPrompt(testCatalogue(), fakeTemplate)
	for _, forbidden := range []string{"/install bindcraft", "/install proteinmpnn", "/model qwen"} {
		if strings.Contains(prompt, forbidden) {
			t.Errorf("system prompt unexpectedly embeds dynamic value %q", forbidden)
		}
	}
}

func TestSystemPromptHasNoTemplateMarker(t *testing.T) {
	for _, cat := range [][]SlashCommand{nil, {}, testCatalogue()} {
		prompt := BuildSystemPrompt(cat, fakeTemplate)
		if strings.Contains(prompt, "{{COMMAND_CATALOGUE}}") {
			t.Errorf("template marker leaked for catalogue %+v", cat)
		}
	}
}

func TestSystemPromptForbidsInventingSlashCommands(t *testing.T) {
	prompt := BuildSystemPrompt(testCatalogue(), fakeTemplate)
	want := "Never invent a slash command."
	if !strings.Contains(prompt, want) {
		t.Errorf("grounding clause missing; expected %q", want)
	}
}
```

(The dropped tests `TestSystemPromptHasLongRunningJobRule` and `TestSystemPromptStillEmbeddedBaseText` asserted `system.md` *content* — that content is now covered by `TestDefaultSystemPromptHasMarkerAndPreamble` in `internal/assets/system_test.go`, which already asserts the `jobs.cancel` rule and the `You are fova` preamble.)

- [ ] **Step 3: Fix the remaining `SystemPrompt` references.**

In `internal/agent/session_test.go`, `TestSystemPromptEmbedded` references the deleted `SystemPrompt` var — replace it:

```go
func TestSystemPromptEmbedded(t *testing.T) {
	prompt := BuildSystemPrompt(nil, fakeTemplate)
	if !strings.Contains(prompt, "You are fova") {
		t.Errorf("base preamble missing from rendered prompt")
	}
}
```

In `internal/agent/smoke_test.go:50`, replace `NewSession(SystemPrompt)` with `NewSession(BuildSystemPrompt(nil, fakeTemplate))`.

- [ ] **Step 4: Delete the orphaned embedded system prompt.**

`prompts.go` no longer embeds `prompts/system.md`; the copy at `internal/assets/embed/system.md` is now the only one.

```bash
git rm internal/agent/prompts/system.md
```

- [ ] **Step 5: Run the tests.**

Run: `go test ./internal/agent/...`
Expected: PASS.

- [ ] **Step 6: Commit.**

```bash
git add -A internal/agent/
git commit -m "feat(agent): render the system prompt from a supplied template"
```

---

### Task 12: `/skills` command

**Files:**
- Modify: `internal/tui/commands.go`
- Create: `internal/tui/skillscmd.go`
- Create: `internal/tui/skillscmd_test.go`
- Modify: `internal/tui/app.go` (dispatch + Model fields)

- [ ] **Step 1: Register `/skills` in `internal/tui/commands.go`.**

In the `slashCommands` slice, replace nothing — add a new entry after the `reload` line:

```go
	{Name: "reload", Description: "reload config.toml and models.toml without restarting", Arguments: ArgsNone},
	{
		Name:        "skills",
		Description: "list, show, create, edit, validate or reset skills",
		Subcommands: []Subcommand{
			{Name: "list", Description: "list every loaded skill"},
			{Name: "show", Description: "print one skill's markdown: /skills show <name>"},
			{Name: "new", Description: "scaffold and open a new skill: /skills new <name>"},
			{Name: "edit", Description: "open a skill in $EDITOR: /skills edit <name>"},
			{Name: "validate", Description: "report skill validation errors and warnings"},
			{Name: "reset", Description: "restore a built-in skill: /skills reset <name>"},
			{Name: "path", Description: "print the skills directory path"},
		},
		Arguments: ArgsNone,
	},
```

Also remove `"skills"` from the placeholder case in `app.go` (Step 4).

- [ ] **Step 2: Add Model fields for the loaded bundle.**

In `internal/tui/app.go`, in the `Model` struct, add below `systemPrompt string`:

```go
	skillLoader *skills.Loader // backs the skills.list/read tools and /skills
	assetReport assets.Report  // validation Report from the last assets.Load()
```

Add to the imports of `app.go`: `"github.com/alvarogonjim/fova/internal/assets"` and `"github.com/alvarogonjim/fova/internal/skills"`.

In `Deps`, add:

```go
	SkillLoader *skills.Loader
	AssetReport assets.Report
```

In `New(d Deps)` where the Model is built, add `skillLoader: d.SkillLoader,` and `assetReport: d.AssetReport,`.

- [ ] **Step 3: Write the failing test `internal/tui/skillscmd_test.go`.**

```go
package tui

import (
	"strings"
	"testing"

	"github.com/alvarogonjim/fova/internal/assets"
)

func TestSkillsListRendersNamesAndSources(t *testing.T) {
	set := []assets.Skill{
		{Name: "design-binder", Description: "binders", Source: assets.SourceBuiltin},
		{Name: "my-skill", Description: "mine", Source: assets.SourceUser},
	}
	out := renderSkillsList(set)
	if !strings.Contains(out, "design-binder") || !strings.Contains(out, "built-in") {
		t.Errorf("missing built-in row:\n%s", out)
	}
	if !strings.Contains(out, "my-skill") || !strings.Contains(out, "user") {
		t.Errorf("missing user row:\n%s", out)
	}
}

func TestSkillsValidateRendersReport(t *testing.T) {
	rep := assets.Report{Errors: []assets.AssetIssue{{Asset: "skills/bad.md", Message: "boom"}}}
	out := renderAssetReport(rep, "skills")
	if !strings.Contains(out, "skills/bad.md") || !strings.Contains(out, "boom") {
		t.Errorf("report not rendered:\n%s", out)
	}
}
```

- [ ] **Step 4: (Tests run at Task 14.)** `internal/tui` does not compile standalone here — the `/config` helper (Task 13) and the reload/`editorFileDoneMsg` wiring (Task 14) are still missing. The `TestSkills*` tests written above are executed at Task 14, Step 6, when the package is complete. Tasks 12, 13 and 14 are implemented by one agent and committed once (Task 14, Step 7).

- [ ] **Step 5: Implement `internal/tui/skillscmd.go`.**

```go
package tui

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/alvarogonjim/fova/internal/assets"
)

// renderSkillsList formats the loaded skill set as an aligned table.
func renderSkillsList(set []assets.Skill) string {
	if len(set) == 0 {
		return "No skills loaded."
	}
	rows := append([]assets.Skill{}, set...)
	sort.Slice(rows, func(i, j int) bool { return rows[i].Name < rows[j].Name })
	width := 0
	for _, s := range rows {
		if len(s.Name) > width {
			width = len(s.Name)
		}
	}
	var b strings.Builder
	for _, s := range rows {
		desc := s.Description
		if desc == "" {
			desc = "(no description)"
		}
		fmt.Fprintf(&b, "%-*s  %-10s  %s\n", width, s.Name, s.Source.String(), desc)
	}
	return strings.TrimRight(b.String(), "\n")
}

// renderAssetReport formats the part of a Report relevant to scope ("skills"
// or "config"); scope filters by AssetIssue.Asset prefix.
func renderAssetReport(rep assets.Report, scope string) string {
	match := func(asset string) bool {
		if scope == "skills" {
			return strings.HasPrefix(asset, "skills/")
		}
		return !strings.HasPrefix(asset, "skills/")
	}
	var b strings.Builder
	for _, e := range rep.Errors {
		if match(e.Asset) {
			fmt.Fprintf(&b, "  error   %s: %s\n", e.Asset, e.Message)
		}
	}
	for _, w := range rep.Warnings {
		if match(w.Asset) {
			fmt.Fprintf(&b, "  warning %s: %s\n", w.Asset, w.Message)
		}
	}
	if b.Len() == 0 {
		return "No problems found."
	}
	return strings.TrimRight(b.String(), "\n")
}

// skillFrontmatterTemplate is the scaffold written by /skills new.
const skillFrontmatterTemplate = `---
name: %s
description: One-line summary shown in skills.list and /skills list.
---
# Skill: %s

## When to use

## Steps

`

// cmdSkills dispatches /skills and its sub-commands.
func (m *Model) cmdSkills(arg string) (tea.Model, tea.Cmd) {
	fields := strings.Fields(arg)
	sub := ""
	if len(fields) > 0 {
		sub = fields[0]
	}
	rest := strings.TrimSpace(strings.TrimPrefix(arg, sub))

	switch sub {
	case "", "list":
		m.chat.appendSlashOutput(renderSkillsList(m.loadedSkills()))
		return m, nil
	case "validate":
		m.chat.appendSlashOutput(renderAssetReport(m.assetReport, "skills"))
		return m, nil
	case "path":
		m.chat.appendAgentDeltaBlock(filepath.Join(assets.Dir(), "skills"))
		return m, nil
	case "show":
		if rest == "" {
			m.chat.appendError("usage: /skills show <name>")
			return m, nil
		}
		for _, s := range m.loadedSkills() {
			if s.Name == rest {
				m.chat.appendSlashOutput(s.Body)
				return m, nil
			}
		}
		m.chat.appendError("unknown skill: " + rest)
		return m, nil
	case "new":
		return m.cmdSkillNew(rest)
	case "edit":
		return m.cmdSkillEdit(rest)
	case "reset":
		return m.cmdSkillReset(rest)
	default:
		m.chat.appendError("unknown /skills argument; try /skills list")
		return m, nil
	}
}

// loadedSkills returns the current skill set from the loader.
func (m *Model) loadedSkills() []assets.Skill {
	if m.skillLoader == nil {
		return nil
	}
	out := make([]assets.Skill, 0)
	for _, n := range m.skillLoader.Names() {
		out = append(out, m.skillLoader.Skill(n))
	}
	return out
}

func (m *Model) cmdSkillNew(name string) (tea.Model, tea.Cmd) {
	if name == "" {
		m.chat.appendError("usage: /skills new <name>")
		return m, nil
	}
	for _, s := range m.loadedSkills() {
		if s.Name == name {
			m.chat.appendError("skill already exists: " + name)
			return m, nil
		}
	}
	path := assets.Path("skills/" + name)
	if path == "" {
		m.chat.appendError("invalid skill name: " + name)
		return m, nil
	}
	m.pendingAssetPath = path
	m.pendingAssetReload = true
	body := fmt.Sprintf(skillFrontmatterTemplate, name, name)
	return m, openEditorFileCmd(path, body)
}

func (m *Model) cmdSkillEdit(name string) (tea.Model, tea.Cmd) {
	if name == "" {
		m.chat.appendError("usage: /skills edit <name>")
		return m, nil
	}
	path := assets.Path("skills/" + name)
	for _, s := range m.loadedSkills() {
		if s.Name == name {
			m.pendingAssetPath = path
			m.pendingAssetReload = true
			return m, openEditorFileCmd(path, s.Body)
		}
	}
	m.chat.appendError("unknown skill: " + name)
	return m, nil
}

func (m *Model) cmdSkillReset(name string) (tea.Model, tea.Cmd) {
	if name == "" {
		m.chat.appendError("usage: /skills reset <name>")
		return m, nil
	}
	if err := assets.Reset("skills/" + name); err != nil {
		m.chat.appendError("reset failed: " + err.Error())
		return m, nil
	}
	m.chat.appendAgentDeltaBlock("skill " + name + " reset to the built-in version")
	return m.cmdReload()
}
```

Note `openEditorFileCmd` is added in Task 13; `m.skillLoader.Skill(name)` accessor and the `pendingAssetPath`/`pendingAssetReload` Model fields are added in Task 14. This task's commit therefore depends on Tasks 13 and 14 having landed — within a single parallel TUI agent, implement Tasks 12–14 together and commit once at the end of Task 14. (The Task 12/13/14 split is for review structure; they share `internal/tui` and are not parallel with each other.)

- [ ] **Step 6: Wire dispatch in `internal/tui/app.go`.**

In `runSlashCommand`, remove `"skills"` from the `case "jobs", "designs", "lab", "export", "cost", "project", "skills":` line (leave the rest), and add a real case:

```go
	case "skills":
		return m.cmdSkills(arg)
```

- [ ] **Step 7: Commit happens at the end of Task 14.** Proceed to Task 13.

---

### Task 13: `/config` command and the file-editor helper

**Files:**
- Modify: `internal/tui/commands.go`
- Create: `internal/tui/configcmd.go`
- Create: `internal/tui/configcmd_test.go`
- Modify: `internal/tui/editor.go`
- Modify: `internal/tui/app.go`

- [ ] **Step 1: Register `/config` in `internal/tui/commands.go`.**

Add after the `skills` entry:

```go
	{
		Name:        "config",
		Description: "edit, reset or validate config.toml, models.toml and the system prompt",
		Subcommands: []Subcommand{
			{Name: "edit", Description: "open an asset in $EDITOR: /config edit config|models|system"},
			{Name: "reset", Description: "restore an asset to its default: /config reset config|models|system"},
			{Name: "validate", Description: "report config/models/system validation problems"},
			{Name: "path", Description: "print the fova config directory"},
		},
		Arguments: ArgsNone,
	},
```

- [ ] **Step 2: Add a file-targeted editor command to `internal/tui/editor.go`.**

The existing `openEditorCmd` edits an anonymous temp file. Add a sibling that edits a real path:

```go
// editorFileDoneMsg is delivered after an asset-file edit session closes.
// Path is the file that was edited; Err is non-nil if the editor failed.
type editorFileDoneMsg struct {
	Path string
	Err  error
}

// openEditorFileCmd ensures path exists (seeding it with initial when absent),
// then hands it to $EDITOR. Unlike openEditorCmd it edits the real file in
// place — the caller re-validates and reloads on editorFileDoneMsg.
func openEditorFileCmd(path, initial string) tea.Cmd {
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		if mkErr := os.MkdirAll(filepath.Dir(path), 0o755); mkErr != nil {
			return func() tea.Msg { return editorFileDoneMsg{Path: path, Err: mkErr} }
		}
		if wErr := os.WriteFile(path, []byte(initial), 0o644); wErr != nil {
			return func() tea.Msg { return editorFileDoneMsg{Path: path, Err: wErr} }
		}
	}
	fields := strings.Fields(resolveEditor())
	args := append(fields[1:], path)
	cmd := exec.Command(fields[0], args...)
	return tea.ExecProcess(cmd, func(execErr error) tea.Msg {
		return editorFileDoneMsg{Path: path, Err: execErr}
	})
}
```

Add `"errors"`, `"io/fs"`, `"path/filepath"` to `editor.go`'s imports.

- [ ] **Step 3: Write the failing test `internal/tui/configcmd_test.go`.**

```go
package tui

import (
	"strings"
	"testing"
)

func TestConfigAssetKeyValidation(t *testing.T) {
	for _, ok := range []string{"config", "models", "system"} {
		if _, valid := configAssetKey(ok); !valid {
			t.Errorf("%q should be a valid /config asset", ok)
		}
	}
	if _, valid := configAssetKey("nonsense"); valid {
		t.Error("nonsense should be rejected")
	}
}

func TestConfigAssetKeyMapsSystem(t *testing.T) {
	key, _ := configAssetKey("system")
	if key != "system" {
		t.Errorf("system maps to %q", key)
	}
}

func TestConfigUsageOnBadArg(t *testing.T) {
	if !strings.Contains(configEditUsage(), "config|models|system") {
		t.Error("usage string should list the valid assets")
	}
}
```

- [ ] **Step 4: (Tests run at Task 14.)** As in Task 12, `internal/tui` compiles only once Task 14 lands. The `TestConfig*` tests written above are executed at Task 14, Step 6.

- [ ] **Step 5: Implement `internal/tui/configcmd.go`.**

```go
package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/alvarogonjim/fova/internal/assets"
)

// configAssetKey validates a /config asset word and returns its assets key.
func configAssetKey(word string) (string, bool) {
	switch strings.TrimSpace(word) {
	case "config", "models", "system":
		return strings.TrimSpace(word), true
	default:
		return "", false
	}
}

func configEditUsage() string { return "usage: /config edit config|models|system" }

// cmdConfig dispatches /config and its sub-commands.
func (m *Model) cmdConfig(arg string) (tea.Model, tea.Cmd) {
	fields := strings.Fields(arg)
	sub := ""
	if len(fields) > 0 {
		sub = fields[0]
	}
	target := ""
	if len(fields) > 1 {
		target = fields[1]
	}

	switch sub {
	case "validate":
		m.chat.appendSlashOutput(renderAssetReport(m.assetReport, "config"))
		return m, nil
	case "path":
		m.chat.appendAgentDeltaBlock(assets.Dir())
		return m, nil
	case "edit":
		key, ok := configAssetKey(target)
		if !ok {
			m.chat.appendError(configEditUsage())
			return m, nil
		}
		path, err := assets.Export(key)
		if err != nil {
			m.chat.appendError("could not locate " + key + ": " + err.Error())
			return m, nil
		}
		m.pendingAssetPath = path
		m.pendingAssetReload = true
		return m, openEditorFileCmd(path, "")
	case "reset":
		key, ok := configAssetKey(target)
		if !ok {
			m.chat.appendError("usage: /config reset config|models|system")
			return m, nil
		}
		if err := assets.Reset(key); err != nil {
			m.chat.appendError("reset failed: " + err.Error())
			return m, nil
		}
		m.chat.appendAgentDeltaBlock(key + " reset to its built-in default")
		return m.cmdReload()
	default:
		m.chat.appendError("unknown /config argument; try /config validate")
		return m, nil
	}
}
```

- [ ] **Step 6: Wire dispatch in `internal/tui/app.go`.**

In `runSlashCommand`, add:

```go
	case "config":
		return m.cmdConfig(arg)
```

- [ ] **Step 7: Commit happens at the end of Task 14.** Proceed to Task 14.

---

### Task 14: Extend `/reload`, add `Skill` accessor, handle the editor-done message

**Files:**
- Modify: `internal/assets` — add `Loader.Skill` accessor in `internal/skills/loader.go`
- Modify: `internal/agent/session.go`
- Modify: `internal/tui/app.go`

- [ ] **Step 1: Add a `Skill` accessor to `internal/skills/loader.go`.**

```go
// Skill returns the loaded skill with the given name (zero value if absent).
func (l *Loader) Skill(name string) assets.Skill { return l.skills[name] }
```

- [ ] **Step 2: Add `SetSystemPrompt` to `internal/agent/session.go`.**

The `Session` holds `system string` read by the loop each turn. Guard it so `/reload` can hot-swap it. Add a `sync.Mutex` field `mu` to the `Session` struct, then:

```go
// SystemPrompt returns the current system prompt.
func (s *Session) SystemPrompt() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.system
}

// SetSystemPrompt swaps the system prompt; the next turn picks it up.
func (s *Session) SetSystemPrompt(p string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.system = p
}
```

Replace the existing one-line `SystemPrompt()` method with the locked version above. Add `"sync"` to the imports.

- [ ] **Step 3: Add Model fields for the pending editor target (`internal/tui/app.go`).**

Below the `assetReport assets.Report` field added in Task 12:

```go
	// pendingAssetPath is the file an in-flight /skills or /config edit is
	// editing; pendingAssetReload requests a bundle reload when it closes.
	pendingAssetPath   string
	pendingAssetReload bool
```

- [ ] **Step 4: Handle `editorFileDoneMsg` in the `app.go` Update switch.**

Next to the existing `case editorDoneMsg:` (around app.go:401), add:

```go
	case editorFileDoneMsg:
		if msg.Err != nil {
			m.chat.appendError("editor: " + msg.Err.Error())
			m.pendingAssetReload = false
			m.pendingAssetPath = ""
			return m, nil
		}
		if m.pendingAssetReload {
			m.pendingAssetReload = false
			m.pendingAssetPath = ""
			return m.cmdReload()
		}
		return m, nil
```

- [ ] **Step 5: Rewrite `cmdReload` to reload the whole bundle (`internal/tui/app.go`).**

Replace the body of `cmdReload`:

```go
// cmdReload re-reads every asset (config.toml, models.toml, system.md,
// skills) without restarting the TUI. The theme is applied live, the model
// registry and skill set are swapped, and the running agent's system prompt
// is hot-swapped. Conversation history is untouched.
func (m *Model) cmdReload() (tea.Model, tea.Cmd) {
	dir := m.configDir
	if dir == "" {
		dir = assets.Dir()
	}
	prev, hadPrev := lookupEnv("FOVA_CONFIG_DIR")
	_ = os.Setenv("FOVA_CONFIG_DIR", dir)
	defer func() {
		if hadPrev {
			_ = os.Setenv("FOVA_CONFIG_DIR", prev)
		} else {
			_ = os.Unsetenv("FOVA_CONFIG_DIR")
		}
	}()
	bundle, err := assets.Load()
	if err != nil {
		m.chat.appendError("reload: " + err.Error())
		return m, nil
	}
	if m.models == nil {
		m.models = llm.NewModelRegistry(bundle.Models)
	} else {
		m.models.Reload(bundle.Models)
	}
	if err := m.models.SelectDefault(bundle.Config.Defaults); err != nil {
		m.chat.appendError("apply [defaults] from config.toml: " + err.Error())
	}
	ApplyTheme(bundle.Config.UI.Theme)
	m.budgetLimit = bundle.Config.Budget.SessionSoftLimitUSD
	m.status.costLimit = bundle.Config.Budget.SessionSoftLimitUSD
	m.webhookURL = bundle.Config.Webhook.EffectiveURL()
	m.status.model = m.models.ActiveModel()
	m.status.provider = m.models.ActiveProviderName()

	// Swap the skill set and re-register the skills.list/read tools.
	m.skillLoader = skills.NewLoader(bundle.Skills)
	if m.registry != nil {
		m.registry.Register(m.skillLoader.ListTool())
		m.registry.Register(m.skillLoader.ReadTool())
	}
	// Hot-swap the system prompt for the next agent turn.
	m.systemPrompt = agent.BuildSystemPrompt(Commands(), bundle.SystemPrompt)
	if m.session != nil {
		m.session.SetSystemPrompt(m.systemPrompt)
	}
	m.assetReport = bundle.Report

	msg := "reloaded config.toml, models.toml, system.md and skills"
	if s := bundle.Report.Summary(); s != "" {
		msg += " — " + s
	}
	m.chat.appendAgentDeltaBlock(msg)
	return m, nil
}
```

Ensure `app.go` imports `"github.com/alvarogonjim/fova/internal/agent"` (already present), `assets`, and `skills`.

- [ ] **Step 6: Run the full TUI test suite.**

Run: `go test ./internal/tui/...`
Expected: PASS — including the Task 12/13 tests (`TestSkills*`, `TestConfig*`).

- [ ] **Step 7: Commit Tasks 12–14 together.**

```bash
git add internal/tui/ internal/skills/loader.go internal/agent/session.go
git commit -m "feat(tui): add /skills and /config, reload all assets on /reload"
```

---

## Phase 3 — Integration & cleanup

### Task 15: Wire `assets.Load()` into `cmd/fova`

**Files:**
- Modify: `cmd/fova/main.go`
- Modify: `cmd/fova/replay.go`

- [ ] **Step 1: Load the bundle in `runTUI` (`cmd/fova/main.go`).**

Replace the `cat, err := config.LoadModels()` … `cfg, err := config.LoadConfig()` block (lines ~115–123) with:

```go
	bundle, err := assets.Load()
	if err != nil {
		return err
	}
	models := llm.NewModelRegistry(bundle.Models)
	cfg := bundle.Config
	tui.ApplyTheme(cfg.UI.Theme)
	if err := models.SelectDefault(cfg.Defaults); err != nil {
		return err
	}
	skillLoader := skills.NewLoader(bundle.Skills)
```

- [ ] **Step 2: Update `buildRegistry` to take the skill loader.**

Change its signature and the skills wiring:

```go
func buildRegistry(workspace string, st *store.Store, mgr *jobmgr.Manager, models *llm.ModelRegistry, cfg assets.Config, installer *local.Installer, skillLoader *skills.Loader) *tools.Registry {
```

Replace the three lines `loader := skills.NewLoader()` / `registry.Register(loader.ListTool())` / `registry.Register(loader.ReadTool())` with:

```go
	registry.Register(skillLoader.ListTool())
	registry.Register(skillLoader.ReadTool())
```

Update the call site: `registry := buildRegistry(workspace, st, mgr, models, cfg, installer, skillLoader)`.

- [ ] **Step 3: Update the `tui.Deps` literal in `runTUI`.**

```go
		SystemPrompt:       agent.BuildSystemPrompt(tui.Commands(), bundle.SystemPrompt),
		...
		ConfigDir:          assets.Dir(),
		...
		SkillLoader:        skillLoader,
		AssetReport:        bundle.Report,
```

- [ ] **Step 4: Update imports in `main.go`.**

Add `"github.com/alvarogonjim/fova/internal/assets"`. Keep `"github.com/alvarogonjim/fova/internal/skills"`. Remove `"github.com/alvarogonjim/fova/internal/config"`.

- [ ] **Step 5: Update `cmd/fova/replay.go`.**

Line 83 `cat, err := config.LoadModels()` → load the bundle once: `bundle, err := assets.Load()`, then use `bundle.Models` where `cat` was used and `bundle.SystemPrompt` for the prompt. Line 95 `SystemPrompt: agent.BuildSystemPrompt(tui.Commands())` → `agent.BuildSystemPrompt(tui.Commands(), bundle.SystemPrompt)`. Replace the `internal/config` import with `internal/assets`.

- [ ] **Step 6: Build and smoke-test.**

Run: `go build ./... && go run ./cmd/fova version`
Expected: build PASS; prints `fova <version>`.

- [ ] **Step 7: Commit.**

```bash
git add cmd/fova/
git commit -m "feat(cmd): load the asset bundle at startup and thread it through"
```

---

### Task 16: Migrate remaining importers, delete the shim

**Files:**
- Modify: `internal/llm/modelregistry.go`, `internal/llm/modelregistry_test.go`
- Modify: `internal/tui/app.go`, `internal/tui/app_test.go`, `internal/tui/replay_test.go`, `internal/tui/setup_test.go`
- Modify: `cmd/fova/main_test.go`
- Delete: `internal/config/`

- [ ] **Step 1: Find every remaining `internal/config` importer.**

Run: `grep -rl '"github.com/alvarogonjim/fova/internal/config"' --include='*.go' .`
Expected: a list — `internal/llm/modelregistry.go`, `internal/llm/modelregistry_test.go`, `internal/tui/app.go`, `internal/tui/app_test.go`, `internal/tui/replay_test.go`, `internal/tui/setup_test.go`, `cmd/fova/main_test.go` (and any others the grep surfaces).

- [ ] **Step 2: In each file, migrate the import and the references.**

In every file from Step 1: change the import path `"github.com/alvarogonjim/fova/internal/config"` → `"github.com/alvarogonjim/fova/internal/assets"`, change the package selector `config.` → `assets.`, and change `config.ConfigDir()` → `assets.Dir()`. The shim aliased every type and function 1:1, so this is a pure rename — no signature changes. (`internal/tui/app.go` may already import `assets`; in that case just drop the `config` import and rename selectors.)

- [ ] **Step 3: Delete the shim.**

```bash
git rm -r internal/config
```

- [ ] **Step 4: Verify nothing references the old package.**

Run: `grep -rn "internal/config" --include='*.go' . ; echo "exit: $?"`
Expected: no matches (`grep` exit 1).

- [ ] **Step 5: Build and run the full test suite.**

Run: `go build ./... && go test ./...`
Expected: build PASS; all tests PASS.

- [ ] **Step 6: Commit.**

```bash
git add -A
git commit -m "refactor(assets): migrate all importers off internal/config and delete it"
```

---

## Acceptance verification

After Task 16, run this end-to-end check against a clean config dir:

```bash
TMP=$(mktemp -d)
FOVA_CONFIG_DIR=$TMP go run ./cmd/fova version          # materializes the tree
ls "$TMP" "$TMP/skills"                                 # config.toml models.toml system.md + 7 skills
echo '---\nname: x\n---\nbody' > "$TMP/skills/x.md"     # add a user skill
echo "broken" > "$TMP/skills/Bad Name.md"               # add a bad skill
FOVA_CONFIG_DIR=$TMP go test ./internal/assets/ -run TestLoad -v
```

Expected: `$TMP` holds `config.toml`, `models.toml`, `system.md` and `skills/` with the 7 built-ins; `assets.Load()` reports the bad skill without dropping the good ones; `go build ./... && go test ./...` is fully green; `internal/config` no longer exists.

Cross-check against the spec's §11 acceptance criteria — every bullet there is covered by Tasks 3, 7, 10, 12, 13 and 16.
