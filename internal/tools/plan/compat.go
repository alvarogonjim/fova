package plan

import (
	"sort"
	"strings"

	"github.com/alvarogonjim/fova/internal/domain"
)

// Method is the canonical, mixed-case identifier for a design method as it
// appears in DesignPlan.Method on the wire. It is a planner-local type — the
// domain still stores Method as a free-form string for back-compatibility, so
// this enum lives here rather than in internal/domain.
//
// Adding a new method:
//  1. Add a const below.
//  2. Add it to compat for every applicable Application.
//  3. Add its tools.toml key to toolForMethod.
//  4. Add at least one name alias to parseMethod (canonical + lowercase tool
//     name + "design.<lower>" form covers the three shapes the LLM emits).
type Method string

const (
	MethodBindCraft    Method = "BindCraft"
	MethodRFdiffusion  Method = "RFdiffusion"
	MethodRFdiffusion2 Method = "RFdiffusion2"
	MethodProteinMPNN  Method = "ProteinMPNN"
	MethodLigandMPNN   Method = "LigandMPNN"
	MethodRFantibody   Method = "RFantibody"
	MethodChai2        Method = "Chai2"
	// BoltzGen — Stark et al. (2026), "Toward Universal Binder Design".
	// PyRosetta-free generative binder design that runs on aarch64 (Grace);
	// added as the SPECS-documented alternative when BindCraft is blocked
	// by the upstream PyRosetta wheel gap.
	MethodBoltzGen Method = "BoltzGen"
)

// compat is the single source of truth for the application↔method matrix.
// plan.create rejects any pairing not listed here. The matrix encodes the
// physical-suitability constraints documented in the design-tool catalogue:
//
//   - binder:   de novo binder design pipelines.
//   - antibody: heavy/light-chain CDR design (RFantibody is the dedicated
//     stack; RFdiffusion is admitted because it can scaffold an Ab template
//     in a pinch).
//   - enzyme:   motif scaffolding for catalytic sites + ligand-aware MPNN
//     for sequence design.
//   - redesign: fixed-backbone sequence redesign on an existing scaffold.
//
// Every domain.Application enum value must appear here. The exhaustive check
// is enforced by TestCompatCoversEveryApplication in compat_test.go.
var compat = map[domain.Application][]Method{
	domain.AppBinder: {
		MethodBindCraft,
		MethodBoltzGen,
		MethodRFdiffusion,
		MethodRFdiffusion2,
		MethodProteinMPNN,
		MethodChai2,
	},
	domain.AppAntibody: {
		MethodRFantibody,
		MethodBoltzGen,
		MethodRFdiffusion,
	},
	domain.AppEnzyme: {
		MethodRFdiffusion,
		MethodRFdiffusion2,
		MethodLigandMPNN,
	},
	domain.AppRedesign: {
		MethodProteinMPNN,
		MethodLigandMPNN,
	},
}

// methodAllowed reports whether method m is on the compat list for app.
func methodAllowed(app domain.Application, m Method) bool {
	for _, ok := range compat[app] {
		if ok == m {
			return true
		}
	}
	return false
}

// compatibleMethods returns the sorted canonical names of every method
// allowed for app. Used in error messages so the user sees a stable
// alphabetical list of alternatives.
func compatibleMethods(app domain.Application) []string {
	ms := compat[app]
	out := make([]string, 0, len(ms))
	for _, m := range ms {
		out = append(out, string(m))
	}
	sort.Strings(out)
	return out
}

// methodAliases maps every accepted spelling of a method name to its
// canonical Method. Three shapes are accepted so the LLM doesn't have to
// memorise our exact casing:
//
//	"BindCraft"          (canonical mixed-case)
//	"bindcraft"          (lowercase tool name, matches tools.toml)
//	"design.bindcraft"   (registered tool name, matches the tools registry)
var methodAliases = map[string]Method{
	// BindCraft
	"bindcraft":        MethodBindCraft,
	"design.bindcraft": MethodBindCraft,
	// RFdiffusion
	"rfdiffusion":        MethodRFdiffusion,
	"design.rfdiffusion": MethodRFdiffusion,
	// RFdiffusion2
	"rfdiffusion2":        MethodRFdiffusion2,
	"design.rfdiffusion2": MethodRFdiffusion2,
	// ProteinMPNN
	"proteinmpnn":        MethodProteinMPNN,
	"design.proteinmpnn": MethodProteinMPNN,
	// LigandMPNN
	"ligandmpnn":        MethodLigandMPNN,
	"design.ligandmpnn": MethodLigandMPNN,
	// RFantibody
	"rfantibody":        MethodRFantibody,
	"design.rfantibody": MethodRFantibody,
	// Chai2 (no separate tools.toml entry — runs via chai1 weights + design head)
	"chai2":        MethodChai2,
	"design.chai2": MethodChai2,
	// BoltzGen
	"boltzgen":        MethodBoltzGen,
	"design.boltzgen": MethodBoltzGen,
}

// parseMethod normalises a free-form method string into its canonical
// Method. It returns ok=false if the string is empty or unknown — the
// caller is expected to surface a clear error in that case.
func parseMethod(s string) (Method, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", false
	}
	// Exact canonical match (mixed-case) wins first so a typo like "bindCraft"
	// goes through the alias map (lowercased) rather than failing here.
	for _, m := range []Method{
		MethodBindCraft, MethodRFdiffusion, MethodRFdiffusion2,
		MethodProteinMPNN, MethodLigandMPNN, MethodRFantibody, MethodChai2,
		MethodBoltzGen,
	} {
		if string(m) == s {
			return m, true
		}
	}
	if m, ok := methodAliases[strings.ToLower(s)]; ok {
		return m, true
	}
	return "", false
}

// toolForMethod returns the tools.toml key for the local tool that
// implements method m. The InstallChecker.Status call uses this key to
// decide whether the plan can actually run. Returns "" for an unknown
// method — the caller treats that as "no install check possible".
func toolForMethod(m Method) string {
	switch m {
	case MethodBindCraft:
		return "bindcraft"
	case MethodRFdiffusion:
		return "rfdiffusion"
	case MethodRFdiffusion2:
		return "rfdiffusion2"
	case MethodProteinMPNN:
		return "proteinmpnn"
	case MethodLigandMPNN:
		return "ligandmpnn"
	case MethodRFantibody:
		return "rfantibody"
	case MethodChai2:
		// Chai2 piggybacks on the chai1 weights; the install probe targets
		// the chai1 image (the only on-disk artefact).
		return "chai1"
	case MethodBoltzGen:
		return "boltzgen"
	}
	return ""
}
