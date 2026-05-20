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

func TestParseBindCraftOutput(t *testing.T) {
	designPath := t.TempDir()
	accepted := filepath.Join(designPath, "Accepted")
	if err := os.MkdirAll(accepted, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"design_1.pdb", "design_2.pdb"} {
		if err := os.WriteFile(filepath.Join(accepted, n), []byte("ATOM\nEND\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	statsCSV := "Design,Sequence,Average_pLDDT,Average_i_pTM\n" +
		"design_1,MKLV,0.91,0.78\n" +
		"design_2,GSHM,0.88,0.72\n"
	if err := os.WriteFile(filepath.Join(designPath, "final_design_stats.csv"), []byte(statsCSV), 0o644); err != nil {
		t.Fatal(err)
	}

	designs, err := parseBindCraftOutput(designPath)
	if err != nil {
		t.Fatalf("parseBindCraftOutput: %v", err)
	}
	if len(designs) != 2 {
		t.Fatalf("want 2 designs, got %d", len(designs))
	}
	if designs[0].StructureFile == "" {
		t.Error("structure_file must be set")
	}
	if designs[0].Sequence["A"] != "MKLV" {
		t.Errorf("design_1 sequence = %q, want MKLV", designs[0].Sequence["A"])
	}
	if designs[0].Scores["Average_pLDDT"] != 0.91 {
		t.Errorf("design_1 Average_pLDDT = %v, want 0.91", designs[0].Scores["Average_pLDDT"])
	}
	if designs[1].Scores["Average_i_pTM"] != 0.72 {
		t.Errorf("design_2 Average_i_pTM = %v, want 0.72", designs[1].Scores["Average_i_pTM"])
	}
}

func TestParseBindCraftOutputNoCSV(t *testing.T) {
	designPath := t.TempDir()
	accepted := filepath.Join(designPath, "Accepted")
	if err := os.MkdirAll(accepted, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(accepted, "d.pdb"), []byte("ATOM\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	designs, err := parseBindCraftOutput(designPath)
	if err != nil {
		t.Fatalf("parseBindCraftOutput: %v", err)
	}
	if len(designs) != 1 || designs[0].StructureFile == "" {
		t.Fatalf("want 1 design with a structure file, got %+v", designs)
	}
}

func TestParseBindCraftOutputEmptyErrors(t *testing.T) {
	if _, err := parseBindCraftOutput(t.TempDir()); err == nil {
		t.Fatal("expected an error when no accepted designs are present")
	}
}

// bindCraftStubRunner records commands and, on the bindcraft.py call, reads the
// settings file named after --settings, then writes a fixture results dir
// (Accepted/*.pdb + final_design_stats.csv) into that settings' design_path.
func bindCraftStubRunner(ran *[]string) CmdRunner {
	return func(ctx context.Context, dir, cmd string, log io.Writer) (string, error) {
		*ran = append(*ran, cmd)
		if log != nil {
			_, _ = log.Write([]byte("stub: " + cmd + "\n"))
		}
		_, after, ok := strings.Cut(cmd, "--settings ")
		if !ok {
			return "", nil
		}
		settingsFile, _, _ := strings.Cut(after, " ")
		body, err := os.ReadFile(settingsFile)
		if err != nil {
			return "", err
		}
		var s map[string]any
		if err := json.Unmarshal(body, &s); err != nil {
			return "", err
		}
		designPath, _ := s["design_path"].(string)
		accepted := filepath.Join(designPath, "Accepted")
		if err := os.MkdirAll(accepted, 0o755); err != nil {
			return "", err
		}
		for _, n := range []string{"design_1.pdb", "design_2.pdb"} {
			if err := os.WriteFile(filepath.Join(accepted, n), []byte("ATOM\nEND\n"), 0o644); err != nil {
				return "", err
			}
		}
		statsCSV := "Design,Sequence,Average_pLDDT\ndesign_1,MKLV,0.91\ndesign_2,GSHM,0.88\n"
		if err := os.WriteFile(filepath.Join(designPath, "final_design_stats.csv"), []byte(statsCSV), 0o644); err != nil {
			return "", err
		}
		return "ok", nil
	}
}

// bindCraftTestEnv builds an AdapterEnv with an installed-looking recipe and a
// registry whose alphafold_params directory exists on disk. logBuf and
// progress (when non-nil) capture the adapter's log writes and progress ticks.
func bindCraftTestEnv(t *testing.T, ran *[]string, logBuf *bytes.Buffer, progress *[]float64) AdapterEnv {
	t.Helper()
	home := t.TempDir()
	reg, err := LoadRegistry(home)
	if err != nil {
		t.Fatal(err)
	}
	asset, ok := reg.DataAsset("alphafold_params")
	if !ok {
		t.Fatal("alphafold_params data asset is not registered")
	}
	if err := os.MkdirAll(asset.ExtractTo, 0o755); err != nil {
		t.Fatal(err)
	}
	env := AdapterEnv{
		Recipe:   ToolRecipe{Name: "bindcraft", InstallDir: t.TempDir(), VenvDir: t.TempDir()},
		Run:      bindCraftStubRunner(ran),
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

func TestBindCraftAdapterInvoke(t *testing.T) {
	var ran []string
	var logBuf bytes.Buffer
	var progress []float64
	env := bindCraftTestEnv(t, &ran, &logBuf, &progress)
	starting := filepath.Join(t.TempDir(), "target.pdb")
	if err := os.WriteFile(starting, []byte("ATOM\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := bindCraftAdapter{}.Invoke(context.Background(), env,
		[]byte(`{"settings":{"starting_pdb":"`+starting+`","chains":"A","number_of_final_designs":2}}`))
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
	if env2.Designs[0].StructureFile == "" {
		t.Error("design structure_file must be set")
	}
	if !strings.HasPrefix(env2.Designs[0].StructureFile, env.Registry.Home()) {
		t.Errorf("structure_file %q must be under FOVA_HOME %q (outlives the temp WorkDir)",
			env2.Designs[0].StructureFile, env.Registry.Home())
	}
	if env2.Designs[0].Sequence["A"] != "MKLV" {
		t.Errorf("design sequence should come from the stats CSV, got %q", env2.Designs[0].Sequence["A"])
	}
	if len(ran) != 1 || !strings.Contains(ran[0], "bindcraft.py --settings ") {
		t.Fatalf("want one bindcraft.py --settings command, got: %v", ran)
	}
	// Bug 2: log must be written and progress must be ticked.
	if logBuf.Len() == 0 {
		t.Error("env.Log should receive the stubbed bindcraft.py output")
	}
	if !strings.Contains(logBuf.String(), "bindcraft.py") {
		t.Errorf("env.Log should carry the bindcraft.py invocation, got: %q", logBuf.String())
	}
	if len(progress) < 2 {
		t.Errorf("env.Progress should have been called at least twice, got %v", progress)
	}
}

func TestBindCraftAdapterInvokeMissingSettings(t *testing.T) {
	var ran []string
	env := bindCraftTestEnv(t, &ran, nil, nil)
	if _, err := (bindCraftAdapter{}).Invoke(context.Background(), env, []byte(`{}`)); err == nil {
		t.Fatal("expected an error when settings is missing")
	}
}

func TestBindCraftAdapterInvokeBadStartingPDB(t *testing.T) {
	var ran []string
	env := bindCraftTestEnv(t, &ran, nil, nil)
	_, err := bindCraftAdapter{}.Invoke(context.Background(), env,
		[]byte(`{"settings":{"starting_pdb":"/no/such/file.pdb"}}`))
	if err == nil {
		t.Fatal("expected an error when starting_pdb does not exist")
	}
	// Bug 4: error should steer the agent at fs.read_structure.
	if !strings.Contains(err.Error(), "fs.read_structure") {
		t.Errorf("error %q should point at fs.read_structure", err)
	}
	if len(ran) != 0 {
		t.Errorf("a bad starting_pdb must not run any command, got %d", len(ran))
	}
}

func TestBindCraftAdapterInvokeParamsMissing(t *testing.T) {
	reg, err := LoadRegistry(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// alphafold_params directory is deliberately NOT created.
	env := AdapterEnv{
		Recipe:   ToolRecipe{Name: "bindcraft", InstallDir: t.TempDir(), VenvDir: t.TempDir()},
		WorkDir:  t.TempDir(),
		Registry: reg,
	}
	_, err = bindCraftAdapter{}.Invoke(context.Background(), env, []byte(`{"settings":{"chains":"A"}}`))
	if err == nil {
		t.Fatal("expected an error when the alphafold_params directory is absent")
	}
}

func TestBindCraftAdapterInvokeNotInstalled(t *testing.T) {
	reg, err := LoadRegistry(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	env := AdapterEnv{
		Recipe:   ToolRecipe{InstallDir: filepath.Join(t.TempDir(), "gone"), VenvDir: t.TempDir()},
		WorkDir:  t.TempDir(),
		Registry: reg,
	}
	_, err = bindCraftAdapter{}.Invoke(context.Background(), env, []byte(`{"settings":{"chains":"A"}}`))
	if err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Fatalf("want a 'not installed' error, got: %v", err)
	}
}

func TestRunDesignBindCraftIsRegistered(t *testing.T) {
	reg, err := LoadRegistry(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// Missing settings makes Invoke fail fast — still proves design.bindcraft
	// is registered and dispatched.
	_, err = RunDesign(context.Background(), reg, "design.bindcraft", []byte(`{}`), io.Discard, nil)
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), "no local adapter") {
		t.Fatalf("design.bindcraft must be registered, got: %v", err)
	}
}
