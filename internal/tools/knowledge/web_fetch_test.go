package knowledge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWebFetchStripsHTML(t *testing.T) {
	const page = `<!DOCTYPE html>
<html>
<head>
<style>body { color: red; }</style>
<script>var secret = "DO_NOT_LEAK";</script>
</head>
<body>
<h1>Protein&nbsp;Design</h1>
<p>Folding   is   fun.</p>
</body>
</html>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(page))
	}))
	defer srv.Close()

	tool := NewWebFetch()
	input, _ := json.Marshal(map[string]string{"url": srv.URL})
	res, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var out struct {
		URL       string `json:"url"`
		Text      string `json:"text"`
		Truncated bool   `json:"truncated"`
	}
	if err := json.Unmarshal(res.Output, &out); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if strings.Contains(out.Text, "DO_NOT_LEAK") {
		t.Errorf("script content leaked into text: %q", out.Text)
	}
	if strings.Contains(out.Text, "color: red") {
		t.Errorf("style content leaked into text: %q", out.Text)
	}
	if !strings.Contains(out.Text, "Protein Design") {
		t.Errorf("heading missing from text: %q", out.Text)
	}
	if !strings.Contains(out.Text, "Folding is fun.") {
		t.Errorf("paragraph missing or whitespace not collapsed: %q", out.Text)
	}
	if strings.Contains(out.Text, "<") {
		t.Errorf("tags not stripped: %q", out.Text)
	}
	if out.Truncated {
		t.Errorf("unexpected truncation for short page")
	}
	if !strings.Contains(res.Display, "fetched") {
		t.Errorf("Display = %q, want it to mention fetched", res.Display)
	}
}

func TestWebFetchRejectsNonHTTPScheme(t *testing.T) {
	tool := NewWebFetch()
	for _, u := range []string{"file:///etc/passwd", "ftp://example.com/x"} {
		input, _ := json.Marshal(map[string]string{"url": u})
		if _, err := tool.Execute(context.Background(), input); err == nil {
			t.Errorf("expected error for scheme of %q", u)
		}
	}
}

func TestWebFetchPlainBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("just plain text, no markup"))
	}))
	defer srv.Close()

	tool := NewWebFetch()
	input, _ := json.Marshal(map[string]string{"url": srv.URL})
	res, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var out struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(res.Output, &out); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if out.Text != "just plain text, no markup" {
		t.Errorf("plain body altered: %q", out.Text)
	}
}
