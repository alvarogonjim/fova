package agent

import (
	"fmt"
	"strings"
)

// SlashSubcommand mirrors tui.Subcommand for the system-prompt catalogue —
// kept in this package so the prompt builder does not import internal/tui
// (which would create a cycle: tui already imports agent).
type SlashSubcommand struct {
	Name        string
	Description string
}

// SlashCommand mirrors tui.Command's display fields (Name, Description,
// Subcommands). Dynamic argument sources (installed tools, model IDs, auth
// providers) are intentionally omitted — those resolve at suggest time, not
// in the prompt, and would bloat the system message every turn.
type SlashCommand struct {
	Name        string
	Description string
	Subcommands []SlashSubcommand
}

// catalogueMarker is the template token replaced by the rendered slash-
// command catalogue inside prompts/system.md.
const catalogueMarker = "{{COMMAND_CATALOGUE}}"

// BuildSystemPrompt renders template with cat substituted for the
// {{COMMAND_CATALOGUE}} marker. template is the system.md source loaded by
// internal/assets; a nil or empty cat yields an empty catalogue block.
func BuildSystemPrompt(cat []SlashCommand, template string) string {
	return strings.Replace(template, catalogueMarker, renderCatalogue(cat), 1)
}

// renderCatalogue formats cat as one row per command and one per sub-command,
// aligned so the descriptions line up:
//
//	/plan                  — show or act on the current design plan
//	/plan approve          — approve and commit the current design plan
//	/plan cancel           — discard the current design plan
func renderCatalogue(cat []SlashCommand) string {
	if len(cat) == 0 {
		return ""
	}
	const labelWidth = 22
	var b strings.Builder
	for _, c := range cat {
		fmt.Fprintf(&b, "  %-*s — %s\n", labelWidth, "/"+c.Name, c.Description)
		for _, sub := range c.Subcommands {
			fmt.Fprintf(&b, "  %-*s — %s\n", labelWidth, "/"+c.Name+" "+sub.Name, sub.Description)
		}
	}
	return b.String()
}
