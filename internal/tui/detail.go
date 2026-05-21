package tui

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/alvarogonjim/fova/internal/domain"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

// readLog reads and returns the whole contents of a job log file. A missing
// file or an empty path yields "" with no error — the full-screen view simply
// shows nothing rather than crashing (design doc §6).
func readLog(path string) string {
	if path == "" {
		return ""
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(b)
}

// tailLines returns the last n lines of the file at path, newest-last. A
// missing/empty file or a non-positive n yields an empty slice.
func tailLines(path string, n int) []string {
	if path == "" || n <= 0 {
		return []string{}
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return []string{}
	}
	text := strings.TrimRight(string(b), "\n")
	if text == "" {
		return []string{}
	}
	lines := strings.Split(text, "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines
}

// detailView is the full-screen, scrollable view of a job's complete log
// (design doc §4.5). It wraps a bubbles/viewport for the log body and renders
// a styled header line above it.
type detailView struct {
	theme    Theme
	viewport viewport.Model
	header   string
}

// newDetailView returns a detailView with an empty viewport.
func newDetailView(th Theme) detailView {
	return detailView{theme: th, viewport: viewport.New(0, 0)}
}

// setSize resizes the inner viewport. The header occupies one line, so the
// viewport gets the remaining height.
func (v *detailView) setSize(w, h int) {
	if w < 0 {
		w = 0
	}
	v.viewport.Width = w
	vh := h - 1
	if vh < 0 {
		vh = 0
	}
	v.viewport.Height = vh
}

// setContent stores the header line and sets the viewport content to the full
// log body.
func (v *detailView) setContent(header, body string) {
	v.header = header
	v.viewport.SetContent(body)
}

// update routes scroll keys (↑/↓/PgUp/PgDn, and k/j) to the viewport and
// returns the updated detailView. The viewport's default key map already binds
// all of these.
func (v detailView) update(msg tea.KeyMsg) detailView {
	v.viewport, _ = v.viewport.Update(msg)
	return v
}

// View renders the styled header line above the viewport's view.
func (v detailView) View() string {
	return v.theme.Header.Render(v.header) + "\n" + v.viewport.View()
}

// renderJobDetail builds the full-screen detail view for a job — a header
// line plus a metadata + log body. It works for any job status.
func renderJobDetail(th Theme, j domain.Job) (header, body string) {
	header = glyph(j.Status) + " " + j.Tool + " · " + string(j.ID) + " · " + string(j.Status)

	var b strings.Builder
	fmt.Fprintf(&b, " status     %-16s kind     %s\n", j.Status, j.Kind)
	fmt.Fprintf(&b, " backend    %-16s cost     $%.2f\n", orDash(j.Backend), j.CostUSD)
	fmt.Fprintf(&b, " created    %s\n", j.Created.Format("15:04:05"))
	if j.Started != nil {
		fmt.Fprintf(&b, " started    %s\n", j.Started.Format("15:04:05"))
	}
	if j.Finished != nil {
		fmt.Fprintf(&b, " finished   %s\n", j.Finished.Format("15:04:05"))
	}
	if elapsed, eta, ok := jobETA(j); ok {
		fmt.Fprintf(&b, " progress   %s  %d%%\n", progressBar(elapsed, eta, 24), int(j.Progress*100))
	}
	if n := len(j.ProducedDesigns); n > 0 {
		fmt.Fprintf(&b, " designs    %d produced\n", n)
	} else {
		b.WriteString(" designs    none yet\n")
	}
	if j.Status == domain.JobFailed && j.Error != "" {
		b.WriteString("\n")
		b.WriteString(th.Error.Render(" error ─────────────────────────────────") + "\n")
		b.WriteString(th.Error.Render(" "+j.Error) + "\n")
	}
	b.WriteString("\n" + th.SectionRule.Render(" log ───────────────────────────────────") + "\n")
	log := readLog(j.LogFile)
	if strings.TrimSpace(log) == "" {
		log = "(no output yet)"
	}
	b.WriteString(log)
	return header, b.String()
}

// renderDesignDetail builds the detail view for a design — scores, sequence
// chains, provenance, and lab status.
func renderDesignDetail(th Theme, d domain.Design) (header, body string) {
	header = string(d.ID) + " · " + string(d.Origin) + " · " + string(d.Application)
	if isShortlisted(d) {
		header += "    ★ shortlisted"
	}

	var b strings.Builder
	fmt.Fprintf(&b, " created    %s\n", d.Created.Format("15:04:05"))
	fmt.Fprintf(&b, " structure  %s\n", orDash(d.StructureFile))
	if len(d.Tags) > 0 {
		fmt.Fprintf(&b, " tags       %s\n", strings.Join(d.Tags, ", "))
	}

	b.WriteString("\n" + th.SectionRule.Render(" scores ────────────────────────────────") + "\n")
	if len(d.Scores) == 0 {
		b.WriteString(" (none)\n")
	} else {
		for _, k := range sortedScoreKeys(d.Scores) {
			fmt.Fprintf(&b, " %-16s %.2f\n", k, d.Scores[k])
		}
	}

	b.WriteString("\n" + th.SectionRule.Render(" sequence ──────────────────────────────") + "\n")
	b.WriteString(renderSequenceChains(d.Sequence))
	b.WriteString("\n")

	b.WriteString("\n" + th.SectionRule.Render(" provenance ────────────────────────────") + "\n")
	if len(d.Provenance) == 0 {
		b.WriteString(" (none)\n")
	} else {
		for _, p := range d.Provenance {
			fmt.Fprintf(&b, " %-14s %-8s %s  #%s\n",
				p.Tool, p.Version, p.Timestamp.Format("15:04"), shortHash(p.InputHash))
		}
	}

	lab := "not submitted"
	if len(d.LabResults) > 0 {
		lab = fmt.Sprintf("%d result(s)", len(d.LabResults))
	}
	fmt.Fprintf(&b, "\n lab        %s\n", lab)
	if d.Notes != "" {
		fmt.Fprintf(&b, " notes      %s\n", d.Notes)
	}
	return header, b.String()
}

// renderExperimentDetail builds the detail view for a wet-lab experiment —
// metadata plus a per-design results table.
func renderExperimentDetail(th Theme, e domain.Experiment) (header, body string) {
	header = orDash(e.TargetName) + " · " + orDash(e.AssayType) + " · " + orDash(e.Status)

	var b strings.Builder
	fmt.Fprintf(&b, " backend    %-16s external %s\n", orDash(e.Backend), orDash(e.ExternalID))
	fmt.Fprintf(&b, " submitted  %-16s cost     $%.2f\n", e.SubmittedAt.Format("Jan 2 15:04"), e.CostUSD)
	fmt.Fprintf(&b, " designs    %d submitted\n", len(e.Designs))

	b.WriteString("\n" + th.SectionRule.Render(" results ───────────────────────────────") + "\n")
	if len(e.Results) == 0 {
		b.WriteString(" no results yet\n")
		return header, b.String()
	}
	fmt.Fprintf(&b, " %-14s %-12s %-12s %s\n", "design", "Kd", "binding", "R²")
	for _, r := range e.Results {
		kd := "—"
		if r.Kd != nil {
			kd = fmt.Sprintf("%.3g %s", *r.Kd, r.KdUnits)
		}
		rsq := "—"
		if r.RSquared != nil {
			rsq = fmt.Sprintf("%.2f", *r.RSquared)
		}
		fmt.Fprintf(&b, " %-14s %-12s %-12s %s\n",
			shortID(string(r.DesignID)), kd, orDash(r.BindingStrength), rsq)
	}
	return header, b.String()
}

// sortedScoreKeys returns a design's score keys in deterministic order.
func sortedScoreKeys(scores map[string]float64) []string {
	keys := make([]string, 0, len(scores))
	for k := range scores {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// shortHash truncates a provenance input hash for compact display.
func shortHash(h string) string {
	if len(h) > 6 {
		return h[:6]
	}
	return h
}

// renderSequenceChains formats every chain of a design sequence in 10-residue
// groups, labelled by chain id. domain.Sequence is multi-chain, so each chain
// is shown separately.
func renderSequenceChains(seq domain.Sequence) string {
	if len(seq.Chains) == 0 {
		return " (no sequence)"
	}
	ids := make([]string, 0, len(seq.Chains))
	for id := range seq.Chains {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var b strings.Builder
	for i, id := range ids {
		if i > 0 {
			b.WriteString("\n")
		}
		chain := seq.Chains[id]
		fmt.Fprintf(&b, " chain %s (%d aa)\n", id, len(chain))
		b.WriteString(wrapResidues(chain))
	}
	return b.String()
}

// wrapResidues groups a chain into 10-residue blocks, five blocks per line.
func wrapResidues(seq string) string {
	if seq == "" {
		return "  (empty)"
	}
	var b strings.Builder
	for i := 0; i < len(seq); i += 10 {
		end := i + 10
		if end > len(seq) {
			end = len(seq)
		}
		if i%50 == 0 && i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(" " + seq[i:end])
	}
	return b.String()
}
