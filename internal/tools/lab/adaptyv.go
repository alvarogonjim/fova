package lab

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/alvarogonjim/proteus/internal/version"
)

// defaultBaseURL is the public Adaptyv Foundry API root (SPECS §12.1).
const defaultBaseURL = "https://foundry-api-public.adaptyvbio.com/api/v1"

// userAgent identifies Proteus on every Adaptyv request.
var userAgent = "Proteus/" + version.String() + " (https://github.com/alvarogonjim/proteus)"

// Client is a minimal Adaptyv Foundry API client.
//
// The request/response types in this file are hand-written, not generated.
// Verify their field names against the live OpenAPI spec
// (<baseURL>/openapi.json) before relying on a real submission.
type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

// NewClient returns a Client authenticated with the given bearer token.
func NewClient(token string) *Client {
	return &Client{
		baseURL: defaultBaseURL,
		token:   token,
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

// --- API types — verify field names against the live OpenAPI spec ---

// Target is one entry in the Adaptyv target catalog.
type Target struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Organism string `json:"organism,omitempty"`
}

// SequenceInput is one design sequence submitted for an assay.
type SequenceInput struct {
	Name     string `json:"name"`
	Sequence string `json:"sequence"`
}

// CostRequest asks Adaptyv to price an assay before submission.
type CostRequest struct {
	TargetID  string          `json:"target_id"`
	AssayType string          `json:"assay_type"`
	Sequences []SequenceInput `json:"sequences"`
}

// CostEstimate is the pre-flight pricing response.
type CostEstimate struct {
	TotalUSD       float64 `json:"total_usd"`
	TurnaroundDays int     `json:"turnaround_days"`
}

// SubmitRequest submits sequences for an assay against a target.
type SubmitRequest struct {
	TargetID   string          `json:"target_id"`
	AssayType  string          `json:"assay_type"`
	Sequences  []SequenceInput `json:"sequences"`
	WebhookURL string          `json:"webhook_url,omitempty"`
}

// Experiment is the Adaptyv-side experiment record.
type Experiment struct {
	ID         string  `json:"id"`
	Status     string  `json:"status"`
	TargetID   string  `json:"target_id"`
	TargetName string  `json:"target_name,omitempty"`
	AssayType  string  `json:"assay_type,omitempty"`
	CostUSD    float64 `json:"cost_usd,omitempty"`
}

// Result is one design's measured kinetic data.
type Result struct {
	SequenceName string   `json:"sequence_name"`
	Kd           *float64 `json:"kd,omitempty"`
	KdUnits      string   `json:"kd_units,omitempty"`
	Kon          *float64 `json:"kon,omitempty"`
	Koff         *float64 `json:"koff,omitempty"`
}

// ListTargets returns the Adaptyv target catalog.
func (c *Client) ListTargets(ctx context.Context) ([]Target, error) {
	var out []Target
	return out, c.do(ctx, http.MethodGet, "/targets", nil, &out)
}

// EstimateCost prices an assay before submission.
func (c *Client) EstimateCost(ctx context.Context, req CostRequest) (*CostEstimate, error) {
	var out CostEstimate
	if err := c.do(ctx, http.MethodPost, "/experiments/estimate", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// SubmitExperiment submits sequences for an assay and returns the new record.
func (c *Client) SubmitExperiment(ctx context.Context, req SubmitRequest) (*Experiment, error) {
	var out Experiment
	if err := c.do(ctx, http.MethodPost, "/experiments", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetExperiment fetches one experiment by its Adaptyv id.
func (c *Client) GetExperiment(ctx context.Context, id string) (*Experiment, error) {
	var out Experiment
	if err := c.do(ctx, http.MethodGet, "/experiments/"+id, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetResults fetches the measured results for an experiment.
func (c *Client) GetResults(ctx context.Context, id string) ([]Result, error) {
	var out []Result
	return out, c.do(ctx, http.MethodGet, "/experiments/"+id+"/results", nil, &out)
}

// do performs an HTTP request: it JSON-encodes body when non-nil, sets the
// bearer auth header, and JSON-decodes a 2xx response into out when non-nil.
func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reqBody = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reqBody)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("User-Agent", userAgent)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("adaptyv %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20)) // 8 MiB cap
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("adaptyv %s %s returned %d: %s", method, path,
			resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	if out != nil {
		if err := json.Unmarshal(respBody, out); err != nil {
			return fmt.Errorf("decode adaptyv response: %w", err)
		}
	}
	return nil
}
