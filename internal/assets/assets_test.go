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
	if len(b.Skills) != 10 {
		t.Errorf("want 10 built-in skills, got %d", len(b.Skills))
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
	if _, err := Load(); err != nil { // first run materializes the 10 built-ins
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
	if len(b.Skills) != 10 {
		t.Errorf("the 10 good skills should still load, got %d", len(b.Skills))
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
		"config":     filepath.Join(dir, "config.toml"),
		"models":     filepath.Join(dir, "models.toml"),
		"system":     filepath.Join(dir, "system.md"),
		"skills/foo": filepath.Join(dir, "skills", "foo.md"),
	}
	for key, want := range cases {
		if got := Path(key); got != want {
			t.Errorf("Path(%q) = %q, want %q", key, got, want)
		}
	}
}

func TestEmbeddedSkillsAllHaveDescriptions(t *testing.T) {
	for _, s := range embeddedSkills() {
		if s.Description == "" {
			t.Errorf("built-in skill %q has no frontmatter description", s.Name)
		}
	}
}
