package viz

import (
	"context"
	"encoding/json"
	"fmt"
	"image/color"
	"os"
	"sort"

	"gonum.org/v1/plot"
	"gonum.org/v1/plot/plotter"
	"gonum.org/v1/plot/vg"
	"gonum.org/v1/plot/vg/draw"
	"gonum.org/v1/plot/vg/vgimg"

	"github.com/alvarogonjim/fova/internal/domain"
	"github.com/alvarogonjim/fova/internal/tools"
	"github.com/alvarogonjim/fova/internal/tools/knowledge"
)

// MetricPlot implements viz.metric_plot: render histograms of per-metric value
// distributions, one panel per metric, into a single PNG.
type MetricPlot struct {
	noopMeta
	workspace string
	results   *knowledge.Results
}

// NewMetricPlot builds the viz.metric_plot tool. workspace is the project
// root; outputs land under <workspace>/designs/. results is shared with the
// knowledge package so the tool can consume a results_id from a prior search.
func NewMetricPlot(workspace string, results *knowledge.Results) *MetricPlot {
	return &MetricPlot{workspace: workspace, results: results}
}

func (*MetricPlot) Concurrent() bool { return true }
func (*MetricPlot) Name() string     { return "viz.metric_plot" }
func (*MetricPlot) Description() string {
	return "Render side-by-side histograms of one or more numeric metric distributions to a PNG."
}
func (*MetricPlot) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"metrics": map[string]any{
				"type":        "object",
				"description": "Map of metric name to a list of numeric samples.",
			},
			"results_id": map[string]any{
				"type":        "string",
				"description": "Optional: results_id from a knowledge.* search; the per-paper year is plotted under the metric name \"year\".",
			},
			"title": map[string]any{"type": "string", "description": "Optional plot title."},
		},
	}
}

func (t *MetricPlot) Execute(_ context.Context, input json.RawMessage) (tools.Result, error) {
	var in struct {
		Metrics   map[string][]float64 `json:"metrics"`
		ResultsID string               `json:"results_id"`
		Title     string               `json:"title"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return tools.Result{}, fmt.Errorf("viz.metric_plot: parse input: %w", err)
	}

	metrics := in.Metrics
	if len(metrics) == 0 && in.ResultsID != "" {
		papers, ok := t.results.Get(in.ResultsID)
		if !ok {
			return tools.Result{}, fmt.Errorf("viz.metric_plot: unknown results_id %q", in.ResultsID)
		}
		years := make([]float64, 0, len(papers))
		for _, p := range papers {
			if p.Year > 0 {
				years = append(years, float64(p.Year))
			}
		}
		if len(years) == 0 {
			return tools.Result{}, fmt.Errorf("viz.metric_plot: results_id %q has no numeric fields", in.ResultsID)
		}
		metrics = map[string][]float64{"year": years}
	}
	if len(metrics) == 0 {
		return tools.Result{}, fmt.Errorf("viz.metric_plot: metrics or results_id is required")
	}

	out, err := OutputPath(t.workspace, "metric_plot", "png")
	if err != nil {
		return tools.Result{}, fmt.Errorf("viz.metric_plot: %w", err)
	}
	if err := renderHistograms(metrics, in.Title, out); err != nil {
		return tools.Result{}, fmt.Errorf("viz.metric_plot: render: %w", err)
	}
	body, _ := json.Marshal(map[string]any{
		"path":    out,
		"metrics": sortedKeys(metrics),
		"count":   sampleCount(metrics),
	})
	return tools.Result{
		Output:     body,
		Display:    fmt.Sprintf("metric_plot: %d metric(s), %d samples → %s", len(metrics), sampleCount(metrics), out),
		Provenance: domain.NewToolCallRef("viz.metric_plot", input),
	}, nil
}

// renderHistograms draws one histogram panel per metric, side-by-side, into a
// single PNG. Each panel auto-bins to roughly sqrt(N) buckets (min 8, max 32).
func renderHistograms(metrics map[string][]float64, title, outPath string) error {
	names := sortedKeys(metrics)
	const panelW, panelH = 3 * vg.Inch, 3 * vg.Inch
	totalW := panelW * vg.Length(len(names))
	canvas := vgimg.New(totalW, panelH)
	dc := draw.New(canvas)

	for i, name := range names {
		samples := metrics[name]
		p := plot.New()
		p.Title.Text = name
		if title != "" && i == 0 {
			p.Title.Text = title + " · " + name
		}
		p.Y.Label.Text = "count"
		vals := make(plotter.Values, len(samples))
		copy(vals, samples)
		bins := bucketCount(len(samples))
		h, err := plotter.NewHist(vals, bins)
		if err != nil {
			return err
		}
		h.FillColor = color.RGBA{R: 60, G: 110, B: 200, A: 255}
		p.Add(h)

		// Lay out the i-th panel left-to-right inside the canvas.
		x0 := panelW * vg.Length(i)
		panel := draw.Crop(dc, x0, x0+panelW-totalW, 0, 0)
		p.Draw(panel)
	}

	f, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer f.Close()
	png := vgimg.PngCanvas{Canvas: canvas}
	if _, err := png.WriteTo(f); err != nil {
		return err
	}
	return nil
}

// sortedKeys returns metric names in a deterministic order so tests, prompts
// and the resulting PNG panels are stable.
func sortedKeys(m map[string][]float64) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sampleCount(m map[string][]float64) int {
	n := 0
	for _, v := range m {
		n += len(v)
	}
	return n
}

// bucketCount returns a sane histogram bin count for n samples.
func bucketCount(n int) int {
	if n <= 1 {
		return 1
	}
	b := 1
	// roughly sqrt(n); clamp to [8, 32].
	for b*b < n {
		b++
	}
	if b < 8 {
		b = 8
	}
	if b > 32 {
		b = 32
	}
	return b
}
