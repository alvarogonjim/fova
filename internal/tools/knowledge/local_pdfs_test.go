package knowledge

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alvarogonjim/fova/internal/tools"
)

var _ tools.Tool = (*LocalPDFs)(nil)

// makeFixturePDF writes a tiny PDF whose extracted text contains marker. It
// prefers a checked-in testdata/sample.pdf when present; otherwise it copies a
// minimal in-memory PDF into the dir.
func makeFixturePDF(t *testing.T, dir, name, marker string) string {
	t.Helper()
	dst := filepath.Join(dir, name)
	if body, err := os.ReadFile("testdata/sample.pdf"); err == nil && strings.Contains(string(body), marker) {
		if err := os.WriteFile(dst, body, 0o644); err != nil {
			t.Fatal(err)
		}
		return dst
	}
	// Fall back to a minimal valid PDF body. If extraction fails, the test
	// expects the implementer to check in a proper fixture (see plan §3 Step 2).
	body := minimalPDF(marker)
	if err := os.WriteFile(dst, body, 0o644); err != nil {
		t.Fatal(err)
	}
	return dst
}

func newTestLocalPDFs(t *testing.T) *LocalPDFs {
	t.Helper()
	dir := t.TempDir()
	return NewLocalPDFs(NewResults(), stubMapper{answer: "fixed answer"},
		filepath.Join(dir, "local_pdfs.bleve"), "")
}

func TestLocalPDFsAddSearchAsk(t *testing.T) {
	pdfDir := t.TempDir()
	pdfPath := makeFixturePDF(t, pdfDir, "sample.pdf", "alpha helix folding kinetics")

	tool := newTestLocalPDFs(t)

	// add
	addReq := `{"action":"add","dir":"` + pdfDir + `","recursive":true}`
	addOut, err := tool.Execute(context.Background(), json.RawMessage(addReq))
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	var added struct {
		Added int      `json:"added"`
		Files []string `json:"files"`
	}
	if err := json.Unmarshal(addOut.Output, &added); err != nil {
		t.Fatal(err)
	}
	if added.Added != 1 {
		t.Fatalf("added = %d, want 1", added.Added)
	}
	if len(added.Files) == 0 || !strings.Contains(added.Files[0], pdfPath) {
		t.Fatalf("files = %v, expected to contain %s", added.Files, pdfPath)
	}

	// search
	searchOut, err := tool.Execute(context.Background(),
		json.RawMessage(`{"action":"search","query":"folding","k":5}`))
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	var searched struct {
		Count   int `json:"count"`
		Results []struct {
			Path    string `json:"path"`
			Snippet string `json:"snippet"`
		} `json:"results"`
	}
	if err := json.Unmarshal(searchOut.Output, &searched); err != nil {
		t.Fatal(err)
	}
	if searched.Count != 1 {
		t.Fatalf("search count = %d, want 1", searched.Count)
	}
	if !strings.Contains(searched.Results[0].Snippet, "folding") {
		t.Errorf("snippet = %q, expected to contain 'folding'", searched.Results[0].Snippet)
	}

	// ask
	askOut, err := tool.Execute(context.Background(),
		json.RawMessage(`{"action":"ask","question":"what is the topic?","k":5}`))
	if err != nil {
		t.Fatalf("ask: %v", err)
	}
	var asked struct {
		Answer string `json:"answer"`
	}
	if err := json.Unmarshal(askOut.Output, &asked); err != nil {
		t.Fatal(err)
	}
	if asked.Answer != "fixed answer" {
		t.Errorf("answer = %q, want 'fixed answer'", asked.Answer)
	}
}

func TestLocalPDFsAddUsesDefaultDir(t *testing.T) {
	pdfDir := t.TempDir()
	makeFixturePDF(t, pdfDir, "sample.pdf", "alpha helix folding kinetics")

	indexDir := filepath.Join(t.TempDir(), "local_pdfs.bleve")
	tool := NewLocalPDFs(NewResults(), stubMapper{answer: "ok"}, indexDir, pdfDir)

	addOut, err := tool.Execute(context.Background(),
		json.RawMessage(`{"action":"add","recursive":true}`)) // no dir → use defaultDir
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	var added struct {
		Added int `json:"added"`
	}
	_ = json.Unmarshal(addOut.Output, &added)
	if added.Added != 1 {
		t.Fatalf("added (default dir) = %d, want 1", added.Added)
	}
}

func TestLocalPDFsAddRejectsMissingDir(t *testing.T) {
	tool := newTestLocalPDFs(t) // defaultDir is "" — must be supplied per call
	_, err := tool.Execute(context.Background(),
		json.RawMessage(`{"action":"add"}`))
	if err == nil {
		t.Fatal("expected error when neither defaultDir nor input.dir is set")
	}
}

func TestLocalPDFsUnknownAction(t *testing.T) {
	tool := newTestLocalPDFs(t)
	if _, err := tool.Execute(context.Background(),
		json.RawMessage(`{"action":"bogus"}`)); err == nil {
		t.Fatal("expected error for unknown action")
	}
}

// minimalPDF returns the bytes of a hand-rolled PDF that, when parsed by
// github.com/ledongthuc/pdf, yields a page containing marker. See plan §3.
func minimalPDF(marker string) []byte {
	// Construct a minimal PDF whose single page contains marker.
	// Header.
	header := "%PDF-1.4\n%\xff\xff\xff\xff\n"
	objs := []string{
		// 1: catalog
		"<< /Type /Catalog /Pages 2 0 R >>",
		// 2: pages
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		// 3: page
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>",
		// 4: contents stream (filled below)
		"",
		// 5: font
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}
	stream := "BT /F1 12 Tf 20 100 Td (" + marker + ") Tj ET"
	objs[3] = "<< /Length " + itoa(len(stream)) + " >>\nstream\n" + stream + "\nendstream"

	var buf []byte
	buf = append(buf, header...)
	offsets := make([]int, len(objs)+1)
	offsets[0] = 0
	for i, body := range objs {
		offsets[i+1] = len(buf)
		obj := itoa(i+1) + " 0 obj\n" + body + "\nendobj\n"
		buf = append(buf, obj...)
	}
	xrefStart := len(buf)
	buf = append(buf, "xref\n0 "+itoa(len(objs)+1)+"\n"...)
	buf = append(buf, "0000000000 65535 f \n"...)
	for _, off := range offsets[1:] {
		buf = append(buf, pad10(off)+" 00000 n \n"...)
	}
	buf = append(buf, "trailer\n<< /Size "+itoa(len(objs)+1)+" /Root 1 0 R >>\nstartxref\n"+itoa(xrefStart)+"\n%%EOF\n"...)
	return buf
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

func pad10(n int) string {
	s := itoa(n)
	for len(s) < 10 {
		s = "0" + s
	}
	return s
}
