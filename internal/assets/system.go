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
