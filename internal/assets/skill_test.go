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
