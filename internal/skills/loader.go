// Package skills exposes loaded skills as the skills.list and skills.read
// agent tools. The skills themselves are loaded by internal/assets.
package skills

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/alvarogonjim/fova/internal/assets"
	"github.com/alvarogonjim/fova/internal/domain"
	"github.com/alvarogonjim/fova/internal/tools"
)

// Loader holds the loaded skills, keyed by name.
type Loader struct {
	skills map[string]assets.Skill
}

// NewLoader wraps an already-loaded skill set (from assets.Bundle.Skills).
func NewLoader(skills []assets.Skill) *Loader {
	m := make(map[string]assets.Skill, len(skills))
	for _, s := range skills {
		m[s.Name] = s
	}
	return &Loader{skills: m}
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

// Skill returns the loaded skill with the given name (zero value if absent).
func (l *Loader) Skill(name string) assets.Skill { return l.skills[name] }

// ListTool returns the skills.list tool.
func (l *Loader) ListTool() tools.Tool { return skillsList{l} }

// ReadTool returns the skills.read tool.
func (l *Loader) ReadTool() tools.Tool { return skillsRead{l} }

// --- skills.list ---

type skillsList struct{ l *Loader }

func (skillsList) Name() string        { return "skills.list" }
func (skillsList) Description() string { return "List available fova skills." }
func (skillsList) InputSchema() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{}}
}
func (skillsList) RequiresConfirmation(json.RawMessage) bool       { return false }
func (skillsList) EstimatedCostUSD(json.RawMessage) float64        { return 0 }
func (skillsList) EstimatedDuration(json.RawMessage) time.Duration { return time.Millisecond }
func (t skillsList) Execute(_ context.Context, input json.RawMessage) (tools.Result, error) {
	var b strings.Builder
	for _, n := range t.l.Names() {
		if d := t.l.skills[n].Description; d != "" {
			fmt.Fprintf(&b, "- %s — %s\n", n, d)
		} else {
			fmt.Fprintf(&b, "- %s\n", n)
		}
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
	s, ok := t.l.skills[in.Name]
	if !ok {
		return tools.Result{}, fmt.Errorf("unknown skill %q", in.Name)
	}
	return tools.Result{
		Display:    s.Body,
		Provenance: domain.NewToolCallRef("skills.read", input),
	}, nil
}
