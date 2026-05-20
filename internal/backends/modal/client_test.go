package modal

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClientRun(t *testing.T) {
	var gotTool string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Tool  string          `json:"tool"`
			Input json.RawMessage `json:"input"`
		}
		_ = json.Unmarshal(body, &req)
		gotTool = req.Tool
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"exit_code":0,"stdout":"done"}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	out, err := c.Run(context.Background(), "design.bindcraft", []byte(`{"settings":"x"}`))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if gotTool != "design.bindcraft" {
		t.Errorf("server saw tool %q", gotTool)
	}
	if !strings.Contains(string(out), `"stdout":"done"`) {
		t.Errorf("output = %s", out)
	}
}

func TestClientRunHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	c := NewClient(srv.URL)
	if _, err := c.Run(context.Background(), "x", []byte(`{}`)); err == nil {
		t.Error("a 500 response should produce an error")
	}
}

func TestClientConfigured(t *testing.T) {
	if NewClient("").Configured() {
		t.Error("an empty endpoint must report not configured")
	}
	if !NewClient("https://x.modal.run").Configured() {
		t.Error("a non-empty endpoint must report configured")
	}
}
