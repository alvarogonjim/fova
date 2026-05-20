package local

import (
	"context"
	"strings"
	"testing"
)

func TestRunnerRendersAndRuns(t *testing.T) {
	reg, _ := LoadRegistry(t.TempDir())
	runner := NewRunner(reg)

	var got string
	runner.run = func(ctx context.Context, dir, command string) (string, error) {
		got = command
		return "tool output", nil
	}
	out, err := runner.Run(context.Background(), "bindcraft", map[string]string{
		"input_json": "/tmp/req.json",
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out != "tool output" {
		t.Errorf("out = %q", out)
	}
	// The runtime placeholder was filled.
	if !strings.Contains(got, "/tmp/req.json") {
		t.Errorf("command did not get the input path: %q", got)
	}
	if strings.Contains(got, "{{") {
		t.Errorf("command still has an unfilled placeholder: %q", got)
	}
}

func TestRunnerUnknownTool(t *testing.T) {
	reg, _ := LoadRegistry(t.TempDir())
	runner := NewRunner(reg)
	if _, err := runner.Run(context.Background(), "no-such-tool", nil); err == nil {
		t.Error("running an unknown tool should error")
	}
}
