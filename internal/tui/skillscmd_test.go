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
