package assets

import (
	"os"
	"path/filepath"
	"testing"
)

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
