package knowledge

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alvarogonjim/fova/internal/tools"
)

var _ tools.Tool = (*Paperclip)(nil)

func TestPaperclipForwardsBodyAndAuth(t *testing.T) {
	var seenAuth, seenBody, seenPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenAuth = r.Header.Get("Authorization")
		seenPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		seenBody = string(body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"echo":` + seenBody + `}`))
	}))
	defer srv.Close()

	tool := NewPaperclip("secret-token-123", srv.URL)

	out, err := tool.Execute(context.Background(),
		json.RawMessage(`{"action":"search","query":"protein binders"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if seenAuth != "Bearer secret-token-123" {
		t.Errorf("Authorization header = %q, want Bearer secret-token-123", seenAuth)
	}
	if seenPath != "/mcp" {
		t.Errorf("URL path = %q, want /mcp", seenPath)
	}
	if !strings.Contains(seenBody, "search") || !strings.Contains(seenBody, "protein binders") {
		t.Errorf("forwarded body = %q, want the input JSON verbatim", seenBody)
	}

	// Response is returned verbatim as Output.
	var resp map[string]any
	if err := json.Unmarshal(out.Output, &resp); err != nil {
		t.Fatalf("response is not JSON: %v", err)
	}
	if resp["ok"] != true {
		t.Errorf("output = %v, expected ok=true", resp)
	}
	if out.Display == "" {
		t.Error("Display should be set")
	}
}

func TestPaperclipServerErrorIsForwardedNotLogged(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "rate limited", http.StatusTooManyRequests)
	}))
	defer srv.Close()

	tool := NewPaperclip("secret-token-xyz", srv.URL)
	_, err := tool.Execute(context.Background(),
		json.RawMessage(`{"action":"search","query":"x"}`))
	if err == nil {
		t.Fatal("expected an error for 429 response")
	}
	if strings.Contains(err.Error(), "secret-token-xyz") {
		t.Fatalf("error must not leak the bearer token: %v", err)
	}
}

func TestPaperclipRequiresActionField(t *testing.T) {
	tool := NewPaperclip("t", "http://unused")
	if _, err := tool.Execute(context.Background(), json.RawMessage(`{}`)); err == nil {
		t.Fatal("expected an error when action is missing")
	}
}

func TestPaperclipRejectsEmptyToken(t *testing.T) {
	if tool := NewPaperclip("", "http://unused"); tool != nil {
		// NewPaperclip is allowed to return non-nil with an empty token, but
		// Execute must then fail loudly — never silently POST without auth.
		_, err := tool.Execute(context.Background(),
			json.RawMessage(`{"action":"search"}`))
		if err == nil {
			t.Fatal("expected an error when token is empty")
		}
	}
}
