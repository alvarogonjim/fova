package knowledge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/alvarogonjim/fova/internal/domain"
	"github.com/alvarogonjim/fova/internal/tools"
)

// blastEndpoint is the NCBI BLAST URL API.
const blastEndpoint = "https://blast.ncbi.nlm.nih.gov/Blast.cgi"

// validBLASTPrograms is the small subset SP-D exposes.
var validBLASTPrograms = map[string]bool{"blastp": true, "blastn": true}

// ridRe extracts the BLAST RID from the submit-reply HTML body.
var ridRe = regexp.MustCompile(`RID\s*=\s*(\S+)`)

// statusWaitingRe detects a "still computing" poll reply.
var statusWaitingRe = regexp.MustCompile(`(?i)Status\s*=\s*WAITING`)

// BLAST implements knowledge.blast: submit a sequence to NCBI BLAST, poll
// for completion, and return the top hits.
type BLAST struct {
	// BaseURL is the BLAST.cgi endpoint. Tests override it with httptest.
	BaseURL string
	// PollInterval is the gap between two consecutive Get polls. Defaults to
	// 15 s; tests set it to a millisecond.
	PollInterval time.Duration
	// MaxWait caps the total polling time. Defaults to 5 minutes.
	MaxWait time.Duration
	// HTTPClient is exported so tests can inject a server-bound client if
	// needed; nil means use the package-shared httpClient.
	HTTPClient *http.Client
}

// NewBLAST returns the knowledge.blast tool with the production defaults.
func NewBLAST() *BLAST {
	return &BLAST{
		BaseURL:      blastEndpoint,
		PollInterval: 15 * time.Second,
		MaxWait:      5 * time.Minute,
	}
}

func (*BLAST) Name() string     { return "knowledge.blast" }
func (*BLAST) Concurrent() bool { return true }
func (*BLAST) Description() string {
	return "Submit a protein or nucleotide sequence to NCBI BLAST, poll for " +
		"results, and return the top hits (accession, e-value, %identity)."
}
func (*BLAST) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"sequence": map[string]any{"type": "string", "description": "Query sequence (no FASTA header needed)"},
			"max_hits": map[string]any{"type": "integer", "description": "Max hits to return (default 10)"},
			"program":  map[string]any{"type": "string", "description": "blastp (default) or blastn"},
		},
		"required": []string{"sequence"},
	}
}
func (*BLAST) RequiresConfirmation(json.RawMessage) bool { return false }
func (*BLAST) EstimatedCostUSD(json.RawMessage) float64  { return 0 }
func (*BLAST) EstimatedDuration(json.RawMessage) time.Duration {
	// BLAST queue times vary wildly; the median trivial-query is ~30 s.
	return 30 * time.Second
}

type blastInput struct {
	Sequence string `json:"sequence"`
	MaxHits  int    `json:"max_hits"`
	Program  string `json:"program"`
}

// BLASTHit is one row of the parsed result.
type BLASTHit struct {
	Accession   string  `json:"accession"`
	Title       string  `json:"title,omitempty"`
	EValue      float64 `json:"evalue"`
	IdentityPct float64 `json:"identity_pct"`
}

// Execute submits the sequence, polls, parses and returns hits.
func (t *BLAST) Execute(ctx context.Context, input json.RawMessage) (tools.Result, error) {
	var in blastInput
	if err := json.Unmarshal(input, &in); err != nil {
		return tools.Result{}, err
	}
	in.Sequence = strings.TrimSpace(in.Sequence)
	if in.Sequence == "" {
		return tools.Result{}, fmt.Errorf("knowledge.blast: sequence is required")
	}
	if in.Program == "" {
		in.Program = "blastp"
	}
	if !validBLASTPrograms[in.Program] {
		return tools.Result{}, fmt.Errorf("knowledge.blast: program %q must be blastp or blastn", in.Program)
	}
	max := in.MaxHits
	if max <= 0 {
		max = 10
	}

	rid, err := t.submit(ctx, in)
	if err != nil {
		return tools.Result{}, err
	}
	body, err := t.poll(ctx, rid)
	if err != nil {
		return tools.Result{}, err
	}
	hits, err := parseBLASTJSON(body, max)
	if err != nil {
		return tools.Result{}, err
	}

	prov := domain.NewToolCallRef("knowledge.blast", input)
	out, _ := json.Marshal(map[string]any{
		"rid": rid, "count": len(hits), "hits": hits,
	})
	return tools.Result{
		Output:     out,
		Display:    fmt.Sprintf("blast %s: %d hits (RID %s)", in.Program, len(hits), rid),
		Provenance: prov,
	}, nil
}

// submit POSTs the BLAST query and parses the RID from the HTML reply.
func (t *BLAST) submit(ctx context.Context, in blastInput) (string, error) {
	form := url.Values{}
	form.Set("CMD", "Put")
	form.Set("PROGRAM", in.Program)
	if in.Program == "blastp" {
		form.Set("DATABASE", "nr")
	} else {
		form.Set("DATABASE", "nt")
	}
	form.Set("QUERY", in.Sequence)
	form.Set("FORMAT_TYPE", "HTML")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.BaseURL,
		strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", userAgent)
	resp, err := t.client().Do(req)
	if err != nil {
		return "", fmt.Errorf("knowledge.blast: submit: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("knowledge.blast: submit returned %d: %s",
			resp.StatusCode, strings.TrimSpace(string(body)))
	}
	m := ridRe.FindStringSubmatch(string(body))
	if len(m) < 2 {
		return "", fmt.Errorf("knowledge.blast: could not parse RID from submit reply")
	}
	return strings.TrimSpace(m[1]), nil
}

// poll fetches the result every PollInterval until JSON is returned, ctx is
// cancelled, or MaxWait elapses.
func (t *BLAST) poll(ctx context.Context, rid string) ([]byte, error) {
	deadline := time.Now().Add(t.MaxWait)
	for {
		body, err := t.fetchResult(ctx, rid)
		if err != nil {
			return nil, err
		}
		// A JSON body starts with `{` or whitespace+`{`; a still-running poll
		// returns HTML with `Status=WAITING`. We treat anything that does not
		// match WAITING as a final reply.
		trimmed := strings.TrimSpace(string(body))
		if strings.HasPrefix(trimmed, "{") {
			return body, nil
		}
		if !statusWaitingRe.MatchString(trimmed) {
			return nil, fmt.Errorf("knowledge.blast: unexpected poll body (first 120 bytes): %s",
				truncate(trimmed, 120))
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("knowledge.blast: timed out after %s waiting on RID %s",
				t.MaxWait, rid)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(t.PollInterval):
		}
	}
}

func (t *BLAST) fetchResult(ctx context.Context, rid string) ([]byte, error) {
	q := url.Values{}
	q.Set("CMD", "Get")
	q.Set("RID", rid)
	q.Set("FORMAT_TYPE", "JSON2_S")
	u := t.BaseURL + "?" + q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := t.client().Do(req)
	if err != nil {
		// A cancelled context surfaces here; propagate it verbatim.
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		return nil, fmt.Errorf("knowledge.blast: poll: %w", err)
	}
	defer resp.Body.Close()
	return io.ReadAll(io.LimitReader(resp.Body, 16<<20))
}

func (t *BLAST) client() *http.Client {
	if t.HTTPClient != nil {
		return t.HTTPClient
	}
	return httpClient
}

// parseBLASTJSON walks the JSON2_S envelope and returns up to `max` hits.
func parseBLASTJSON(body []byte, max int) ([]BLASTHit, error) {
	var env struct {
		BlastOutput2 []struct {
			Report struct {
				Results struct {
					Search struct {
						Hits []struct {
							Description []struct {
								Accession string `json:"accession"`
								Title     string `json:"title"`
							} `json:"description"`
							HSPs []struct {
								EValue   float64 `json:"evalue"`
								Identity float64 `json:"identity"`
								AlignLen float64 `json:"align_len"`
							} `json:"hsps"`
						} `json:"hits"`
					} `json:"search"`
				} `json:"results"`
			} `json:"report"`
		} `json:"BlastOutput2"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("knowledge.blast: decode JSON2_S: %w", err)
	}
	if len(env.BlastOutput2) == 0 {
		return nil, nil
	}
	rawHits := env.BlastOutput2[0].Report.Results.Search.Hits
	out := make([]BLASTHit, 0, len(rawHits))
	for _, h := range rawHits {
		accession, title := "", ""
		if len(h.Description) > 0 {
			accession = h.Description[0].Accession
			title = h.Description[0].Title
		}
		var ev, identityPct float64
		if len(h.HSPs) > 0 {
			ev = h.HSPs[0].EValue
			if h.HSPs[0].AlignLen > 0 {
				identityPct = 100 * h.HSPs[0].Identity / h.HSPs[0].AlignLen
			}
		}
		out = append(out, BLASTHit{
			Accession:   accession,
			Title:       title,
			EValue:      ev,
			IdentityPct: identityPct,
		})
		if len(out) >= max {
			break
		}
	}
	return out, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
