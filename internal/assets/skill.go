package assets

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

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

// kebabRE matches a valid skill name: lowercase words joined by single dashes.
var kebabRE = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// loadSkills reads every *.md file in skillsDir, parses and validates each,
// and returns the valid skills sorted by name. A file that fails validation
// is skipped and recorded as a Report error; advisory problems are warnings.
// A missing skillsDir yields no skills and no error.
func loadSkills(skillsDir string) ([]Skill, Report) {
	var (
		out  []Skill
		rep  Report
		seen = map[string]string{} // name -> file that claimed it
	)
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		if !os.IsNotExist(err) {
			rep.Errors = append(rep.Errors, AssetIssue{"skills/", err.Error()})
		}
		return nil, rep
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		file := e.Name()
		asset := "skills/" + file
		full := filepath.Join(skillsDir, file)
		raw, err := os.ReadFile(full)
		if err != nil {
			rep.Errors = append(rep.Errors, AssetIssue{asset, err.Error()})
			continue
		}
		if !utf8.Valid(raw) {
			rep.Errors = append(rep.Errors, AssetIssue{asset, "file is not valid UTF-8"})
			continue
		}
		stem := strings.TrimSuffix(file, ".md")
		if !kebabRE.MatchString(stem) {
			rep.Errors = append(rep.Errors, AssetIssue{asset,
				"filename stem must be kebab-case (lowercase, digits, single dashes)"})
			continue
		}
		fmName, fmDesc, unknown, body, err := parseFrontmatter(raw)
		if err != nil {
			rep.Errors = append(rep.Errors, AssetIssue{asset, err.Error()})
			continue
		}
		if fmName != "" && fmName != stem {
			rep.Errors = append(rep.Errors, AssetIssue{asset,
				fmt.Sprintf("frontmatter name %q must equal the filename stem %q", fmName, stem)})
			continue
		}
		if strings.TrimSpace(body) == "" {
			rep.Errors = append(rep.Errors, AssetIssue{asset, "skill body is empty"})
			continue
		}
		for _, k := range unknown {
			rep.Warnings = append(rep.Warnings, AssetIssue{asset, "unknown frontmatter key: " + k})
		}
		if strings.ContainsRune(fmDesc, '\n') {
			rep.Warnings = append(rep.Warnings, AssetIssue{asset, "description should be a single line"})
		}
		if len(fmDesc) > 120 {
			rep.Warnings = append(rep.Warnings, AssetIssue{asset, "description exceeds 120 characters"})
		}
		if prev, dup := seen[stem]; dup {
			rep.Errors = append(rep.Errors, AssetIssue{asset,
				fmt.Sprintf("duplicate skill name %q (also defined by %s)", stem, prev)})
			continue
		}
		seen[stem] = file
		out = append(out, Skill{
			Name:        stem,
			Description: fmDesc,
			Body:        body,
			Path:        full,
			Source:      skillSource(file, raw),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, rep
}

// skillSource compares an on-disk skill file to its embedded counterpart.
func skillSource(file string, onDisk []byte) Source {
	emb, ok := embeddedBytes("skills/" + file)
	if !ok {
		return SourceUser
	}
	if string(emb) == string(onDisk) {
		return SourceBuiltin
	}
	return SourceBuiltinModified
}
