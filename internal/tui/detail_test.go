package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alvarogonjim/fova/internal/domain"
	tea "github.com/charmbracelet/bubbletea"
)

func TestJobLogReadLog(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "job.log")
	want := "line1\nline2\nline3\n"
	if err := os.WriteFile(path, []byte(want), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	if got := readLog(path); got != want {
		t.Errorf("readLog(existing) = %q, want %q", got, want)
	}

	if got := readLog(filepath.Join(dir, "missing.log")); got != "" {
		t.Errorf("readLog(missing) = %q, want \"\"", got)
	}

	if got := readLog(""); got != "" {
		t.Errorf("readLog(empty path) = %q, want \"\"", got)
	}
}

func TestJobLogTailLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "job.log")
	var sb strings.Builder
	for i := 1; i <= 10; i++ {
		sb.WriteString("line")
		sb.WriteByte(byte('0' + i%10))
		sb.WriteByte('\n')
	}
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	got := tailLines(path, 3)
	want := []string{"line8", "line9", "line0"}
	if len(got) != len(want) {
		t.Fatalf("tailLines(n=3) = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("tailLines[%d] = %q, want %q", i, got[i], want[i])
		}
	}

	if got := tailLines(filepath.Join(dir, "missing.log"), 3); len(got) != 0 {
		t.Errorf("tailLines(missing) = %v, want empty", got)
	}

	if got := tailLines("", 3); len(got) != 0 {
		t.Errorf("tailLines(empty path) = %v, want empty", got)
	}
}

func TestJobLogView(t *testing.T) {
	v := newDetailView(NewTheme())
	v.setSize(80, 20)
	v.setContent("install bindcraft", "line1\nline2")

	out := v.View()
	if !strings.Contains(out, "install bindcraft") {
		t.Errorf("View() missing header; got:\n%s", out)
	}
	if !strings.Contains(out, "line1") {
		t.Errorf("View() missing body line; got:\n%s", out)
	}
}

func TestJobLogViewUpdateScroll(t *testing.T) {
	v := newDetailView(NewTheme())
	v.setSize(40, 5)
	var sb strings.Builder
	for i := 0; i < 50; i++ {
		sb.WriteString("row\n")
	}
	v.setContent("hdr", sb.String())

	// Routing a scroll key through update returns a detailView without panicking.
	v = v.update(tea.KeyMsg{Type: tea.KeyPgDown})
	if got := v.View(); !strings.Contains(got, "hdr") {
		t.Errorf("View() after scroll missing header; got:\n%s", got)
	}
}

func TestRenderJobDetailRunning(t *testing.T) {
	started := time.Now().Add(-2 * time.Minute)
	j := domain.Job{
		ID: "job-abc", Tool: "design.bindcraft", Status: domain.JobRunning,
		Backend: "modal", Progress: 0.5, Created: time.Now(), Started: &started,
	}
	header, body := renderJobDetail(NewTheme(), j)
	if !strings.Contains(header, "design.bindcraft") || !strings.Contains(header, "job-abc") {
		t.Errorf("header missing job identity: %q", header)
	}
	for _, want := range []string{"status", "backend", "modal", "log"} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q: %q", want, body)
		}
	}
}

func TestRenderJobDetailFailedShowsError(t *testing.T) {
	j := domain.Job{ID: "j", Tool: "t", Status: domain.JobFailed, Error: "boom", Created: time.Now()}
	_, body := renderJobDetail(NewTheme(), j)
	if !strings.Contains(body, "error") || !strings.Contains(body, "boom") {
		t.Errorf("failed job body should show the error: %q", body)
	}
}

func TestRenderDesignDetail(t *testing.T) {
	d := domain.Design{
		ID: "d-1", Origin: domain.OriginBindCraft, Application: domain.AppBinder,
		Created:  time.Now(),
		Sequence: domain.Sequence{Chains: map[string]string{"A": "MKTAYIAKQR"}},
		Scores:   map[string]float64{"ipsae": 0.71, "plddt_mean": 88.4},
	}
	header, body := renderDesignDetail(NewTheme(), d)
	if !strings.Contains(header, "d-1") {
		t.Errorf("header missing design id: %q", header)
	}
	for _, want := range []string{"scores", "ipsae", "sequence", "MKTAYIAKQR", "provenance"} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q: %q", want, body)
		}
	}
}

func TestRenderExperimentDetailNoResults(t *testing.T) {
	e := domain.Experiment{ID: "e1", TargetName: "PD-L1", AssayType: "binding", Status: "in_progress"}
	_, body := renderExperimentDetail(NewTheme(), e)
	if !strings.Contains(body, "no results yet") {
		t.Errorf("an experiment with no results should say so: %q", body)
	}
}

func TestRenderExperimentDetailWithResults(t *testing.T) {
	kd := 12.0
	e := domain.Experiment{
		ID: "e1", TargetName: "PD-L1", AssayType: "binding", Status: "done",
		Results: []domain.ExperimentResult{
			{DesignID: "d-1", Kd: &kd, KdUnits: "nM", BindingStrength: "strong"},
		},
	}
	_, body := renderExperimentDetail(NewTheme(), e)
	for _, want := range []string{"results", "d-1", "strong"} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q: %q", want, body)
		}
	}
}

func TestSortedScoreKeys(t *testing.T) {
	got := sortedScoreKeys(map[string]float64{"z": 1, "a": 2, "m": 3})
	if len(got) != 3 || got[0] != "a" || got[1] != "m" || got[2] != "z" {
		t.Errorf("sortedScoreKeys = %v, want [a m z]", got)
	}
}

func TestShortHash(t *testing.T) {
	if got := shortHash("abcdef1234"); got != "abcdef" {
		t.Errorf("a long hash should truncate to 6 chars, got %q", got)
	}
	if got := shortHash("abc"); got != "abc" {
		t.Errorf("a short hash should be returned as-is, got %q", got)
	}
}

func TestWrapResidues(t *testing.T) {
	if got := wrapResidues(""); got != "  (empty)" {
		t.Errorf("empty chain: got %q, want %q", got, "  (empty)")
	}
	if got := wrapResidues("ABC"); !strings.Contains(got, "ABC") {
		t.Errorf("short chain should appear verbatim: got %q", got)
	}
	if got := wrapResidues(strings.Repeat("A", 50)); strings.Contains(got, "\n") {
		t.Errorf("50 residues (5 blocks) should fit on one line, got %q", got)
	}
	if got := wrapResidues(strings.Repeat("A", 51)); strings.Count(got, "\n") != 1 {
		t.Errorf("51 residues should wrap to 2 lines (1 newline), got %d newlines in %q",
			strings.Count(got, "\n"), got)
	}
}

func TestRenderSequenceChains(t *testing.T) {
	if got := renderSequenceChains(domain.Sequence{}); got != " (no sequence)" {
		t.Errorf("no chains: got %q, want %q", got, " (no sequence)")
	}
	out := renderSequenceChains(domain.Sequence{Chains: map[string]string{
		"B": "WWWWWWWWWW", "A": "MMMMMMMMMM",
	}})
	ia, ib := strings.Index(out, "chain A"), strings.Index(out, "chain B")
	if ia < 0 || ib < 0 || ia > ib {
		t.Errorf("chains should render sorted, A before B: got %q", out)
	}
}
