package local

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseProteinMPNNOutput(t *testing.T) {
	seqsDir := filepath.Join(t.TempDir(), "seqs")
	if err := os.MkdirAll(seqsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	fixture, err := os.ReadFile("testdata/proteinmpnn_sample.fa")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(seqsDir, "5L33.fa"), fixture, 0o644); err != nil {
		t.Fatal(err)
	}

	designs, err := parseProteinMPNNOutput(seqsDir)
	if err != nil {
		t.Fatalf("parseProteinMPNNOutput: %v", err)
	}
	if len(designs) != 2 {
		t.Fatalf("want 2 designs (record 0 is native), got %d", len(designs))
	}
	if got := designs[0].Sequence["A"]; got == "" {
		t.Error("design 0 chain A sequence is empty")
	}
	if got := designs[0].Scores["score"]; got != 0.8227 {
		t.Errorf("design 0 score = %v, want 0.8227", got)
	}
	if got := designs[0].Scores["seq_recovery"]; got != 0.5094 {
		t.Errorf("design 0 seq_recovery = %v, want 0.5094", got)
	}
	if got := designs[1].Scores["global_score"]; got != 0.8361 {
		t.Errorf("design 1 global_score = %v, want 0.8361", got)
	}
}

func TestParseProteinMPNNOutputEmptyDirErrors(t *testing.T) {
	if _, err := parseProteinMPNNOutput(t.TempDir()); err == nil {
		t.Fatal("expected an error when no designs are present")
	}
}

func TestProteinMPNNAdapterInvoke(t *testing.T) {
	workDir := t.TempDir()
	target := filepath.Join(t.TempDir(), "backbone.pdb")
	if err := os.WriteFile(target, []byte("ATOM      1  N   MET A   1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fixture, err := os.ReadFile("testdata/proteinmpnn_sample.fa")
	if err != nil {
		t.Fatal(err)
	}

	var ran []string
	var logBuf bytes.Buffer
	var progress []float64
	stub := func(ctx context.Context, dir, cmd string, log io.Writer) (string, error) {
		ran = append(ran, cmd)
		// Mimic bashRunner: also write a line to log so we can assert log is wired.
		if log != nil {
			_, _ = log.Write([]byte("stub: " + cmd + "\n"))
		}
		if strings.Contains(cmd, "protein_mpnn_run.py") {
			seqs := filepath.Join(workDir, "seqs")
			if err := os.MkdirAll(seqs, 0o755); err != nil {
				return "", err
			}
			if err := os.WriteFile(filepath.Join(seqs, "backbone.fa"), fixture, 0o644); err != nil {
				return "", err
			}
		}
		return "ok", nil
	}
	env := AdapterEnv{
		Recipe:   ToolRecipe{Name: "proteinmpnn", InstallDir: t.TempDir(), VenvDir: t.TempDir()},
		Run:      stub,
		WorkDir:  workDir,
		Log:      &logBuf,
		Progress: func(f float64) { progress = append(progress, f) },
	}

	out, err := proteinMPNNAdapter{}.Invoke(context.Background(), env,
		[]byte(`{"target":"`+target+`","num_designs":2}`))
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	var env2 designsEnvelope
	if err := json.Unmarshal(out, &env2); err != nil {
		t.Fatalf("output is not valid designs JSON: %v", err)
	}
	if len(env2.Designs) != 2 {
		t.Fatalf("want 2 designs, got %d", len(env2.Designs))
	}
	if len(ran) != 2 {
		t.Fatalf("want 2 commands (parse + inference), got %d: %v", len(ran), ran)
	}
	if !strings.Contains(ran[0], "parse_multiple_chains.py") {
		t.Errorf("command 1 should be the parse step: %s", ran[0])
	}
	if !strings.Contains(ran[1], "--num_seq_per_target 2") {
		t.Errorf("command 2 should request 2 sequences: %s", ran[1])
	}
	// Bug 2: the adapter must stream stdout+stderr to env.Log and tick env.Progress.
	if logBuf.Len() == 0 {
		t.Error("env.Log should receive the stubbed command output")
	}
	if !strings.Contains(logBuf.String(), "parse_multiple_chains.py") {
		t.Errorf("env.Log should carry the parse step's output, got: %q", logBuf.String())
	}
	if len(progress) < 2 {
		t.Errorf("env.Progress should have been called at least twice, got %v", progress)
	}
}

func TestProteinMPNNAdapterInvokeMissingTarget(t *testing.T) {
	env := AdapterEnv{Recipe: ToolRecipe{VenvDir: t.TempDir()}, WorkDir: t.TempDir()}
	if _, err := (proteinMPNNAdapter{}).Invoke(context.Background(), env, []byte(`{"num_designs":1}`)); err == nil {
		t.Fatal("expected an error when target is missing")
	}
}

// Bug 4 — a missing-target error must steer the agent at fs.read_structure.
func TestProteinMPNNAdapterInvokeNotFoundIncludesHint(t *testing.T) {
	env := AdapterEnv{Recipe: ToolRecipe{VenvDir: t.TempDir()}, WorkDir: t.TempDir()}
	_, err := proteinMPNNAdapter{}.Invoke(context.Background(), env,
		[]byte(`{"target":"/no/such/file.pdb"}`))
	if err == nil {
		t.Fatal("expected a 'not found' error")
	}
	if !strings.Contains(err.Error(), "fs.read_structure") {
		t.Errorf("error %q should point at fs.read_structure", err)
	}
}

func TestProteinMPNNAdapterInvokeNotInstalled(t *testing.T) {
	target := filepath.Join(t.TempDir(), "b.pdb")
	if err := os.WriteFile(target, []byte("ATOM\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	env := AdapterEnv{
		Recipe:  ToolRecipe{VenvDir: filepath.Join(t.TempDir(), "does-not-exist")},
		WorkDir: t.TempDir(),
	}
	if _, err := (proteinMPNNAdapter{}).Invoke(context.Background(), env, []byte(`{"target":"`+target+`"}`)); err == nil {
		t.Fatal("expected a 'not installed' error")
	}
}

func TestRunDesignProteinMPNNIsRegistered(t *testing.T) {
	reg, err := LoadRegistry(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// A nonexistent target makes Invoke fail fast before any command runs —
	// which still proves design.proteinmpnn is registered and dispatched.
	_, err = RunDesign(context.Background(), reg, "design.proteinmpnn", []byte(`{"target":"/no/such/file.pdb"}`), io.Discard, nil)
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), "no local adapter") {
		t.Fatalf("design.proteinmpnn must be registered, got: %v", err)
	}
}

func TestProteinMPNNAdapterInvokeInstallDirMissing(t *testing.T) {
	target := filepath.Join(t.TempDir(), "b.pdb")
	if err := os.WriteFile(target, []byte("ATOM\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// VenvDir exists but InstallDir does not — still "not installed".
	env := AdapterEnv{
		Recipe:  ToolRecipe{VenvDir: t.TempDir(), InstallDir: filepath.Join(t.TempDir(), "gone")},
		WorkDir: t.TempDir(),
	}
	_, err := proteinMPNNAdapter{}.Invoke(context.Background(), env, []byte(`{"target":"`+target+`"}`))
	if err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Fatalf("want a 'not installed' error, got: %v", err)
	}
}

func TestSplitChains(t *testing.T) {
	single := splitChains("ACDEFG")
	if len(single) != 1 || single["A"] != "ACDEFG" {
		t.Errorf("single-chain split = %v", single)
	}
	multi := splitChains("ACDE/FGHI")
	if len(multi) != 2 || multi["A"] != "ACDE" || multi["B"] != "FGHI" {
		t.Errorf("multi-chain split = %v", multi)
	}
}

func TestProteinMPNNScoresPartialHeader(t *testing.T) {
	// A header missing global_score and seq_recovery yields only score.
	got := proteinMPNNScores("T=0.1, sample=1, score=0.42")
	if got["score"] != 0.42 {
		t.Errorf("score = %v, want 0.42", got["score"])
	}
	if _, ok := got["seq_recovery"]; ok {
		t.Error("seq_recovery must be absent when not in the header")
	}
	// Bracketed fields from a native-record header must not break parsing.
	native := proteinMPNNScores("5L33, score=1.59, designed_chains=['A'], fixed_chains=[]")
	if native["score"] != 1.59 {
		t.Errorf("native score = %v, want 1.59", native["score"])
	}
}

func TestParseProteinMPNNOutputNativeOnly(t *testing.T) {
	// A .fa with only the native record (no designed sequences) yields no designs.
	seqsDir := filepath.Join(t.TempDir(), "seqs")
	if err := os.MkdirAll(seqsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(seqsDir, "x.fa"),
		[]byte(">5L33, score=1.59\nHMPEEEKAARLF\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := parseProteinMPNNOutput(seqsDir); err == nil {
		t.Fatal("expected an error: a native-only file has no designs")
	}
}

func TestParseProteinMPNNOutputSkipsEmptySequence(t *testing.T) {
	seqsDir := filepath.Join(t.TempDir(), "seqs")
	if err := os.MkdirAll(seqsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Record 0 native; record 1 a header with no sequence; record 2 a real design.
	body := ">native, score=1.0\nHMPEEE\n" +
		">T=0.1, sample=1, score=0.8, seq_recovery=0.5\n" +
		">T=0.1, sample=2, score=0.7, seq_recovery=0.6\nMINEEE\n"
	if err := os.WriteFile(filepath.Join(seqsDir, "x.fa"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	designs, err := parseProteinMPNNOutput(seqsDir)
	if err != nil {
		t.Fatalf("parseProteinMPNNOutput: %v", err)
	}
	if len(designs) != 1 {
		t.Fatalf("want 1 design (the empty-sequence record skipped), got %d", len(designs))
	}
	if designs[0].Sequence["A"] != "MINEEE" {
		t.Errorf("design sequence = %q, want MINEEE", designs[0].Sequence["A"])
	}
}
