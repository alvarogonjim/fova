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
