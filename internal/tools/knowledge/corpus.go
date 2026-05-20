package knowledge

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/blevesearch/bleve/v2"

	"github.com/alvarogonjim/proteus/internal/domain"
	"github.com/alvarogonjim/proteus/internal/store"
	"github.com/alvarogonjim/proteus/internal/tools"
)

// Mapper runs one LLM prompt over one paper's text. Stubbed in tests; the real
// implementation wraps an llm.Provider.
type Mapper interface {
	Map(ctx context.Context, prompt, text string) (string, error)
}

// Corpus implements the knowledge.corpus tool: a per-project literature corpus
// with full-text search (bleve), regex grep, and LLM map/reduce over papers.
type Corpus struct {
	st      *store.Store
	results *Results
	mapper  Mapper

	indexDir string
	mu       sync.Mutex // guards index access
	index    bleve.Index

	// FetchText fetches a paper's full text. Exported so tests can stub it.
	// The default best-effort implementation queries Europe PMC.
	FetchText func(ctx context.Context, paperID string) (string, error)
}

// NewCorpus builds the knowledge.corpus tool. indexDir is where the bleve
// full-text index lives (e.g. <workspace>/corpus.bleve).
func NewCorpus(st *store.Store, results *Results, mapper Mapper, indexDir string) *Corpus {
	c := &Corpus{st: st, results: results, mapper: mapper, indexDir: indexDir}
	c.FetchText = c.fetchTextEuropePMC
	return c
}

func (*Corpus) Name() string { return "knowledge.corpus" }
func (*Corpus) Description() string {
	return "Manage the per-project literature corpus: add, search, grep, map, " +
		"reduce, list, read, and remove papers."
}
func (*Corpus) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"command": map[string]any{
				"type": "string",
				"description": "Sub-operation: add, search, grep, map, " +
					"reduce, list, read, remove.",
			},
			"paper_ids":   map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"from_search": map[string]any{"type": "string", "description": "results_id from a prior search"},
			"max_papers":  map[string]any{"type": "integer", "description": "Max papers to add (default 30)"},
			"query":       map[string]any{"type": "string", "description": "Full-text search query"},
			"pattern":     map[string]any{"type": "string", "description": "Go regular expression for grep"},
			"ignore_case": map[string]any{"type": "boolean"},
			"prompt":      map[string]any{"type": "string", "description": "LLM prompt for map/reduce"},
			"from":        map[string]any{"type": "string", "description": "results_id to scope map"},
			"concurrency": map[string]any{"type": "integer", "description": "Map worker pool size (default 5)"},
			"map_results": map[string]any{"type": "array", "items": map[string]any{"type": "object"}},
			"paper_id":    map[string]any{"type": "string", "description": "Paper id for read"},
		},
		"required": []string{"command"},
	}
}
func (*Corpus) RequiresConfirmation(json.RawMessage) bool       { return false }
func (*Corpus) EstimatedCostUSD(json.RawMessage) float64        { return 0 }
func (*Corpus) EstimatedDuration(json.RawMessage) time.Duration { return 5 * time.Second }

// corpusInput is the union of every sub-command's parameters.
type corpusInput struct {
	Command     string      `json:"command"`
	PaperIDs    []string    `json:"paper_ids"`
	FromSearch  string      `json:"from_search"`
	MaxPapers   int         `json:"max_papers"`
	Query       string      `json:"query"`
	Pattern     string      `json:"pattern"`
	IgnoreCase  bool        `json:"ignore_case"`
	Prompt      string      `json:"prompt"`
	From        string      `json:"from"`
	Concurrency int         `json:"concurrency"`
	MapResults  []mapResult `json:"map_results"`
	PaperID     string      `json:"paper_id"`
}

// mapResult is one paper's answer in a map phase.
type mapResult struct {
	PaperID string `json:"paper_id"`
	Answer  string `json:"answer"`
}

// Execute dispatches on the command field.
func (c *Corpus) Execute(ctx context.Context, input json.RawMessage) (tools.Result, error) {
	var in corpusInput
	if err := json.Unmarshal(input, &in); err != nil {
		return tools.Result{}, err
	}
	prov := domain.NewToolCallRef("knowledge.corpus", input)
	result := func(out any, display string) (tools.Result, error) {
		body, err := json.Marshal(out)
		if err != nil {
			return tools.Result{}, err
		}
		return tools.Result{Output: body, Display: display, Provenance: prov}, nil
	}

	switch in.Command {
	case "add":
		return c.cmdAdd(ctx, in, result)
	case "search":
		return c.cmdSearch(in, result)
	case "grep":
		return c.cmdGrep(in, result)
	case "map":
		return c.cmdMap(ctx, in, result)
	case "reduce":
		return c.cmdReduce(ctx, in, result)
	case "list":
		return c.cmdList(result)
	case "read":
		return c.cmdRead(in, result)
	case "remove":
		return c.cmdRemove(in, result)
	default:
		return tools.Result{}, fmt.Errorf("knowledge.corpus: unknown command %q", in.Command)
	}
}

type resultFn func(out any, display string) (tools.Result, error)

// --- add ---

func (c *Corpus) cmdAdd(ctx context.Context, in corpusInput, result resultFn) (tools.Result, error) {
	max := in.MaxPapers
	if max <= 0 {
		max = 30
	}
	var papers []Paper
	if in.FromSearch != "" {
		got, ok := c.results.Get(in.FromSearch)
		if !ok {
			return tools.Result{}, fmt.Errorf("knowledge.corpus: unknown from_search %q", in.FromSearch)
		}
		papers = got
	} else {
		for _, id := range in.PaperIDs {
			papers = append(papers, Paper{ID: id, Source: "manual"})
		}
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

func (c *Corpus) cmdSearch(in corpusInput, result resultFn) (tools.Result, error) {
	if in.Query == "" {
		return tools.Result{}, fmt.Errorf("knowledge.corpus: query is required for search")
	}
	idx, err := c.openIndex()
	if err != nil {
		return tools.Result{}, err
	}
	c.mu.Lock()
	q := bleve.NewMatchQuery(in.Query)
	req := bleve.NewSearchRequest(q)
	req.Size = 100
	sr, err := idx.Search(req)
	c.mu.Unlock()
	if err != nil {
		return tools.Result{}, err
	}
	// Resolve hit ids against the store so search and grep agree on the rows.
	byID := map[string]bool{}
	for _, hit := range sr.Hits {
		byID[hit.ID] = true
	}
	all, err := c.st.ListCorpusPapers(store.DefaultProjectID)
	if err != nil {
		return tools.Result{}, err
	}
	var matched []Paper
	for _, p := range all {
		if byID[p.ID] {
			matched = append(matched, toPaper(p))
		}
	}
	resultsID := c.results.Put("corpus", matched)
	return result(
		map[string]any{"results_id": resultsID, "count": len(matched), "papers": matched},
		fmt.Sprintf("corpus search: %d papers (results_id %s)", len(matched), resultsID),
	)
}

// --- grep ---

func (c *Corpus) cmdGrep(in corpusInput, result resultFn) (tools.Result, error) {
	if in.Pattern == "" {
		return tools.Result{}, fmt.Errorf("knowledge.corpus: pattern is required for grep")
	}
	pattern := in.Pattern
	if in.IgnoreCase {
		pattern = "(?i)" + pattern
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return tools.Result{}, fmt.Errorf("knowledge.corpus: bad pattern: %w", err)
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

func (c *Corpus) cmdMap(ctx context.Context, in corpusInput, result resultFn) (tools.Result, error) {
	if in.Prompt == "" {
		return tools.Result{}, fmt.Errorf("knowledge.corpus: prompt is required for map")
	}
	papers, err := c.selectPapers(in.From, in.PaperIDs)
	if err != nil {
		return tools.Result{}, err
	}
	concurrency := in.Concurrency
	if concurrency <= 0 {
		concurrency = 5
	}
	out := make([]mapResult, len(papers))
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	var mapErr error
	var errMu sync.Mutex
	for i, p := range papers {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, p domain.CorpusPaper) {
			defer wg.Done()
			defer func() { <-sem }()
			ans, err := c.mapper.Map(ctx, in.Prompt, p.FullText)
			if err != nil {
				errMu.Lock()
				if mapErr == nil {
					mapErr = err
				}
				errMu.Unlock()
				return
			}
			out[i] = mapResult{PaperID: p.ID, Answer: ans}
		}(i, p)
	}
	wg.Wait()
	if mapErr != nil {
		return tools.Result{}, mapErr
	}
	return result(
		map[string]any{"per_paper": out},
		fmt.Sprintf("corpus map: %d papers", len(out)),
	)
}

// --- reduce ---

func (c *Corpus) cmdReduce(ctx context.Context, in corpusInput, result resultFn) (tools.Result, error) {
	if in.Prompt == "" {
		return tools.Result{}, fmt.Errorf("knowledge.corpus: prompt is required for reduce")
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

// --- list ---

func (c *Corpus) cmdList(result resultFn) (tools.Result, error) {
	all, err := c.st.ListCorpusPapers(store.DefaultProjectID)
	if err != nil {
		return tools.Result{}, err
	}
	type row struct {
		ID     string `json:"id"`
		Title  string `json:"title"`
		Year   int    `json:"year,omitempty"`
		Source string `json:"source"`
	}
	rows := make([]row, 0, len(all))
	for _, p := range all {
		rows = append(rows, row{ID: p.ID, Title: p.Title, Year: p.Year, Source: p.Source})
	}
	return result(
		map[string]any{"count": len(rows), "papers": rows},
		fmt.Sprintf("corpus: %d papers", len(rows)),
	)
}

// --- read ---

func (c *Corpus) cmdRead(in corpusInput, result resultFn) (tools.Result, error) {
	if in.PaperID == "" {
		return tools.Result{}, fmt.Errorf("knowledge.corpus: paper_id is required for read")
	}
	p, err := c.st.GetCorpusPaper(in.PaperID)
	if err != nil {
		return tools.Result{}, err
	}
	return result(
		map[string]any{"paper_id": p.ID, "title": p.Title, "full_text": p.FullText},
		fmt.Sprintf("corpus read: %s", p.ID),
	)
}

// --- remove ---

func (c *Corpus) cmdRemove(in corpusInput, result resultFn) (tools.Result, error) {
	idx, err := c.openIndex()
	if err != nil {
		return tools.Result{}, err
	}
	removed := 0
	for _, id := range in.PaperIDs {
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
			return nil, fmt.Errorf("knowledge.corpus: unknown from %q", from)
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
