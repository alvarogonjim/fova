package score

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestIPSAEParsesScore(t *testing.T) {
	tool := NewIPSAETool()
	// Stub the runner: return ipSAE tool output containing a score line.
	tool.run = func(ctx context.Context, scoresJSON, structureFile string) (string, error) {
		return "ipSAE analysis complete\nipSAE_max 0.73\n", nil
	}
	res, err := tool.Execute(context.Background(),
		json.RawMessage(`{"scores_json":"s.json","structure_file":"c.pdb"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var out struct {
		Metrics map[string]float64 `json:"metrics"`
	}
	if err := json.Unmarshal(res.Output, &out); err != nil {
		t.Fatal(err)
	}
	if out.Metrics["ipsae"] != 0.73 {
		t.Errorf("ipsae = %v, want 0.73", out.Metrics["ipsae"])
	}
}

func TestIPSAEReportsToolFailure(t *testing.T) {
	tool := NewIPSAETool()
	tool.run = func(ctx context.Context, scoresJSON, structureFile string) (string, error) {
		return "", errExample
	}
	_, err := tool.Execute(context.Background(),
		json.RawMessage(`{"scores_json":"s.json","structure_file":"c.pdb"}`))
	if err == nil || !strings.Contains(err.Error(), "ipsae") {
		t.Errorf("expected an ipsae error, got %v", err)
	}
}

// errExample is a sentinel error for the failure test.
var errExample = exampleError("ipsae not installed")

type exampleError string

func (e exampleError) Error() string { return string(e) }
