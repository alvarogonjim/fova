package modal

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// EndpointEnvVar names the environment variable holding the deployed Modal
// web-endpoint URL.
const EndpointEnvVar = "PROTEUS_MODAL_ENDPOINT"

// Client invokes the deployed Modal functions app over HTTP.
type Client struct {
	endpoint string
	http     *http.Client
}

// NewClient builds a Modal client for the given web-endpoint URL.
func NewClient(endpoint string) *Client {
	return &Client{endpoint: endpoint, http: &http.Client{Timeout: time.Hour}}
}

// NewClientFromEnv builds a client from PROTEUS_MODAL_ENDPOINT. The returned
// client reports Configured()==false when the variable is unset.
func NewClientFromEnv() *Client {
	return NewClient(os.Getenv(EndpointEnvVar))
}

// Configured reports whether a Modal endpoint URL is set.
func (c *Client) Configured() bool { return c.endpoint != "" }

// Run posts {"tool": tool, "input": input} to the Modal endpoint and returns
// the tool's output JSON. The output schema matches the local backend's.
func (c *Client) Run(ctx context.Context, tool string, input json.RawMessage) (json.RawMessage, error) {
	if !c.Configured() {
		return nil, fmt.Errorf("modal backend not configured (set %s)", EndpointEnvVar)
	}
	if len(input) == 0 {
		input = json.RawMessage(`{}`)
	}
	payload, err := json.Marshal(map[string]any{"tool": tool, "input": input})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint,
		bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("modal request failed: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("modal endpoint returned %d: %s",
			resp.StatusCode, string(body))
	}
	return body, nil
}
