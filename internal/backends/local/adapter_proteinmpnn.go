package local

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// fastaRecord is one header/sequence pair from a FASTA file.
type fastaRecord struct {
	header string // the text after '>'
	seq    string
}

// parseFASTARecords splits FASTA text into header/sequence records, in order.
func parseFASTARecords(text string) []fastaRecord {
	var recs []fastaRecord
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, ">") {
			recs = append(recs, fastaRecord{header: strings.TrimPrefix(line, ">")})
			continue
		}
		if len(recs) > 0 {
			recs[len(recs)-1].seq += line
		}
	}
	return recs
}

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
		recs := parseFASTARecords(string(body))
		for i, rec := range recs {
			if i == 0 {
				continue // native input sequence
			}
			if strings.TrimSpace(rec.seq) == "" {
				continue // malformed record — header with no sequence
			}
			designs = append(designs, designOut{
				Sequence:      splitChains(rec.seq),
				StructureFile: "",
				Scores:        proteinMPNNScores(rec.header),
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
		return nil, fmt.Errorf("design.proteinmpnn: target %q does not exist", target)
	}
	if info, err := os.Stat(env.Recipe.InstallDir); err != nil || !info.IsDir() {
		return nil, fmt.Errorf("design.proteinmpnn: proteinmpnn is not installed (run /install proteinmpnn)")
	}
	if info, err := os.Stat(env.Recipe.VenvDir); err != nil || !info.IsDir() {
		return nil, fmt.Errorf("design.proteinmpnn: proteinmpnn is not installed (run /install proteinmpnn)")
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

	python := filepath.Join(env.Recipe.VenvDir, "bin", "python")
	parsedJSONL := filepath.Join(env.WorkDir, "parsed.jsonl")
	parseCmd := fmt.Sprintf("%s %s --input_path=%s --output_path=%s",
		python,
		filepath.Join(env.Recipe.InstallDir, "helper_scripts", "parse_multiple_chains.py"),
		inputsDir, parsedJSONL)
	if out, err := env.Run(ctx, env.Recipe.InstallDir, parseCmd); err != nil {
		return nil, fmt.Errorf("design.proteinmpnn: parse step failed: %w\n%s", err, out)
	}

	runCmd := fmt.Sprintf(
		"%s %s --jsonl_path %s --out_folder %s --num_seq_per_target %d --sampling_temp 0.1 --seed 37 --batch_size 1",
		python,
		filepath.Join(env.Recipe.InstallDir, "protein_mpnn_run.py"),
		parsedJSONL, env.WorkDir, numDesigns)
	if out, err := env.Run(ctx, env.Recipe.InstallDir, runCmd); err != nil {
		return nil, fmt.Errorf("design.proteinmpnn: inference step failed: %w\n%s", err, out)
	}

	designs, err := parseProteinMPNNOutput(filepath.Join(env.WorkDir, "seqs"))
	if err != nil {
		return nil, err
	}
	return json.Marshal(designsEnvelope{Designs: designs})
}
