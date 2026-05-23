package knowledge

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/alvarogonjim/fova/internal/domain"
	"github.com/alvarogonjim/fova/internal/tools"
)

// maxFetchRunes caps the plain text returned by web_fetch.
const maxFetchRunes = 100000

var _ tools.Tool = (*WebFetch)(nil)

// WebFetch implements the knowledge.web_fetch tool: it retrieves a URL and
// returns its readable plain-text content.
type WebFetch struct{}

// NewWebFetch returns the knowledge.web_fetch tool.
func NewWebFetch() *WebFetch { return &WebFetch{} }

type webFetchOutput struct {
	URL       string `json:"url"`
	Text      string `json:"text"`
	Truncated bool   `json:"truncated"`
}

func (*WebFetch) Name() string     { return "knowledge.web_fetch" }
func (*WebFetch) Concurrent() bool { return true }
func (*WebFetch) Description() string {
	return "Fetch a web page or document by URL and return its readable plain-text content. " +
		"HTML is stripped of scripts, styles and tags."
}
func (*WebFetch) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"url": map[string]any{
				"type":        "string",
				"description": "The http or https URL to fetch",
			},
		},
		"required": []string{"url"},
	}
}
func (*WebFetch) RequiresConfirmation(json.RawMessage) bool       { return false }
func (*WebFetch) EstimatedCostUSD(json.RawMessage) float64        { return 0 }
func (*WebFetch) EstimatedDuration(json.RawMessage) time.Duration { return 5 * time.Second }

// Execute fetches the URL and returns its readable text.
func (*WebFetch) Execute(ctx context.Context, input json.RawMessage) (tools.Result, error) {
	var in struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return tools.Result{}, err
	}
	if in.URL == "" {
		return tools.Result{}, fmt.Errorf("knowledge.web_fetch: url is required")
	}
	u, err := url.Parse(in.URL)
	if err != nil {
		return tools.Result{}, fmt.Errorf("knowledge.web_fetch: invalid url %q: %w", in.URL, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return tools.Result{}, fmt.Errorf("knowledge.web_fetch: unsupported url scheme %q: only http and https are allowed", u.Scheme)
	}

	body, err := getBytes(ctx, in.URL, "")
	if err != nil {
		return tools.Result{}, err
	}

	text := string(body)
	lower := strings.ToLower(text)
	// NOTE: the shared getBytes helper returns only the response body, not the
	// response headers, so HTML is detected by sniffing the body bytes (and the
	// URL path suffix) rather than the Content-Type header. An unwrapped HTML
	// fragment with no <html>/<body> tag may therefore not be detected.
	isHTML := strings.Contains(lower, "<html") || strings.Contains(lower, "<body") ||
		strings.HasSuffix(strings.ToLower(u.Path), ".html")
	if isHTML {
		text = htmlToText(text)
	}

	runes := []rune(text)
	truncated := false
	if len(runes) > maxFetchRunes {
		runes = runes[:maxFetchRunes]
		truncated = true
		text = string(runes)
	}

	out := webFetchOutput{URL: in.URL, Text: text, Truncated: truncated}
	outJSON, _ := json.Marshal(out)
	return tools.Result{
		Output:     outJSON,
		Display:    fmt.Sprintf("fetched %s (%d chars)", in.URL, len(runes)),
		Provenance: domain.NewToolCallRef("knowledge.web_fetch", input),
	}, nil
}

var (
	scriptStyleRe = regexp.MustCompile(`(?is)<(script|style)\b[^>]*>.*?</(script|style)>`)
	tagRe         = regexp.MustCompile(`(?s)<[^>]*>`)
	whitespaceRe  = regexp.MustCompile(`\s+`)
)

// htmlToText strips an HTML document down to readable plain text.
func htmlToText(s string) string {
	s = scriptStyleRe.ReplaceAllString(s, " ")
	s = tagRe.ReplaceAllString(s, " ")
	s = html.UnescapeString(s)
	// Normalize non-breaking spaces so they collapse with ordinary runs.
	s = strings.ReplaceAll(s, " ", " ")
	s = whitespaceRe.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}
