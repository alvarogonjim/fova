package score

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

const samplePDB = `ATOM      1  N   MET A   1      10.000  10.000  10.000  1.00 50.00
ATOM      2  CA  MET A   1      11.000  12.000  13.000  1.00 90.00
ATOM      3  CA  GLY A   2      14.000  15.000  16.000  1.00 80.00
ATOM      4  CA  SER A   3      17.000  18.000  19.000  1.00 70.00
TER
END
`

func TestMetricsParsesPLDDT(t *testing.T) {
	dir := t.TempDir()
	pdb := filepath.Join(dir, "s.pdb")
	if err := os.WriteFile(pdb, []byte(samplePDB), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := NewMetricsTool().Execute(context.Background(),
		json.RawMessage(`{"structure_file":"`+pdb+`"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var out struct {
		Metrics map[string]float64 `json:"metrics"`
	}
	if err := json.Unmarshal(res.Output, &out); err != nil {
		t.Fatal(err)
	}
	if out.Metrics["plddt_mean"] != 80.0 {
		t.Errorf("plddt_mean = %v, want 80.0", out.Metrics["plddt_mean"])
	}
	if out.Metrics["plddt_min"] != 70.0 {
		t.Errorf("plddt_min = %v, want 70.0", out.Metrics["plddt_min"])
	}
}

func TestMetricsMissingFile(t *testing.T) {
	if _, err := NewMetricsTool().Execute(context.Background(),
		json.RawMessage(`{"structure_file":"/no/such/file.pdb"}`)); err == nil {
		t.Error("a missing structure file should error")
	}
}
