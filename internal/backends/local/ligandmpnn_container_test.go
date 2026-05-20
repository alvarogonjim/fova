package local

import (
	"context"
	"os/exec"
	"strings"
	"testing"
)

// TestLigandMPNNRecipeIsContainerMode asserts that the [tools.ligandmpnn]
// block in tools.toml decodes into the container-mode schema (image_tag,
// containerfile, entrypoint, gpu, weights_paths, smoke_test, weights). The
// legacy install_steps/run_command fields must be absent so the platform
// branches into installContainer.
func TestLigandMPNNRecipeIsContainerMode(t *testing.T) {
	reg, err := LoadRegistry(t.TempDir())
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	rec, ok := reg.Tool("ligandmpnn")
	if !ok {
		t.Fatal("ligandmpnn missing from registry")
	}
	if rec.ImageTag == "" {
		t.Error("ImageTag must be set for container-mode ligandmpnn")
	}
	if rec.Containerfile != "ligandmpnn.Containerfile" {
		t.Errorf("Containerfile = %q, want ligandmpnn.Containerfile", rec.Containerfile)
	}
	if !strings.Contains(rec.Entrypoint, "/opt/ligandmpnn/run.py") {
		t.Errorf("Entrypoint must invoke /opt/ligandmpnn/run.py, got %q", rec.Entrypoint)
	}
	if !rec.GPU {
		t.Error("ligandmpnn is GPU-bound; GPU field must be true")
	}
	if len(rec.WeightsPaths) == 0 {
		t.Error("WeightsPaths must declare at least /models so the runner mounts the cache")
	}
	if rec.SmokeTest == "" {
		t.Error("smoke_test must be set")
	}
	// The shared verify_gpu.py fragment must be exercised by the smoke step
	// (verbatim or as an equivalent inline torch.cuda.is_available() assertion).
	if !strings.Contains(rec.SmokeTest, "torch.cuda.is_available()") {
		t.Errorf("smoke_test must assert torch.cuda.is_available(), got %q", rec.SmokeTest)
	}
	// The smoke must do tool-specific work — not just the GPU probe — so we
	// know ligandmpnn imports and a checkpoint loads from /models.
	if !strings.Contains(rec.SmokeTest, "/opt/ligandmpnn/run.py") {
		t.Errorf("smoke_test must run /opt/ligandmpnn/run.py after the GPU probe, got %q", rec.SmokeTest)
	}
	if !strings.Contains(rec.SmokeTest, "/models/") {
		t.Errorf("smoke_test must reference a checkpoint under /models/, got %q", rec.SmokeTest)
	}
	if len(rec.InstallSteps) != 0 {
		t.Errorf("install_steps must be removed for container-mode recipe, got %v", rec.InstallSteps)
	}
	if rec.RunCommand != "" {
		t.Errorf("run_command must be removed for container-mode recipe, got %q", rec.RunCommand)
	}
}

// TestLigandMPNNRecipeDeclaresAllWeightVariants asserts every checkpoint
// listed as an active download in upstream's get_model_params.sh is wired
// into the recipe. Missing one means the post-install hook leaves a
// checkpoint absent and any --model_type targeting it would fail at run
// time. The reference list is the uncommented `wget` block in
// https://github.com/dauparas/LigandMPNN/blob/main/get_model_params.sh
// (15 files across five model families).
func TestLigandMPNNRecipeDeclaresAllWeightVariants(t *testing.T) {
	reg, _ := LoadRegistry(t.TempDir())
	rec, _ := reg.Tool("ligandmpnn")
	if len(rec.Weights) == 0 {
		t.Fatal("ligandmpnn.weights must be populated for the post-install hook to fetch them")
	}
	wantFiles := []string{
		// ProteinMPNN (num_edges=48)
		"proteinmpnn_v_48_002.pt",
		"proteinmpnn_v_48_010.pt",
		"proteinmpnn_v_48_020.pt",
		"proteinmpnn_v_48_030.pt",
		// LigandMPNN (num_edges=32, atom_context_num=25)
		"ligandmpnn_v_32_005_25.pt",
		"ligandmpnn_v_32_010_25.pt",
		"ligandmpnn_v_32_020_25.pt",
		"ligandmpnn_v_32_030_25.pt",
		// SolubleMPNN
		"solublempnn_v_48_002.pt",
		"solublempnn_v_48_010.pt",
		"solublempnn_v_48_020.pt",
		"solublempnn_v_48_030.pt",
		// Membrane MPNN
		"per_residue_label_membrane_mpnn_v_48_020.pt",
		"global_label_membrane_mpnn_v_48_020.pt",
		// LigandMPNN side-chain packing (multi-step denoising)
		"ligandmpnn_sc_v_32_002_16.pt",
	}
	gotPaths := map[string]bool{}
	for _, w := range rec.Weights {
		gotPaths[w.Path] = true
		if !strings.HasPrefix(w.URL, "https://files.ipd.uw.edu/pub/ligandmpnn/") {
			t.Errorf("weight URL %q is not on the Baker lab LigandMPNN CDN", w.URL)
		}
		// URL's last segment must match the declared path so the cached
		// file's name aligns with the upstream filename — the smoke test
		// and the user-facing --checkpoint_* flags reference it literally.
		urlLast := w.URL[strings.LastIndex(w.URL, "/")+1:]
		if urlLast != w.Path {
			t.Errorf("weight URL last segment %q does not match path %q", urlLast, w.Path)
		}
	}
	for _, f := range wantFiles {
		if !gotPaths[f] {
			t.Errorf("missing weight file %q in ligandmpnn.weights", f)
		}
	}
	// Final length check: catches accidental ADDITIONS not declared in the
	// reference list (e.g. if a future agent toggles a commented variant
	// without updating the spec doc).
	if len(rec.Weights) != len(wantFiles) {
		t.Errorf("got %d weights, want %d (the active list in upstream's get_model_params.sh)",
			len(rec.Weights), len(wantFiles))
	}
}

// TestLigandMPNNContainerfileEmbedded confirms the //go:embed directive on
// containerfilesFS picks up the per-tool Containerfile. Without this, the
// Installer would fail at install time with "no such file".
func TestLigandMPNNContainerfileEmbedded(t *testing.T) {
	body, err := loadContainerfile("ligandmpnn.Containerfile")
	if err != nil {
		t.Fatalf("loadContainerfile: %v", err)
	}
	src := string(body)
	if !strings.HasPrefix(src, "# LigandMPNN") {
		t.Errorf("Containerfile body unexpected first line: %q",
			strings.SplitN(src, "\n", 2)[0])
	}
	if !strings.Contains(src, "FROM "+BaseImage) {
		t.Errorf("Containerfile must FROM the BaseImage constant; got: %q", src)
	}
	if !strings.Contains(src, "dauparas/LigandMPNN") {
		t.Error("Containerfile must clone dauparas/LigandMPNN")
	}
	// Minimal pip deps from the task spec — keep torch out (NGC ships it).
	if !strings.Contains(src, "numpy") || !strings.Contains(src, "scipy") {
		t.Error("Containerfile must pip-install numpy and scipy")
	}
	// ProDy is the PDB parser LigandMPNN's run.py imports directly.
	if !strings.Contains(src, "prody") {
		t.Error("Containerfile must pip-install prody (LigandMPNN's PDB parser)")
	}
	// Do NOT reinstall torch — upstream's requirements.txt pins 2.2.1 which
	// would clobber the NGC base's sm_121-capable build on the GB10. Walk
	// each non-comment line and reject any pip-install line whose package
	// list mentions a torch token (matches `torch`, `torch==…`, `torch>=…`,
	// or `pytorch`; ignores `--torch-backend` flag forms).
	for _, raw := range strings.Split(src, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if !strings.Contains(line, "pip install") {
			continue
		}
		// Strip flags (anything starting with `-`) and look at packages.
		// We split on whitespace and check each non-flag token.
		for _, tok := range strings.Fields(line) {
			if strings.HasPrefix(tok, "-") {
				continue
			}
			base := tok
			// Trim version specifiers (==, >=, <, etc.) so `torch==2.2.1`
			// reduces to `torch`.
			for _, sep := range []string{"==", ">=", "<=", "~=", ">", "<"} {
				if i := strings.Index(base, sep); i >= 0 {
					base = base[:i]
				}
			}
			if base == "torch" || base == "pytorch" {
				t.Errorf("Containerfile must NOT pip-install torch (NGC base already ships an sm_121-capable build); offending line: %q", line)
			}
		}
	}
	if !strings.Contains(src, `ENTRYPOINT ["python", "/opt/ligandmpnn/run.py"]`) {
		t.Error("Containerfile ENTRYPOINT must invoke /opt/ligandmpnn/run.py via python")
	}
	// The shared _base/verify_gpu.py fragment must be exercised at build
	// time so `/install ligandmpnn` fails loudly if the NGC base + driver
	// combo can't reach the GPU. The build context contains only the
	// Containerfile itself (no _base/ dir), so a literal `COPY
	// _base/verify_gpu.py` would fail — inlining the same assert in a
	// `RUN python -c "..."` is the convention shared with rfdiffusion.
	if !strings.Contains(src, "torch.cuda.is_available()") {
		t.Error("Containerfile must inline the verify_gpu.py fragment (torch.cuda.is_available()) so the build sanity-checks cuda")
	}
}

// TestInstallerBuildsLigandMPNNImage exercises the full installer container
// path end-to-end via the stubbed runtime exec seam: the recipe loaded from
// tools.toml drives a Pull (if needed) + Build of the ligandmpnn image,
// followed by the post-install EnsureWeights hook. No real podman invoked.
func TestInstallerBuildsLigandMPNNImage(t *testing.T) {
	home := t.TempDir()
	reg, err := LoadRegistry(home)
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	inst := NewInstaller(reg)
	inst.runtime = Runtime{Bin: "/usr/bin/podman", Kind: RuntimePodman}

	// Stub the embedded Containerfile loader: we don't want this test
	// to depend on the on-disk fixture's exact byte sequence.
	oldLoad := loadContainerfile
	defer func() { loadContainerfile = oldLoad }()
	loadContainerfile = func(name string) ([]byte, error) {
		if name != "ligandmpnn.Containerfile" {
			t.Errorf("Installer asked for %q, want ligandmpnn.Containerfile", name)
		}
		return []byte("FROM " + BaseImage + "\n"), nil
	}

	// Capture every runtime call so we can assert on the build invocation.
	var calls [][]string
	oldRun := runCmd
	defer func() { runCmd = oldRun }()
	runCmd = func(cmd *exec.Cmd) error {
		calls = append(calls, append([]string(nil), cmd.Args...))
		return nil
	}
	// Pretend the base NGC image is cached locally so the install path
	// exercises the Build step (not Pull).
	oldOut := runCmdOutput
	defer func() { runCmdOutput = oldOut }()
	runCmdOutput = func(cmd *exec.Cmd) ([]byte, error) {
		image := cmd.Args[len(cmd.Args)-1]
		if image == BaseImage {
			return []byte("[{}]\n"), nil
		}
		return nil, &exec.ExitError{}
	}

	// Capture EnsureWeights — the post-install hook must fire with the 15
	// recipe-declared variants in the exact order they appear in tools.toml.
	var gotTool, gotHome string
	var gotSpecs []WeightSpec
	oldEW := inst.ensureWeights
	defer func() { inst.ensureWeights = oldEW }()
	inst.ensureWeights = func(ctx context.Context, home, toolName string, specs []WeightSpec) (string, error) {
		gotHome = home
		gotTool = toolName
		gotSpecs = specs
		return ModelsRoot(home, toolName), nil
	}

	if err := inst.Install(context.Background(), "ligandmpnn"); err != nil {
		t.Fatalf("Install ligandmpnn: %v", err)
	}

	// Build must have happened.
	var sawBuild bool
	for _, a := range calls {
		if strings.Contains(strings.Join(a, " "), "build -t fova/ligandmpnn:v1.0.0") {
			sawBuild = true
		}
	}
	if !sawBuild {
		t.Errorf("expected the runtime to run `build -t fova/ligandmpnn:v1.0.0 …`, got: %v", calls)
	}

	// EnsureWeights wiring.
	if gotTool != "ligandmpnn" {
		t.Errorf("EnsureWeights tool name = %q, want %q", gotTool, "ligandmpnn")
	}
	if gotHome != home {
		t.Errorf("EnsureWeights home = %q, want %q", gotHome, home)
	}
	if len(gotSpecs) != 15 {
		t.Fatalf("EnsureWeights got %d specs, want 15", len(gotSpecs))
	}
	// Spot-check one URL from each model family to prove the recipe is
	// being passed through verbatim (not transformed or filtered).
	wantOneFrom := []string{
		"proteinmpnn_v_48_002.pt",
		"ligandmpnn_v_32_010_25.pt",
		"solublempnn_v_48_030.pt",
		"global_label_membrane_mpnn_v_48_020.pt",
		"ligandmpnn_sc_v_32_002_16.pt",
	}
	gotByPath := map[string]WeightSpec{}
	for _, s := range gotSpecs {
		gotByPath[s.Path] = s
	}
	for _, p := range wantOneFrom {
		s, ok := gotByPath[p]
		if !ok {
			t.Errorf("EnsureWeights specs missing %q", p)
			continue
		}
		want := "https://files.ipd.uw.edu/pub/ligandmpnn/" + p
		if s.URL != want {
			t.Errorf("spec for %q has URL %q, want %q", p, s.URL, want)
		}
	}
}
