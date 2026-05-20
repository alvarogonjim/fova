package knowledge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"
)

const bioRxivBody = `{
  "collection": [
    {"doi": "10.1101/br1", "title": "De Novo Binders",
     "authors": "Kim S; Park J", "date": "2024-03-01"},
    {"doi": "10.1101/br2", "title": "Diffusion Backbones",
     "authors": "Wong L", "date": "2024-03-15"}
  ]
}`

func TestBioRxivExecute(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(bioRxivBody))
	}))
	defer srv.Close()

	res := NewResults()
	tool := NewBioRxiv(res, 0)
	tool.BaseURL = srv.URL

	out, err := tool.Execute(context.Background(),
		json.RawMessage(`{"from":"2024-03-01","to":"2024-03-31"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(gotPath, "2024-03-01") || !strings.Contains(gotPath, "2024-03-31") {
		t.Errorf("request path = %q, want from/to date segments", gotPath)
	}
	var parsed struct {
		ResultsID string  `json:"results_id"`
		Count     int     `json:"count"`
		Papers    []Paper `json:"papers"`
	}
	if err := json.Unmarshal(out.Output, &parsed); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if parsed.Count != 2 {
		t.Fatalf("count = %d, want 2", parsed.Count)
	}
	p := parsed.Papers[0]
	if p.ID != "10.1101/br1" {
		t.Errorf("papers[0].ID = %q, want 10.1101/br1", p.ID)
	}
	if p.Title != "De Novo Binders" {
		t.Errorf("papers[0].Title = %q", p.Title)
	}
	if p.Authors != "Kim S; Park J" {
		t.Errorf("papers[0].Authors = %q", p.Authors)
	}
	if p.Year != 2024 {
		t.Errorf("papers[0].Year = %d, want 2024", p.Year)
	}
	if p.Source != "biorxiv" {
		t.Errorf("papers[0].Source = %q, want biorxiv", p.Source)
	}
	if _, ok := res.Get(parsed.ResultsID); !ok {
		t.Fatalf("results_id %q not in cache", parsed.ResultsID)
	}
}

func TestBioRxivDefaultsDateRange(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"collection": []}`))
	}))
	defer srv.Close()

	tool := NewBioRxiv(NewResults(), 0)
	tool.BaseURL = srv.URL

	// Empty input should default both from and to without error.
	if _, err := tool.Execute(context.Background(), json.RawMessage(`{}`)); err != nil {
		t.Fatalf("Execute with default dates: %v", err)
	}

	// The request path must carry two YYYY-MM-DD date segments: /{from}/{to}.
	dateRe := regexp.MustCompile(`\d{4}-\d{2}-\d{2}`)
	dates := dateRe.FindAllString(gotPath, -1)
	if len(dates) != 2 {
		t.Fatalf("request path = %q, want two YYYY-MM-DD date segments, got %d", gotPath, len(dates))
	}
	gotFrom, gotTo := dates[0], dates[1]

	// 'to' defaults to today (UTC).
	wantTo := time.Now().UTC().Format("2006-01-02")
	if gotTo != wantTo {
		t.Errorf("'to' date = %q, want today %q", gotTo, wantTo)
	}

	// 'from' defaults to exactly 30 days before 'to'.
	toTime, err := time.Parse("2006-01-02", gotTo)
	if err != nil {
		t.Fatalf("parse 'to' date %q: %v", gotTo, err)
	}
	wantFrom := toTime.AddDate(0, 0, -30).Format("2006-01-02")
	if gotFrom != wantFrom {
		t.Errorf("'from' date = %q, want %q (30 days before 'to')", gotFrom, wantFrom)
	}
}
