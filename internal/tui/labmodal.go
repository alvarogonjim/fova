package tui

import (
	"fmt"
	"strings"
)

// submitModal is the rich confirmation overlay shown before
// lab.submit_experiment executes (SPECS §12.2). It is a distinct overlay from
// the generic modalModel; the fields are filled from the submit request.
type submitModal struct {
	TargetName string   // e.g. "HER2 / ERBB2 (comp-her2-human)"
	AssayType  string   // e.g. "binding"
	Sequences  []string // raw amino-acid sequences being submitted
	CostUSD    float64  // estimated cost
	WebhookURL string   // URL Adaptyv will POST results to
}

// seqPreviewLen is how many leading residues a sequence preview shows.
const seqPreviewLen = 10

// commaUSD formats a whole-dollar amount with thousands separators, e.g.
// 3600 -> "3,600". Go's fmt has no grouping verb, so it is done by hand.
func commaUSD(amount float64) string {
	n := int64(amount)
	neg := n < 0
	if neg {
		n = -n
	}
	digits := fmt.Sprintf("%d", n)
	var groups []string
	for len(digits) > 3 {
		groups = append([]string{digits[len(digits)-3:]}, groups...)
		digits = digits[:len(digits)-3]
	}
	groups = append([]string{digits}, groups...)
	out := strings.Join(groups, ",")
	if neg {
		out = "-" + out
	}
	return out
}

// sequencePreview renders one sequence as "MAQVQLVESG... (124 aa)" — the first
// seqPreviewLen residues, an ellipsis when truncated, and the full length.
func sequencePreview(seq string) string {
	r := []rune(seq)
	head := string(r)
	if len(r) > seqPreviewLen {
		head = string(r[:seqPreviewLen]) + "..."
	}
	return fmt.Sprintf("%s (%d aa)", head, len(r))
}

// view renders the submit-confirmation box. The box itself inherits the
// saffron-bordered Theme.ModalBox (rebrand spec §3.7); the action row at the
// bottom delegates to RenderKeyRow so the bracketed keys take Accent and the
// labels take Fg, keeping a single source of truth for modal key rows.
func (m submitModal) view(th Theme, width int) string {
	var b strings.Builder
	b.WriteString(th.StatusBar.Render("Submit to Adaptyv Bio"))
	b.WriteString("\n\n")
	fmt.Fprintf(&b, "Target:         %s\n", m.TargetName)
	fmt.Fprintf(&b, "Assay:          %s\n", m.AssayType)
	fmt.Fprintf(&b, "Sequences:      %d\n", len(m.Sequences))
	for i, seq := range m.Sequences {
		if i >= 3 {
			b.WriteString("  ...\n")
			break
		}
		fmt.Fprintf(&b, "  %d. %s\n", i+1, sequencePreview(seq))
	}
	fmt.Fprintf(&b, "Estimated cost: $%s USD\n", commaUSD(m.CostUSD))
	b.WriteString("Turnaround:     ~21 days\n")
	fmt.Fprintf(&b, "Webhook URL:    %s\n", m.WebhookURL)
	b.WriteString("\nSubmit? ")
	b.WriteString(RenderKeyRow(th,
		KeyRowEntry{Key: "y", Label: "submit"},
		KeyRowEntry{Key: "n", Label: "cancel"},
		KeyRowEntry{Key: "r", Label: "review"},
		KeyRowEntry{Key: "s", Label: "save for later"},
	))
	return th.ModalBox.Width(min(width-4, 70)).Render(b.String())
}
