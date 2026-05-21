package skills

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/alvarogonjim/fova/internal/assets"
)

func testSkills() []assets.Skill {
	return []assets.Skill{
		{Name: "filter-thresholds", Description: "Standard score cutoffs", Body: "rank by ipSAE"},
		{Name: "design-binder", Description: "Binder design", Body: "use design.boltzgen"},
	}
}

func TestLoaderListsSkill(t *testing.T) {
	l := NewLoader(testSkills())
	found := false
	for _, n := range l.Names() {
		if n == "filter-thresholds" {
			found = true
		}
	}
	if !found {
		t.Fatalf("filter-thresholds not loaded; got %v", l.Names())
	}
}

func TestSkillsListToolShowsDescriptions(t *testing.T) {
	l := NewLoader(testSkills())
	res, err := l.ListTool().Execute(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Display, "filter-thresholds — Standard score cutoffs") {
		t.Fatalf("skills.list missing the description column: %q", res.Display)
	}
}

func TestSkillsReadToolReturnsBody(t *testing.T) {
	l := NewLoader(testSkills())
	res, err := l.ReadTool().Execute(context.Background(),
		json.RawMessage(`{"name":"filter-thresholds"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Display, "ipSAE") {
		t.Fatalf("skills.read returned wrong content: %q", res.Display)
	}
	if _, err := l.ReadTool().Execute(context.Background(),
		json.RawMessage(`{"name":"does-not-exist"}`)); err == nil {
		t.Fatal("reading an unknown skill should error")
	}
}
