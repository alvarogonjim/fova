// Package skills loads markdown skill files and exposes them as agent tools.
package skills

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/alvarogonjim/proteus/internal/domain"
	"github.com/alvarogonjim/proteus/internal/tools"
)

//go:embed builtin/*.md
var builtinFS embed.FS

// Loader holds the loaded skills, keyed by name (filename without ".md").
type Loader struct {
	skills map[string]string
}

// NewLoader reads every embedded built-in skill.
func NewLoader() *Loader {
	l := &Loader{skills: map[string]string{}}
	entries, _ := builtinFS.ReadDir("builtin")
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		data, err := builtinFS.ReadFile("builtin/" + e.Name())
		if err != nil {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".md")
		l.skills[name] = string(data)
	}
	return l
}

// Names returns the loaded skill names, sorted.
func (l *Loader) Names() []string {
	names := make([]string, 0, len(l.skills))
	for n := range l.skills {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// ListTool returns the skills.list tool.
func (l *Loader) ListTool() tools.Tool { return skillsList{l} }

// ReadTool returns the skills.read tool.
func (l *Loader) ReadTool() tools.Tool { return skillsRead{l} }

// --- skills.list ---

type skillsList struct{ l *Loader }

func (skillsList) Name() string        { return "skills.list" }
func (skillsList) Description() string { return "List available Proteus skills." }
func (skillsList) InputSchema() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{}}
}
func (skillsList) RequiresConfirmation(json.RawMessage) bool       { return false }
func (skillsList) EstimatedCostUSD(json.RawMessage) float64        { return 0 }
func (skillsList) EstimatedDuration(json.RawMessage) time.Duration { return time.Millisecond }
func (t skillsList) Execute(_ context.Context, input json.RawMessage) (tools.Result, error) {
	var b strings.Builder
	for _, n := range t.l.Names() {
		fmt.Fprintf(&b, "- %s\n", n)
	}
	return tools.Result{
		Display:    b.String(),
		Provenance: domain.NewToolCallRef("skills.list", input),
	}, nil
}

// --- skills.read ---

type skillsRead struct{ l *Loader }

func (skillsRead) Name() string        { return "skills.read" }
func (skillsRead) Description() string { return "Read the full markdown of one skill by name." }
func (skillsRead) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{"type": "string", "description": "Skill name"},
		},
		"required": []string{"name"},
	}
}
func (skillsRead) RequiresConfirmation(json.RawMessage) bool       { return false }
func (skillsRead) EstimatedCostUSD(json.RawMessage) float64        { return 0 }
func (skillsRead) EstimatedDuration(json.RawMessage) time.Duration { return time.Millisecond }
func (t skillsRead) Execute(_ context.Context, input json.RawMessage) (tools.Result, error) {
	var in struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return tools.Result{}, err
	}
	body, ok := t.l.skills[in.Name]
	if !ok {
		return tools.Result{}, fmt.Errorf("unknown skill %q", in.Name)
	}
	return tools.Result{
		Display:    body,
		Provenance: domain.NewToolCallRef("skills.read", input),
	}, nil
}
