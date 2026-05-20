package local

import (
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

func TestLoadRegistryParsesTools(t *testing.T) {
	r, err := LoadRegistry("/home/u/fova")
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	for _, name := range []string{"ipsae", "proteinmpnn", "rfdiffusion", "bindcraft"} {
		if _, ok := r.Tool(name); !ok {
			t.Errorf("tool %q missing", name)
		}
	}
	if len(r.Tools()) < 8 {
		t.Errorf("expected >=8 tools, got %d", len(r.Tools()))
	}
}

func TestRecipePlaceholdersExpanded(t *testing.T) {
	// Use a still-legacy recipe (rfantibody) to assert the venv-style placeholder
	// expansion contract. proteinmpnn migrated to container mode in v0.7 (Bug 15).
	r, _ := LoadRegistry("/home/u/fova")
	rec, ok := r.Tool("legacy_fixture")
	if !ok {
		t.Fatal("legacy_fixture missing")
	}
	// ${FOVA_HOME} expanded.
	if rec.VenvDir != "/home/u/fova/tools/legacy_fixture/.venv" {
		t.Errorf("venv_dir = %q", rec.VenvDir)
	}
	// install_dir defaulted and expanded.
	if rec.InstallDir != "/home/u/fova/tools/legacy_fixture" {
		t.Errorf("install_dir = %q", rec.InstallDir)
	}
	// {{ }} placeholders expanded in install_steps — no braces should remain.
	for _, s := range rec.InstallSteps {
		if strings.Contains(s, "{{") {
			t.Errorf("unexpanded placeholder in step: %q", s)
		}
		if strings.Contains(s, "${FOVA_HOME}") {
			t.Errorf("unexpanded ${FOVA_HOME} in step: %q", s)
		}
	}
	// git_ref defaults to "main".
	if rec.GitRef != "main" {
		t.Errorf("git_ref = %q, want main", rec.GitRef)
	}
}

// TestProteinMPNNRecipeIsContainerMode locks in the v0.7 Bug 15 migration:
// proteinmpnn's tools.toml block declares the container-mode schema with a
// smoke test that exercises verify_gpu.py before the tool-specific call.
func TestProteinMPNNRecipeIsContainerMode(t *testing.T) {
	r, _ := LoadRegistry("/home/u/fova")
	rec, ok := r.Tool("proteinmpnn")
	if !ok {
		t.Fatal("proteinmpnn missing")
	}
	if rec.ImageTag != "fova/proteinmpnn:v1.0.1" {
		t.Errorf("image_tag = %q, want fova/proteinmpnn:v1.0.1", rec.ImageTag)
	}
	if rec.Containerfile != "proteinmpnn.Containerfile" {
		t.Errorf("containerfile = %q, want proteinmpnn.Containerfile", rec.Containerfile)
	}
	if rec.Entrypoint != "python /opt/proteinmpnn/protein_mpnn_run.py" {
		t.Errorf("entrypoint = %q", rec.Entrypoint)
	}
	if !rec.GPU {
		t.Error("gpu = false, want true for proteinmpnn (GPU required)")
	}
	if len(rec.WeightsPaths) != 0 {
		t.Errorf("weights_paths = %v, want empty (weights ship in-repo)", rec.WeightsPaths)
	}
	if rec.TimeoutSeconds != 1800 {
		t.Errorf("timeout_seconds = %d, want 1800", rec.TimeoutSeconds)
	}
	// Smoke MUST exercise verify_gpu.py before the tool-specific call.
	if !strings.Contains(rec.SmokeTest, "verify_gpu.py") {
		t.Errorf("smoke_test must reference verify_gpu.py, got: %q", rec.SmokeTest)
	}
	if !strings.Contains(rec.SmokeTest, "protein_mpnn_run.py --help") {
		t.Errorf("smoke_test must end with protein_mpnn_run.py --help, got: %q", rec.SmokeTest)
	}
	// verify_gpu must come BEFORE the tool-specific call (fail fast on broken base).
	gpuIdx := strings.Index(rec.SmokeTest, "verify_gpu.py")
	toolIdx := strings.Index(rec.SmokeTest, "protein_mpnn_run.py")
	if gpuIdx < 0 || toolIdx < 0 || gpuIdx > toolIdx {
		t.Errorf("verify_gpu.py must run BEFORE protein_mpnn_run.py in smoke_test: %q", rec.SmokeTest)
	}
	// Legacy fields must be empty — the container-mode block replaces them.
	if rec.RunCommand != "" {
		t.Errorf("run_command = %q, want empty for container-mode recipe", rec.RunCommand)
	}
	if len(rec.InstallSteps) != 0 {
		t.Errorf("install_steps = %v, want empty for container-mode recipe", rec.InstallSteps)
	}
	if rec.VenvDir != "" {
		t.Errorf("venv_dir = %q, want empty for container-mode recipe", rec.VenvDir)
	}
}

func TestRunCommandKeepsRuntimePlaceholders(t *testing.T) {
	r, _ := LoadRegistry("/home/u/fova")
	// Use a legacy tool (rfantibody) — proteinmpnn moved to container mode in
	// Bug 14 and no longer has a RunCommand. rfantibody's run_command still
	// uses the recipe-field/runtime-placeholder mix that this test covers.
	rec, _ := r.Tool("legacy_fixture")
	// run_command keeps runtime placeholders (filled by the runner, not here).
	if !strings.Contains(rec.RunCommand, "{{ args_file }}") {
		t.Errorf("run_command lost its runtime placeholder: %q", rec.RunCommand)
	}
	// but recipe-field placeholders ARE expanded.
	if strings.Contains(rec.RunCommand, "{{ venv_dir }}") {
		t.Errorf("run_command venv_dir not expanded: %q", rec.RunCommand)
	}
}

func TestRecipeContainerFieldsDefaultEmpty(t *testing.T) {
	// Legacy recipes (no container_* fields in tools.toml) must unmarshal with
	// zero-valued container fields so the platform can branch on them. Once
	// every real tool has migrated to container mode, no entry in tools.toml
	// is in legacy mode any more, so we test the unmarshalling contract
	// against an inline legacy-shape TOML literal.
	var doc struct {
		Tools map[string]ToolRecipe `toml:"tools"`
	}
	src := `[tools.legacy_fixture]
display_name = "legacy fixture"
version = "0.0.0"
repo = "https://example.com/legacy"
install_steps = ["echo legacy"]
run_command = "echo legacy"
`
	if _, err := toml.Decode(src, &doc); err != nil {
		t.Fatalf("toml.Decode: %v", err)
	}
	rec, ok := doc.Tools["legacy_fixture"]
	if !ok {
		t.Fatal("legacy_fixture missing from decoded tools")
	}
	if rec.ImageTag != "" {
		t.Errorf("ImageTag = %q, want empty for legacy recipe", rec.ImageTag)
	}
	if rec.Containerfile != "" {
		t.Errorf("Containerfile = %q, want empty for legacy recipe", rec.Containerfile)
	}
	if rec.Entrypoint != "" {
		t.Errorf("Entrypoint = %q, want empty for legacy recipe", rec.Entrypoint)
	}
	if rec.GPU {
		t.Error("GPU = true, want false for legacy recipe without container fields")
	}
	if len(rec.WeightsPaths) != 0 {
		t.Errorf("WeightsPaths = %v, want empty", rec.WeightsPaths)
	}
	if rec.TimeoutSeconds != 0 {
		t.Errorf("TimeoutSeconds = %d, want 0 for legacy recipe", rec.TimeoutSeconds)
	}
	if rec.SmokeTest != "" {
		t.Errorf("SmokeTest = %q, want empty for legacy recipe", rec.SmokeTest)
	}
}

func TestBaseImageConstant(t *testing.T) {
	// BaseImage is the NGC PyTorch tag every container-mode tool FROMs.
	if BaseImage == "" {
		t.Fatal("BaseImage constant is empty")
	}
	if !strings.HasPrefix(BaseImage, "nvcr.io/nvidia/pytorch:") {
		t.Errorf("BaseImage = %q, want nvcr.io/nvidia/pytorch:* tag", BaseImage)
	}
}

func TestDataAssets(t *testing.T) {
	r, _ := LoadRegistry("/home/u/fova")
	a, ok := r.DataAsset("alphafold_params")
	if !ok {
		t.Fatal("alphafold_params missing")
	}
	if a.ExtractTo != "/home/u/fova/data/alphafold_params" {
		t.Errorf("extract_to = %q", a.ExtractTo)
	}
}
