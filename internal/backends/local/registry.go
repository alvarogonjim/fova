// Package local implements Proteus's uv-managed local tool backend: parsing
// install recipes, installing tools, running them, and diagnostics.
package local

import (
	_ "embed"
	"fmt"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

//go:embed tools.toml
var toolsTOML string

// ToolRecipe describes how one Python tool is installed and invoked.
type ToolRecipe struct {
	Name         string   `toml:"-"`
	DisplayName  string   `toml:"display_name"`
	Version      string   `toml:"version"`
	Python       string   `toml:"python"`
	Repo         string   `toml:"repo"`
	GitRef       string   `toml:"git_ref"`
	InstallDir   string   `toml:"install_dir"`
	VenvDir      string   `toml:"venv_dir"`
	RequiresGPU  bool     `toml:"requires_gpu"`
	DiskGB       float64  `toml:"disk_gb"`
	ExtraData    []string `toml:"extra_data"`
	InstallSteps []string `toml:"install_steps"`
	RunCommand   string   `toml:"run_command"`
}

// DataAsset is a large shared download (model weights) used by some tools.
type DataAsset struct {
	Name        string   `toml:"-"`
	DisplayName string   `toml:"display_name"`
	URL         string   `toml:"url"`
	URLs        []string `toml:"urls"`
	SHA256      string   `toml:"sha256"`
	ExtractTo   string   `toml:"extract_to"`
	TargetDir   string   `toml:"target_dir"`
	SizeGB      float64  `toml:"size_gb"`
}

// Registry is the parsed, placeholder-expanded set of install recipes.
type Registry struct {
	home  string
	tools map[string]ToolRecipe
	data  map[string]DataAsset
}

// LoadRegistry parses the embedded tools.toml and expands ${PROTEUS_HOME} and
// recipe-field {{ }} placeholders against the given Proteus home directory.
func LoadRegistry(home string) (*Registry, error) {
	var doc struct {
		Tools map[string]ToolRecipe `toml:"tools"`
		Data  map[string]DataAsset  `toml:"data"`
	}
	if _, err := toml.Decode(toolsTOML, &doc); err != nil {
		return nil, fmt.Errorf("parse tools.toml: %w", err)
	}
	r := &Registry{home: home, tools: map[string]ToolRecipe{}, data: map[string]DataAsset{}}
	for name, rec := range doc.Tools {
		rec.Name = name
		if rec.GitRef == "" {
			rec.GitRef = "main"
		}
		if rec.InstallDir == "" {
			rec.InstallDir = "${PROTEUS_HOME}/tools/" + name
		}
		r.tools[name] = expandRecipe(rec, home)
	}
	for name, d := range doc.Data {
		d.Name = name
		d.ExtractTo = expandHome(d.ExtractTo, home)
		d.TargetDir = expandHome(d.TargetDir, home)
		r.data[name] = d
	}
	return r, nil
}

// Tool returns the recipe for a tool by name.
func (r *Registry) Tool(name string) (ToolRecipe, bool) {
	rec, ok := r.tools[name]
	return rec, ok
}

// Tools returns all recipes, sorted by name.
func (r *Registry) Tools() []ToolRecipe {
	out := make([]ToolRecipe, 0, len(r.tools))
	for _, rec := range r.tools {
		out = append(out, rec)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// DataAsset returns a data asset by name.
func (r *Registry) DataAsset(name string) (DataAsset, bool) {
	d, ok := r.data[name]
	return d, ok
}

// Home returns the Proteus home directory the registry was loaded for.
func (r *Registry) Home() string { return r.home }

func expandHome(s, home string) string {
	return strings.ReplaceAll(s, "${PROTEUS_HOME}", home)
}

// expandRecipe expands ${PROTEUS_HOME} and the recipe-field {{ }} placeholders.
// Runtime placeholders in run_command (e.g. {{ input_json }}) that do not match
// a recipe field are deliberately left intact for the runner to fill.
func expandRecipe(rec ToolRecipe, home string) ToolRecipe {
	rec.InstallDir = expandHome(rec.InstallDir, home)
	rec.VenvDir = expandHome(rec.VenvDir, home)
	fields := map[string]string{
		"repo":        rec.Repo,
		"git_ref":     rec.GitRef,
		"python":      rec.Python,
		"venv_dir":    rec.VenvDir,
		"install_dir": rec.InstallDir,
	}
	steps := make([]string, len(rec.InstallSteps))
	for i, s := range rec.InstallSteps {
		steps[i] = expandPlaceholders(expandHome(s, home), fields)
	}
	rec.InstallSteps = steps
	rec.RunCommand = expandPlaceholders(expandHome(rec.RunCommand, home), fields)
	return rec
}

// expandPlaceholders replaces {{ key }} (and {{key}}) with the mapped value.
func expandPlaceholders(s string, fields map[string]string) string {
	for k, v := range fields {
		s = strings.ReplaceAll(s, "{{ "+k+" }}", v)
		s = strings.ReplaceAll(s, "{{"+k+"}}", v)
	}
	return s
}
