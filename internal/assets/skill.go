package assets

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// Source classifies a loaded skill by its relationship to the embedded set.
type Source int

const (
	SourceUser            Source = iota // no embedded counterpart
	SourceBuiltin                       // on-disk bytes match the embedded copy
	SourceBuiltinModified               // embedded counterpart exists, on-disk bytes differ
)

// String renders a Source for /skills list.
func (s Source) String() string {
	switch s {
	case SourceBuiltin:
		return "built-in"
	case SourceBuiltinModified:
		return "built-in*"
	default:
		return "user"
	}
}

// Skill is one loaded skill markdown file.
type Skill struct {
	Name        string // frontmatter name, else the filename stem
	Description string // frontmatter description, else ""
	Body        string // markdown after the frontmatter block
	Path        string // absolute on-disk path
	Source      Source
}

// parseFrontmatter splits a skill file into optional YAML frontmatter and the
// markdown body. A file not beginning with a "---" fence has no frontmatter.
// Unknown frontmatter keys are returned (for a warning), not an error.
func parseFrontmatter(src []byte) (name, description string, unknownKeys []string, body string, err error) {
	s := string(src)
	if !strings.HasPrefix(s, "---\n") && !strings.HasPrefix(s, "---\r\n") {
		return "", "", nil, s, nil
	}
	rest := s[strings.IndexByte(s, '\n')+1:]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return "", "", nil, "", fmt.Errorf("frontmatter opened with --- but was never closed")
	}
	yamlText := rest[:end]
	afterFence := rest[end+1:] // begins at the closing "---" line
	if nl := strings.IndexByte(afterFence, '\n'); nl >= 0 {
		body = afterFence[nl+1:]
	}
	var raw map[string]any
	if err := yaml.Unmarshal([]byte(yamlText), &raw); err != nil {
		return "", "", nil, "", fmt.Errorf("invalid frontmatter YAML: %w", err)
	}
	for k := range raw {
		if k != "name" && k != "description" {
			unknownKeys = append(unknownKeys, k)
		}
	}
	name, _ = raw["name"].(string)
	description, _ = raw["description"].(string)
	return name, description, unknownKeys, body, nil
}
