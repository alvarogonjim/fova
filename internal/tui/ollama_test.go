package tui

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProbeOllamaDetected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	if !probeOllama(srv.URL) {
		t.Error("probeOllama should detect a responding server")
	}
}

func TestProbeOllamaNotDetected(t *testing.T) {
	if probeOllama("http://127.0.0.1:1") {
		t.Error("probeOllama should return false for an unreachable server")
	}
}
