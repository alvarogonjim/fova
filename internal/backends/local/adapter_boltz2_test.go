package local

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// boltz2TestEnv builds an AdapterEnv with a container-mode recipe and a
// registry whose ~/.fova/models/boltz2 directory exists on disk.
func boltz2TestEnv(t *testing.T, logBuf *bytes.Buffer, progress *[]float64) AdapterEnv {
	t.Helper()
	home := t.TempDir()
	reg, err := LoadRegistry(home)
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	if err := os.MkdirAll(ModelsRoot(home, "boltz2"), 0o755); err != nil {
		t.Fatal(err)
	}
	rec, ok := reg.Tool("boltz2")
	if !ok {
		t.Fatal("boltz2 missing from registry")
	}
	env := AdapterEnv{
		Recipe:   rec,
		WorkDir:  t.TempDir(),
		Registry: reg,
	}
	if logBuf != nil {
		env.Log = logBuf
	}
	if progress != nil {
		env.Progress = func(f float64) { *progress = append(*progress, f) }
	}
	return env
}

// stubContainerRuntime makes Detect() see /usr/bin/podman, ImageExists return
// true, and RunContainer drop drop into the runCmd seam where `onRun` may
// stage outputs and inspect argv. Returns a slice that captures every argv
// runCmd was asked to run.
func stubContainerRuntime(t *testing.T, onRun func(args []string) error) *[][]string {
	t.Helper()
	calls := &[][]string{}
	oldLook := lookPath
	t.Cleanup(func() { lookPath = oldLook })
	lookPath = func(bin string) (string, error) {
		if bin == "podman" {
			return "/usr/bin/podman", nil
		}
		return "", exec.ErrNotFound
	}
	oldRun := runCmd
	t.Cleanup(func() { runCmd = oldRun })
	runCmd = func(cmd *exec.Cmd) error {
		args := append([]string(nil), cmd.Args...)
		*calls = append(*calls, args)
		if onRun != nil {
			return onRun(args)
		}
		return nil
	}
	oldOut := runCmdOutput
	t.Cleanup(func() { runCmdOutput = oldOut })
	runCmdOutput = func(cmd *exec.Cmd) ([]byte, error) {
		// ImageExists: pretend the tool image is cached locally.
		return []byte("[{}]\n"), nil
	}
	return calls
}

func TestBuildBoltz2YAML(t *testing.T) {
	req := boltz2Request{Entities: []boltz2Entity{
		{Type: "protein", ID: chainIDs{"A"}, Sequence: "MKQ", MSA: "empty"},
		{Type: "protein", ID: chainIDs{"B", "C"}, Sequence: "AAA"},
		{Type: "ligand", ID: chainIDs{"L"}, SMILES: "CCO"},
		{Type: "rna", ID: chainIDs{"R"}, Sequence: "ACGU", Cyclic: true},
	}}
	got := buildBoltz2YAML(req)
	want := "version: 1\n" +
		"sequences:\n" +
		"  - protein:\n      id: A\n      sequence: MKQ\n      msa: empty\n" +
		// An entity with no MSA field defaults to single-sequence (msa: empty) —
		// Boltz-2 requires an MSA unless --use_msa_server is set.
		"  - protein:\n      id: [B, C]\n      sequence: AAA\n      msa: empty\n" +
		"  - ligand:\n      id: L\n      smiles: CCO\n" +
		"  - rna:\n      id: R\n      sequence: ACGU\n      msa: empty\n      cyclic: true\n"
	if got != want {
		t.Errorf("yaml mismatch\n got:\n%s\nwant:\n%s", got, want)
	}
}

func TestBuildBoltz2YAMLServerMSAOmitsLine(t *testing.T) {
	req := boltz2Request{Entities: []boltz2Entity{
		{Type: "protein", ID: chainIDs{"A"}, Sequence: "MKQ", MSA: "server"}}}
	if strings.Contains(buildBoltz2YAML(req), "msa:") {
		t.Error("msa: server must omit the msa line so --use_msa_server fills it")
	}
}

func TestBoltz2Args(t *testing.T) {
	rs, ss := 5, 100
	got := strings.Join(boltz2Args(boltz2Request{
		RecyclingSteps: &rs, SamplingSteps: &ss}), " ")
	for _, want := range []string{"--recycling_steps 5", "--sampling_steps 100"} {
		if !strings.Contains(got, want) {
			t.Errorf("args missing %q in %q", want, got)
		}
	}
	// Unset pointers omit the flag entirely.
	if strings.Contains(strings.Join(boltz2Args(boltz2Request{}), " "), "--diffusion_samples") {
		t.Error("an unset diffusion_samples must omit the flag")
	}
}

func TestParseBoltz2OutputWithScores(t *testing.T) {
	outDir := t.TempDir()
	sub := filepath.Join(outDir, "predictions", "in")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "in_model_0.pdb"),
		[]byte("ATOM\nEND\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	conf := `{"confidence_score":0.84,"ptm":0.81,"iptm":0.79,` +
		`"complex_plddt":0.88,"chains_ptm":{"0":0.9,"1":0.7}}`
	if err := os.WriteFile(filepath.Join(sub, "confidence_in_model_0.json"),
		[]byte(conf), 0o644); err != nil {
		t.Fatal(err)
	}
	designs, err := parseBoltz2Output(outDir)
	if err != nil {
		t.Fatalf("parseBoltz2Output: %v", err)
	}
	if len(designs) != 1 {
		t.Fatalf("want 1 design, got %d", len(designs))
	}
	s := designs[0].Scores
	if s["plddt"] != 0.88 || s["ptm"] != 0.81 || s["iptm"] != 0.79 {
		t.Errorf("standard scores wrong: %v", s)
	}
	if s["chain_0_ptm"] != 0.9 || s["chain_1_ptm"] != 0.7 {
		t.Errorf("chains_ptm not flattened: %v", s)
	}
}

func TestParseBoltz2OutputNoConfidenceFile(t *testing.T) {
	outDir := t.TempDir()
	sub := filepath.Join(outDir, "predictions", "in")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "in_model_0.pdb"),
		[]byte("ATOM\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	designs, err := parseBoltz2Output(outDir)
	if err != nil {
		t.Fatalf("a prediction without a confidence file must not error: %v", err)
	}
	if len(designs) != 1 || len(designs[0].Scores) != 0 {
		t.Errorf("want 1 design with empty scores, got %+v", designs)
	}
}

func TestParseBoltz2OutputEmptyErrors(t *testing.T) {
	if _, err := parseBoltz2Output(t.TempDir()); err == nil {
		t.Fatal("expected an error when no structures are present")
	}
}

func TestBoltz2AdapterInvoke(t *testing.T) {
	var progress []float64
	env := boltz2TestEnv(t, nil, &progress)
	stubContainerRuntime(t, func(args []string) error {
		if len(args) < 2 || args[1] != "run" {
			return nil
		}
		sub := filepath.Join(env.WorkDir, "out", "predictions", "in")
		if err := os.MkdirAll(sub, 0o755); err != nil {
			return err
		}
		_ = os.WriteFile(filepath.Join(sub, "confidence_in_model_0.json"),
			[]byte(`{"complex_plddt":0.9,"ptm":0.8,"iptm":0.7}`), 0o644)
		return os.WriteFile(filepath.Join(sub, "in_model_0.pdb"),
			[]byte("ATOM\nEND\n"), 0o644)
	})
	body := []byte(`{"entities":[{"type":"protein","id":"A","sequence":"MKQ","msa":"empty"}],` +
		`"output_format":"pdb"}`)
	out, err := boltz2Adapter{}.Invoke(context.Background(), env, body)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	var resp designsEnvelope
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("not a designs envelope: %v", err)
	}
	if len(resp.Designs) != 1 || resp.Designs[0].Scores["plddt"] != 0.9 {
		t.Fatalf("want 1 scored design, got %+v", resp.Designs)
	}
	yaml, _ := os.ReadFile(filepath.Join(env.WorkDir, "in.yaml"))
	if !strings.Contains(string(yaml), "version: 1") ||
		!strings.Contains(string(yaml), "sequence: MKQ") {
		t.Errorf("YAML wrong:\n%s", yaml)
	}
}

func TestBoltz2AdapterInvokeMissingWeightsCacheErrors(t *testing.T) {
	home := t.TempDir()
	reg, err := LoadRegistry(home)
	if err != nil {
		t.Fatal(err)
	}
	rec, _ := reg.Tool("boltz2")
	env := AdapterEnv{Recipe: rec, WorkDir: t.TempDir(), Registry: reg}
	stubContainerRuntime(t, nil)
	// Boltz-2 weights are fetched at /install time; a missing cache means
	// install did not complete — the adapter must say so, not silently run.
	_, err = boltz2Adapter{}.Invoke(context.Background(), env,
		[]byte(`{"entities":[{"type":"protein","id":"A","sequence":"MKQ"}]}`))
	if err == nil || !strings.Contains(err.Error(), "install boltz2") {
		t.Fatalf("want a 'run /install boltz2' error, got: %v", err)
	}
}

func TestBoltz2AdapterInvokeRejectsBadRequest(t *testing.T) {
	env := boltz2TestEnv(t, nil, nil)
	if _, err := (boltz2Adapter{}).Invoke(context.Background(), env, []byte(`{"entities":[]}`)); err == nil {
		t.Fatal("expected an error for empty entities")
	}
}

func TestRunDesignBoltz2IsRegistered(t *testing.T) {
	reg, err := LoadRegistry(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// Missing sequences makes Invoke fail fast — which still proves
	// fold.boltz2 is registered and dispatched.
	_, err = RunDesign(context.Background(), reg, "fold.boltz2", []byte(`{}`), io.Discard, nil)
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), "no local adapter") {
		t.Fatalf("fold.boltz2 must be registered, got: %v", err)
	}
}
