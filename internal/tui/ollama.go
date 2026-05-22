package tui

import (
	"net/http"
	"time"
)

// probeOllama reports whether an Ollama server answers at baseURL. It does a
// short-timeout GET of /api/tags; an OK response counts as detected. Any
// error or non-OK status is simply "not detected" — the probe never fails the
// wizard.
func probeOllama(baseURL string) bool {
	client := http.Client{Timeout: 300 * time.Millisecond}
	resp, err := client.Get(baseURL + "/api/tags")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}
