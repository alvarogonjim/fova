package local

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// init registers the Chai-1 fold adapter with the local backend.
func init() { registerAdapter(chai1Adapter{}) }

// chai1Adapter wires fold.chai1 to the container-mode Chai-1 image: it
// compiles the agent's typed multi-entity request into a Chai-1 input FASTA,
// runs `chai-lab fold` inside the tool image with the host weights cache
// bind-mounted at /models (CHAI_DOWNLOADS_DIR is set to /models inside the
// image), and returns the produced CIF/PDB(s) in the {"designs":[...]}
// envelope.
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

// parseChai1Output collects every CIF and PDB under outDir, returning one
// designOut per file. Chai-1 emits CIFs by default; we accept both to keep
// the parser tolerant of an upstream flag flip.
func parseChai1Output(outDir string) ([]designOut, error) {
	var files []string
	err := filepath.Walk(outDir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		low := strings.ToLower(p)
		if strings.HasSuffix(low, ".cif") || strings.HasSuffix(low, ".pdb") {
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
		designs = append(designs, designOut{
			Sequence:      map[string]string{},
			StructureFile: p,
			Scores:        map[string]float64{},
		})
	}
	if len(designs) == 0 {
		return nil, fmt.Errorf("fold.chai1: no CIF/PDB files found under %s", outDir)
	}
	return designs, nil
}

// Invoke writes the FASTA, runs `chai-lab fold` inside the tool image, and
// parses the produced structure file(s) into the {"designs":[...]} envelope.
// The image's ENTRYPOINT is ["chai-lab"], so Cmd starts with "fold".
func (chai1Adapter) Invoke(ctx context.Context, env AdapterEnv, request []byte) ([]byte, error) {
	var req chai1Request
	if err := json.Unmarshal(request, &req); err != nil {
		return nil, fmt.Errorf("fold.chai1: invalid request: %w", err)
	}
	if len(req.Entities) == 0 {
		return nil, fmt.Errorf("fold.chai1: at least one entity is required")
	}
	if env.Registry == nil {
		return nil, fmt.Errorf("fold.chai1: adapter registry unavailable")
	}
	if env.Recipe.ImageTag == "" {
		return nil, fmt.Errorf("fold.chai1: container image is not configured (run /install chai1)")
	}

	inputFASTA := filepath.Join(env.WorkDir, "in.fasta")
	if err := os.WriteFile(inputFASTA, []byte(buildChai1FASTA(req)), 0o644); err != nil {
		return nil, fmt.Errorf("fold.chai1: write input FASTA: %w", err)
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

	mounts := []Mount{{HostPath: env.WorkDir, ContainerPath: "/work"}}
	modelsCache := ModelsRoot(env.Registry.Home(), "chai1")
	if err := os.MkdirAll(modelsCache, 0o755); err != nil {
		return nil, fmt.Errorf("fold.chai1: create weights cache %s: %w", modelsCache, err)
	}
	mounts = append(mounts, Mount{HostPath: modelsCache, ContainerPath: "/models"})

	cmd := []string{"fold", "/work/in.fasta", "/work/out"}
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
