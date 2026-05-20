package local

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/alvarogonjim/fova/pkg/proteinio"
)

// proteinMPNNScores pulls score / global_score / seq_recovery out of a header
// like "T=0.1, sample=1, score=0.82, global_score=0.82, seq_recovery=0.50".
func proteinMPNNScores(header string) map[string]float64 {
	scores := map[string]float64{}
	for _, field := range strings.Split(header, ",") {
		kv := strings.SplitN(strings.TrimSpace(field), "=", 2)
		if len(kv) != 2 {
			continue
		}
		switch kv[0] {
		case "score", "global_score", "seq_recovery":
			if v, err := strconv.ParseFloat(strings.TrimSpace(kv[1]), 64); err == nil {
				scores[kv[0]] = v
			}
		}
	}
	return scores
}

// splitChains maps a ProteinMPNN sequence (chains joined by '/') to chain IDs
// A, B, C, ...
func splitChains(seq string) map[string]string {
	out := map[string]string{}
	for i, chain := range strings.Split(seq, "/") {
		out[string(rune('A'+i))] = chain
	}
	return out
}

// parseProteinMPNNOutput reads every *.fa in seqsDir and returns one design per
// designed sequence (record 0 in each file is the native input — skipped).
func parseProteinMPNNOutput(seqsDir string) ([]designOut, error) {
	files, err := filepath.Glob(filepath.Join(seqsDir, "*.fa"))
	if err != nil {
		return nil, err
	}
	var designs []designOut
	for _, f := range files {
		body, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		recs, err := proteinio.ParseFASTA(bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("design.proteinmpnn: parse %s: %w", f, err)
		}
		for i, rec := range recs {
			if i == 0 {
				continue // native input sequence
			}
			if strings.TrimSpace(rec.Sequence) == "" {
				continue // malformed record — header with no sequence
			}
			designs = append(designs, designOut{
				Sequence:      splitChains(rec.Sequence),
				StructureFile: "",
				Scores:        proteinMPNNScores(rec.Header),
			})
		}
	}
	if len(designs) == 0 {
		return nil, fmt.Errorf("design.proteinmpnn: no designed sequences found in %s", seqsDir)
	}
	return designs, nil
}

// init registers the ProteinMPNN adapter with the local backend.
func init() { registerAdapter(proteinMPNNAdapter{}) }

// proteinMPNNAdapter wires design.proteinmpnn to the installed ProteinMPNN tool.
type proteinMPNNAdapter struct{}

func (proteinMPNNAdapter) AgentTool() string { return "design.proteinmpnn" }
func (proteinMPNNAdapter) Recipe() string    { return "proteinmpnn" }

// proteinMPNNRequest is the subset of the design.proteinmpnn input the adapter
// uses (hotspots is accepted by the schema but unused in SP1).
type proteinMPNNRequest struct {
	Target     string `json:"target"`
	NumDesigns int    `json:"num_designs"`
}

// copyFile copies a small file (e.g. a PDB) from src to dst.
func copyFile(src, dst string) error {
	body, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, body, 0o644)
}

// Invoke runs ProteinMPNN for one backbone: parse the PDB into a jsonl, run
// inference, then parse the FASTA output into the {"designs":[...]} schema.
func (proteinMPNNAdapter) Invoke(ctx context.Context, env AdapterEnv, request []byte) ([]byte, error) {
	var req proteinMPNNRequest
	if err := json.Unmarshal(request, &req); err != nil {
		return nil, fmt.Errorf("design.proteinmpnn: invalid request: %w", err)
	}
	target := strings.TrimSpace(req.Target)
	if target == "" {
		return nil, fmt.Errorf("design.proteinmpnn: target is required (path to a .pdb backbone)")
	}
	if !strings.HasSuffix(target, ".pdb") {
		return nil, fmt.Errorf("design.proteinmpnn: target %q must be a .pdb file", target)
	}
	if info, err := os.Stat(target); err != nil || info.IsDir() {
		return nil, fmt.Errorf(
			"design.proteinmpnn: target %q not found (workspace root). "+
				"Use fs.read_structure or fs.bash to confirm the file exists, "+
				"or pass an absolute path.",
			target)
	}
	numDesigns := req.NumDesigns
	if numDesigns < 1 {
		numDesigns = 1
	}

	inputsDir := filepath.Join(env.WorkDir, "inputs")
	if err := os.MkdirAll(inputsDir, 0o755); err != nil {
		return nil, err
	}
	if err := copyFile(target, filepath.Join(inputsDir, filepath.Base(target))); err != nil {
		return nil, fmt.Errorf("design.proteinmpnn: stage target: %w", err)
	}
	env.Tick(0.05) // target staged

	if env.Recipe.ImageTag != "" {
		// Container-mode: parse + inference run in a single container invocation
		// against the bind-mounted workdir at /work. The image holds the
		// proteinmpnn source at /opt/proteinmpnn (per the Containerfile).
		rt := Detect()
		if !rt.Available() {
			return nil, fmt.Errorf("design.proteinmpnn: no container runtime — install podman or docker")
		}
		if ok, _ := rt.ImageExists(env.Recipe.ImageTag); !ok {
			return nil, fmt.Errorf(
				"design.proteinmpnn: image %s is missing — run /install proteinmpnn",
				env.Recipe.ImageTag)
		}
		driver := filepath.Join(env.WorkDir, "run.sh")
		body := fmt.Sprintf(`#!/bin/bash
set -euo pipefail
cd /work
python /opt/proteinmpnn/helper_scripts/parse_multiple_chains.py \
    --input_path=/work/inputs --output_path=/work/parsed.jsonl
python /opt/proteinmpnn/protein_mpnn_run.py \
    --jsonl_path /work/parsed.jsonl --out_folder /work \
    --num_seq_per_target %d --sampling_temp 0.1 --seed 37 --batch_size 1
`, numDesigns)
		if err := os.WriteFile(driver, []byte(body), 0o755); err != nil {
			return nil, fmt.Errorf("design.proteinmpnn: write driver: %w", err)
		}
		if _, err := rt.RunContainer(ctx, ContainerRunArgs{
			Image:   env.Recipe.ImageTag,
			Cmd:     []string{"bash", "/work/run.sh"},
			GPU:     env.Recipe.GPU,
			Mounts:  []Mount{{HostPath: env.WorkDir, ContainerPath: "/work"}},
			Workdir: "/work",
			Log:     env.LogWriter(),
		}); err != nil {
			return nil, fmt.Errorf("design.proteinmpnn: container run failed: %w", err)
		}
		env.Tick(0.95)
	} else {
		// Legacy venv-mode (pre-v0.7 install). Preserved so the adapter still
		// runs against an InstallDir + VenvDir layout if a recipe ships
		// without a Containerfile.
		if info, err := os.Stat(env.Recipe.InstallDir); err != nil || !info.IsDir() {
			return nil, fmt.Errorf("design.proteinmpnn: proteinmpnn is not installed (run /install proteinmpnn)")
		}
		if info, err := os.Stat(env.Recipe.VenvDir); err != nil || !info.IsDir() {
			return nil, fmt.Errorf("design.proteinmpnn: proteinmpnn is not installed (run /install proteinmpnn)")
		}
		python := filepath.Join(env.Recipe.VenvDir, "bin", "python")
		parsedJSONL := filepath.Join(env.WorkDir, "parsed.jsonl")
		parseCmd := fmt.Sprintf("%s %s --input_path=%s --output_path=%s",
			python,
			filepath.Join(env.Recipe.InstallDir, "helper_scripts", "parse_multiple_chains.py"),
			inputsDir, parsedJSONL)
		if out, err := env.Run(ctx, env.Recipe.InstallDir, parseCmd, env.LogWriter()); err != nil {
			return nil, fmt.Errorf("design.proteinmpnn: parse step failed: %w\n%s", err, out)
		}
		env.Tick(0.20)
		runCmd := fmt.Sprintf(
			"%s %s --jsonl_path %s --out_folder %s --num_seq_per_target %d --sampling_temp 0.1 --seed 37 --batch_size 1",
			python,
			filepath.Join(env.Recipe.InstallDir, "protein_mpnn_run.py"),
			parsedJSONL, env.WorkDir, numDesigns)
		if out, err := env.Run(ctx, env.Recipe.InstallDir, runCmd, env.LogWriter()); err != nil {
			return nil, fmt.Errorf("design.proteinmpnn: inference step failed: %w\n%s", err, out)
		}
		env.Tick(0.95)
	}

	designs, err := parseProteinMPNNOutput(filepath.Join(env.WorkDir, "seqs"))
	if err != nil {
		return nil, err
	}
	return json.Marshal(designsEnvelope{Designs: designs})
}
