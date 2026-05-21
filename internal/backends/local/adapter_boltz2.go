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

// init registers the Boltz-2 fold adapter with the local backend.
func init() { registerAdapter(boltz2Adapter{}) }

// boltz2Adapter wires fold.boltz2 to the container-mode Boltz-2 image: it
// turns the agent's {sequences, save_as} request into a Boltz-format YAML,
// runs `boltz predict` inside the tool image with the host weights cache
// bind-mounted at /models, and returns the produced PDB(s) in the
// {"designs":[...]} envelope shared with the design adapters.
type boltz2Adapter struct{}

func (boltz2Adapter) AgentTool() string { return "fold.boltz2" }
func (boltz2Adapter) Recipe() string    { return "boltz2" }

// chainIDs unmarshals a JSON chain id given either as a string ("A") or a
// string array (["B","C"]) into a uniform []string.
type chainIDs []string

func (c *chainIDs) UnmarshalJSON(b []byte) error {
	var one string
	if err := json.Unmarshal(b, &one); err == nil {
		*c = chainIDs{one}
		return nil
	}
	var many []string
	if err := json.Unmarshal(b, &many); err != nil {
		return fmt.Errorf("id must be a string or a list of strings: %w", err)
	}
	*c = chainIDs(many)
	return nil
}

// boltz2Entity is one molecular component of the predicted complex.
type boltz2Entity struct {
	Type     string   `json:"type"`     // protein | dna | rna | ligand
	ID       chainIDs `json:"id"`       // one or more chain ids
	Sequence string   `json:"sequence"` // protein/dna/rna
	SMILES   string   `json:"smiles"`   // ligand (exclusive with ccd)
	CCD      string   `json:"ccd"`      // ligand (exclusive with smiles)
	MSA      string   `json:"msa"`      // "empty" | "server" | workspace path; protein/dna/rna
	Cyclic   bool     `json:"cyclic"`   // protein/dna/rna
}

// boltz2Request is the full fold.boltz2 input. Pointer fields are model
// parameters: nil ⇒ omit the CLI flag and let Boltz-2 use its own default.
type boltz2Request struct {
	Entities         []boltz2Entity `json:"entities"`
	RecyclingSteps   *int           `json:"recycling_steps"`
	SamplingSteps    *int           `json:"sampling_steps"`
	DiffusionSamples *int           `json:"diffusion_samples"`
	StepScale        *float64       `json:"step_scale"`
	OutputFormat     string         `json:"output_format"` // "pdb" (default) | "mmcif"
	SaveAs           string         `json:"save_as"`
}

// buildBoltz2YAML renders the Boltz-2 v1 input document for any entity mix.
// One list item per entity in input order; the item key is the entity type.
// `id` is a scalar for a single chain or a flow list ([B, C]) for several.
// `sequence` is emitted for protein/dna/rna; `smiles`/`ccd` for ligand. The
// `msa` line is emitted for protein/dna/rna when MSA is "" or "empty" (as
// `msa: empty`) or a staged path; it is omitted when MSA is "server" so
// `--use_msa_server` fills it. `cyclic: true` is emitted only when set.
func buildBoltz2YAML(req boltz2Request) string {
	var b strings.Builder
	b.WriteString("version: 1\n")
	b.WriteString("sequences:\n")
	for _, e := range req.Entities {
		fmt.Fprintf(&b, "  - %s:\n", e.Type)
		if len(e.ID) == 1 {
			fmt.Fprintf(&b, "      id: %s\n", e.ID[0])
		} else {
			fmt.Fprintf(&b, "      id: [%s]\n", strings.Join(e.ID, ", "))
		}
		switch e.Type {
		case "ligand":
			if e.SMILES != "" {
				fmt.Fprintf(&b, "      smiles: %s\n", e.SMILES)
			} else if e.CCD != "" {
				fmt.Fprintf(&b, "      ccd: %s\n", e.CCD)
			}
		default: // protein / dna / rna
			fmt.Fprintf(&b, "      sequence: %s\n", e.Sequence)
			// `msa: empty` or a staged path is emitted as given; an unset MSA
			// ("") omits the line entirely, as does "server" (so
			// --use_msa_server fills it).
			if e.MSA != "" && e.MSA != "server" {
				fmt.Fprintf(&b, "      msa: %s\n", e.MSA)
			}
			if e.Cyclic {
				b.WriteString("      cyclic: true\n")
			}
		}
	}
	return b.String()
}

// boltz2Args maps the model-parameter fields of a boltz2Request to `boltz
// predict` CLI flags. A nil pointer omits its flag entirely so Boltz-2 falls
// back to its own default. Infrastructure flags (--out_dir, --cache,
// --output_format, --no_kernels, --override, --use_msa_server) are fixed or
// derived in Invoke, not here.
func boltz2Args(req boltz2Request) []string {
	var args []string
	if req.RecyclingSteps != nil {
		args = append(args, "--recycling_steps", strconv.Itoa(*req.RecyclingSteps))
	}
	if req.SamplingSteps != nil {
		args = append(args, "--sampling_steps", strconv.Itoa(*req.SamplingSteps))
	}
	if req.DiffusionSamples != nil {
		args = append(args, "--diffusion_samples", strconv.Itoa(*req.DiffusionSamples))
	}
	if req.StepScale != nil {
		args = append(args, "--step_scale", strconv.FormatFloat(*req.StepScale, 'g', -1, 64))
	}
	return args
}

// boltz2Confidence is the subset of Boltz-2's confidence_in_model_<N>.json
// that fova ingests into a design's Scores. All pLDDT/pTM-family values are
// in [0, 1] (higher is better); pde/ipde are in Ångström (lower is better).
type boltz2Confidence struct {
	ConfidenceScore float64            `json:"confidence_score"`
	PTM             float64            `json:"ptm"`
	IPTM            float64            `json:"iptm"`
	LigandIPTM      float64            `json:"ligand_iptm"`
	ProteinIPTM     float64            `json:"protein_iptm"`
	ComplexPLDDT    float64            `json:"complex_plddt"`
	ComplexIPLDDT   float64            `json:"complex_iplddt"`
	ComplexPDE      float64            `json:"complex_pde"`
	ComplexIPDE     float64            `json:"complex_ipde"`
	ChainsPTM       map[string]float64 `json:"chains_ptm"`
}

// boltz2Scores reads the confidence JSON sibling of a structure file and maps
// it to the score keys THE CONTRACT pins. A missing or unparseable file yields
// an empty (non-nil) map and no error — a successful prediction without
// confidence still returns the structure.
func boltz2Scores(structurePath string) map[string]float64 {
	scores := map[string]float64{}
	dir := filepath.Dir(structurePath)
	ext := filepath.Ext(structurePath)
	stem := strings.TrimSuffix(filepath.Base(structurePath), ext)
	// Structure file is in_model_<N>.<ext>; the sibling confidence file is
	// confidence_in_model_<N>.json.
	confPath := filepath.Join(dir, "confidence_"+stem+".json")
	body, err := os.ReadFile(confPath)
	if err != nil {
		return scores
	}
	var c boltz2Confidence
	if err := json.Unmarshal(body, &c); err != nil {
		return scores
	}
	scores["plddt"] = c.ComplexPLDDT
	scores["ptm"] = c.PTM
	scores["iptm"] = c.IPTM
	scores["confidence_score"] = c.ConfidenceScore
	scores["ligand_iptm"] = c.LigandIPTM
	scores["protein_iptm"] = c.ProteinIPTM
	scores["complex_iplddt"] = c.ComplexIPLDDT
	scores["complex_pde"] = c.ComplexPDE
	scores["complex_ipde"] = c.ComplexIPDE
	for k, v := range c.ChainsPTM {
		scores["chain_"+k+"_ptm"] = v
	}
	return scores
}

// parseBoltz2Output collects every PDB/CIF under outDir (Boltz writes per-input
// subdirectories like predictions/<stem>/<stem>_model_0.pdb), returning one
// designOut per file sorted by path. Each model's Scores are read from its
// sibling confidence_<stem>.json; a model without a confidence file simply
// gets an empty Scores map. The structure_file path is host-side so the
// caller can open it directly.
func parseBoltz2Output(outDir string) ([]designOut, error) {
	var structures []string
	err := filepath.Walk(outDir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		low := strings.ToLower(p)
		if strings.HasSuffix(low, ".pdb") || strings.HasSuffix(low, ".cif") {
			structures = append(structures, p)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(structures)
	var designs []designOut
	for _, p := range structures {
		designs = append(designs, designOut{
			Sequence:      map[string]string{},
			StructureFile: p,
			Scores:        boltz2Scores(p),
		})
	}
	if len(designs) == 0 {
		return nil, fmt.Errorf("fold.boltz2: no PDB/CIF files found under %s", outDir)
	}
	return designs, nil
}

// Invoke writes the YAML, runs `boltz predict` inside the tool image, and
// parses the produced PDB(s) into the {"designs":[...]} envelope. The image's
// ENTRYPOINT is ["boltz", "predict"], so Cmd starts with the YAML path.
func (boltz2Adapter) Invoke(ctx context.Context, env AdapterEnv, request []byte) ([]byte, error) {
	var req boltz2Request
	if err := json.Unmarshal(request, &req); err != nil {
		return nil, fmt.Errorf("fold.boltz2: invalid request: %w", err)
	}
	if len(req.Entities) == 0 {
		return nil, fmt.Errorf("fold.boltz2: at least one entity is required")
	}
	if env.Registry == nil {
		return nil, fmt.Errorf("fold.boltz2: adapter registry unavailable")
	}
	if env.Recipe.ImageTag == "" {
		return nil, fmt.Errorf("fold.boltz2: container image is not configured (run /install boltz2)")
	}

	inputYAML := filepath.Join(env.WorkDir, "in.yaml")
	if err := os.WriteFile(inputYAML, []byte(buildBoltz2YAML(req)), 0o644); err != nil {
		return nil, fmt.Errorf("fold.boltz2: write input YAML: %w", err)
	}
	outDir := filepath.Join(env.WorkDir, "out")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, err
	}
	env.Tick(0.05) // input staged

	rt := Detect()
	if !rt.Available() {
		return nil, fmt.Errorf("fold.boltz2: no container runtime — install podman or docker")
	}
	if ok, _ := rt.ImageExists(env.Recipe.ImageTag); !ok {
		return nil, fmt.Errorf(
			"fold.boltz2: image %s is missing — run /install boltz2",
			env.Recipe.ImageTag)
	}

	mounts := []Mount{{HostPath: env.WorkDir, ContainerPath: "/work"}}
	// The weights cache is a bind-mount source; Boltz-2 downloads its weights
	// into it at runtime, so an empty directory is the correct pre-state.
	// Create it if absent rather than failing.
	modelsCache := ModelsRoot(env.Registry.Home(), "boltz2")
	if err := os.MkdirAll(modelsCache, 0o755); err != nil {
		return nil, fmt.Errorf("fold.boltz2: create weights cache %s: %w", modelsCache, err)
	}
	mounts = append(mounts, Mount{HostPath: modelsCache, ContainerPath: "/models"})

	// ENTRYPOINT is ["boltz", "predict"]; Cmd carries the YAML and flags.
	// --no_kernels is required on sm_121 (GB10) per upstream issue #663.
	// --override prevents Boltz from skipping the run when /work/out exists
	// (the workdir is fresh per call, but rerunning a cached subdir would
	// be a silent no-op without it).
	cmd := []string{
		"/work/in.yaml",
		"--out_dir", "/work/out",
		"--cache", "/models",
		"--output_format", "pdb",
		"--no_kernels",
		"--override",
	}
	if _, err := rt.RunContainer(ctx, ContainerRunArgs{
		Image:   env.Recipe.ImageTag,
		Cmd:     cmd,
		GPU:     env.Recipe.GPU,
		Mounts:  mounts,
		Workdir: "/work",
		Log:     env.LogWriter(),
	}); err != nil {
		return nil, fmt.Errorf("fold.boltz2: container run failed: %w", err)
	}
	env.Tick(0.95) // boltz predict done

	designs, err := parseBoltz2Output(outDir)
	if err != nil {
		return nil, err
	}

	// Optional: copy the first structure to the workspace-side path the caller
	// requested. The temp WorkDir is removed when RunDesign returns, so without
	// this hop the structure_file path would dangle.
	if dst := strings.TrimSpace(req.SaveAs); dst != "" {
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return nil, fmt.Errorf("fold.boltz2: stage save_as parent: %w", err)
		}
		if err := copyFile(designs[0].StructureFile, dst); err != nil {
			return nil, fmt.Errorf("fold.boltz2: copy structure to %s: %w", dst, err)
		}
		designs[0].StructureFile = dst
	}
	return json.Marshal(designsEnvelope{Designs: designs})
}
