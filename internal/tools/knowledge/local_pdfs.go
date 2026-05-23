package knowledge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/blevesearch/bleve/v2"
	"github.com/ledongthuc/pdf"

	"github.com/alvarogonjim/fova/internal/domain"
	"github.com/alvarogonjim/fova/internal/tools"
)

// LocalPDFs implements knowledge.local_pdfs: index and search PDFs on disk.
type LocalPDFs struct {
	results    *Results
	mapper     Mapper
	indexDir   string
	defaultDir string

	mu    sync.Mutex
	index bleve.Index
}

// NewLocalPDFs returns the tool. indexDir is where the bleve index lives;
// defaultDir is the folder add() scans when an action omits "dir" (empty
// means "dir is required per call").
func NewLocalPDFs(results *Results, mapper Mapper, indexDir, defaultDir string) *LocalPDFs {
	return &LocalPDFs{
		results:    results,
		mapper:     mapper,
		indexDir:   indexDir,
		defaultDir: defaultDir,
	}
}

func (*LocalPDFs) Name() string       { return "knowledge.local_pdfs" }
func (*LocalPDFs) Concurrent() bool { return true }
func (*LocalPDFs) Description() string {
	return "Index a folder of local PDFs into a bleve full-text index, then " +
		"search or ask questions over them."
}
func (*LocalPDFs) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action":    map[string]any{"type": "string", "description": "add | search | ask"},
			"dir":       map[string]any{"type": "string", "description": "Folder to scan (action=add)"},
			"recursive": map[string]any{"type": "boolean", "description": "Recurse subfolders (action=add, default true)"},
			"query":     map[string]any{"type": "string", "description": "Search query (action=search)"},
			"question":  map[string]any{"type": "string", "description": "Question for the LLM (action=ask)"},
			"k":         map[string]any{"type": "integer", "description": "Top-K results (default 10)"},
		},
		"required": []string{"action"},
	}
}
func (*LocalPDFs) RequiresConfirmation(json.RawMessage) bool       { return false }
func (*LocalPDFs) EstimatedCostUSD(json.RawMessage) float64        { return 0 }
func (*LocalPDFs) EstimatedDuration(json.RawMessage) time.Duration { return 5 * time.Second }

type localPDFsInput struct {
	Action    string `json:"action"`
	Dir       string `json:"dir"`
	Recursive *bool  `json:"recursive"`
	Query     string `json:"query"`
	Question  string `json:"question"`
	K         int    `json:"k"`
}

// Execute dispatches on the action field.
func (t *LocalPDFs) Execute(ctx context.Context, input json.RawMessage) (tools.Result, error) {
	var in localPDFsInput
	if err := json.Unmarshal(input, &in); err != nil {
		return tools.Result{}, err
	}
	prov := domain.NewToolCallRef("knowledge.local_pdfs", input)
	wrap := func(out any, display string) (tools.Result, error) {
		body, err := json.Marshal(out)
		if err != nil {
			return tools.Result{}, err
		}
		return tools.Result{Output: body, Display: display, Provenance: prov}, nil
	}
	switch in.Action {
	case "add":
		return t.cmdAdd(ctx, in, wrap)
	case "search":
		return t.cmdSearch(in, wrap)
	case "ask":
		return t.cmdAsk(ctx, in, wrap)
	default:
		return tools.Result{}, fmt.Errorf("knowledge.local_pdfs: unknown action %q", in.Action)
	}
}

// cmdAdd scans dir for *.pdf, extracts text and indexes each PDF.
func (t *LocalPDFs) cmdAdd(ctx context.Context, in localPDFsInput, wrap resultFn) (tools.Result, error) {
	dir := strings.TrimSpace(in.Dir)
	if dir == "" {
		dir = t.defaultDir
	}
	if dir == "" {
		return tools.Result{}, fmt.Errorf("knowledge.local_pdfs: dir is required (no default configured)")
	}
	info, err := os.Stat(dir)
	if err != nil {
		return tools.Result{}, fmt.Errorf("knowledge.local_pdfs: dir %q: %w", dir, err)
	}
	if !info.IsDir() {
		return tools.Result{}, fmt.Errorf("knowledge.local_pdfs: %q is not a directory", dir)
	}
	recursive := true
	if in.Recursive != nil {
		recursive = *in.Recursive
	}

	idx, err := t.openIndex()
	if err != nil {
		return tools.Result{}, err
	}

	files, err := scanPDFs(dir, recursive)
	if err != nil {
		return tools.Result{}, err
	}
	added := make([]string, 0, len(files))
	for _, f := range files {
		if err := ctx.Err(); err != nil {
			return tools.Result{}, err
		}
		text, err := extractPDFText(f)
		if err != nil {
			// Skip unreadable PDFs but keep going — best-effort.
			continue
		}
		t.mu.Lock()
		err = idx.Index(f, map[string]any{
			"path":    f,
			"text":    text,
			"indexed": time.Now().UTC(),
		})
		t.mu.Unlock()
		if err != nil {
			return tools.Result{}, err
		}
		added = append(added, f)
	}
	return wrap(
		map[string]any{"added": len(added), "files": added},
		fmt.Sprintf("local_pdfs: indexed %d PDF(s)", len(added)),
	)
}

// cmdSearch runs a bleve match query and returns top-K {path, snippet}.
func (t *LocalPDFs) cmdSearch(in localPDFsInput, wrap resultFn) (tools.Result, error) {
	if strings.TrimSpace(in.Query) == "" {
		return tools.Result{}, fmt.Errorf("knowledge.local_pdfs: query is required for search")
	}
	k := in.K
	if k <= 0 {
		k = 10
	}
	hits, err := t.search(in.Query, k)
	if err != nil {
		return tools.Result{}, err
	}
	return wrap(
		map[string]any{"count": len(hits), "results": hits},
		fmt.Sprintf("local_pdfs search: %d hit(s)", len(hits)),
	)
}

// cmdAsk searches then asks the mapper to answer the question from the
// concatenated snippets.
func (t *LocalPDFs) cmdAsk(ctx context.Context, in localPDFsInput, wrap resultFn) (tools.Result, error) {
	if strings.TrimSpace(in.Question) == "" {
		return tools.Result{}, fmt.Errorf("knowledge.local_pdfs: question is required for ask")
	}
	k := in.K
	if k <= 0 {
		k = 10
	}
	hits, err := t.search(in.Question, k)
	if err != nil {
		return tools.Result{}, err
	}
	var sb strings.Builder
	for _, h := range hits {
		sb.WriteString("# ")
		sb.WriteString(h.Path)
		sb.WriteString("\n")
		sb.WriteString(h.Snippet)
		sb.WriteString("\n\n")
	}
	if t.mapper == nil {
		return tools.Result{}, fmt.Errorf("knowledge.local_pdfs: ask requires a configured LLM mapper")
	}
	answer, err := t.mapper.Map(ctx, in.Question, sb.String())
	if err != nil {
		return tools.Result{}, err
	}
	return wrap(
		map[string]any{"answer": answer, "sources": hits},
		fmt.Sprintf("local_pdfs ask: 1 answer from %d snippet(s)", len(hits)),
	)
}

// localPDFHit is one row of search/ask output.
type localPDFHit struct {
	Path    string `json:"path"`
	Snippet string `json:"snippet"`
}

// search runs the bleve query and gathers snippets from the index'd full text.
// The snippet is the window around the first occurrence of the query token.
func (t *LocalPDFs) search(query string, k int) ([]localPDFHit, error) {
	idx, err := t.openIndex()
	if err != nil {
		return nil, err
	}
	t.mu.Lock()
	q := bleve.NewMatchQuery(query)
	req := bleve.NewSearchRequest(q)
	req.Size = k
	req.Fields = []string{"text"}
	sr, err := idx.Search(req)
	t.mu.Unlock()
	if err != nil {
		return nil, err
	}
	hits := make([]localPDFHit, 0, len(sr.Hits))
	for _, h := range sr.Hits {
		text, _ := h.Fields["text"].(string)
		hits = append(hits, localPDFHit{
			Path:    h.ID,
			Snippet: snippetAround(text, query, 240),
		})
	}
	return hits, nil
}

// openIndex lazily creates or opens the bleve index.
func (t *LocalPDFs) openIndex() (bleve.Index, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.index != nil {
		return t.index, nil
	}
	var idx bleve.Index
	var err error
	if _, statErr := os.Stat(t.indexDir); statErr == nil {
		idx, err = bleve.Open(t.indexDir)
	} else {
		idx, err = bleve.New(t.indexDir, bleve.NewIndexMapping())
	}
	if err != nil {
		return nil, fmt.Errorf("knowledge.local_pdfs: open index: %w", err)
	}
	t.index = idx
	return idx, nil
}

// Close releases the bleve index. Safe to call when nothing was opened.
func (t *LocalPDFs) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.index == nil {
		return nil
	}
	idx := t.index
	t.index = nil
	return idx.Close()
}

// scanPDFs walks dir and returns absolute paths to all *.pdf files.
func scanPDFs(dir string, recursive bool) ([]string, error) {
	var out []string
	if recursive {
		err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			if strings.EqualFold(filepath.Ext(path), ".pdf") {
				abs, _ := filepath.Abs(path)
				out = append(out, abs)
			}
			return nil
		})
		return out, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if strings.EqualFold(filepath.Ext(e.Name()), ".pdf") {
			abs, _ := filepath.Abs(filepath.Join(dir, e.Name()))
			out = append(out, abs)
		}
	}
	return out, nil
}

// extractPDFText reads every page of a PDF and concatenates the plain text.
// Errors surface to the caller; an empty document returns ("", nil).
func extractPDFText(path string) (string, error) {
	f, r, err := pdf.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	var buf bytes.Buffer
	totalPage := r.NumPage()
	for i := 1; i <= totalPage; i++ {
		p := r.Page(i)
		if p.V.IsNull() {
			continue
		}
		text, err := p.GetPlainText(nil)
		if err != nil {
			continue
		}
		buf.WriteString(text)
		buf.WriteString("\n")
	}
	return buf.String(), nil
}

// snippetAround returns a window of `width` runes around the first occurrence
// of `query` in `text` (case-insensitive). If no match, the first `width`
// runes are returned.
func snippetAround(text, query string, width int) string {
	if text == "" {
		return ""
	}
	lower := strings.ToLower(text)
	idx := strings.Index(lower, strings.ToLower(strings.TrimSpace(query)))
	if idx < 0 {
		runes := []rune(text)
		if len(runes) <= width {
			return string(runes)
		}
		return string(runes[:width]) + "..."
	}
	half := width / 2
	start := idx - half
	if start < 0 {
		start = 0
	}
	end := start + width
	if end > len(text) {
		end = len(text)
	}
	prefix := ""
	suffix := ""
	if start > 0 {
		prefix = "..."
	}
	if end < len(text) {
		suffix = "..."
	}
	return prefix + text[start:end] + suffix
}
