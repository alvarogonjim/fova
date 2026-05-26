package knowledge

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/blevesearch/bleve/v2"

	"github.com/alvarogonjim/fova/internal/domain"
	"github.com/alvarogonjim/fova/internal/jobs"
	"github.com/alvarogonjim/fova/internal/store"
	"github.com/alvarogonjim/fova/internal/tools"
	"github.com/alvarogonjim/fova/internal/transport"
)

// Mapper runs one LLM prompt over one paper's text. Stubbed in tests; the real
// implementation wraps an llm.Provider.
type Mapper interface {
	Map(ctx context.Context, prompt, text string) (string, error)
}

// Corpus is the shared backend for the eight knowledge.corpus_* tools. It owns
// the SQLite-backed paper rows, the bleve full-text index, and the LLM Mapper.
// Each per-action tool (corpusAdd, corpusSearch, ...) is a thin wrapper that
// delegates to one method here, so the dispatch logic is not duplicated.
//
// History: through v0.6 this was a single umbrella tool `knowledge.corpus`
// with an `action` field on its JSON input. LLMs trained on OpenAI-style
// flat tool naming kept calling `knowledge.corpus.add` and hitting the
// unknown-tool error (v0.7 Bug 3). v0.7 flattened the umbrella into eight
// per-action tools; the dispatcher logic moved here so the eight tools all
// share it.
type Corpus struct {
	st      *store.Store
	results *Results
	mapper  Mapper
	jobs    *jobs.Manager

	indexDir         string
	defaultMaxPapers int
	mu               sync.Mutex // guards index access
	index            bleve.Index

	// FetchText fetches a paper's full text. Exported so tests can stub it.
	// The default best-effort implementation queries Europe PMC.
	FetchText func(ctx context.Context, paperID string) (string, error)

	// PMCBaseURL is the Europe PMC search endpoint used by corpus.search.
	// Overridable for tests; defaults to the live europePMCEndpoint.
	PMCBaseURL string

	// searchBackoff overrides the transport.Client backoff schedule (tests
	// inject near-zero delays).
	searchBackoff func(attempt int) time.Duration
}

// NewCorpus builds the shared corpus backend. indexDir is where the bleve
// full-text index lives; defaultMaxPapers is the cap used when add_from_search
// omits max_papers (a value <= 0 falls back to 30). mgr runs the async
// corpus_map job (v0.7 Bug 4).
//
// Call Register on the returned *Corpus to wire its eight per-action tools
// into a tools.Registry.
func NewCorpus(st *store.Store, results *Results, mapper Mapper, mgr *jobs.Manager, indexDir string, defaultMaxPapers int) *Corpus {
	c := &Corpus{st: st, results: results, mapper: mapper, jobs: mgr, indexDir: indexDir, defaultMaxPapers: defaultMaxPapers}
	c.FetchText = c.fetchTextEuropePMC
	c.PMCBaseURL = europePMCEndpoint
	return c
}

// Register adds the eight per-action tools to reg:
//
//	knowledge.corpus_add, knowledge.corpus_add_from_search,
//	knowledge.corpus_search, knowledge.corpus_grep,
//	knowledge.corpus_map, knowledge.corpus_reduce,
//	knowledge.corpus_read, knowledge.corpus_remove.
func (c *Corpus) Register(reg *tools.Registry) {
	reg.Register(&corpusAdd{c: c})
	reg.Register(&corpusAddFromSearch{c: c})
	reg.Register(&corpusSearch{c: c})
	reg.Register(&corpusGrep{c: c})
	reg.Register(&corpusMap{c: c})
	reg.Register(&corpusReduce{c: c})
	reg.Register(&corpusRead{c: c})
	reg.Register(&corpusRemove{c: c})
}

// mapResult is one paper's answer in a map phase.
type mapResult struct {
	PaperID string `json:"paper_id"`
	Answer  string `json:"answer"`
}

// resultFn lets each action serialise its output to a tools.Result with the
// tool's name as the provenance source.
type resultFn func(out any, display string) (tools.Result, error)

func makeResultFn(toolName string, input json.RawMessage) resultFn {
	prov := domain.NewToolCallRef(toolName, input)
	return func(out any, display string) (tools.Result, error) {
		body, err := json.Marshal(out)
		if err != nil {
			return tools.Result{}, err
		}
		return tools.Result{Output: body, Display: display, Provenance: prov}, nil
	}
}

// --- add (explicit paper IDs) ---

type corpusAddInput struct {
	PaperIDs  []string `json:"paper_ids"`
	MaxPapers int      `json:"max_papers"`
}

type corpusAdd struct{ c *Corpus }

func (*corpusAdd) Name() string { return "knowledge.corpus_add" }
func (*corpusAdd) Description() string {
	return "Add papers to the per-project literature corpus by their ids. " +
		"Fetches full text best-effort; falls back to the abstract if the " +
		"fetch fails. Use knowledge.corpus_add_from_search to add the hits " +
		"of a prior search by results_id."
}
func (*corpusAdd) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"paper_ids":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"max_papers": map[string]any{"type": "integer", "description": "Cap on number of papers (default 30)"},
		},
		"required": []string{"paper_ids"},
	}
}
func (*corpusAdd) RequiresConfirmation(json.RawMessage) bool       { return false }
func (*corpusAdd) EstimatedCostUSD(json.RawMessage) float64        { return 0 }
func (*corpusAdd) EstimatedDuration(json.RawMessage) time.Duration { return 5 * time.Second }
func (t *corpusAdd) Execute(ctx context.Context, input json.RawMessage) (tools.Result, error) {
	var in corpusAddInput
	if err := json.Unmarshal(input, &in); err != nil {
		return tools.Result{}, err
	}
	papers := make([]Paper, 0, len(in.PaperIDs))
	for _, id := range in.PaperIDs {
		papers = append(papers, Paper{ID: id, Source: "manual"})
	}
	return t.c.addPapers(ctx, papers, in.MaxPapers, makeResultFn(t.Name(), input))
}

// --- add_from_search (paper ids resolved from a prior search results_id) ---

type corpusAddFromSearchInput struct {
	FromSearch string `json:"from_search"`
	MaxPapers  int    `json:"max_papers"`
}

type corpusAddFromSearch struct{ c *Corpus }

func (*corpusAddFromSearch) Name() string { return "knowledge.corpus_add_from_search" }
func (*corpusAddFromSearch) Description() string {
	return "Add to the corpus every paper returned by a prior knowledge.* " +
		"search, identified by its results_id. Useful pattern: search → " +
		"add_from_search → map. Caps the number added at max_papers " +
		"(default 30)."
}
func (*corpusAddFromSearch) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"from_search": map[string]any{"type": "string", "description": "results_id from a prior search"},
			"max_papers":  map[string]any{"type": "integer", "description": "Cap on number of papers (default 30)"},
		},
		"required": []string{"from_search"},
	}
}
func (*corpusAddFromSearch) RequiresConfirmation(json.RawMessage) bool       { return false }
func (*corpusAddFromSearch) EstimatedCostUSD(json.RawMessage) float64        { return 0 }
func (*corpusAddFromSearch) EstimatedDuration(json.RawMessage) time.Duration { return 30 * time.Second }
func (t *corpusAddFromSearch) Execute(ctx context.Context, input json.RawMessage) (tools.Result, error) {
	var in corpusAddFromSearchInput
	if err := json.Unmarshal(input, &in); err != nil {
		return tools.Result{}, err
	}
	if in.FromSearch == "" {
		return tools.Result{}, fmt.Errorf("knowledge.corpus_add_from_search: from_search is required")
	}
	papers, ok := t.c.results.Get(in.FromSearch)
	if !ok {
		return tools.Result{}, fmt.Errorf("knowledge.corpus_add_from_search: unknown from_search %q", in.FromSearch)
	}
	return t.c.addPapers(ctx, papers, in.MaxPapers, makeResultFn(t.Name(), input))
}

// addPapers is the shared implementation of the two add tools. It applies the
// max-papers cap, best-effort fetches each paper's full text (falling back to
// the abstract for offline/rate-limited cases), and inserts the row + bleve
// index entry. The SQLite insert and the bleve index update are not
// transactional: if indexing fails the row exists but is unindexed.
func (c *Corpus) addPapers(ctx context.Context, papers []Paper, maxPapers int, result resultFn) (tools.Result, error) {
	max := maxPapers
	if max <= 0 {
		max = c.defaultMaxPapers
	}
	if max <= 0 {
		max = 30
	}
	if len(papers) > max {
		papers = papers[:max]
	}

	added := make([]string, 0, len(papers))
	for _, p := range papers {
		if p.ID == "" {
			continue
		}
		text, err := c.FetchText(ctx, p.ID)
		if err != nil {
			text = "" // best-effort: store the paper anyway
		}
		if text == "" {
			// Offline/rate-limited: fall back to the abstract so the paper
			// is still searchable, greppable and mappable.
			text = p.Abstract
		}
		meta, _ := json.Marshal(p)
		source := p.Source
		if source == "" {
			source = "manual"
		}
		cp := domain.CorpusPaper{
			ID:        p.ID,
			ProjectID: store.DefaultProjectID,
			Title:     p.Title,
			Authors:   p.Authors,
			Year:      p.Year,
			Source:    source,
			FullText:  text,
			Metadata:  string(meta),
			Added:     time.Now().UTC(),
		}
		if err := c.st.InsertCorpusPaper(cp); err != nil {
			return tools.Result{}, err
		}
		if err := c.indexPaper(cp); err != nil {
			return tools.Result{}, err
		}
		added = append(added, p.ID)
	}
	return result(
		map[string]any{"added": len(added), "paper_ids": added},
		fmt.Sprintf("corpus: added %d papers", len(added)),
	)
}

// --- search ---

type corpusSearchInput struct {
	Query string `json:"query"`
}

type corpusSearch struct{ c *Corpus }

func (*corpusSearch) Name() string     { return "knowledge.corpus_search" }
func (*corpusSearch) Concurrent() bool { return true }
func (*corpusSearch) Description() string {
	return "Search the Europe PMC literature corpus by free-text query. " +
		"Returns matched papers, a sanitised echo of the query that was sent, " +
		"and a results_id that downstream tools (corpus_map, " +
		"corpus_add_from_search) can refer to. Empty responses surface a hint " +
		"so the agent can broaden instead of fabricating citations."
}
func (*corpusSearch) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{"type": "string", "description": "Free-text query against Europe PMC"},
		},
		"required": []string{"query"},
	}
}
func (*corpusSearch) RequiresConfirmation(json.RawMessage) bool       { return false }
func (*corpusSearch) EstimatedCostUSD(json.RawMessage) float64        { return 0 }
func (*corpusSearch) EstimatedDuration(json.RawMessage) time.Duration { return 2 * time.Second }
func (t *corpusSearch) Execute(ctx context.Context, input json.RawMessage) (tools.Result, error) {
	var in corpusSearchInput
	if err := json.Unmarshal(input, &in); err != nil {
		return tools.Result{}, err
	}
	return t.c.searchPMC(ctx, in, makeResultFn(t.Name(), input))
}

// searchCap is the maximum number of papers corpus.search returns in one call.
// PMC responses larger than this are truncated and truncated_at is reported.
const searchCap = 50

// emptyNote is surfaced to the agent when PMC returns zero hits so the agent
// can broaden instead of fabricating citations.
const emptyNote = "no results — try broader terms or fewer filters"

// stopwords are dropped from queries before being sent to PMC. Common
// determiners and articles add noise without selectivity.
var stopwords = map[string]bool{
	"the": true, "a": true, "an": true, "of": true, "and": true, "or": true,
	"in": true, "on": true, "to": true, "for": true, "with": true, "by": true,
	"is": true, "are": true, "be": true, "as": true, "at": true,
}

// sanitiseQuery strips stopwords, trims, collapses whitespace and quotes
// multi-word literal terms wrapped in single or double quotes by the caller.
func sanitiseQuery(q string) string {
	q = strings.TrimSpace(q)
	if q == "" {
		return ""
	}
	// Preserve quoted phrases verbatim. Tokens outside quotes are filtered.
	var out []string
	var inQuote bool
	var quote rune
	var current strings.Builder
	flushToken := func() {
		tok := strings.TrimSpace(current.String())
		current.Reset()
		if tok == "" {
			return
		}
		if stopwords[strings.ToLower(tok)] {
			return
		}
		out = append(out, tok)
	}
	for _, r := range q {
		switch {
		case inQuote:
			if r == quote {
				inQuote = false
				// Emit the quoted phrase as-is, with quotes.
				phrase := strings.TrimSpace(current.String())
				current.Reset()
				if phrase != "" {
					out = append(out, `"`+phrase+`"`)
				}
			} else {
				current.WriteRune(r)
			}
		case r == '"' || r == '\'':
			flushToken()
			inQuote = true
			quote = r
		case r == ' ' || r == '\t' || r == '\n':
			flushToken()
		default:
			current.WriteRune(r)
		}
	}
	flushToken()
	return strings.Join(out, " ")
}

// pmcSearchResponse is the subset of the Europe PMC response we consume.
type pmcSearchResponse struct {
	ResultList struct {
		Result []struct {
			DOI          string `json:"doi"`
			PMCID        string `json:"pmcid"`
			Title        string `json:"title"`
			AuthorString string `json:"authorString"`
			PubYear      string `json:"pubYear"`
			AbstractText string `json:"abstractText"`
		} `json:"result"`
	} `json:"resultList"`
}

// searchPMC queries Europe PMC via the shared transport.Client wrapper. The
// response is sanitised (stop-tokens dropped, junk rows filtered), capped at
// searchCap, and shaped into a structured-empty contract so the agent can tell
// "no results — broaden" from "PMC hiccuped".
func (c *Corpus) searchPMC(ctx context.Context, in corpusSearchInput, result resultFn) (tools.Result, error) {
	if in.Query == "" {
		return tools.Result{}, fmt.Errorf("knowledge.corpus_search: query is required")
	}
	sent := sanitiseQuery(in.Query)
	if sent == "" {
		// Caller passed only stopwords; surface the empty contract without
		// hitting PMC.
		return result(
			map[string]any{"papers": []Paper{}, "count": 0, "note": emptyNote, "query_sent": ""},
			"corpus search: 0 papers (query collapsed to empty after sanitisation)",
		)
	}

	q := url.Values{}
	q.Set("query", sent)
	q.Set("format", "json")
	q.Set("pageSize", strconv.Itoa(searchCap+10)) // ask for a bit more so truncation is observable

	endpoint := c.PMCBaseURL
	if endpoint == "" {
		endpoint = europePMCEndpoint
	}
	req, err := http.NewRequest(http.MethodGet, endpoint+"?"+q.Encode(), nil)
	if err != nil {
		return tools.Result{}, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")

	opts := []transport.Option{}
	if c.searchBackoff != nil {
		opts = append(opts, transport.WithBackoff(c.searchBackoff))
	}
	client := transport.New(opts...)
	resp, err := client.Do(ctx, req, "corpus.search")
	if err != nil {
		return tools.Result{}, fmt.Errorf("knowledge.corpus search: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return tools.Result{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return tools.Result{}, fmt.Errorf("knowledge.corpus search: PMC returned %d: %s",
			resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var raw pmcSearchResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		return tools.Result{}, fmt.Errorf("knowledge.corpus search: decode: %w", err)
	}

	rawHits := len(raw.ResultList.Result)
	papers := make([]Paper, 0, rawHits)
	for _, r := range raw.ResultList.Result {
		// Junk filter: drop rows without both a title and an abstract.
		if strings.TrimSpace(r.Title) == "" && strings.TrimSpace(r.AbstractText) == "" {
			continue
		}
		id := r.DOI
		if id == "" {
			id = r.PMCID
		}
		if id == "" {
			continue
		}
		year, _ := strconv.Atoi(r.PubYear)
		papers = append(papers, Paper{
			ID:       id,
			Title:    r.Title,
			Authors:  r.AuthorString,
			Year:     year,
			Source:   "europepmc",
			Abstract: r.AbstractText,
		})
	}

	truncated := 0
	if len(papers) > searchCap {
		papers = papers[:searchCap]
		truncated = searchCap
	}

	out := map[string]any{
		"count":      len(papers),
		"papers":     papers,
		"query_sent": sent,
	}
	if len(papers) == 0 {
		out["note"] = emptyNote
	}
	if truncated > 0 {
		out["truncated_at"] = truncated
	}

	// Stash for follow-up corpus.add via results_id (existing convention).
	if len(papers) > 0 {
		resultsID := c.results.Put("corpus", papers)
		out["results_id"] = resultsID
		return result(out, fmt.Sprintf("corpus search: %d papers (results_id %s)", len(papers), resultsID))
	}
	return result(out, fmt.Sprintf("corpus search: 0 papers — %s", emptyNote))
}

// --- grep ---

type corpusGrepInput struct {
	Pattern    string `json:"pattern"`
	IgnoreCase bool   `json:"ignore_case"`
}

type corpusGrep struct{ c *Corpus }

func (*corpusGrep) Name() string     { return "knowledge.corpus_grep" }
func (*corpusGrep) Concurrent() bool { return true }
func (*corpusGrep) Description() string {
	return "Literal Go-regex match across paper titles and full text. Unlike " +
		"corpus_search there is no tokenizing or stemming, so results may " +
		"differ. Set ignore_case=true to fold case."
}
func (*corpusGrep) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"pattern":     map[string]any{"type": "string", "description": "Go regular expression"},
			"ignore_case": map[string]any{"type": "boolean"},
		},
		"required": []string{"pattern"},
	}
}
func (*corpusGrep) RequiresConfirmation(json.RawMessage) bool       { return false }
func (*corpusGrep) EstimatedCostUSD(json.RawMessage) float64        { return 0 }
func (*corpusGrep) EstimatedDuration(json.RawMessage) time.Duration { return 2 * time.Second }
func (t *corpusGrep) Execute(_ context.Context, input json.RawMessage) (tools.Result, error) {
	var in corpusGrepInput
	if err := json.Unmarshal(input, &in); err != nil {
		return tools.Result{}, err
	}
	return t.c.grepCorpus(in.Pattern, in.IgnoreCase, makeResultFn(t.Name(), input))
}

func (c *Corpus) grepCorpus(pattern string, ignoreCase bool, result resultFn) (tools.Result, error) {
	if pattern == "" {
		return tools.Result{}, fmt.Errorf("knowledge.corpus_grep: pattern is required")
	}
	if ignoreCase {
		pattern = "(?i)" + pattern
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return tools.Result{}, fmt.Errorf("knowledge.corpus_grep: bad pattern: %w", err)
	}
	all, err := c.st.ListCorpusPapers(store.DefaultProjectID)
	if err != nil {
		return tools.Result{}, err
	}
	var matched []Paper
	for _, p := range all {
		if re.MatchString(p.Title + "\n" + p.FullText) {
			matched = append(matched, toPaper(p))
		}
	}
	return result(
		map[string]any{"count": len(matched), "papers": matched},
		fmt.Sprintf("corpus grep: %d papers match", len(matched)),
	)
}

// --- map ---

type corpusMapInput struct {
	Prompt      string   `json:"prompt"`
	From        string   `json:"from"`
	PaperIDs    []string `json:"paper_ids"`
	Concurrency int      `json:"concurrency"`
}

type corpusMap struct{ c *Corpus }

func (*corpusMap) Name() string     { return "knowledge.corpus_map" }
func (*corpusMap) Concurrent() bool { return true }
func (*corpusMap) Description() string {
	return "Apply the same LLM prompt to every paper in the corpus (or a " +
		"subset via from=results_id or explicit paper_ids), returning one " +
		"answer per paper. Runs as a background job — returns a JobID; poll " +
		"jobs.status to track progress. The reduce step then combines them."
}
func (*corpusMap) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"prompt":      map[string]any{"type": "string", "description": "LLM prompt evaluated per paper"},
			"from":        map[string]any{"type": "string", "description": "results_id to scope the map"},
			"paper_ids":   map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"concurrency": map[string]any{"type": "integer", "description": "Map worker pool size (default 5)"},
		},
		"required": []string{"prompt"},
	}
}
func (*corpusMap) RequiresConfirmation(json.RawMessage) bool       { return false }
func (*corpusMap) EstimatedCostUSD(json.RawMessage) float64        { return 0 }
func (*corpusMap) EstimatedDuration(json.RawMessage) time.Duration { return 2 * time.Minute }
func (t *corpusMap) Execute(ctx context.Context, input json.RawMessage) (tools.Result, error) {
	var in corpusMapInput
	if err := json.Unmarshal(input, &in); err != nil {
		return tools.Result{}, err
	}
	return t.c.submitMapJob(ctx, in, input, domain.NewToolCallRef(t.Name(), input))
}

// submitMapJob submits the per-paper LLM fan-out as a background job and
// returns the JobID immediately. The agent polls jobs.status to surface
// progress and elapsed/estimated to the user — multi-minute maps no longer
// block the loop (v0.7 Bug 4).
func (c *Corpus) submitMapJob(ctx context.Context, in corpusMapInput, input json.RawMessage, prov domain.ToolCallRef) (tools.Result, error) {
	if c.jobs == nil {
		return tools.Result{}, fmt.Errorf("knowledge.corpus_map: requires a job manager")
	}
	if in.Prompt == "" {
		return tools.Result{}, fmt.Errorf("knowledge.corpus_map: prompt is required")
	}
	papers, err := c.selectPapers(in.From, in.PaperIDs)
	if err != nil {
		return tools.Result{}, err
	}
	concurrency := in.Concurrency
	if concurrency <= 0 {
		concurrency = 5
	}
	prompt := in.Prompt
	mapper := c.mapper

	spec := jobs.Spec{
		Kind:  domain.JobCompute,
		Tool:  "knowledge.corpus_map",
		Input: input,
		// Rough estimate: 2s per paper, divided by the worker pool size.
		// Tuned per session by jobs.status, just used for the initial display.
		Run: func(ctx context.Context, progress func(float64), _ io.Writer) ([]byte, error) {
			return runCorpusMap(ctx, mapper, papers, prompt, concurrency, progress)
		},
	}
	jobID, err := c.jobs.Submit(spec)
	if err != nil {
		return tools.Result{}, err
	}
	return tools.Result{
		JobID: jobID,
		Display: fmt.Sprintf("corpus map: started job %s over %d papers — poll jobs.status",
			jobID, len(papers)),
		Provenance: prov,
	}, nil
}

// runCorpusMap is the bounded-concurrency fan-out that drives one LLM Map call
// per paper. Progress ticks once per finished paper (whether or not the call
// errored, so an error path still updates the fraction before the job exits).
// On the first error it cancels its children to stop wasting LLM calls.
func runCorpusMap(ctx context.Context, mapper Mapper, papers []domain.CorpusPaper, prompt string, concurrency int, progress func(float64)) ([]byte, error) {
	total := len(papers)
	out := make([]mapResult, total)
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	var mapErr error
	var errMu sync.Mutex
	var done atomic.Int64
	// Cancel in-flight and queued workers as soon as one Map call errors so
	// we stop wasting (possibly paid) LLM calls.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	emit := func() {
		if progress == nil || total == 0 {
			return
		}
		progress(float64(done.Add(1)) / float64(total))
	}
dispatch:
	for i, p := range papers {
		// Honour cancellation between papers so a cancelled job doesn't
		// queue work it will only abandon.
		select {
		case <-ctx.Done():
			break dispatch
		case sem <- struct{}{}:
		}
		wg.Add(1)
		go func(i int, p domain.CorpusPaper) {
			defer wg.Done()
			defer func() { <-sem }()
			ans, err := mapper.Map(ctx, prompt, p.FullText)
			emit()
			if err != nil {
				errMu.Lock()
				if mapErr == nil {
					mapErr = err
				}
				errMu.Unlock()
				cancel()
				return
			}
			out[i] = mapResult{PaperID: p.ID, Answer: ans}
		}(i, p)
	}
	wg.Wait()
	if ctxErr := ctx.Err(); ctxErr != nil && mapErr == nil {
		// Parent ctx was cancelled (job cancellation): surface that so the
		// job lands in JobCancelled rather than JobSucceeded with partial
		// results.
		return nil, ctxErr
	}
	if mapErr != nil {
		return nil, mapErr
	}
	return json.Marshal(map[string]any{"per_paper": out})
}

// --- reduce ---

type corpusReduceInput struct {
	Prompt     string      `json:"prompt"`
	MapResults []mapResult `json:"map_results"`
}

type corpusReduce struct{ c *Corpus }

func (*corpusReduce) Name() string { return "knowledge.corpus_reduce" }
func (*corpusReduce) Description() string {
	return "Combine per-paper map answers into a single summary using the " +
		"provided LLM prompt. Typically called with the per_paper field " +
		"emitted by knowledge.corpus_map."
}
func (*corpusReduce) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"prompt":      map[string]any{"type": "string", "description": "LLM prompt that combines the per-paper answers"},
			"map_results": map[string]any{"type": "array", "items": map[string]any{"type": "object"}},
		},
		"required": []string{"prompt"},
	}
}
func (*corpusReduce) RequiresConfirmation(json.RawMessage) bool       { return false }
func (*corpusReduce) EstimatedCostUSD(json.RawMessage) float64        { return 0 }
func (*corpusReduce) EstimatedDuration(json.RawMessage) time.Duration { return 10 * time.Second }
func (t *corpusReduce) Execute(ctx context.Context, input json.RawMessage) (tools.Result, error) {
	var in corpusReduceInput
	if err := json.Unmarshal(input, &in); err != nil {
		return tools.Result{}, err
	}
	return t.c.reduceCorpus(ctx, in, makeResultFn(t.Name(), input))
}

func (c *Corpus) reduceCorpus(ctx context.Context, in corpusReduceInput, result resultFn) (tools.Result, error) {
	if in.Prompt == "" {
		return tools.Result{}, fmt.Errorf("knowledge.corpus_reduce: prompt is required")
	}
	var sb strings.Builder
	for _, m := range in.MapResults {
		if m.PaperID != "" {
			sb.WriteString("# ")
			sb.WriteString(m.PaperID)
			sb.WriteString("\n")
		}
		sb.WriteString(m.Answer)
		sb.WriteString("\n\n")
	}
	summary, err := c.mapper.Map(ctx, in.Prompt, sb.String())
	if err != nil {
		return tools.Result{}, err
	}
	return result(
		map[string]any{"summary": summary},
		"corpus reduce: 1 summary",
	)
}

// --- read ---

type corpusReadInput struct {
	PaperID string `json:"paper_id"`
}

type corpusRead struct{ c *Corpus }

func (*corpusRead) Name() string     { return "knowledge.corpus_read" }
func (*corpusRead) Concurrent() bool { return true }
func (*corpusRead) Description() string {
	return "Return the full text of one paper from the corpus by paper_id."
}
func (*corpusRead) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"paper_id": map[string]any{"type": "string"},
		},
		"required": []string{"paper_id"},
	}
}
func (*corpusRead) RequiresConfirmation(json.RawMessage) bool       { return false }
func (*corpusRead) EstimatedCostUSD(json.RawMessage) float64        { return 0 }
func (*corpusRead) EstimatedDuration(json.RawMessage) time.Duration { return time.Second }
func (t *corpusRead) Execute(_ context.Context, input json.RawMessage) (tools.Result, error) {
	var in corpusReadInput
	if err := json.Unmarshal(input, &in); err != nil {
		return tools.Result{}, err
	}
	return t.c.readCorpus(in.PaperID, makeResultFn(t.Name(), input))
}

func (c *Corpus) readCorpus(paperID string, result resultFn) (tools.Result, error) {
	if paperID == "" {
		return tools.Result{}, fmt.Errorf("knowledge.corpus_read: paper_id is required")
	}
	p, err := c.st.GetCorpusPaper(paperID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return tools.Result{}, fmt.Errorf(
				"knowledge.corpus_read: paper %q is not in this project's corpus — "+
					"use a paper_id from a knowledge.corpus_search or knowledge.corpus_grep "+
					"result, or add it first with knowledge.corpus_add", paperID)
		}
		return tools.Result{}, err
	}
	return result(
		map[string]any{"paper_id": p.ID, "title": p.Title, "full_text": p.FullText},
		fmt.Sprintf("corpus read: %s", p.ID),
	)
}

// --- remove ---

type corpusRemoveInput struct {
	PaperIDs []string `json:"paper_ids"`
}

type corpusRemove struct{ c *Corpus }

func (*corpusRemove) Name() string { return "knowledge.corpus_remove" }
func (*corpusRemove) Description() string {
	return "Remove one or more papers from the corpus and the full-text index."
}
func (*corpusRemove) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"paper_ids": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		},
		"required": []string{"paper_ids"},
	}
}
func (*corpusRemove) RequiresConfirmation(json.RawMessage) bool       { return false }
func (*corpusRemove) EstimatedCostUSD(json.RawMessage) float64        { return 0 }
func (*corpusRemove) EstimatedDuration(json.RawMessage) time.Duration { return time.Second }
func (t *corpusRemove) Execute(_ context.Context, input json.RawMessage) (tools.Result, error) {
	var in corpusRemoveInput
	if err := json.Unmarshal(input, &in); err != nil {
		return tools.Result{}, err
	}
	return t.c.removeCorpus(in.PaperIDs, makeResultFn(t.Name(), input))
}

func (c *Corpus) removeCorpus(ids []string, result resultFn) (tools.Result, error) {
	idx, err := c.openIndex()
	if err != nil {
		return tools.Result{}, err
	}
	removed := 0
	for _, id := range ids {
		if err := c.st.DeleteCorpusPaper(id); err != nil {
			return tools.Result{}, err
		}
		c.mu.Lock()
		_ = idx.Delete(id) // best-effort
		c.mu.Unlock()
		removed++
	}
	return result(
		map[string]any{"removed": removed},
		fmt.Sprintf("corpus: removed %d papers", removed),
	)
}

// --- helpers ---

// selectPapers resolves the map target: from results id, then explicit ids,
// else every paper in the project.
func (c *Corpus) selectPapers(from string, ids []string) ([]domain.CorpusPaper, error) {
	if from != "" {
		got, ok := c.results.Get(from)
		if !ok {
			return nil, fmt.Errorf("knowledge.corpus_map: unknown from %q", from)
		}
		out := make([]domain.CorpusPaper, 0, len(got))
		for _, p := range got {
			cp, err := c.st.GetCorpusPaper(p.ID)
			if err != nil {
				continue // not yet in the corpus — skip
			}
			out = append(out, cp)
		}
		return out, nil
	}
	if len(ids) > 0 {
		out := make([]domain.CorpusPaper, 0, len(ids))
		for _, id := range ids {
			cp, err := c.st.GetCorpusPaper(id)
			if err != nil {
				continue
			}
			out = append(out, cp)
		}
		return out, nil
	}
	return c.st.ListCorpusPapers(store.DefaultProjectID)
}

// openIndex lazily opens or creates the bleve index.
func (c *Corpus) openIndex() (bleve.Index, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.index != nil {
		return c.index, nil
	}
	var idx bleve.Index
	var err error
	if _, statErr := os.Stat(c.indexDir); statErr == nil {
		idx, err = bleve.Open(c.indexDir)
	} else {
		idx, err = bleve.New(c.indexDir, bleve.NewIndexMapping())
	}
	if err != nil {
		return nil, fmt.Errorf("knowledge.corpus: open index: %w", err)
	}
	c.index = idx
	return idx, nil
}

// Close releases the bleve index if it was lazily opened; safe to call when
// the index was never opened and safe to call more than once.
func (c *Corpus) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.index == nil {
		return nil
	}
	idx := c.index
	c.index = nil
	return idx.Close()
}

// indexPaper adds one paper to the bleve full-text index.
func (c *Corpus) indexPaper(p domain.CorpusPaper) error {
	idx, err := c.openIndex()
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	doc := map[string]any{
		"title":     p.Title,
		"authors":   p.Authors,
		"full_text": p.FullText,
	}
	return idx.Index(p.ID, doc)
}

// fetchTextEuropePMC best-effort fetches a paper's full text from Europe PMC.
// Failures return ("", nil) so that add can still store the paper.
func (c *Corpus) fetchTextEuropePMC(ctx context.Context, paperID string) (string, error) {
	q := url.Values{}
	q.Set("query", paperID)
	q.Set("resultType", "core")
	q.Set("format", "json")
	u := europePMCEndpoint + "?" + q.Encode()
	var raw struct {
		ResultList struct {
			Result []struct {
				AbstractText string `json:"abstractText"`
			} `json:"result"`
		} `json:"resultList"`
	}
	if err := getJSON(ctx, u, &raw); err != nil {
		return "", nil
	}
	if len(raw.ResultList.Result) == 0 {
		return "", nil
	}
	return raw.ResultList.Result[0].AbstractText, nil
}

// toPaper converts a stored corpus paper to the search-result Paper shape.
func toPaper(p domain.CorpusPaper) Paper {
	return Paper{
		ID:      p.ID,
		Title:   p.Title,
		Authors: p.Authors,
		Year:    p.Year,
		Source:  p.Source,
	}
}
