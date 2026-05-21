package tui

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/alvarogonjim/fova/internal/assets"
)

// renderSkillsList formats the loaded skill set as an aligned table.
func renderSkillsList(set []assets.Skill) string {
	if len(set) == 0 {
		return "No skills loaded."
	}
	rows := append([]assets.Skill{}, set...)
	sort.Slice(rows, func(i, j int) bool { return rows[i].Name < rows[j].Name })
	width := 0
	for _, s := range rows {
		if len(s.Name) > width {
			width = len(s.Name)
		}
	}
	var b strings.Builder
	for _, s := range rows {
		desc := s.Description
		if desc == "" {
			desc = "(no description)"
		}
		fmt.Fprintf(&b, "%-*s  %-10s  %s\n", width, s.Name, s.Source.String(), desc)
	}
	return strings.TrimRight(b.String(), "\n")
}

// renderAssetReport formats the part of a Report relevant to scope ("skills"
// or "config"); scope filters by AssetIssue.Asset prefix.
func renderAssetReport(rep assets.Report, scope string) string {
	match := func(asset string) bool {
		if scope == "skills" {
			return strings.HasPrefix(asset, "skills/")
		}
		return !strings.HasPrefix(asset, "skills/")
	}
	var b strings.Builder
	for _, e := range rep.Errors {
		if match(e.Asset) {
			fmt.Fprintf(&b, "  error   %s: %s\n", e.Asset, e.Message)
		}
	}
	for _, w := range rep.Warnings {
		if match(w.Asset) {
			fmt.Fprintf(&b, "  warning %s: %s\n", w.Asset, w.Message)
		}
	}
	if b.Len() == 0 {
		return "No problems found."
	}
	return strings.TrimRight(b.String(), "\n")
}

// skillFrontmatterTemplate is the scaffold written by /skills new.
const skillFrontmatterTemplate = `---
name: %s
description: One-line summary shown in skills.list and /skills list.
---
# Skill: <title>

## When to use

## Steps

`

// cmdSkills dispatches /skills and its sub-commands.
func (m *Model) cmdSkills(arg string) (tea.Model, tea.Cmd) {
	fields := strings.Fields(arg)
	sub := ""
	if len(fields) > 0 {
		sub = fields[0]
	}
	rest := strings.TrimSpace(strings.TrimPrefix(arg, sub))

	switch sub {
	case "", "list":
		m.chat.appendSlashOutput(renderSkillsList(m.loadedSkills()))
		return m, nil
	case "validate":
		m.chat.appendSlashOutput(renderAssetReport(m.assetReport, "skills"))
		return m, nil
	case "path":
		m.chat.appendAgentDeltaBlock(filepath.Join(assets.Dir(), "skills"))
		return m, nil
	case "show":
		if rest == "" {
			m.chat.appendError("usage: /skills show <name>")
			return m, nil
		}
		for _, s := range m.loadedSkills() {
			if s.Name == rest {
				m.chat.appendSlashOutput(s.Body)
				return m, nil
			}
		}
		m.chat.appendError("unknown skill: " + rest)
		return m, nil
	case "new":
		return m.cmdSkillNew(rest)
	case "edit":
		return m.cmdSkillEdit(rest)
	case "reset":
		return m.cmdSkillReset(rest)
	default:
		m.chat.appendError("unknown /skills argument; try /skills list")
		return m, nil
	}
}

// loadedSkills returns the current skill set from the loader.
func (m *Model) loadedSkills() []assets.Skill {
	if m.skillLoader == nil {
		return nil
	}
	out := make([]assets.Skill, 0)
	for _, n := range m.skillLoader.Names() {
		out = append(out, m.skillLoader.Skill(n))
	}
	return out
}

func (m *Model) cmdSkillNew(name string) (tea.Model, tea.Cmd) {
	if name == "" {
		m.chat.appendError("usage: /skills new <name>")
		return m, nil
	}
	for _, s := range m.loadedSkills() {
		if s.Name == name {
			m.chat.appendError("skill already exists: " + name)
			return m, nil
		}
	}
	path := assets.Path("skills/" + name)
	if path == "" {
		m.chat.appendError("invalid skill name: " + name)
		return m, nil
	}
	m.pendingAssetPath = path
	m.pendingAssetReload = true
	body := fmt.Sprintf(skillFrontmatterTemplate, name)
	return m, openEditorFileCmd(path, body)
}

func (m *Model) cmdSkillEdit(name string) (tea.Model, tea.Cmd) {
	if name == "" {
		m.chat.appendError("usage: /skills edit <name>")
		return m, nil
	}
	path := assets.Path("skills/" + name)
	for _, s := range m.loadedSkills() {
		if s.Name == name {
			m.pendingAssetPath = path
			m.pendingAssetReload = true
			return m, openEditorFileCmd(path, s.Body)
		}
	}
	m.chat.appendError("unknown skill: " + name)
	return m, nil
}

func (m *Model) cmdSkillReset(name string) (tea.Model, tea.Cmd) {
	if name == "" {
		m.chat.appendError("usage: /skills reset <name>")
		return m, nil
	}
	if err := assets.Reset("skills/" + name); err != nil {
		m.chat.appendError("reset failed: " + err.Error())
		return m, nil
	}
	m.chat.appendAgentDeltaBlock("skill " + name + " reset to the built-in version")
	return m.cmdReload()
}
