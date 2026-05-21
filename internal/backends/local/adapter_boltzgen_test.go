package local

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// boltzGenStubRuntime swaps in lookPath + runCmd + runCmdOutput stubs so the
// adapter's container-mode path can run without invoking podman/docker.
// The returned ranArgs slice collects every argv the adapter dispatched
// through runCmd. fakeOutDir is the host path the stub seeds with a fake
// BoltzGen output tree when it sees a `run` subcommand.
func boltzGenStubRuntime(t *testing.T, ranArgs *[][]string, fakeOutDir string) func() {
	t.Helper()
	oldLook := lookPath
	oldRun := runCmd
	oldOut := runCmdOutput
	lookPath = func(bin string) (string, error) {
		if bin == "podman" {
			return "/usr/bin/podman", nil
		}
		return "", errors.New("not found")
	}
	runCmd = func(cmd *exec.Cmd) error {
		args := append([]string(nil), cmd.Args...)
		*ranArgs = append(*ranArgs, args)
		// Seed a fake BoltzGen final-set directory. The final dir name is
		// final_<budget>_designs; the budget is taken from --budget when
		// present, else a default so a run without --budget still produces
		// an ingestible tree.
		budget := "2"
		for i, a := range args {
			if a == "--budget" && i+1 < len(args) {
				budget = args[i+1]
			}
		}
		finalDir := filepath.Join(fakeOutDir, "final_ranked_designs",
			"final_"+budget+"_designs")
		if err := os.MkdirAll(finalDir, 0o755); err != nil {
			return err
		}
		for j := 0; j < 3; j++ {
			p := filepath.Join(finalDir, fmt.Sprintf("design_%d.cif", j))
			if err := os.WriteFile(p, []byte("data_design\n"), 0o644); err != nil {
				return err
			}
		}
		// Seed the metrics CSV + the results overview PDF.
		metrics, err := os.ReadFile(filepath.Join("testdata", "boltzgen_metrics.csv"))
		if err != nil {
			return err
		}
		csvPath := filepath.Join(fakeOutDir, "final_ranked_designs",
			"final_designs_metrics_"+budget+".csv")
		if err := os.WriteFile(csvPath, metrics, 0o644); err != nil {
			return err
		}
		pdfPath := filepath.Join(fakeOutDir, "final_ranked_designs", "results_overview.pdf")
		return os.WriteFile(pdfPath, []byte("%PDF-fake\n"), 0o644)
	}
	runCmdOutput = func(cmd *exec.Cmd) ([]byte, error) {
		// image inspect → success means "image exists" in ImageExists.
		return []byte("[{}]\n"), nil
	}
	return func() {
		lookPath = oldLook
		runCmd = oldRun
		runCmdOutput = oldOut
	}
}

// boltzGenWriteSpec writes a BoltzGen spec YAML referencing a single target
// structure file in the same directory, and returns the spec path. The target
// file lives next to the spec so the relative-path staging is exercised.
func boltzGenWriteSpec(t *testing.T, dir string) (specPath, targetName string) {
	t.Helper()
	targetName = "target.cif"
	if err := os.WriteFile(filepath.Join(dir, targetName), []byte("data_target\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	specPath = filepath.Join(dir, "binder.yaml")
	body := "entities:\n" +
		"  - protein:\n" +
		"      id: B\n" +
		"      sequence: 80..140\n" +
		"  - file:\n" +
		"      path: " + targetName + "\n"
	if err := os.WriteFile(specPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return specPath, targetName
}

// boltzGenTestEnv assembles an AdapterEnv with a container-mode boltzgen
// recipe, a registry whose ~/.fova/models/boltzgen cache exists on disk,
// and a fresh empty workdir.
func boltzGenTestEnv(t *testing.T) (env AdapterEnv, fakeOutDir string) {
	t.Helper()
	home := t.TempDir()
	reg, err := LoadRegistry(home)
	if err != nil {
		t.Fatal(err)
	}
	weightsDir := ModelsRoot(home, "boltzgen")
	if err := os.MkdirAll(weightsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	workDir := t.TempDir()
	fakeOutDir = filepath.Join(workDir, "out")
	env = AdapterEnv{
		Recipe: ToolRecipe{
			Name:     "boltzgen",
			ImageTag: "fova/boltzgen:v0.3.2",
			GPU:      true,
		},
		WorkDir:  workDir,
		Registry: reg,
	}
	return env, fakeOutDir
}

func TestBoltzGenAdapterInvokeContainerMode(t *testing.T) {
	env, fakeOutDir := boltzGenTestEnv(t)
	var ran [][]string
	restore := boltzGenStubRuntime(t, &ran, fakeOutDir)
	defer restore()
	var logBuf bytes.Buffer
	env.Log = &logBuf

	specDir := t.TempDir()
	specPath, targetName := boltzGenWriteSpec(t, specDir)

	req, _ := json.Marshal(map[string]any{
		"spec_path":   specPath,
		"protocol":    "peptide-anything",
		"num_designs": 4,
		"budget":      2,
	})
	out, err := boltzGenAdapter{}.Invoke(context.Background(), env, req)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	var envOut designsEnvelope
	if err := json.Unmarshal(out, &envOut); err != nil {
		t.Fatalf("output is not valid designs JSON: %v", err)
	}
	if len(envOut.Designs) != 3 {
		t.Fatalf("want 3 designs (from the seeded final-set CIFs), got %d", len(envOut.Designs))
	}
	if envOut.Designs[0].StructureFile == "" {
		t.Error("structure_file must be set on each design")
	}
	if !strings.HasPrefix(envOut.Designs[0].StructureFile, env.Registry.Home()) {
		t.Errorf("structure_file %q must live under FOVA_HOME %q so it outlives the temp WorkDir",
			envOut.Designs[0].StructureFile, env.Registry.Home())
	}

	if len(ran) != 1 {
		t.Fatalf("want 1 runCmd invocation, got %d: %v", len(ran), ran)
	}
	argv := strings.Join(ran[0], " ")
	if !strings.Contains(argv, "fova/boltzgen:v0.3.2") {
		t.Errorf("argv must reference the boltzgen image tag, got: %s", argv)
	}
	if !strings.Contains(argv, "--num_designs 4") {
		t.Errorf("argv must carry --num_designs 4, got: %s", argv)
	}
	if !strings.Contains(argv, "--budget 2") {
		t.Errorf("argv must carry --budget 2, got: %s", argv)
	}
	if !strings.Contains(argv, "--protocol peptide-anything") {
		t.Errorf("argv must carry the requested protocol, got: %s", argv)
	}
	if !strings.Contains(argv, env.WorkDir+":/work") {
		t.Errorf("argv must bind-mount env.WorkDir at /work, got: %s", argv)
	}
	if !strings.Contains(argv, ModelsRoot(env.Registry.Home(), "boltzgen")+":/models") {
		t.Errorf("argv must bind-mount the boltzgen weights cache at /models, got: %s", argv)
	}
	if !strings.Contains(argv, "run /work/in.yaml") {
		t.Errorf("argv must call `boltzgen run /work/in.yaml`, got: %s", argv)
	}

	// The spec must have been staged verbatim to in.yaml, and the referenced
	// target file copied next to it.
	staged, err := os.ReadFile(filepath.Join(env.WorkDir, "in.yaml"))
	if err != nil {
		t.Fatalf("in.yaml: %v", err)
	}
	if !strings.Contains(string(staged), "path: "+targetName) {
		t.Errorf("staged spec must keep the original file reference, got:\n%s", staged)
	}
	if _, err := os.Stat(filepath.Join(env.WorkDir, targetName)); err != nil {
		t.Errorf("the referenced structure file must be staged into WorkDir: %v", err)
	}

	// Scores must have been ingested from the metrics CSV.
	withScores := 0
	for _, d := range envOut.Designs {
		if len(d.Scores) > 0 {
			withScores++
		}
	}
	if withScores != 3 {
		t.Errorf("all 3 designs should carry scores from the metrics CSV, got %d", withScores)
	}
	_ = logBuf
}

func TestBoltzGenAdapterInvokeMissingSpecPath(t *testing.T) {
	env, _ := boltzGenTestEnv(t)
	_, err := boltzGenAdapter{}.Invoke(context.Background(), env, []byte(`{"num_designs":1}`))
	if err == nil {
		t.Fatal("expected an error when spec_path is missing")
	}
	if !strings.Contains(err.Error(), "spec_path") {
		t.Errorf("error %q should mention spec_path", err)
	}
}

func TestBoltzGenAdapterInvokeSpecNotFound(t *testing.T) {
	env, _ := boltzGenTestEnv(t)
	_, err := boltzGenAdapter{}.Invoke(context.Background(), env,
		[]byte(`{"spec_path":"/no/such/spec.yaml"}`))
	if err == nil {
		t.Fatal("expected an error when the spec file does not exist")
	}
	if !strings.Contains(err.Error(), "boltzgen-spec") {
		t.Errorf("error %q should point at the boltzgen-spec skill", err)
	}
}

func TestBoltzGenAdapterInvokeNotInstalled(t *testing.T) {
	// No ImageTag => container-only adapter must complain that boltzgen is
	// not installed.
	specDir := t.TempDir()
	specPath, _ := boltzGenWriteSpec(t, specDir)
	reg, err := LoadRegistry(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	env := AdapterEnv{
		Recipe:   ToolRecipe{Name: "boltzgen"}, // no ImageTag
		WorkDir:  t.TempDir(),
		Registry: reg,
	}
	_, err = boltzGenAdapter{}.Invoke(context.Background(), env,
		[]byte(`{"spec_path":"`+specPath+`"}`))
	if err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Fatalf("want a 'not installed' error, got: %v", err)
	}
}

func TestBoltzGenAdapterInvokeCreatesWeightsCache(t *testing.T) {
	// BoltzGen downloads its weights at container runtime, so a missing
	// weights cache must be created on demand, not rejected.
	home := t.TempDir()
	reg, err := LoadRegistry(home)
	if err != nil {
		t.Fatal(err)
	}
	specDir := t.TempDir()
	specPath, _ := boltzGenWriteSpec(t, specDir)
	env := AdapterEnv{
		Recipe: ToolRecipe{
			Name:     "boltzgen",
			ImageTag: "fova/boltzgen:v0.3.2",
			GPU:      true,
		},
		WorkDir:  t.TempDir(),
		Registry: reg,
	}
	var ran [][]string
	restore := boltzGenStubRuntime(t, &ran, filepath.Join(env.WorkDir, "out"))
	defer restore()
	_, err = boltzGenAdapter{}.Invoke(context.Background(), env,
		[]byte(`{"spec_path":"`+specPath+`","budget":2}`))
	if err != nil && strings.Contains(err.Error(), "weights cache") {
		t.Fatalf("a missing weights cache must not error, got: %v", err)
	}
	cache := ModelsRoot(reg.Home(), "boltzgen")
	if info, statErr := os.Stat(cache); statErr != nil || !info.IsDir() {
		t.Fatalf("Invoke must create the weights cache %s; stat err = %v", cache, statErr)
	}
}

func TestBoltzGenRecipeIsRegistered(t *testing.T) {
	reg, err := LoadRegistry(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	rec, ok := reg.Tool("boltzgen")
	if !ok {
		t.Fatal("boltzgen missing from registry")
	}
	if rec.ImageTag == "" {
		t.Error("boltzgen must be a container-mode recipe (ImageTag is empty)")
	}
}

func TestRunDesignBoltzGenIsRegistered(t *testing.T) {
	reg, err := LoadRegistry(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// A missing spec makes Invoke fail fast — which still proves
	// design.boltzgen is registered and dispatched through RunDesign.
	_, err = RunDesign(context.Background(), reg, "design.boltzgen",
		[]byte(`{"spec_path":"/no/such/spec.yaml"}`), io.Discard, nil)
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), "no local adapter") {
		t.Fatalf("design.boltzgen must be registered, got: %v", err)
	}
}

func TestBoltzGenArgs(t *testing.T) {
	f64 := func(v float64) *float64 { return &v }
	b := func(v bool) *bool { return &v }

	cases := []struct {
		name string
		req  boltzGenRequest
		want []string // flags that must be present
		omit []string // flags that must NOT be present
	}{
		{
			name: "empty request defaults the protocol and omits unset flags",
			req:  boltzGenRequest{},
			want: []string{"--protocol", boltzGenDefaultProtocol},
			omit: []string{"--num_designs", "--budget", "--alpha", "--filter_biased",
				"--diffusion_batch_size", "--steps", "--reuse", "--step_scale",
				"--noise_scale", "--refolding_rmsd_threshold", "--additional_filters",
				"--inverse_fold_num_sequences", "--inverse_fold_avoid"},
		},
		{
			name: "ints emitted only when positive",
			req: boltzGenRequest{
				NumDesigns: 10000, Budget: 60, DiffusionBatchSize: 10,
				InverseFoldNumSequences: 4,
			},
			want: []string{"--num_designs", "10000", "--budget", "60",
				"--diffusion_batch_size", "10", "--inverse_fold_num_sequences", "4"},
		},
		{
			name: "pointer floats emitted only when set",
			req: boltzGenRequest{
				Alpha: f64(0.2), RefoldingRMSDThreshold: f64(2), StepScale: f64(1.8),
				NoiseScale: f64(0.98),
			},
			want: []string{"--alpha", "0.2", "--refolding_rmsd_threshold", "2",
				"--step_scale", "1.8", "--noise_scale", "0.98"},
		},
		{
			name: "filter_biased false is still emitted (pointer distinguishes unset)",
			req:  boltzGenRequest{FilterBiased: b(false)},
			want: []string{"--filter_biased", "false"},
		},
		{
			name: "reuse emits a bare flag",
			req:  boltzGenRequest{Reuse: true},
			want: []string{"--reuse"},
		},
		{
			name: "steps and additional_filters expand to space-separated lists",
			req: boltzGenRequest{
				Steps:             []string{"design", "filtering"},
				AdditionalFilters: []string{"design_ALA>0.3", "design_GLY<0.2"},
			},
			want: []string{"--steps", "design", "filtering",
				"--additional_filters", "design_ALA>0.3", "design_GLY<0.2"},
		},
		{
			name: "inverse_fold_avoid emitted when non-empty",
			req:  boltzGenRequest{InverseFoldAvoid: "KEC"},
			want: []string{"--inverse_fold_avoid", "KEC"},
		},
		{
			name: "explicit protocol overrides the default",
			req:  boltzGenRequest{Protocol: "nanobody-anything"},
			want: []string{"--protocol", "nanobody-anything"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			args := boltzGenArgs(c.req)
			if !containsSubseq(args, c.want) {
				t.Errorf("boltzGenArgs(%+v) = %v, want it to contain %v", c.req, args, c.want)
			}
			for _, o := range c.omit {
				for _, a := range args {
					if a == o {
						t.Errorf("boltzGenArgs(%+v) = %v, must omit %q", c.req, args, o)
					}
				}
			}
		})
	}
}

// containsSubseq reports whether want appears as a contiguous run inside args.
func containsSubseq(args, want []string) bool {
	if len(want) == 0 {
		return true
	}
	for i := 0; i+len(want) <= len(args); i++ {
		if reflect.DeepEqual(args[i:i+len(want)], want) {
			return true
		}
	}
	return false
}

func TestStageBoltzGenSpec(t *testing.T) {
	specDir := t.TempDir()
	workDir := t.TempDir()
	// A spec referencing a structure file in a nested subdirectory.
	if err := os.MkdirAll(filepath.Join(specDir, "structures"), 0o755); err != nil {
		t.Fatal(err)
	}
	cif := filepath.Join(specDir, "structures", "antigen.cif")
	if err := os.WriteFile(cif, []byte("data_antigen\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	specPath := filepath.Join(specDir, "spec.yaml")
	body := "entities:\n" +
		"  - protein:\n" +
		"      id: B\n" +
		"      sequence: 60..90\n" +
		"  - file:\n" +
		"      path: structures/antigen.cif\n"
	if err := os.WriteFile(specPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := stageBoltzGenSpec(specPath, workDir); err != nil {
		t.Fatalf("stageBoltzGenSpec: %v", err)
	}
	// The spec lands at in.yaml.
	if _, err := os.Stat(filepath.Join(workDir, "in.yaml")); err != nil {
		t.Errorf("spec must be staged to in.yaml: %v", err)
	}
	// The referenced file keeps its layout relative to the spec.
	staged := filepath.Join(workDir, "structures", "antigen.cif")
	if _, err := os.Stat(staged); err != nil {
		t.Errorf("referenced structure file must be staged at structures/antigen.cif: %v", err)
	}
}

func TestStageBoltzGenSpecMissingReference(t *testing.T) {
	specDir := t.TempDir()
	workDir := t.TempDir()
	specPath := filepath.Join(specDir, "spec.yaml")
	body := "entities:\n  - file:\n      path: ghost.cif\n"
	if err := os.WriteFile(specPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := stageBoltzGenSpec(specPath, workDir); err == nil {
		t.Fatal("stageBoltzGenSpec must fail when a referenced file is missing")
	}
}

func TestParseBoltzGenMetrics(t *testing.T) {
	m, err := parseBoltzGenMetrics(filepath.Join("testdata", "boltzgen_metrics.csv"))
	if err != nil {
		t.Fatalf("parseBoltzGenMetrics: %v", err)
	}
	if len(m) != 3 {
		t.Fatalf("want 3 designs, got %d: %v", len(m), m)
	}
	// Keyed by the CIF stem (file_name column, extension stripped).
	d0, ok := m["design_0"]
	if !ok {
		t.Fatalf("want a design keyed by the CIF stem design_0, got keys: %v", keys(m))
	}
	if got := d0["design_to_target_iptm"]; got != 0.87 {
		t.Errorf("design_0 design_to_target_iptm = %v, want 0.87", got)
	}
	// Unknown columns are carried through as raw score keys, not dropped.
	if got, ok := d0["some_future_metric"]; !ok || got != 0.42 {
		t.Errorf("unknown column some_future_metric must survive as a raw score: got %v ok=%v", got, ok)
	}
	// final_rank is numeric and carried through too.
	if got := d0["final_rank"]; got != 1 {
		t.Errorf("design_0 final_rank = %v, want 1", got)
	}
	// The name column itself must not leak into the scores.
	if _, ok := d0["file_name"]; ok {
		t.Error("the file_name key column must not appear as a score")
	}
}

func TestParseBoltzGenMetricsMissingFile(t *testing.T) {
	m, err := parseBoltzGenMetrics(filepath.Join(t.TempDir(), "nope.csv"))
	if err != nil {
		t.Fatalf("a missing metrics file must not error, got: %v", err)
	}
	if len(m) != 0 {
		t.Errorf("want an empty map for a missing file, got %v", m)
	}
}

func keys(m map[string]map[string]float64) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
