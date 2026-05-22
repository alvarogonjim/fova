package local

import (
	"bytes"
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
