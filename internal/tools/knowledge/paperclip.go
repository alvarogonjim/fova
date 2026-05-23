package knowledge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/alvarogonjim/fova/internal/domain"
	"github.com/alvarogonjim/fova/internal/tools"
)

// defaultPaperclipBaseURL is the public Paperclip endpoint. The MCP transport
// lives under /mcp.
const defaultPaperclipBaseURL = "https://api.paperclip.dev"

// Paperclip is the optional MCP forwarder. It is only registered when a
// PAPERCLIP_TOKEN environment variable is set.
type Paperclip struct {
	token   string
	baseURL string
}

// NewPaperclip returns the tool. token must be non-empty for Execute to work
// (the constructor itself never fails — registration in main.go gates it on
// the env var). baseURL may be empty, in which case the public default is
// used.
func NewPaperclip(token, baseURL string) *Paperclip {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = defaultPaperclipBaseURL
	}
	return &Paperclip{token: token, baseURL: strings.TrimRight(baseURL, "/")}
}

func (*Paperclip) Name() string     { return "knowledge.paperclip" }
func (*Paperclip) Concurrent() bool { return true }
func (*Paperclip) Description() string {
	return "Forward an MCP-shaped request to Paperclip and return the response " +
		"verbatim. Requires PAPERCLIP_TOKEN."
}
func (*Paperclip) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{"type": "string", "description": "Paperclip MCP action (search, fetch, ...)"},
		},
		"required":             []string{"action"},
		"additionalProperties": true,
	}
}
func (*Paperclip) RequiresConfirmation(json.RawMessage) bool       { return false }
func (*Paperclip) EstimatedCostUSD(json.RawMessage) float64        { return 0 }
func (*Paperclip) EstimatedDuration(json.RawMessage) time.Duration { return 5 * time.Second }

// Execute forwards the raw input JSON to Paperclip and returns the response.
func (t *Paperclip) Execute(ctx context.Context, input json.RawMessage) (tools.Result, error) {
	if strings.TrimSpace(t.token) == "" {
		return tools.Result{}, fmt.Errorf("knowledge.paperclip: PAPERCLIP_TOKEN is required")
	}
	// Sanity-check that input is JSON and that it carries an "action" field —
	// any other validation belongs server-side.
	var generic map[string]any
	if err := json.Unmarshal(input, &generic); err != nil {
		return tools.Result{}, fmt.Errorf("knowledge.paperclip: invalid JSON input: %w", err)
	}
	action, _ := generic["action"].(string)
	if strings.TrimSpace(action) == "" {
		return tools.Result{}, fmt.Errorf("knowledge.paperclip: action is required")
	}

	endpoint := t.baseURL + "/mcp"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint,
		bytes.NewReader(input))
	if err != nil {
		return tools.Result{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+t.token)
	req.Header.Set("User-Agent", userAgent)

	resp, err := httpClient.Do(req)
	if err != nil {
		// Never include the bearer token in surfaced errors. The url.Error
		// here only carries the URL, not the headers, so it's safe.
		return tools.Result{}, fmt.Errorf("knowledge.paperclip: forward: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return tools.Result{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Do NOT log or include t.token in the error. The response body is
		// safe to surface — Paperclip should never echo it back, but if it
		// did the token would already be the caller's, so still no exposure
		// of a secret we control.
		return tools.Result{}, fmt.Errorf("knowledge.paperclip: %s/mcp returned %d: %s",
			t.baseURL, resp.StatusCode, truncatePaperclip(string(body), 240))
	}

	display := fmt.Sprintf("paperclip %s: %d bytes", action, len(body))
	return tools.Result{
		Output:     body,
		Display:    display,
		Provenance: domain.NewToolCallRef("knowledge.paperclip", input),
	}, nil
}

// truncatePaperclip is the local truncate helper. (Defined separately from
// blast.go's truncate to avoid forcing an order-of-implementation between
// SP-D tasks if one ships ahead of the other.)
func truncatePaperclip(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
