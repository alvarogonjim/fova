package agent

import (
	"strings"
	"testing"
)

// TestSystemPromptHasLongRunningJobRule is Bug 3's snapshot-style assertion
// (AC3 of the v0.6 design-path-resolution spec): the prompt must teach the
// agent not to cancel long-running jobs while elapsed < estimated. The exact
// substrings are checked verbatim — if the wording is reworded, this test
// fails and the spec owner reviews the change.
func TestSystemPromptHasLongRunningJobRule(t *testing.T) {
	prompt := BuildSystemPrompt(nil)
	wantPhrase := "Do not invoke `jobs.cancel`"
	if !strings.Contains(prompt, wantPhrase) {
		t.Errorf("system prompt missing Bug 3 rule %q; have:\n%s", wantPhrase, prompt)
	}
	wantElapsedRule := "`elapsed < estimated`"
	if !strings.Contains(prompt, wantElapsedRule) {
		t.Errorf("system prompt missing elapsed < estimated rule %q", wantElapsedRule)
	}
	want2xRule := "2 × estimated"
	if !strings.Contains(prompt, want2xRule) {
		t.Errorf("system prompt missing 2x estimated cancel threshold %q", want2xRule)
	}
}

// testCatalogue mirrors the shape of the real tui catalogue closely enough to
// exercise BuildSystemPrompt's templating without dragging in the tui package
// (which would be an import cycle). The names match the live catalogue so the
// grounding test also doubles as a smoke check.
func testCatalogue() []SlashCommand {
	return []SlashCommand{
		{Name: "model", Description: "switch the model (and its provider)"},
		{
			Name:        "plan",
			Description: "show or act on the current design plan",
			Subcommands: []SlashSubcommand{
				{Name: "approve", Description: "approve and commit the current design plan"},
				{Name: "cancel", Description: "discard the current design plan"},
			},
		},
		{Name: "doctor", Description: "diagnose the local tool environment"},
		{Name: "install", Description: "install a local design tool"},
		{Name: "auth", Description: "store an API token, e.g. /auth adaptyv <token>"},
	}
}

// TestSystemPromptContainsSlashCatalogue is Bug 2's primary assertion: every
// command and sub-command from the catalogue must appear verbatim in the
// rendered system prompt so the LLM has ground truth on every turn.
func TestSystemPromptContainsSlashCatalogue(t *testing.T) {
	prompt := BuildSystemPrompt(testCatalogue())
	for _, want := range []string{
		"/model",
		"/plan",
		"/plan approve",
		"/plan cancel",
		"/doctor",
		"/install",
		"/auth",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("system prompt missing command %q; have:\n%s", want, prompt)
		}
	}
}

// TestSystemPromptRendersSubcommandDescriptions guarantees each sub-command's
// description text is reachable — not just its label — so the LLM can pick
// the right one in context.
func TestSystemPromptRendersSubcommandDescriptions(t *testing.T) {
	prompt := BuildSystemPrompt(testCatalogue())
	for _, want := range []string{
		"approve and commit the current design plan",
		"discard the current design plan",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("system prompt missing sub-command description %q", want)
		}
	}
}

// TestSystemPromptOmitsDynamicArgumentValues asserts that the catalogue listing
// names a command like /install but does NOT enumerate every live tool name
// (those are suggest-time, not prompt-time, and would bloat the system prompt
// every turn). The catalogue passed in carries no Subcommands for /install so
// the only acceptable inclusion is the bare "/install" entry.
func TestSystemPromptOmitsDynamicArgumentValues(t *testing.T) {
	prompt := BuildSystemPrompt(testCatalogue())
	for _, forbidden := range []string{
		"/install bindcraft",
		"/install proteinmpnn",
		"/install rfdiffusion",
		"/model qwen",
	} {
		if strings.Contains(prompt, forbidden) {
			t.Errorf("system prompt unexpectedly embeds dynamic value %q", forbidden)
		}
	}
}

// TestSystemPromptForbidsInventingSlashCommands is the grounding clause: the
// prompt must state, in the verbatim wording the spec calls out, that the
// model cannot invent slash commands. The check is exact-string so any
// reword goes through the spec owner.
func TestSystemPromptForbidsInventingSlashCommands(t *testing.T) {
	prompt := BuildSystemPrompt(testCatalogue())
	want := "Never invent a slash command. If a needed verb doesn't exist as a command, tell the user to describe the change in plain English instead."
	if !strings.Contains(prompt, want) {
		t.Errorf("grounding clause missing or reworded; expected:\n%s\n\ngot:\n%s", want, prompt)
	}
}

// TestSystemPromptHasNoTemplateMarker confirms the {{COMMAND_CATALOGUE}}
// placeholder is always substituted, even when the catalogue is empty. A
// leftover marker would leak the templating mechanism into the LLM.
func TestSystemPromptHasNoTemplateMarker(t *testing.T) {
	for _, cat := range [][]SlashCommand{nil, {}, testCatalogue()} {
		prompt := BuildSystemPrompt(cat)
		if strings.Contains(prompt, "{{COMMAND_CATALOGUE}}") {
			t.Errorf("template marker leaked for catalogue %+v", cat)
		}
	}
}

// TestSystemPromptStillEmbeddedBaseText guards the embedded markdown loader:
// the "You are fova" preamble must survive every render.
func TestSystemPromptStillEmbeddedBaseText(t *testing.T) {
	prompt := BuildSystemPrompt(nil)
	if !strings.Contains(prompt, "You are fova") {
		t.Errorf("base preamble missing from rendered prompt")
	}
}
