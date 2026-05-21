// Package assets owns every materializable, user-editable asset fova ships:
// config.toml, models.toml, the system prompt, and skills. All four
// materialize into Dir() on first run, are validated by one engine, and are
// editable and resettable through the /skills and /config TUI commands.
package assets

import (
	"fmt"
	"os"
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
