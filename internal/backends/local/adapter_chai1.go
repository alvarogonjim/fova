package local

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// init registers the Chai-1 fold adapter with the local backend.
func init() { registerAdapter(chai1Adapter{}) }

// chai1Adapter wires fold.chai1 to the container-mode Chai-1 image: it
// compiles the agent's typed multi-entity request (protein/dna/rna/ligand/
// glycan entities, restraints, templates, MSA choice, model parameters) into a
// Chai-1 input FASTA and an optional restraint CSV, stages any precomputed MSA
// directory and template-hits file into the workdir, runs `chai-lab fold`
// inside the tool image with the install-time weights cache bind-mounted at
// /models (CHAI_DOWNLOADS_DIR is set to /models inside the image), and returns
// the produced structures — with their .npz confidence scores — in the
// {"designs":[...]} envelope.
type chai1Adapter struct{}

func (chai1Adapter) AgentTool() string { return "fold.chai1" }
func (chai1Adapter) Recipe() string    { return "chai1" }

// chai1Entity is one molecular component of the predicted complex.
type chai1Entity struct {
	Type     string `json:"type"`     // protein | dna | rna | ligand | glycan
	ID       string `json:"id"`       // one chain id
	Sequence string `json:"sequence"` // protein/dna/rna
	SMILES   string `json:"smiles"`   // ligand
	Glycan   string `json:"glycan"`   // glycan
}

// chai1Restraint is one inter-chain restraint. Pointer fields are optional
// numerics: nil ⇒ the CSV cell is left blank.
type chai1Restraint struct {
	ConnectionType string   `json:"connection_type"` // contact | pocket | covalent
	ChainA         string   `json:"chain_a"`
	ResA           string   `json:"res_a"`
	ChainB         string   `json:"chain_b"`
	ResB           string   `json:"res_b"`
	MinDistance    *float64 `json:"min_distance"`
	MaxDistance    *float64 `json:"max_distance"`
	Confidence     *float64 `json:"confidence"`
	Comment        string   `json:"comment"`
}

// chai1Templates is the optional request-level template configuration.
type chai1Templates struct {
	Server   bool   `json:"server"`
	HitsPath string `json:"hits_path"`
}

// chai1Request is the full fold.chai1 input. Pointer fields are model
// parameters: nil ⇒ omit the CLI flag and let Chai-1 use its own default.
type chai1Request struct {
	Entities            []chai1Entity    `json:"entities"`
	MSA                 string           `json:"msa"` // "default" | "server" | workspace path
	Restraints          []chai1Restraint `json:"restraints"`
	Templates           *chai1Templates  `json:"templates"`
	NumTrunkRecycles    *int             `json:"num_trunk_recycles"`
	NumDiffnTimesteps   *int             `json:"num_diffn_timesteps"`
	NumDiffnSamples     *int             `json:"num_diffn_samples"`
	NumTrunkSamples     *int             `json:"num_trunk_samples"`
	RecycleMSASubsample *int             `json:"recycle_msa_subsample"`
	Seed                *int             `json:"seed"`
	SaveAs              string           `json:"save_as"`
}

// buildChai1FASTA renders the multi-entity Chai-1 input FASTA: one record per
// entity in input order, `">" + type + "|name=" + id` then the body line —
// Sequence for protein/dna/rna, SMILES for ligand, Glycan for glycan.
func buildChai1FASTA(req chai1Request) string {
	var b strings.Builder
	for _, e := range req.Entities {
		fmt.Fprintf(&b, ">%s|name=%s\n", e.Type, e.ID)
		var body string
		switch e.Type {
		case "ligand":
			body = e.SMILES
		case "glycan":
			body = e.Glycan
		default: // protein / dna / rna
			body = e.Sequence
		}
		b.WriteString(body)
		b.WriteString("\n")
	}
	return b.String()
}

// buildChai1Restraints renders the Chai-1 restraint CSV (`--constraint-path`):
// a fixed header row then one row per restraint. restraint_id is generated as
// `restraint_<index>`. The chain/residue/connection_type/comment cells are
// written verbatim; min/max distance cells are blank when the pointer is nil;
// confidence is the formatted float when set, `1` when nil. The caller writes
// the file only when len(rs) > 0.
func buildChai1Restraints(rs []chai1Restraint) string {
	var b strings.Builder
	b.WriteString("restraint_id,chainA,res_idxA,chainB,res_idxB," +
		"min_distance_angstrom,max_distance_angstrom,connection_type,confidence,comment\n")
	for i, r := range rs {
		minCell := ""
		if r.MinDistance != nil {
			minCell = strconv.FormatFloat(*r.MinDistance, 'g', -1, 64)
		}
		maxCell := ""
		if r.MaxDistance != nil {
			maxCell = strconv.FormatFloat(*r.MaxDistance, 'g', -1, 64)
		}
		confCell := "1"
		if r.Confidence != nil {
			confCell = strconv.FormatFloat(*r.Confidence, 'g', -1, 64)
		}
		fmt.Fprintf(&b, "restraint_%d,%s,%s,%s,%s,%s,%s,%s,%s,%s\n",
			i, r.ChainA, r.ResA, r.ChainB, r.ResB,
			minCell, maxCell, r.ConnectionType, confCell, r.Comment)
	}
	return b.String()
}

// chai1Args maps the model-parameter fields of a chai1Request to `chai-lab
// fold` CLI flags. A nil pointer omits its flag entirely so Chai-1 falls back
// to its own default. The infrastructure flags (--use-msa-server,
// --msa-directory, --use-templates-server, --template-hits-path,
// --constraint-path) are derived in Invoke, not here.
func chai1Args(req chai1Request) []string {
	var args []string
	type flag struct {
		name string
		val  *int
	}
	for _, f := range []flag{
		{"--num-trunk-recycles", req.NumTrunkRecycles},
		{"--num-diffn-timesteps", req.NumDiffnTimesteps},
		{"--num-diffn-samples", req.NumDiffnSamples},
		{"--num-trunk-samples", req.NumTrunkSamples},
		{"--recycle-msa-subsample", req.RecycleMSASubsample},
		{"--seed", req.Seed},
	} {
		if f.val != nil {
			args = append(args, f.name, strconv.Itoa(*f.val))
		}
	}
	return args
}

// chai1ScoresFromNPZ maps a decoded scores.model_idx_N.npz to fova's score
// keys (THE CONTRACT "Score keys"): the scalar members aggregate_score / ptm /
// iptm / has_inter_chain_clashes are taken as Data[0]; the 1-D per_chain_ptm
// member is flattened to chain_<i>_ptm. 2-D members and keys not in the table
// are ignored.
func chai1ScoresFromNPZ(npz map[string]npzValue) map[string]float64 {
	scores := map[string]float64{}
	for _, key := range []string{"aggregate_score", "ptm", "iptm", "has_inter_chain_clashes"} {
		if v, ok := npz[key]; ok && len(v.Shape) == 0 && len(v.Data) > 0 {
			scores[key] = v.Data[0]
		}
	}
	if v, ok := npz["per_chain_ptm"]; ok && len(v.Shape) == 1 {
		for i, d := range v.Data {
			scores[fmt.Sprintf("chain_%d_ptm", i)] = d
		}
	}
	return scores
}

// parseChai1Output collects every pred.model_idx_*.cif (and .pdb) under outDir,
// sorted by name, and returns one designOut per file. Each structure's Scores
// are read from its sibling scores.model_idx_N.npz — the `pred.` prefix is
// swapped for `scores.` and the extension for `.npz`. A missing or unreadable
// .npz yields an empty Scores map and no error.
func parseChai1Output(outDir string) ([]designOut, error) {
	var files []string
	err := filepath.Walk(outDir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		base := filepath.Base(p)
		low := strings.ToLower(base)
		if strings.HasPrefix(low, "pred.model_idx_") &&
			(strings.HasSuffix(low, ".cif") || strings.HasSuffix(low, ".pdb")) {
			files = append(files, p)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	var designs []designOut
	for _, p := range files {
		scores := map[string]float64{}
		base := filepath.Base(p)
		ext := filepath.Ext(base)
		stem := strings.TrimSuffix(base, ext)
		// pred.model_idx_N → scores.model_idx_N.npz
		scoresName := strings.Replace(stem, "pred.", "scores.", 1) + ".npz"
		scoresPath := filepath.Join(filepath.Dir(p), scoresName)
		if npz, err := readNPZ(scoresPath); err == nil {
			scores = chai1ScoresFromNPZ(npz)
		}
		designs = append(designs, designOut{
			Sequence:      map[string]string{},
			StructureFile: p,
			Scores:        scores,
		})
	}
	if len(designs) == 0 {
		return nil, fmt.Errorf("fold.chai1: no pred.model_idx_*.cif files found under %s", outDir)
	}
	return designs, nil
}

// chai1MSAIsPath reports whether a request's MSA field names a precomputed MSA
// directory (anything other than the two reserved keywords or the empty
// string, which is treated as "default").
func chai1MSAIsPath(msa string) bool {
	return msa != "" && msa != "default" && msa != "server"
}

// copyChai1Dir recursively copies the directory tree rooted at src into dst,
// recreating sub-directories and copying regular files. It is used to stage a
// precomputed MSA directory into the container workdir.
func copyChai1Dir(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return copyFile(p, target)
	})
}

// Invoke compiles the typed multi-entity request into a Chai-1 input FASTA
// (and a restraint CSV when restraints are present), stages any precomputed
// MSA directory and template-hits file into the workdir, runs `chai-lab fold`
// inside the tool image with the install-time weights cache bind-mounted at
// /models, and parses the produced structures — with their .npz confidence
// scores — into the {"designs":[...]} envelope. The image's ENTRYPOINT is
// ["chai-lab"], so Cmd starts with the `fold` subcommand.
func (chai1Adapter) Invoke(ctx context.Context, env AdapterEnv, request []byte) ([]byte, error) {
	var req chai1Request
	if err := json.Unmarshal(request, &req); err != nil {
		return nil, fmt.Errorf("fold.chai1: invalid request: %w", err)
	}
	// The tool's preflight is the primary input guard; this is the
	// backend-side backstop against a malformed request reaching the runtime.
	if len(req.Entities) == 0 {
		return nil, fmt.Errorf("fold.chai1: at least one entity is required")
	}
	if env.Registry == nil {
		return nil, fmt.Errorf("fold.chai1: adapter registry unavailable")
	}
	if env.Recipe.ImageTag == "" {
		return nil, fmt.Errorf("fold.chai1: container image is not configured (run /install chai1)")
	}

	// Stage any precomputed MSA directory and template-hits file into the
	// workdir so the container flags reference paths resolvable at /work.
	stagedMSADir := ""
	if chai1MSAIsPath(req.MSA) {
		base := filepath.Base(req.MSA)
		if err := copyChai1Dir(req.MSA, filepath.Join(env.WorkDir, base)); err != nil {
			return nil, fmt.Errorf("fold.chai1: stage MSA directory %q: %w", req.MSA, err)
		}
		stagedMSADir = base
	}
	stagedHits := ""
	if req.Templates != nil && req.Templates.HitsPath != "" {
		base := filepath.Base(req.Templates.HitsPath)
		if err := copyFile(req.Templates.HitsPath, filepath.Join(env.WorkDir, base)); err != nil {
			return nil, fmt.Errorf("fold.chai1: stage template hits %q: %w", req.Templates.HitsPath, err)
		}
		stagedHits = base
	}

	inputFASTA := filepath.Join(env.WorkDir, "in.fasta")
	if err := os.WriteFile(inputFASTA, []byte(buildChai1FASTA(req)), 0o644); err != nil {
		return nil, fmt.Errorf("fold.chai1: write input FASTA: %w", err)
	}
	if len(req.Restraints) > 0 {
		restraintsPath := filepath.Join(env.WorkDir, "restraints.csv")
		if err := os.WriteFile(restraintsPath, []byte(buildChai1Restraints(req.Restraints)), 0o644); err != nil {
			return nil, fmt.Errorf("fold.chai1: write restraint CSV: %w", err)
		}
	}
	outDir := filepath.Join(env.WorkDir, "out")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, err
	}
	env.Tick(0.05) // input staged

	rt := Detect()
	if !rt.Available() {
		return nil, fmt.Errorf("fold.chai1: no container runtime — install podman or docker")
	}
	if ok, _ := rt.ImageExists(env.Recipe.ImageTag); !ok {
		return nil, fmt.Errorf(
			"fold.chai1: image %s is missing — run /install chai1",
			env.Recipe.ImageTag)
	}

	// Chai-1 weights (~1.3 GB) are fetched at /install time into this cache.
	// A missing cache means install did not complete, so validate it exists
	// (os.Stat) rather than creating it (spec §2).
	modelsCache := ModelsRoot(env.Registry.Home(), "chai1")
	if info, err := os.Stat(modelsCache); err != nil || !info.IsDir() {
		return nil, fmt.Errorf(
			"fold.chai1: weights cache %s missing — run /install chai1",
			modelsCache)
	}

	// ENTRYPOINT is ["chai-lab"]; the subcommand `fold` plus the FASTA and
	// output dir are passed as positional args, followed by model-parameter
	// flags and the fova-derived infrastructure flags.
	cmd := []string{"fold", "/work/in.fasta", "/work/out"}
	cmd = append(cmd, chai1Args(req)...)
	if req.MSA == "server" {
		cmd = append(cmd, "--use-msa-server")
	}
	if stagedMSADir != "" {
		cmd = append(cmd, "--msa-directory", "/work/"+stagedMSADir)
	}
	if req.Templates != nil && req.Templates.Server {
		cmd = append(cmd, "--use-templates-server")
	}
	if stagedHits != "" {
		cmd = append(cmd, "--template-hits-path", "/work/"+stagedHits)
	}
	if len(req.Restraints) > 0 {
		cmd = append(cmd, "--constraint-path", "/work/restraints.csv")
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
		return nil, fmt.Errorf("fold.chai1: container run failed: %w", err)
	}
	env.Tick(0.95) // chai-lab fold done

	designs, err := parseChai1Output(outDir)
	if err != nil {
		return nil, err
	}

	// Optional: copy the top structure to the workspace-side path the caller
	// requested. env.WorkDir is removed when RunDesign returns, so without
	// this hop the structure_file path would dangle.
	if dst := strings.TrimSpace(req.SaveAs); dst != "" {
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return nil, fmt.Errorf("fold.chai1: stage save_as parent: %w", err)
		}
		if err := copyFile(designs[0].StructureFile, dst); err != nil {
			return nil, fmt.Errorf("fold.chai1: copy structure to %s: %w", dst, err)
		}
		designs[0].StructureFile = dst
	}
	return json.Marshal(designsEnvelope{Designs: designs})
}
