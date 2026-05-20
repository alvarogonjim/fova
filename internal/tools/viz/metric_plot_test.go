package viz

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/alvarogonjim/fova/internal/tools/knowledge"
)

func TestMetricPlotProducesPNG(t *testing.T) {
	ws := t.TempDir()
	tool := NewMetricPlot(ws, knowledge.NewResults())
	in := json.RawMessage(`{"metrics":{"plddt":[80.1,82.3,79.6,85.2,77.9],"iptm":[0.62,0.71,0.68,0.74,0.59]}}`)
	res, err := tool.Execute(context.Background(), in)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var out struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(res.Output, &out); err != nil {
		t.Fatalf("Output is not JSON: %v", err)
	}
	if out.Path == "" {
		t.Fatal("Output.path is empty")
	}
	if !strings.Contains(out.Path, "metric_plot_") || !strings.HasSuffix(out.Path, ".png") {
		t.Errorf("Output.path = %q, want metric_plot_<id>.png", out.Path)
	}
	body, err := os.ReadFile(out.Path)
	if err != nil {
		t.Fatalf("read png: %v", err)
	}
	if !bytes.HasPrefix(body, []byte("\x89PNG\r\n\x1a\n")) {
		t.Fatalf("file %q is not a PNG (header = %q)", out.Path, body[:8])
	}
	if res.Provenance.Tool != "viz.metric_plot" {
		t.Errorf("Provenance.Tool = %q, want viz.metric_plot", res.Provenance.Tool)
	}
}

func TestMetricPlotEmptyMetricsErrors(t *testing.T) {
	tool := NewMetricPlot(t.TempDir(), knowledge.NewResults())
	if _, err := tool.Execute(context.Background(), []byte(`{"metrics":{}}`)); err == nil {
		t.Fatal("expected an error when metrics is empty")
	}
}

func TestMetricPlotFromResultsID(t *testing.T) {
	ws := t.TempDir()
	r := knowledge.NewResults()
	id := r.Put("openalex", []knowledge.Paper{
		{ID: "a", Title: "A", Year: 2020, Source: "openalex"},
		{ID: "b", Title: "B", Year: 2022, Source: "openalex"},
		{ID: "c", Title: "C", Year: 2024, Source: "openalex"},
	})
	tool := NewMetricPlot(ws, r)
	in := []byte(`{"results_id":"` + id + `"}`)
	res, err := tool.Execute(context.Background(), in)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var out struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(res.Output, &out); err != nil {
		t.Fatalf("Output is not JSON: %v", err)
	}
	if _, err := os.Stat(out.Path); err != nil {
		t.Fatalf("expected output file, stat: %v", err)
	}
}

func TestMetricPlotMissingInput(t *testing.T) {
	tool := NewMetricPlot(t.TempDir(), knowledge.NewResults())
	if _, err := tool.Execute(context.Background(), []byte(`{}`)); err == nil {
		t.Fatal("expected an error when neither metrics nor results_id is given")
	}
}
