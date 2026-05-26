package agent

import (
	"strings"
	"testing"

	"github.com/alvarogonjim/fova/internal/assets"
)

// fakeTemplate is a minimal system.md stand-in carrying the marker plus the
// grounding clause, enough to exercise BuildSystemPrompt's templating.
const fakeTemplate = "You are fova.\n" +
	"{{COMMAND_CATALOGUE}}\n" +
	"When suggesting next steps, refer to these commands literally. " +
	"Never invent a slash command. If a needed verb doesn't exist as a command, " +
	"tell the user to describe the change in plain English instead.\n"

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

func TestSystemPromptContainsSlashCatalogue(t *testing.T) {
	prompt := BuildSystemPrompt(testCatalogue(), fakeTemplate)
	for _, want := range []string{"/model", "/plan", "/plan approve", "/plan cancel", "/doctor", "/install", "/auth"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("system prompt missing command %q; have:\n%s", want, prompt)
		}
	}
}

func TestSystemPromptRendersSubcommandDescriptions(t *testing.T) {
	prompt := BuildSystemPrompt(testCatalogue(), fakeTemplate)
	for _, want := range []string{"approve and commit the current design plan", "discard the current design plan"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("system prompt missing sub-command description %q", want)
		}
	}
}

func TestSystemPromptOmitsDynamicArgumentValues(t *testing.T) {
	prompt := BuildSystemPrompt(testCatalogue(), fakeTemplate)
	for _, forbidden := range []string{"/install bindcraft", "/install proteinmpnn", "/model qwen"} {
		if strings.Contains(prompt, forbidden) {
			t.Errorf("system prompt unexpectedly embeds dynamic value %q", forbidden)
		}
	}
}

func TestSystemPromptHasNoTemplateMarker(t *testing.T) {
	for _, cat := range [][]SlashCommand{nil, {}, testCatalogue()} {
		prompt := BuildSystemPrompt(cat, fakeTemplate)
		if strings.Contains(prompt, "{{COMMAND_CATALOGUE}}") {
			t.Errorf("template marker leaked for catalogue %+v", cat)
		}
	}
}

func TestSystemPromptForbidsInventingSlashCommands(t *testing.T) {
	prompt := BuildSystemPrompt(testCatalogue(), fakeTemplate)
	want := "Never invent a slash command."
	if !strings.Contains(prompt, want) {
		t.Errorf("grounding clause missing; expected %q", want)
	}
}

func TestSystemPromptContainsGroundingDirectives(t *testing.T) {
	prompt := BuildSystemPrompt(nil, assets.DefaultSystemPrompt())
	for _, want := range []string{
		"never invent identifiers",
		"Prefer specialized tools",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("system prompt missing %q", want)
		}
	}
}
