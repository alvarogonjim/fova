package skills

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestLoaderListsBuiltinSkill(t *testing.T) {
	l := NewLoader()
	names := l.Names()
	found := false
	for _, n := range names {
		if n == "filter-thresholds" {
			found = true
		}
	}
	if !found {
		t.Fatalf("filter-thresholds not loaded; got %v", names)
	}
}

func TestSkillsListTool(t *testing.T) {
	l := NewLoader()
	res, err := l.ListTool().Execute(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Display, "filter-thresholds") {
		t.Fatalf("skills.list missing skill: %q", res.Display)
	}
}

func TestSkillsReadTool(t *testing.T) {
	l := NewLoader()
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
