package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/alvarogonjim/fova/internal/assets"
)

// configAssetKey validates a /config asset word and returns its assets key.
func configAssetKey(word string) (string, bool) {
	switch strings.TrimSpace(word) {
	case "config", "models", "system":
		return strings.TrimSpace(word), true
	default:
		return "", false
	}
}

func configEditUsage() string { return "usage: /config edit config|models|system" }

// cmdConfig dispatches /config and its sub-commands.
func (m *Model) cmdConfig(arg string) (tea.Model, tea.Cmd) {
	fields := strings.Fields(arg)
	sub := ""
	if len(fields) > 0 {
		sub = fields[0]
	}
	target := ""
	if len(fields) > 1 {
		target = fields[1]
	}

	switch sub {
	case "validate":
		m.chat.appendSlashOutput(renderAssetReport(m.assetReport, "config"))
		return m, nil
	case "path":
		m.chat.appendAgentDeltaBlock(assets.Dir())
		return m, nil
	case "edit":
		key, ok := configAssetKey(target)
		if !ok {
			m.chat.appendError(configEditUsage())
			return m, nil
		}
		path, err := assets.Export(key)
		if err != nil {
			m.chat.appendError("could not locate " + key + ": " + err.Error())
			return m, nil
		}
		m.pendingAssetPath = path
		m.pendingAssetReload = true
		return m, openEditorFileCmd(path, "")
	case "reset":
		key, ok := configAssetKey(target)
		if !ok {
			m.chat.appendError("usage: /config reset config|models|system")
			return m, nil
		}
		if err := assets.Reset(key); err != nil {
			m.chat.appendError("reset failed: " + err.Error())
			return m, nil
		}
		m.chat.appendAgentDeltaBlock(key + " reset to its built-in default")
		return m.cmdReload()
	default:
		m.chat.appendError("unknown /config argument; try /config validate")
		return m, nil
	}
}
