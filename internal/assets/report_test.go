package assets

import "testing"

func TestReportEmptyByDefault(t *testing.T) {
	var r Report
	if !r.OK() {
		t.Fatal("zero Report should be OK")
	}
	if r.Summary() != "" {
		t.Fatalf("zero Report summary should be empty, got %q", r.Summary())
	}
}

func TestReportSummaryCountsIssues(t *testing.T) {
	r := Report{
		Errors:   []AssetIssue{{Asset: "skills/bad.md", Message: "boom"}},
		Warnings: []AssetIssue{{Asset: "system.md", Message: "no Refusals section"}},
	}
	if r.OK() {
		t.Fatal("Report with errors must not be OK")
	}
	got := r.Summary()
	want := "1 error, 1 warning in fova config — run /skills validate and /config validate"
	if got != want {
		t.Fatalf("Summary = %q, want %q", got, want)
	}
}
