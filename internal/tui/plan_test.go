package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/alvarogonjim/proteus/internal/domain"
)

func TestRenderPlan(t *testing.T) {
	approvedAt := time.Date(2026, 5, 18, 9, 30, 0, 0, time.UTC)
	p := domain.DesignPlan{
		ID:             "p_0001",
		ProjectID:      "proj_default",
		Application:    domain.AppBinder,
		Target:         domain.PDBReference{PDBID: "6VXX", Chain: "A"},
		Method:         "design.bindcraft",
		Filters:        domain.FilterConfig{MinIPSAE: 0.5, MinPLDDT: 80},
		ShortlistSize:  24,
		ComputeBackend: "modal",
		EstimatedCost:  12.50,
		EstimatedTime:  "~3h",
		Rationale:      "BindCraft excels at de novo binders.",
		EvidencePapers: []domain.PaperRef{
			{Title: "BindCraft de novo binders", Year: 2024, DOI: "10.1101/2024.01.01"},
		},
		Approved:   true,
		ApprovedAt: &approvedAt,
	}
	out := renderPlan(p)

	for _, want := range []string{
		"6VXX",
		string(domain.AppBinder),
		"design.bindcraft",
		"10.1101/2024.01.01",
		"approved",
		"MinIPSAE",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("renderPlan output missing %q in:\n%s", want, out)
		}
	}
}

func TestRenderPlanPending(t *testing.T) {
	p := domain.DesignPlan{ID: "p_x", Method: "design.bindcraft"}
	out := renderPlan(p)
	if !strings.Contains(out, "pending approval") {
		t.Errorf("unapproved plan should show pending approval:\n%s", out)
	}
}

func TestRenderNoPlan(t *testing.T) {
	out := renderNoPlan()
	if out == "" {
		t.Fatal("renderNoPlan must not be empty")
	}
	if !strings.Contains(out, "plan") {
		t.Errorf("renderNoPlan should mention \"plan\":\n%s", out)
	}
}
