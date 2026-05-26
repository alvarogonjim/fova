// Package transport wraps net/http with the retry, timeout, and telemetry
// policy shared by every external client (Europe PMC, RCSB PDB, UniProt, …).
// Each tool constructs one Client tagged with its name and uses Do for every
// request, so failures are uniformly retried and uniformly logged.
package transport

import (
	"context"
	"errors"
	"io"
	"net/http"
	"sync"
	"time"
)

// Event is emitted once per HTTP attempt. Telemetry consumers (see WithEvent)
// record these to disk; tests capture them in-memory.
type Event struct {
	Tool       string
	URL        string
	Method     string
	Status     int
	Attempt    int
	DurationMS int64
	Err        error
}

// Client is a small wrapper around http.Client that adds retry, per-call
// timeout, and a telemetry hook.
type Client struct {
	base      *http.Client
	retries   int
	backoff   func(attempt int) time.Duration
	perCallTO time.Duration
	onEvent   func(Event)
}

// Option configures a Client at construction time.
type Option func(*Client)

// WithRetries overrides the default retry count (default 3).
func WithRetries(n int) Option { return func(c *Client) { c.retries = n } }

// WithBackoff overrides the default backoff schedule (default 1s, 3s, 9s).
func WithBackoff(fn func(attempt int) time.Duration) Option {
	return func(c *Client) { c.backoff = fn }
}

// WithPerCallTimeout overrides the per-attempt timeout (default 30s).
func WithPerCallTimeout(d time.Duration) Option {
	return func(c *Client) { c.perCallTO = d }
}

// WithEvent installs a telemetry callback. Called once per attempt.
func WithEvent(fn func(Event)) Option { return func(c *Client) { c.onEvent = fn } }

// WithHTTPClient overrides the underlying http.Client (test seam).
func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.base = h } }

// New builds a Client with sensible defaults; options override.
func New(opts ...Option) *Client {
	c := &Client{
		base:      &http.Client{},
		retries:   3,
		backoff:   defaultBackoff,
		perCallTO: 30 * time.Second,
		onEvent:   func(Event) {},
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

func defaultBackoff(attempt int) time.Duration {
	switch attempt {
	case 0:
		return time.Second
	case 1:
		return 3 * time.Second
	default:
		return 9 * time.Second
	}
}

// Do issues req with retry on 5xx and connection errors. tool labels every
// emitted Event. Returns the last attempt's response or error.
//
// The request body must be replayable (bytes.Reader, strings.Reader, or nil) —
// retries re-send the body. Callers that need streaming bodies should retry
// at a higher level.
//
// The returned response's Body holds the per-call timeout open until Close is
// called; the caller MUST Close the body or the per-call context leaks.
func (c *Client) Do(ctx context.Context, req *http.Request, tool string) (*http.Response, error) {
	var lastErr error
	for attempt := 0; attempt <= c.retries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(c.backoff(attempt - 1)):
			}
		}

		callCtx, cancel := context.WithTimeout(ctx, c.perCallTO)
		start := time.Now()
		resp, err := c.base.Do(req.Clone(callCtx))
		dur := time.Since(start).Milliseconds()

		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		c.onEvent(Event{
			Tool:       tool,
			URL:        req.URL.String(),
			Method:     req.Method,
			Status:     status,
			Attempt:    attempt,
			DurationMS: dur,
			Err:        err,
		})

		switch {
		case err != nil:
			cancel()
			lastErr = err
			if !isRetryable(err) {
				return nil, err
			}
		case resp.StatusCode >= 500:
			resp.Body.Close()
			cancel()
			lastErr = &httpStatusError{Status: resp.StatusCode}
		case resp.StatusCode >= 400:
			// 4xx returns the response untouched; the caller closes it. We
			// keep the per-call context alive until then so the body remains
			// readable. The cancel is wired into the body wrapper below.
			resp.Body = &cancellingBody{ReadCloser: resp.Body, cancel: cancel}
			return resp, nil
		default:
			resp.Body = &cancellingBody{ReadCloser: resp.Body, cancel: cancel}
			return resp, nil
		}
	}
	return nil, lastErr
}

// cancellingBody calls cancel exactly once when the underlying body is closed,
// so the per-call timeout context is released only after the caller drained
// the response.
type cancellingBody struct {
	io.ReadCloser
	cancel func()
	once   sync.Once
}

func (b *cancellingBody) Close() error {
	err := b.ReadCloser.Close()
	b.once.Do(b.cancel)
	return err
}

// isRetryable reports whether a transport error is worth retrying. Context
// cancellation is not — that's a deliberate caller signal.
func isRetryable(err error) bool {
	return !errors.Is(err, context.Canceled)
}

type httpStatusError struct{ Status int }

func (e *httpStatusError) Error() string {
	return http.StatusText(e.Status)
}
