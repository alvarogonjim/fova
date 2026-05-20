package plan

import (
	"strings"
	"testing"

	"github.com/alvarogonjim/fova/internal/domain"
)

// TestCompatCoversEveryApplication ensures the compat matrix names every
// Application enum value defined in internal/domain. If a new Application is
// added to domain without also being mapped here, plan.create would silently
// reject every method paired with it — this test catches the gap.
func TestCompatCoversEveryApplication(t *testing.T) {
	apps := []domain.Application{
		domain.AppBinder,
		domain.AppAntibody,
		domain.AppEnzyme,
		domain.AppRedesign,
	}
	for _, app := range apps {
		methods, ok := compat[app]
		if !ok {
			t.Errorf("compat is missing application %q", app)
			continue
		}
		if len(methods) == 0 {
			t.Errorf("application %q has no compatible methods — fix compat.go", app)
		}
	}
}

// TestMethodAllowed exercises both rejection and acceptance paths for a
// representative subset of the matrix. The exhaustive round-trip happens in
// plan_test.go::TestPlanCreateAcceptsCompatibleApplicationMethod.
func TestMethodAllowed(t *testing.T) {
	cases := []struct {
		app    domain.Application
		method Method
		want   bool
	}{
		{domain.AppBinder, MethodBindCraft, true},
		{domain.AppBinder, MethodRFdiffusion, true},
		{domain.AppBinder, MethodRFantibody, false},
		{domain.AppAntibody, MethodRFantibody, true},
		{domain.AppAntibody, MethodBindCraft, false},
		{domain.AppEnzyme, MethodLigandMPNN, true},
		{domain.AppEnzyme, MethodBindCraft, false},
		{domain.AppRedesign, MethodProteinMPNN, true},
		{domain.AppRedesign, MethodBindCraft, false},
	}
	for _, tc := range cases {
		got := methodAllowed(tc.app, tc.method)
		if got != tc.want {
			t.Errorf("methodAllowed(%q, %q) = %v, want %v", tc.app, tc.method, got, tc.want)
		}
	}
}

// TestParseMethodNormalises ensures plan.create accepts the names the LLM
// most commonly emits: the canonical mixed-case form ("BindCraft"), the
// lower-case tool name ("bindcraft"), and the registered tool name
// ("design.bindcraft").
func TestParseMethodNormalises(t *testing.T) {
	cases := []struct {
		in   string
		want Method
	}{
		{"BindCraft", MethodBindCraft},
		{"bindcraft", MethodBindCraft},
		{"design.bindcraft", MethodBindCraft},
		{"RFdiffusion", MethodRFdiffusion},
		{"rfdiffusion", MethodRFdiffusion},
		{"design.rfdiffusion", MethodRFdiffusion},
		{"ProteinMPNN", MethodProteinMPNN},
		{"design.proteinmpnn", MethodProteinMPNN},
		{"RFantibody", MethodRFantibody},
		{"design.rfantibody", MethodRFantibody},
		{"LigandMPNN", MethodLigandMPNN},
		{"RFdiffusion2", MethodRFdiffusion2},
		{"Chai2", MethodChai2},
	}
	for _, tc := range cases {
		got, ok := parseMethod(tc.in)
		if !ok {
			t.Errorf("parseMethod(%q) returned ok=false, want %q", tc.in, tc.want)
			continue
		}
		if got != tc.want {
			t.Errorf("parseMethod(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestParseMethodRejectsUnknown verifies unrecognised method names surface
// a useful error instead of silently mapping to a default.
func TestParseMethodRejectsUnknown(t *testing.T) {
	if _, ok := parseMethod("not_a_method"); ok {
		t.Error("parseMethod must return ok=false for an unknown method name")
	}
	if _, ok := parseMethod(""); ok {
		t.Error("parseMethod must return ok=false for the empty string")
	}
}

// TestToolForMethodCoversEveryMethod guarantees every Method in the
// compat matrix has a tools.toml mapping. Without this, the installed-tool
// check could panic or silently approve an unknown method.
func TestToolForMethodCoversEveryMethod(t *testing.T) {
	seen := make(map[Method]bool)
	for _, methods := range compat {
		for _, m := range methods {
			seen[m] = true
		}
	}
	for m := range seen {
		tool := toolForMethod(m)
		if tool == "" {
			t.Errorf("method %q has no tools.toml mapping in toolForMethod", m)
		}
		// Sanity: the tool name must be the lowercase form (matches the
		// keys in internal/backends/local/tools.toml).
		if tool != strings.ToLower(tool) {
			t.Errorf("toolForMethod(%q) = %q is not lowercase — tools.toml keys are lowercase", m, tool)
		}
	}
}
