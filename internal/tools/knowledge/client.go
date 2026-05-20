// Package knowledge holds the agent's free, no-auth literature- and
// database-retrieval tools and the per-project corpus.
package knowledge

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/alvarogonjim/fova/internal/version"
)

// userAgent is sent on every knowledge request (SPECS §7.2.5).
var userAgent = "fova/" + version.String() + " (https://github.com/alvarogonjim/fova)"

// httpClient is the shared client for all knowledge HTTP calls.
var httpClient = &http.Client{Timeout: 30 * time.Second}

// getJSON performs a GET against url and decodes a JSON body into out.
// A non-2xx response is an error. The fova User-Agent is always set.
func getJSON(ctx context.Context, url string, out any) error {
	body, err := getBytes(ctx, url, "application/json")
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decode %s: %w", url, err)
	}
	return nil
}

// getBytes performs a GET and returns the raw body, erroring on non-2xx.
func getBytes(ctx context.Context, url, accept string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request %s: %w", url, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20)) // 8 MiB cap
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%s returned %d: %s", url, resp.StatusCode,
			strings.TrimSpace(string(body)))
	}
	return body, nil
}

// Paper is the normalized record every search tool returns and the corpus
// consumes. Source is the tool that produced it (europepmc, openalex, ...).
type Paper struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Authors  string `json:"authors,omitempty"`
	Year     int    `json:"year,omitempty"`
	Source   string `json:"source"`
	URL      string `json:"url,omitempty"`
	Abstract string `json:"abstract,omitempty"`
}

// Results caches search hits in-process so corpus.add can scope to a prior
// search via a results_id (SPECS §7.2.6).
type Results struct {
	mu sync.Mutex
	m  map[string][]Paper
	n  int
}

// NewResults returns an empty results cache.
func NewResults() *Results { return &Results{m: map[string][]Paper{}} }

// Put stores papers under a fresh results_id and returns it.
func (r *Results) Put(source string, papers []Paper) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.n++
	id := fmt.Sprintf("S%d-%s", r.n, source)
	r.m[id] = papers
	return id
}

// Get returns the papers stored under id.
func (r *Results) Get(id string) ([]Paper, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.m[id]
	return p, ok
}

// clampLimit returns a sane page size: 0 → 25, otherwise capped to [1,100].
func clampLimit(n int) int {
	if n <= 0 {
		return 25
	}
	if n > 100 {
		return 100
	}
	return n
}
