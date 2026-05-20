package tui

import (
	"strings"

	"github.com/alvarogonjim/fova/internal/agent"
)

// Subcommand is a keyword sub-argument of a slash command — e.g. "approve" for
// /plan approve. The slash menu surfaces these when the user types a trailing
// space after the top-level command.
type Subcommand struct {
	Name        string
	Description string
}

// ArgumentSource selects which dynamic list the slash menu offers after a
// trailing space when a command has no keyword Subcommands. The lists are
// resolved at suggest time so the install catalogue reflects newly-added
// tools and the model list reflects /model-driven changes.
type ArgumentSource int

const (
	// ArgsNone means the command takes no completable argument.
	ArgsNone ArgumentSource = iota
	// ArgsInstalledTools lists tool names from the local registry (tools.toml).
	ArgsInstalledTools
	// ArgsModels lists registered model IDs from the active model registry.
	ArgsModels
	// ArgsAuthProviders lists known auth provider names (e.g. "adaptyv").
	ArgsAuthProviders
	// ArgsThemeModes lists "auto", "light", "dark" — the /theme arguments.
	ArgsThemeModes
	// ArgsFreeText is a free-text argument; the menu shows the Usage hint
	// instead of a completion list.
	ArgsFreeText
)

// Command is one entry in the slash-command catalogue.
type Command struct {
	Name        string
	Description string
	Subcommands []Subcommand
	Arguments   ArgumentSource
	// Usage is shown as a one-line hint when Arguments == ArgsFreeText.
	Usage string
}

// slashCmd is the legacy flat row shape kept for the existing
// top-level matcher; it mirrors a Command's display fields.
type slashCmd struct {
	Name        string
	Description string
}

// slashCommands is the single source of truth for slash-command metadata. It is
// consumed by the autocomplete popup (SPECS §10.7.3), the footer hint
// (§10.7.6), /help, and the agent system prompt grounding clause. Keep in sync
// with the dispatch in runSlashCommand.
var slashCommands = []Command{
	{Name: "model", Description: "switch the model (and its provider)", Arguments: ArgsModels},
	{
		Name:        "plan",
		Description: "show or act on the current design plan",
		Subcommands: []Subcommand{
			{Name: "approve", Description: "approve and commit the current design plan"},
			{Name: "cancel", Description: "discard the current design plan"},
		},
		Arguments: ArgsNone,
	},
	{Name: "clear", Description: "compact the conversation context", Arguments: ArgsNone},
	{Name: "doctor", Description: "diagnose the local tool environment", Arguments: ArgsNone},
	{Name: "tools", Description: "list installable tools and their status", Arguments: ArgsNone},
	{Name: "install", Description: "install a local design tool", Arguments: ArgsInstalledTools},
	{Name: "uninstall", Description: "remove an installed local tool", Arguments: ArgsInstalledTools},
	{
		Name:        "modal",
		Description: "deploy the Modal compute backend",
		Subcommands: []Subcommand{
			{Name: "deploy", Description: "write functions.py and deploy it via the Modal CLI"},
		},
		Arguments: ArgsNone,
	},
	{
		Name:        "auth",
		Description: "store an API token, e.g. /auth adaptyv <token>",
		Arguments:   ArgsAuthProviders,
		Usage:       "/auth <provider> <token>",
	},
	{
		Name:        "theme",
		Description: "switch the colour theme: /theme auto|light|dark",
		Arguments:   ArgsThemeModes,
	},
	{Name: "reload", Description: "reload config.toml and models.toml without restarting", Arguments: ArgsNone},
	{Name: "keys", Description: "show every keybinding", Arguments: ArgsNone},
	{Name: "help", Description: "show keybindings and commands", Arguments: ArgsNone},
	{Name: "quit", Description: "save and exit", Arguments: ArgsNone},
}

// knownAuthProviders is the static list of /auth providers. There is only one
// today (adaptyv), but the slash menu treats it as a registry so adding the
// next provider needs no matcher change.
var knownAuthProviders = []string{"adaptyv"}

// Commands returns the slash-command catalogue translated into the neutral
// agent.SlashCommand shape so the agent system prompt can ground itself in
// the real verbs without importing tui (which would cycle). Dynamic argument
// sources (ArgsInstalledTools, ArgsModels, ...) are intentionally dropped —
// those live values surface at suggest time, not in the system prompt.
func Commands() []agent.SlashCommand {
	out := make([]agent.SlashCommand, 0, len(slashCommands))
	for _, c := range slashCommands {
		entry := agent.SlashCommand{Name: c.Name, Description: c.Description}
		if len(c.Subcommands) > 0 {
			entry.Subcommands = make([]agent.SlashSubcommand, 0, len(c.Subcommands))
			for _, sc := range c.Subcommands {
				entry.Subcommands = append(entry.Subcommands, agent.SlashSubcommand{
					Name:        sc.Name,
					Description: sc.Description,
				})
			}
		}
		out = append(out, entry)
	}
	return out
}

// matchCommands returns top-level catalogue entries whose name has prefix as a
// case-insensitive prefix. An empty prefix returns the whole catalogue.
//
// This is the legacy entry point used by the no-trailing-space path. The
// trailing-space sub-command / argument path goes through MatchSlash.
func matchCommands(prefix string) []slashCmd {
	prefix = strings.ToLower(strings.TrimSpace(prefix))
	out := make([]slashCmd, 0, len(slashCommands))
	for _, c := range slashCommands {
		if strings.HasPrefix(c.Name, prefix) {
			out = append(out, slashCmd{Name: c.Name, Description: c.Description})
		}
	}
	return out
}

// SlashRow is one row offered by the slash-command autocomplete popup. The
// Label is what the user sees (e.g. "/plan approve" or "/install bindcraft");
// Insert is the literal text that Tab writes into the input.
type SlashRow struct {
	Label       string
	Description string
	// Insert is the string the input is replaced with on Tab. For top-level
	// commands it is "/<name> " (trailing space, same as today). For
	// sub-commands and arguments it is the full "/cmd <arg>" so the user can
	// hit Enter to run it.
	Insert string
}

// MatchSlash returns the autocomplete rows for the current input line.
//
//   - If the line has no space after the command word, top-level commands are
//     filtered by prefix (the existing behaviour).
//   - If the line has a trailing space or a partial second word, the matcher
//     switches to per-command mode and offers Subcommands or dynamic argument
//     rows from the matching live list.
//
// The dynamic lists (installedTools, modelIDs, authProviders) are passed in by
// the caller and resolved at every keystroke — no caching here.
func MatchSlash(line string, catalogue []Command, installedTools, modelIDs, authProviders []string) []SlashRow {
	line = strings.TrimLeft(line, " \t")
	if !strings.HasPrefix(line, "/") {
		return nil
	}
	body := strings.TrimPrefix(line, "/")
	// Find the first space — that separates the command word from its
	// arguments. We deliberately do NOT TrimSpace the line first: a trailing
	// space is the trigger for per-command mode.
	spaceIdx := strings.IndexByte(body, ' ')
	if spaceIdx < 0 {
		// Top-level prefix match.
		return matchTopLevel(body, catalogue)
	}
	cmdWord := strings.ToLower(body[:spaceIdx])
	argPrefix := strings.TrimLeft(body[spaceIdx+1:], " ")
	// Look up the command. If unknown, no suggestions.
	var cmd *Command
	for i := range catalogue {
		if catalogue[i].Name == cmdWord {
			cmd = &catalogue[i]
			break
		}
	}
	if cmd == nil {
		return nil
	}
	return matchPerCommand(cmd, argPrefix, installedTools, modelIDs, authProviders)
}

// matchTopLevel filters the catalogue by case-insensitive prefix and wraps
// each entry into a SlashRow.
func matchTopLevel(prefix string, catalogue []Command) []SlashRow {
	prefix = strings.ToLower(strings.TrimSpace(prefix))
	out := make([]SlashRow, 0, len(catalogue))
	for _, c := range catalogue {
		if !strings.HasPrefix(c.Name, prefix) {
			continue
		}
		out = append(out, SlashRow{
			Label:       "/" + c.Name,
			Description: c.Description,
			Insert:      "/" + c.Name + " ",
		})
	}
	return out
}

// matchPerCommand returns sub-command or argument rows for cmd, filtered by
// the argPrefix that the user has typed so far.
func matchPerCommand(cmd *Command, argPrefix string, installedTools, modelIDs, authProviders []string) []SlashRow {
	argPrefix = strings.ToLower(argPrefix)
	// Keyword sub-commands take precedence over dynamic argument sources;
	// today no command has both, but the schema permits it.
	if len(cmd.Subcommands) > 0 {
		out := make([]SlashRow, 0, len(cmd.Subcommands))
		for _, sc := range cmd.Subcommands {
			if !strings.HasPrefix(sc.Name, argPrefix) {
				continue
			}
			out = append(out, SlashRow{
				Label:       "/" + cmd.Name + " " + sc.Name,
				Description: sc.Description,
				Insert:      "/" + cmd.Name + " " + sc.Name,
			})
		}
		return out
	}
	switch cmd.Arguments {
	case ArgsInstalledTools:
		return argRows(cmd.Name, argPrefix, installedTools, "")
	case ArgsModels:
		return argRows(cmd.Name, argPrefix, modelIDs, "")
	case ArgsAuthProviders:
		return argRows(cmd.Name, argPrefix, authProviders, "")
	case ArgsThemeModes:
		return argRows(cmd.Name, argPrefix, []string{"auto", "light", "dark"}, "")
	case ArgsFreeText:
		if cmd.Usage == "" {
			return nil
		}
		return []SlashRow{{
			Label:       "/" + cmd.Name,
			Description: cmd.Usage,
			Insert:      "/" + cmd.Name + " ",
		}}
	}
	return nil
}

// argRows builds rows for a dynamic argument source. desc is an optional
// per-row description; when empty the row leaves Description blank.
func argRows(cmdName, prefix string, items []string, desc string) []SlashRow {
	out := make([]SlashRow, 0, len(items))
	for _, it := range items {
		if !strings.HasPrefix(strings.ToLower(it), prefix) {
			continue
		}
		out = append(out, SlashRow{
			Label:       "/" + cmdName + " " + it,
			Description: desc,
			Insert:      "/" + cmdName + " " + it,
		})
	}
	return out
}

// LongestCommonPrefix returns the longest string that is a prefix of every row
// label, after the leading "/". Used by Tab when several rows match: writing
// the common prefix to the input narrows the popup without committing.
func LongestCommonPrefix(rows []SlashRow) string {
	if len(rows) == 0 {
		return ""
	}
	if len(rows) == 1 {
		return rows[0].Insert
	}
	// Compare the Insert strings, since that is what would be written to
	// the input. Insert is always "/cmd …" or "/cmd <arg>".
	pref := rows[0].Insert
	for _, r := range rows[1:] {
		pref = commonPrefix(pref, r.Insert)
		if pref == "" {
			return ""
		}
	}
	return pref
}

func commonPrefix(a, b string) string {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	i := 0
	for i < n && a[i] == b[i] {
		i++
	}
	return a[:i]
}
