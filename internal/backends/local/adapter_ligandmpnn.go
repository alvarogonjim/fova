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

// ligandMPNNScores pulls the LigandMPNN per-design confidence keys out of a
// FASTA header like "1BC8, id=1, overall_confidence=0.62, ligand_confidence=0.55,
// sequence_recovery=0.38". A missing key is simply absent from the map — never
// an error (FASTA header format may drift; a dropped score must not fail a run).
func ligandMPNNScores(header string) map[string]float64 {
	scores := map[string]float64{}
	for _, field := range strings.Split(header, ",") {
		kv := strings.SplitN(strings.TrimSpace(field), "=", 2)
		if len(kv) != 2 {
			continue
		}
		switch kv[0] {
		case "overall_confidence", "ligand_confidence", "sequence_recovery":
			if v, err := strconv.ParseFloat(strings.TrimSpace(kv[1]), 64); err == nil {
				scores[kv[0]] = v
			}
		}
	}
	return scores
}

// parseLigandMPNNOutput reads every seqs/*.fa under outDir and returns one
// designOut per designed sequence. Record 0 in each FASTA is the native input
// (skipped). Each design record's Sequence is split on '/' into chain ids
// (A, B, ...); its Scores come from the header's key=value tokens; its
// StructureFile is the matching backbones/<stem>_<i>.pdb if present, else the
// packed/<stem>_<i>_1.pdb if present, else "".
func parseLigandMPNNOutput(outDir string) ([]designOut, error) {
	files, err := filepath.Glob(filepath.Join(outDir, "seqs", "*.fa"))
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
			return nil, fmt.Errorf("design.ligandmpnn: parse %s: %w", f, err)
		}
		stem := strings.TrimSuffix(filepath.Base(f), filepath.Ext(f))
		for i, rec := range recs {
			if i == 0 {
				continue // native input sequence
			}
			if strings.TrimSpace(rec.Sequence) == "" {
				continue // malformed record — header with no sequence
			}
			designs = append(designs, designOut{
				Sequence:      splitChains(rec.Sequence),
				StructureFile: ligandMPNNStructureFile(outDir, stem, i),
				Scores:        ligandMPNNScores(rec.Header),
			})
		}
	}
	if len(designs) == 0 {
		return nil, fmt.Errorf("design.ligandmpnn: no designed sequences found in %s", outDir)
	}
	return designs, nil
}

// ligandMPNNCheckpoints maps a run.py --model_type to its checkpoint filename
// under /models. Each filename is verified present in the
// [[tools.ligandmpnn.weights]] path= entries of internal/backends/local/tools.toml.
var ligandMPNNCheckpoints = map[string]string{
	"protein_mpnn":                    "proteinmpnn_v_48_020.pt",
	"ligand_mpnn":                     "ligandmpnn_v_32_010_25.pt",
	"soluble_mpnn":                    "solublempnn_v_48_020.pt",
	"global_label_membrane_mpnn":      "global_label_membrane_mpnn_v_48_020.pt",
	"per_residue_label_membrane_mpnn": "per_residue_label_membrane_mpnn_v_48_020.pt",
}

// checkpointForModelType returns the checkpoint filename for a run.py
// --model_type. An empty model_type defaults to the ligand_mpnn checkpoint
// (the tool is design.ligandmpnn). An unknown model_type yields "".
func checkpointForModelType(modelType string) string {
	if modelType == "" {
		modelType = "ligand_mpnn"
	}
	return ligandMPNNCheckpoints[modelType]
}

// ligandMPNNArgs maps the agent-facing fields of a LigandMPNNParams to run.py
// flags. A flag is omitted when its field is unset — a nil pointer, an empty
// string, or an int <= 0. A *bool renders as "1" (true) / "0" (false). A
// residue-list string (e.g. "A23 A24") is passed as one argv element. The
// fova-owned flags (--pdb_path, --out_folder, --checkpoint_*, --save_stats,
// --checkpoint_path_sc) are added by Invoke, not here.
func ligandMPNNArgs(p domain.LigandMPNNParams) []string {
	var args []string
	addStr := func(flag, val string) {
		if val != "" {
			args = append(args, flag, val)
		}
	}
	addInt := func(flag string, val int) {
		if val > 0 {
			args = append(args, flag, strconv.Itoa(val))
		}
	}
	addFloatP := func(flag string, val *float64) {
		if val != nil {
			args = append(args, flag, strconv.FormatFloat(*val, 'g', -1, 64))
		}
	}
	addIntP := func(flag string, val *int) {
		if val != nil {
			args = append(args, flag, strconv.Itoa(*val))
		}
	}
	addBoolP := func(flag string, val *bool) {
		if val != nil {
			if *val {
				args = append(args, flag, "1")
			} else {
				args = append(args, flag, "0")
			}
		}
	}

	addStr("--model_type", p.ModelType)
	addInt("--number_of_batches", p.NumDesigns)
	addInt("--batch_size", p.BatchSize)
	addFloatP("--temperature", p.Temperature)
	addIntP("--seed", p.Seed)
	addStr("--redesigned_residues", p.RedesignedResidues)
	addStr("--fixed_residues", p.FixedResidues)
	addStr("--chains_to_design", p.ChainsToDesign)
	addStr("--bias_AA", p.BiasAA)
	addStr("--omit_AA", p.OmitAA)
	addStr("--bias_AA_per_residue", p.BiasAAPerResidue)
	addStr("--omit_AA_per_residue", p.OmitAAPerResidue)
	addBoolP("--ligand_mpnn_use_atom_context", p.LigandUseAtomContext)
	addBoolP("--ligand_mpnn_use_side_chain_context", p.LigandUseSideChainContext)
	addFloatP("--ligand_mpnn_cutoff_for_score", p.LigandCutoff)
	addStr("--symmetry_residues", p.SymmetryResidues)
	addStr("--symmetry_weights", p.SymmetryWeights)
	addBoolP("--homo_oligomer", p.HomoOligomer)
	addIntP("--global_transmembrane_label", p.GlobalTransmembraneLabel)
	addStr("--transmembrane_buried", p.TransmembraneBuried)
	addStr("--transmembrane_interface", p.TransmembraneInterface)
	addBoolP("--pack_side_chains", p.PackSideChains)
	addInt("--number_of_packs_per_design", p.NumberOfPacksPerDesign)
	addBoolP("--pack_with_ligand_context", p.PackWithLigandContext)
	addBoolP("--repack_everything", p.RepackEverything)
	return args
}

// ligandMPNNStructureFile resolves the structure file for design record i of
// FASTA stem: the backbone PDB if present, else the first packed PDB if
// present, else "" (a designed sequence with no PDB is still a valid design).
func ligandMPNNStructureFile(outDir, stem string, i int) string {
	backbone := filepath.Join(outDir, "backbones", fmt.Sprintf("%s_%d.pdb", stem, i))
	if info, err := os.Stat(backbone); err == nil && !info.IsDir() {
		return backbone
	}
	packed := filepath.Join(outDir, "packed", fmt.Sprintf("%s_%d_1.pdb", stem, i))
	if info, err := os.Stat(packed); err == nil && !info.IsDir() {
		return packed
	}
	return ""
}

// sideChainPackerCheckpoint is the checkpoint filename for the LigandMPNN
// side-chain packing model — passed as --checkpoint_path_sc when the request
// enables pack_side_chains. It is one of the [[tools.ligandmpnn.weights]]
// path= entries in tools.toml.
const sideChainPackerCheckpoint = "ligandmpnn_sc_v_32_002_16.pt"

// init registers the LigandMPNN design adapter with the local backend.
func init() { registerAdapter(ligandMPNNAdapter{}) }

// ligandMPNNAdapter wires design.ligandmpnn to the container-mode LigandMPNN
// image: it compiles the agent's typed run configuration into run.py flags,
// stages the input PDB and any per-residue bias/omit JSON files into the
// workdir, runs run.py inside the tool image with the install-time weights
// cache bind-mounted at /models, and returns the designed sequences — with
// their FASTA-header confidence scores — in the {"designs":[...]} envelope.
type ligandMPNNAdapter struct{}

func (ligandMPNNAdapter) AgentTool() string { return "design.ligandmpnn" }
func (ligandMPNNAdapter) Recipe() string    { return "ligandmpnn" }

// Invoke runs LigandMPNN for one backbone: stage the PDB (and any bias/omit
// JSON files), compile the typed request into run.py flags, run the container,
// then parse the FASTA output into the {"designs":[...]} envelope. The image's
// ENTRYPOINT is `python /opt/ligandmpnn/run.py`, so Cmd carries the flags only.
func (ligandMPNNAdapter) Invoke(ctx context.Context, env AdapterEnv, request []byte) ([]byte, error) {
	var req domain.LigandMPNNParams
	if err := json.Unmarshal(request, &req); err != nil {
		return nil, fmt.Errorf("design.ligandmpnn: invalid request: %w", err)
	}
	// The tool's preflight is the primary input guard; this is the
	// backend-side backstop against a malformed request reaching the runtime.
	pdb := strings.TrimSpace(req.PDB)
	if pdb == "" {
		return nil, fmt.Errorf("design.ligandmpnn: pdb is required (path to a .pdb backbone)")
	}
	if !strings.HasSuffix(pdb, ".pdb") {
		return nil, fmt.Errorf("design.ligandmpnn: pdb %q must be a .pdb file", pdb)
	}
	if info, err := os.Stat(pdb); err != nil || info.IsDir() {
		return nil, fmt.Errorf("design.ligandmpnn: pdb %q not found", pdb)
	}
	if checkpointForModelType(req.ModelType) == "" {
		return nil, fmt.Errorf("design.ligandmpnn: unknown model_type %q", req.ModelType)
	}
	if env.Registry == nil {
		return nil, fmt.Errorf("design.ligandmpnn: adapter registry unavailable")
	}
	if env.Recipe.ImageTag == "" {
		return nil, fmt.Errorf("design.ligandmpnn: container image is not configured (run /install ligandmpnn)")
	}

	// Stage the input PDB into the workdir so the container flag references a
	// path resolvable at /work.
	pdbBase := filepath.Base(pdb)
	if err := copyFile(pdb, filepath.Join(env.WorkDir, pdbBase)); err != nil {
		return nil, fmt.Errorf("design.ligandmpnn: stage pdb: %w", err)
	}
	// Stage the per-residue bias/omit JSON files when set, remembering the
	// staged base names so the flags can be rewritten to /work paths.
	stagedBiasJSON := ""
	if req.BiasAAPerResidue != "" {
		base := filepath.Base(req.BiasAAPerResidue)
		if err := copyFile(req.BiasAAPerResidue, filepath.Join(env.WorkDir, base)); err != nil {
			return nil, fmt.Errorf("design.ligandmpnn: stage bias_AA_per_residue %q: %w", req.BiasAAPerResidue, err)
		}
		stagedBiasJSON = base
	}
	stagedOmitJSON := ""
	if req.OmitAAPerResidue != "" {
		base := filepath.Base(req.OmitAAPerResidue)
		if err := copyFile(req.OmitAAPerResidue, filepath.Join(env.WorkDir, base)); err != nil {
			return nil, fmt.Errorf("design.ligandmpnn: stage omit_AA_per_residue %q: %w", req.OmitAAPerResidue, err)
		}
		stagedOmitJSON = base
	}

	outDir := filepath.Join(env.WorkDir, "out")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, err
	}
	env.Tick(0.05) // input staged

	rt := Detect()
	if !rt.Available() {
		return nil, fmt.Errorf("design.ligandmpnn: no container runtime — install podman or docker")
	}
	if ok, _ := rt.ImageExists(env.Recipe.ImageTag); !ok {
		return nil, fmt.Errorf(
			"design.ligandmpnn: image %s is missing — run /install ligandmpnn",
			env.Recipe.ImageTag)
	}

	// LigandMPNN checkpoints (~1.5 GB) are fetched at /install time into this
	// cache. A missing cache means install did not complete, so validate it
	// exists (os.Stat) rather than creating it (spec §2).
	modelsCache := ModelsRoot(env.Registry.Home(), "ligandmpnn")
	if info, err := os.Stat(modelsCache); err != nil || !info.IsDir() {
		return nil, fmt.Errorf(
			"design.ligandmpnn: weights cache %s missing — run /install ligandmpnn",
			modelsCache)
	}

	// fova-owned flags: input PDB, output folder, the model checkpoint, and a
	// disabled stats dump. The ENTRYPOINT is `python /opt/ligandmpnn/run.py`,
	// so Cmd is the flags only.
	cmd := []string{
		"--pdb_path", "/work/" + pdbBase,
		"--out_folder", "/work/out",
		"--checkpoint_" + checkpointModelKey(req.ModelType),
		"/models/" + checkpointForModelType(req.ModelType),
		"--save_stats", "0",
	}
	if req.PackSideChains != nil && *req.PackSideChains {
		cmd = append(cmd, "--checkpoint_path_sc", "/models/"+sideChainPackerCheckpoint)
	}
	// Append the agent-facing flags, rewriting any staged JSON-file flag values
	// to their /work paths.
	for _, a := range ligandMPNNArgs(req) {
		switch {
		case a == req.BiasAAPerResidue && stagedBiasJSON != "":
			cmd = append(cmd, "/work/"+stagedBiasJSON)
		case a == req.OmitAAPerResidue && stagedOmitJSON != "":
			cmd = append(cmd, "/work/"+stagedOmitJSON)
		default:
			cmd = append(cmd, a)
		}
	}

	mounts := []Mount{
		{HostPath: env.WorkDir, ContainerPath: "/work"},
		{HostPath: modelsCache, ContainerPath: "/models"},
	}
	if _, err := rt.RunContainer(ctx, ContainerRunArgs{
		Image:   env.Recipe.ImageTag,
		Cmd:     cmd,
		GPU:     env.Recipe.GPU,
		Mounts:  mounts,
		Workdir: "/work",
		Log:     env.LogWriter(),
	}); err != nil {
		return nil, fmt.Errorf("design.ligandmpnn: container run failed: %w", err)
	}
	env.Tick(0.95) // run.py done

	designs, err := parseLigandMPNNOutput(outDir)
	if err != nil {
		return nil, err
	}
	return json.Marshal(designsEnvelope{Designs: designs})
}

// checkpointModelKey returns the run.py --checkpoint_<key> suffix for a
// model_type — the model_type itself, defaulting to ligand_mpnn when empty.
func checkpointModelKey(modelType string) string {
	if modelType == "" {
		return "ligand_mpnn"
	}
	return modelType
}
