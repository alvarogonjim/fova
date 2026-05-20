package lab

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// testClient points a Client at an httptest server.
func testClient(srv *httptest.Server) *Client {
	return &Client{baseURL: srv.URL, token: "test-token", http: srv.Client()}
}

func TestClientListTargets(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("Authorization = %q, want Bearer test-token", got)
		}
		_ = json.NewEncoder(w).Encode([]Target{{ID: "comp-her2", Name: "HER2"}})
	}))
	defer srv.Close()

	got, err := testClient(srv).ListTargets(context.Background())
	if err != nil {
		t.Fatalf("ListTargets: %v", err)
	}
	if len(got) != 1 || got[0].ID != "comp-her2" {
		t.Fatalf("ListTargets = %+v", got)
	}
}

func TestClientSubmitExperiment(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		_ = json.NewEncoder(w).Encode(Experiment{ID: "exp-9", Status: "submitted"})
	}))
	defer srv.Close()

	got, err := testClient(srv).SubmitExperiment(context.Background(),
		SubmitRequest{TargetID: "comp-her2", AssayType: "binding"})
	if err != nil {
		t.Fatalf("SubmitExperiment: %v", err)
	}
	if got.ID != "exp-9" || got.Status != "submitted" {
		t.Fatalf("SubmitExperiment = %+v", got)
	}
}

func TestClientErrorsOnNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("invalid token"))
	}))
	defer srv.Close()

	if _, err := testClient(srv).ListTargets(context.Background()); err == nil {
		t.Fatal("expected an error on a 401 response")
	}
}
