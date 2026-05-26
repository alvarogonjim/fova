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

	"github.com/alvarogonjim/fova/internal/domain"
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
// The agent-facing request is a typed domain.ProteinMPNNParams; this adapter
// alias preserves the historical name for the in-package init/registration.
type proteinMPNNAdapter struct{}

// proteinMPNNRequest is the adapter-side request shape — identical to the
// agent-facing domain.ProteinMPNNParams (the tool already validated it).
type proteinMPNNRequest = domain.ProteinMPNNParams

func (proteinMPNNAdapter) AgentTool() string { return "design.proteinmpnn" }
func (proteinMPNNAdapter) Recipe() string    { return "proteinmpnn" }

// proteinMPNN-default flags. When a ProteinMPNNParams field is unset (zero or
// nil pointer), Invoke applies these so the request always reaches run.py
// with a usable configuration.
const (
	proteinMPNNDefaultNumSeqPerTarget = 1
	proteinMPNNDefaultBatchSize       = 1
	proteinMPNNDefaultSamplingTemp    = 0.1
	proteinMPNNDefaultSeed            = 37
)

// proteinMPNNArgs builds the `protein_mpnn_run.py` argument list from a typed
// ProteinMPNNParams. The two fova-owned arguments (`--jsonl_path` and
// `--out_folder`) come first; the rest follow the CONTRACT mapping in
// docs/superpowers/plans/2026-05-23-design-trio.md.
//
// Defaults: `--num_seq_per_target 1`, `--batch_size 1`, `--sampling_temp 0.1`,
// `--seed 37` are applied when their fields are unset. Each JSONL-path flag is
// appended only when the corresponding field is non-empty; the caller is
// responsible for staging the file under the workdir and rewriting the field
// to its /work path before invoking proteinMPNNArgs.
func proteinMPNNArgs(p domain.ProteinMPNNParams, jsonlPath, outFolder string) []string {
	args := []string{
		"--jsonl_path", jsonlPath,
		"--out_folder", outFolder,
	}

	if p.NumDesigns > 0 {
		args = append(args, "--num_seq_per_target", strconv.Itoa(p.NumDesigns))
	} else {
		args = append(args, "--num_seq_per_target", strconv.Itoa(proteinMPNNDefaultNumSeqPerTarget))
	}
	if p.BatchSize > 0 {
		args = append(args, "--batch_size", strconv.Itoa(p.BatchSize))
	} else {
		args = append(args, "--batch_size", strconv.Itoa(proteinMPNNDefaultBatchSize))
	}
	if p.SamplingTemp != nil {
		args = append(args, "--sampling_temp", strconv.FormatFloat(*p.SamplingTemp, 'g', -1, 64))
	} else {
		args = append(args, "--sampling_temp", strconv.FormatFloat(proteinMPNNDefaultSamplingTemp, 'g', -1, 64))
	}
	if p.Seed != nil {
		args = append(args, "--seed", strconv.Itoa(*p.Seed))
	} else {
		args = append(args, "--seed", strconv.Itoa(proteinMPNNDefaultSeed))
	}

	if p.ChainsToDesign != "" {
		args = append(args, "--chain_id_jsonl", p.ChainsToDesign)
	}
	if p.FixedPositions != "" {
		args = append(args, "--fixed_positions_jsonl", p.FixedPositions)
	}
	if p.OmitAAs != "" {
		args = append(args, "--omit_AAs", p.OmitAAs)
	}
	if p.BiasAA != "" {
		args = append(args, "--bias_AA_jsonl", p.BiasAA)
	}
	if p.BiasByResidue != "" {
		args = append(args, "--bias_by_res_jsonl", p.BiasByResidue)
	}
	if p.TiedPositions != "" {
		args = append(args, "--tied_positions_jsonl", p.TiedPositions)
	}
	if p.SaveScore != nil && *p.SaveScore {
		args = append(args, "--save_score", "1")
	}
	return args
}

// copyFile copies a small file (e.g. a PDB or JSONL) from src to dst.
func copyFile(src, dst string) error {
	body, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, body, 0o644)
}

// writeChainIDJSONL writes a one-line ProteinMPNN chain-id JSONL of the form
// `{"<stem>": [["A","B"], []]}` — the first list is the designed chains, the
// second the fixed chains. fova takes the simpler "no fixed-chain constraint"
// form (`[]` for fixed); the grounding skill explains the trade-off (without
// listing fixed chains, ProteinMPNN treats them as designable too only if
// they appear in chains_to_design).
func writeChainIDJSONL(path, stem string, designed []string) error {
	doc := map[string][][]string{
		stem: {designed, {}},
	}
	body, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	body = append(body, '\n')
	return os.WriteFile(path, body, 0o644)
}

// splitDesignedChains splits a comma-separated chain id list ("A,B") into the
// trimmed token slice ["A","B"]. Empty tokens are dropped.
func splitDesignedChains(s string) []string {
	var out []string
	for _, tok := range strings.Split(s, ",") {
		tok = strings.TrimSpace(tok)
		if tok != "" {
			out = append(out, tok)
		}
	}
	return out
}

// Invoke runs ProteinMPNN for one backbone: stage the PDB (and any chain/
// position JSONLs), parse the PDB into a jsonl with parse_multiple_chains.py,
// run protein_mpnn_run.py, then parse the FASTA output into the
// {"designs":[...]} envelope.
func (proteinMPNNAdapter) Invoke(ctx context.Context, env AdapterEnv, request []byte) ([]byte, error) {
	var req proteinMPNNRequest
	if err := json.Unmarshal(request, &req); err != nil {
		return nil, fmt.Errorf("design.proteinmpnn: invalid request: %w", err)
	}
	// Backend-side backstop against a malformed request reaching the runtime
	// (the bespoke tool's preflight Validate is the primary input guard).
	pdb := strings.TrimSpace(req.PDB)
	if pdb == "" {
		return nil, fmt.Errorf("design.proteinmpnn: pdb is required (path to a .pdb backbone)")
	}
	if !strings.HasSuffix(pdb, ".pdb") {
		return nil, fmt.Errorf("design.proteinmpnn: pdb %q must be a .pdb file", pdb)
	}
	if info, err := os.Stat(pdb); err != nil || info.IsDir() {
		return nil, fmt.Errorf(
			"design.proteinmpnn: pdb %q not found (workspace root). "+
				"Use fs.read_structure or fs.bash to confirm the file exists, "+
				"or pass an absolute path.",
			pdb)
	}

	inputsDir := filepath.Join(env.WorkDir, "inputs")
	if err := os.MkdirAll(inputsDir, 0o755); err != nil {
		return nil, err
	}
	pdbBase := filepath.Base(pdb)
	if err := copyFile(pdb, filepath.Join(inputsDir, pdbBase)); err != nil {
		return nil, fmt.Errorf("design.proteinmpnn: stage pdb: %w", err)
	}

	// Stage the agent-provided JSONL files when set, rewriting each field to
	// the /work/<base> path the run.py flag will reference. We mutate `req`
	// in place because proteinMPNNArgs reads the field values directly.
	stageJSONL := func(field *string, label string) error {
		if *field == "" {
			return nil
		}
		base := filepath.Base(*field)
		if err := copyFile(*field, filepath.Join(env.WorkDir, base)); err != nil {
			return fmt.Errorf("design.proteinmpnn: stage %s %q: %w", label, *field, err)
		}
		*field = "/work/" + base
		return nil
	}
	if err := stageJSONL(&req.FixedPositions, "fixed_positions"); err != nil {
		return nil, err
	}
	if err := stageJSONL(&req.BiasAA, "bias_AA"); err != nil {
		return nil, err
	}
	if err := stageJSONL(&req.BiasByResidue, "bias_by_residue"); err != nil {
		return nil, err
	}
	if err := stageJSONL(&req.TiedPositions, "tied_positions"); err != nil {
		return nil, err
	}

	// When chains_to_design is set, generate the chain-id JSONL fova owns and
	// rewrite the field to point at /work/chain_id.jsonl so proteinMPNNArgs
	// emits the --chain_id_jsonl flag.
	if designed := splitDesignedChains(req.ChainsToDesign); len(designed) > 0 {
		stem := strings.TrimSuffix(pdbBase, filepath.Ext(pdbBase))
		chainJSONL := filepath.Join(env.WorkDir, "chain_id.jsonl")
		if err := writeChainIDJSONL(chainJSONL, stem, designed); err != nil {
			return nil, fmt.Errorf("design.proteinmpnn: write chain_id.jsonl: %w", err)
		}
		req.ChainsToDesign = "/work/chain_id.jsonl"
	}

	env.Tick(0.05) // inputs staged

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
		runArgs := proteinMPNNArgs(req, "/work/parsed.jsonl", "/work")
		driver := filepath.Join(env.WorkDir, "run.sh")
		body := "#!/bin/bash\nset -euo pipefail\ncd /work\n" +
			"python /opt/proteinmpnn/helper_scripts/parse_multiple_chains.py " +
			"--input_path=/work/inputs --output_path=/work/parsed.jsonl\n" +
			"python /opt/proteinmpnn/protein_mpnn_run.py " + strings.Join(runArgs, " ") + "\n"
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
		// Build the run.py invocation from the typed params; in venv-mode the
		// staged JSONL paths the tool wrote to /work/... aren't valid (no /work
		// mount), so we map them back to host-side workdir paths.
		runReq := req
		swapToHost := func(field *string) {
			if strings.HasPrefix(*field, "/work/") {
				*field = filepath.Join(env.WorkDir, strings.TrimPrefix(*field, "/work/"))
			}
		}
		swapToHost(&runReq.FixedPositions)
		swapToHost(&runReq.BiasAA)
		swapToHost(&runReq.BiasByResidue)
		swapToHost(&runReq.TiedPositions)
		swapToHost(&runReq.ChainsToDesign)
		runArgs := proteinMPNNArgs(runReq, parsedJSONL, env.WorkDir)
		runCmd := fmt.Sprintf("%s %s %s",
			python,
			filepath.Join(env.Recipe.InstallDir, "protein_mpnn_run.py"),
			strings.Join(runArgs, " "))
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
