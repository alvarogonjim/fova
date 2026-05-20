package tui

import (
	"github.com/alvarogonjim/fova/internal/domain"
	"github.com/alvarogonjim/fova/internal/tools/plan"
)

// renderPlan formats a design plan as a readable multi-line block. Delegates
// to the shared plan.RenderPlan helper so /plan and plan.create render the
// same labelled-row layout.
func renderPlan(p domain.DesignPlan) string {
	out := plan.RenderPlan(p)
	out += "\n\nUse /plan approve to lock it in, or /plan cancel to discard it."
	return out
}

// renderNoPlan is shown when no design plan exists yet.
func renderNoPlan() string {
	return "No design plan yet.\n" +
		"Ask the agent to plan from a target, e.g. " +
		"\"design VHH binders against SARS-CoV-2 spike RBD\"."
}
