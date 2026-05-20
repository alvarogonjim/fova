package tui

import "strings"

// slashCmd is one entry in the slash-command catalogue.
type slashCmd struct {
	Name        string
	Description string
}

// slashCommands is the single source of truth for slash-command metadata. It is
// consumed by the autocomplete popup (SPECS §10.7.3), the footer hint
// (§10.7.6), and /help. Keep it in sync with the dispatch in runSlashCommand.
var slashCommands = []slashCmd{
	{"model", "switch the model (and its provider)"},
	{"plan", "show or act on the current design plan"},
	{"clear", "compact the conversation context"},
	{"doctor", "diagnose the local tool environment"},
	{"tools", "list installable tools and their status"},
	{"install", "install a local design tool"},
	{"uninstall", "remove an installed local tool"},
	{"modal", "deploy the Modal compute backend"},
	{"auth", "store an API token, e.g. /auth adaptyv <token>"},
	{"help", "show keybindings and commands"},
	{"quit", "save and exit"},
}

// matchCommands returns the catalogue entries whose name has prefix as a
// case-insensitive prefix. An empty prefix returns the whole catalogue.
func matchCommands(prefix string) []slashCmd {
	prefix = strings.ToLower(strings.TrimSpace(prefix))
	out := make([]slashCmd, 0, len(slashCommands))
	for _, c := range slashCommands {
		if strings.HasPrefix(c.Name, prefix) {
			out = append(out, c)
		}
	}
	return out
}
