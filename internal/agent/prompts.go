package agent

import (
	_ "embed"
	"fmt"
	"strings"
)

// systemPromptTemplate is the embedded markdown source containing the
// {{COMMAND_CATALOGUE}} marker; render through BuildSystemPrompt to get the
// fully-templated prompt for the LLM.
//
//go:embed prompts/system.md
var systemPromptTemplate string

// SystemPrompt is the rendered system prompt without a live slash-command
// catalogue (the {{COMMAND_CATALOGUE}} marker resolves to an empty block).
// Callers that own the live tui catalogue (cmd/fova/main.go) should call
// BuildSystemPrompt instead so the agent sees ground truth on every turn.
var SystemPrompt = BuildSystemPrompt(nil)

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

// BuildSystemPrompt renders the embedded system prompt with cat substituted
// for the {{COMMAND_CATALOGUE}} marker. A nil or empty cat yields an empty
// block — callers should pass tui.Commands() so the LLM stays grounded.
func BuildSystemPrompt(cat []SlashCommand) string {
	return strings.Replace(systemPromptTemplate, catalogueMarker, renderCatalogue(cat), 1)
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
