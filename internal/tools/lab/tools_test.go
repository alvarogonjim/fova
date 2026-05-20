package lab

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/alvarogonjim/fova/internal/store"
	"github.com/alvarogonjim/fova/internal/tools"
)

// toolTestClient returns a Client whose requests are served by the given
// handler. The httptest server is closed on test cleanup.
func toolTestClient(t *testing.T, h http.Handler) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return testClient(srv)
}

// TestLabToolNames checks every tool reports its declared name.
func TestLabToolNames(t *testing.T) {
	c := NewClient("tok")
	st := &store.Store{}
	for _, tc := range []struct {
		tool tools.Tool
		name string
	}{
		{NewTargetsSearchTool(c), "lab.targets_search"},
		{NewCostEstimateTool(c), "lab.cost_estimate"},
		{NewExperimentStatusTool(c), "lab.experiment_status"},
		{NewResultsTool(c), "lab.results"},
		{NewSubmitExperimentTool(c, st, ""), "lab.submit_experiment"},
	} {
		if got := tc.tool.Name(); got != tc.name {
			t.Errorf("Name = %q, want %q", got, tc.name)
		}
	}
}

// TestLabToolsImplementToolInterface checks all five satisfy tools.Tool.
func TestLabToolsImplementToolInterface(t *testing.T) {
	c := NewClient("tok")
	st := &store.Store{}
	var _ tools.Tool = NewTargetsSearchTool(c)
	var _ tools.Tool = NewCostEstimateTool(c)
	var _ tools.Tool = NewExperimentStatusTool(c)
	var _ tools.Tool = NewResultsTool(c)
	var _ tools.Tool = NewSubmitExperimentTool(c, st, "")
}

// TestLabToolSubmitRequiresConfirmation checks only the submit tool confirms.
func TestLabToolSubmitRequiresConfirmation(t *testing.T) {
	c := NewClient("tok")
	st := &store.Store{}
	if !NewSubmitExperimentTool(c, st, "").RequiresConfirmation(nil) {
		t.Error("lab.submit_experiment must require confirmation")
	}
	for _, tl := range []tools.Tool{
		NewTargetsSearchTool(c),
		NewCostEstimateTool(c),
		NewExperimentStatusTool(c),
		NewResultsTool(c),
	} {
		if tl.RequiresConfirmation(nil) {
			t.Errorf("%s must not require confirmation", tl.Name())
		}
	}
}

// TestLabToolTargetsSearch checks the targets_search tool decodes the catalog.
func TestLabToolTargetsSearch(t *testing.T) {
	c := toolTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[{"id":"t1","name":"EGFR"},{"id":"t2","name":"VEGF"}]`))
	}))
	res, err := NewTargetsSearchTool(c).Execute(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.Provenance.Tool != "lab.targets_search" {
		t.Errorf("provenance tool = %q", res.Provenance.Tool)
	}
	var out struct {
		Targets []Target `json:"targets"`
	}
	if err := json.Unmarshal(res.Output, &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Targets) != 2 {
		t.Fatalf("expected 2 targets, got %d", len(out.Targets))
	}
}

// TestLabToolCostEstimate checks the cost_estimate tool surfaces pricing.
func TestLabToolCostEstimate(t *testing.T) {
	c := toolTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"total_usd":1200.5,"turnaround_days":21}`))
	}))
	in := json.RawMessage(`{"target_id":"t1","assay_type":"affinity","sequences":[{"name":"d1","sequence":"MAQ"}]}`)
	res, err := NewCostEstimateTool(c).Execute(context.Background(), in)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.Cost != 1200.5 {
		t.Errorf("cost = %v, want 1200.5", res.Cost)
	}
}

// TestLabToolExperimentStatus checks the experiment_status tool reports state.
func TestLabToolExperimentStatus(t *testing.T) {
	c := toolTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"id":"exp_42","status":"running","target_id":"t1"}`))
	}))
	in := json.RawMessage(`{"experiment_id":"exp_42"}`)
	res, err := NewExperimentStatusTool(c).Execute(context.Background(), in)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var exp Experiment
	if err := json.Unmarshal(res.Output, &exp); err != nil {
		t.Fatal(err)
	}
	if exp.Status != "running" {
		t.Errorf("status = %q, want running", exp.Status)
	}
}

// TestLabToolResults checks the results tool decodes measured kinetics.
func TestLabToolResults(t *testing.T) {
	c := toolTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[{"sequence_name":"d1","kd":1.2e-9,"kd_units":"M"}]`))
	}))
	in := json.RawMessage(`{"experiment_id":"exp_42"}`)
	res, err := NewResultsTool(c).Execute(context.Background(), in)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var out struct {
		Results []Result `json:"results"`
	}
	if err := json.Unmarshal(res.Output, &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Results) != 1 || out.Results[0].SequenceName != "d1" {
		t.Fatalf("unexpected results: %+v", out.Results)
	}
}

// TestLabToolSubmitPersistsExperiment checks a submit against an httptest
// server persists a domain.Experiment carrying the returned Adaptyv id.
func TestLabToolSubmitPersistsExperiment(t *testing.T) {
	c := toolTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		w.Write([]byte(`{"id":"adaptyv_777","status":"submitted","target_id":"t1","cost_usd":900}`))
	}))

	st, err := store.Open(filepath.Join(t.TempDir(), "x.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	tool := NewSubmitExperimentTool(c, st, "")
	in := json.RawMessage(`{"target_id":"t1","assay_type":"affinity","sequences":[{"name":"d1","sequence":"MAQVQL"}]}`)
	res, err := tool.Execute(context.Background(), in)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.Provenance.Tool != "lab.submit_experiment" {
		t.Errorf("provenance tool = %q", res.Provenance.Tool)
	}

	list, err := st.ListExperiments(store.DefaultProjectID)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 persisted experiment, got %d", len(list))
	}
	if list[0].ExternalID != "adaptyv_777" {
		t.Errorf("ExternalID = %q, want adaptyv_777", list[0].ExternalID)
	}
	if list[0].Backend != "adaptyv" {
		t.Errorf("Backend = %q, want adaptyv", list[0].Backend)
	}
	if list[0].TargetID != "t1" {
		t.Errorf("TargetID = %q, want t1", list[0].TargetID)
	}
	if list[0].CostUSD != 900 {
		t.Errorf("CostUSD = %v, want 900", list[0].CostUSD)
	}
}

func TestSubmitExperimentToolDefaultsWebhookURL(t *testing.T) {
	tool := &submitExperimentTool{defaultWebhookURL: "https://example.test/webhooks/adaptyv"}

	// Caller omits webhook_url → the configured default fills it in.
	var req SubmitRequest
	if err := json.Unmarshal([]byte(`{"target_id":"t","assay_type":"a","sequences":[]}`), &req); err != nil {
		t.Fatal(err)
	}
	if req.WebhookURL == "" {
		req.WebhookURL = tool.defaultWebhookURL
	}
	if req.WebhookURL != "https://example.test/webhooks/adaptyv" {
		t.Errorf("default not applied: %q", req.WebhookURL)
	}

	// A caller-supplied webhook_url is preserved.
	var req2 SubmitRequest
	if err := json.Unmarshal([]byte(`{"target_id":"t","assay_type":"a","sequences":[],"webhook_url":"https://caller.test/cb"}`), &req2); err != nil {
		t.Fatal(err)
	}
	if req2.WebhookURL == "" {
		req2.WebhookURL = tool.defaultWebhookURL
	}
	if req2.WebhookURL != "https://caller.test/cb" {
		t.Errorf("caller URL overwritten: %q", req2.WebhookURL)
	}
}
