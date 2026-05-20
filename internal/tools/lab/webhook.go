package lab

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/uuid"

	"github.com/alvarogonjim/proteus/internal/domain"
	"github.com/alvarogonjim/proteus/internal/store"
)

// signatureHeader is the HTTP header carrying the HMAC-SHA256 signature of the
// raw request body. Verify the exact name against the live Adaptyv OpenAPI spec.
const signatureHeader = "X-Adaptyv-Signature"

// webhookSecretEnv holds the shared secret used to verify inbound signatures.
const webhookSecretEnv = "ADAPTYV_WEBHOOK_SECRET"

// WebhookEventMsg is sent on the agent bus after a webhook is accepted and the
// matching experiment updated, so the TUI can refresh and notify the user.
type WebhookEventMsg struct {
	ExperimentID domain.ExperimentID
}

// adaptyvWebhookPayload is the body Adaptyv POSTs to /webhooks/adaptyv.
// Verify field names against the live OpenAPI spec before relying on it.
type adaptyvWebhookPayload struct {
	EventType    string   `json:"event_type"`
	ExperimentID string   `json:"experiment_id"` // the Adaptyv-side id (our ExternalID)
	Status       string   `json:"status"`
	Results      []Result `json:"results,omitempty"`
}

// webhookSecret returns the shared HMAC secret from the environment.
func webhookSecret() string { return os.Getenv(webhookSecretEnv) }

// StartReceiver runs an HTTP server exposing POST /webhooks/adaptyv until ctx
// is cancelled. It is meant to run on a goroutine for the TUI's lifetime.
func StartReceiver(ctx context.Context, port int, st *store.Store, bus chan<- tea.Msg) error {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /webhooks/adaptyv", adaptyvHandler(st, bus, webhookSecret()))
	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: mux,
	}
	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
	}()
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

// adaptyvHandler builds the webhook handler. It is factored out of
// StartReceiver so tests can drive it directly with httptest, no real port.
func adaptyvHandler(st *store.Store, bus chan<- tea.Msg, secret string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(io.LimitReader(r.Body, 8<<20)) // 8 MiB cap
		if err != nil {
			http.Error(w, "read body", http.StatusBadRequest)
			return
		}
		if !validSignature(body, r.Header.Get(signatureHeader), secret) {
			// Mismatch: reject and persist nothing.
			http.Error(w, "invalid signature", http.StatusUnauthorized)
			return
		}

		var payload adaptyvWebhookPayload
		if err := json.Unmarshal(body, &payload); err != nil {
			http.Error(w, "invalid payload", http.StatusBadRequest)
			return
		}

		// Resolve our experiment from the Adaptyv-side id (our ExternalID).
		expID := resolveExperimentID(st, payload.ExperimentID)

		event := domain.WebhookEvent{
			ID:           uuid.NewString(),
			ExperimentID: expID,
			EventType:    payload.EventType,
			Received:     time.Now().UTC(),
			Payload:      json.RawMessage(body),
		}
		if err := st.InsertWebhookEvent(event); err != nil {
			log.Printf("webhook: persist event: %v", err)
			http.Error(w, "persist failed", http.StatusInternalServerError)
			return
		}

		if expID != "" {
			if err := applyToExperiment(st, expID, payload); err != nil {
				log.Printf("webhook: update experiment %s: %v", expID, err)
			}
		}

		// Non-blocking send: a full or nil bus must never stall the handler.
		if bus != nil {
			select {
			case bus <- WebhookEventMsg{ExperimentID: expID}:
			default:
			}
		}

		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "ok")
	}
}

// validSignature reports whether sig is the hex HMAC-SHA256 of body under
// secret. Comparison is constant-time. An empty signature never validates.
func validSignature(body []byte, sig, secret string) bool {
	if sig == "" {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	want := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(want), []byte(sig))
}

// signBody returns the hex HMAC-SHA256 of body under secret. Shared with tests
// so they sign payloads exactly as the handler verifies them.
func signBody(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// resolveExperimentID maps an Adaptyv-side experiment id to our local
// ExperimentID by scanning ListExperiments for a matching ExternalID.
func resolveExperimentID(st *store.Store, externalID string) domain.ExperimentID {
	if st == nil || externalID == "" {
		return ""
	}
	exps, err := st.ListExperiments(store.DefaultProjectID)
	if err != nil {
		log.Printf("webhook: list experiments: %v", err)
		return ""
	}
	for _, e := range exps {
		if e.ExternalID == externalID {
			return e.ID
		}
	}
	return ""
}

// applyToExperiment updates the stored experiment's status and results from a
// webhook payload and persists the change via store.UpdateExperiment.
func applyToExperiment(st *store.Store, id domain.ExperimentID, p adaptyvWebhookPayload) error {
	exp, err := st.GetExperiment(id)
	if err != nil {
		return err
	}
	if p.Status != "" {
		exp.Status = p.Status
	}
	if len(p.Results) > 0 {
		exp.Results = mergeResults(exp, p.Results)
	}
	return st.UpdateExperiment(exp)
}

// mergeResults converts Adaptyv Results to domain.ExperimentResult, keyed onto
// the experiment's design list by position (result order matches the
// submission order).
func mergeResults(exp domain.Experiment, results []Result) []domain.ExperimentResult {
	out := make([]domain.ExperimentResult, 0, len(results))
	for i, r := range results {
		var designID domain.DesignID
		if i < len(exp.Designs) {
			designID = exp.Designs[i]
		}
		out = append(out, domain.ExperimentResult{
			DesignID: designID,
			Kd:       r.Kd,
			KdUnits:  r.KdUnits,
			Kon:      r.Kon,
			Koff:     r.Koff,
		})
	}
	return out
}
