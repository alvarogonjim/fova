package tui

import (
	"strings"
	"testing"
)

// TestSubmitModalRendersDetails checks the box shows the target, assay,
// sequence count, cost, and the ~21-day turnaround (SPECS §12.2).
func TestSubmitModalRendersDetails(t *testing.T) {
	m := submitModal{
		TargetName: "HER2 / ERBB2 (comp-her2-human)",
		AssayType:  "binding",
		Sequences: []string{
			"MAQVQLVESGGGLVQPGGSLRLSCAASGFNIKDTYIHWVRQ",
			"MAQVQLQESGGGLVQPGG",
			"MAQVQLVDSGGGLVQPGGSLRLSCAA",
		},
		CostUSD:    3600,
		WebhookURL: "http://localhost:9876/webhooks/adaptyv",
	}
	out := m.view(NewTheme(), 80)
	if !strings.Contains(out, "HER2 / ERBB2") {
		t.Errorf("modal missing the target name: %q", out)
	}
	if !strings.Contains(out, "binding") {
		t.Errorf("modal missing the assay type: %q", out)
	}
	if !strings.Contains(out, "3,600") {
		t.Errorf("modal missing the estimated cost: %q", out)
	}
	if !strings.Contains(out, "~21 days") {
		t.Errorf("modal missing the turnaround: %q", out)
	}
	if !strings.Contains(out, "webhooks/adaptyv") {
		t.Errorf("modal missing the webhook URL: %q", out)
	}
	if !strings.Contains(out, "[ y ] submit") || !strings.Contains(out, "cancel") {
		t.Errorf("modal missing the action prompts: %q", out)
	}
}

// TestSubmitModalSequencePreview checks the box previews the first three
// sequences as "MAQVQLVESG... (N aa)".
func TestSubmitModalSequencePreview(t *testing.T) {
	m := submitModal{
		TargetName: "PD-L1",
		AssayType:  "binding",
		Sequences: []string{
			"MAQVQLVESGGGLVQPGGSLRLSCAASGFNIKDTYIHWVRQ",
			"MAQVQLQESGGGLVQPGG",
			"MAQVQLVDSGGGLVQPGGSLRLSCAA",
			"MAQVQLEXTRAGGGLVQPGG",
		},
		CostUSD:    600,
		WebhookURL: "http://localhost:9876/webhooks/adaptyv",
	}
	out := m.view(NewTheme(), 80)
	if !strings.Contains(out, "MAQVQLVESG... (41 aa)") {
		t.Errorf("modal missing the first sequence preview: %q", out)
	}
	if !strings.Contains(out, "Sequences:") || !strings.Contains(out, "4") {
		t.Errorf("modal missing the sequence count: %q", out)
	}
	// Only the first three are previewed; a fourth collapses to "...".
	if strings.Contains(out, "4. ") {
		t.Errorf("modal should preview at most three sequences: %q", out)
	}
	if !strings.Contains(out, "...") {
		t.Errorf("modal should show an ellipsis for the remaining sequences: %q", out)
	}
}

// TestSubmitModalCommaUSD checks dollar amounts get thousands separators.
func TestSubmitModalCommaUSD(t *testing.T) {
	cases := map[float64]string{
		0:       "0",
		600:     "600",
		3600:    "3,600",
		1234567: "1,234,567",
	}
	for in, want := range cases {
		if got := commaUSD(in); got != want {
			t.Errorf("commaUSD(%v) = %q, want %q", in, got, want)
		}
	}
}
