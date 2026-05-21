package local

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// init registers the Chai-1 fold adapter with the local backend.
func init() { registerAdapter(chai1Adapter{}) }

// chai1Adapter wires fold.chai1 to the container-mode Chai-1 image: it turns
// the agent's {sequences, save_as} request into a multi-chain FASTA, runs
// `chai-lab fold` inside the tool image with the host weights cache
// bind-mounted at /models (CHAI_DOWNLOADS_DIR is set to /models inside the
// image), and returns the produced CIF/PDB(s) in the {"designs":[...]}
// envelope.
type chai1Adapter struct{}

func (chai1Adapter) AgentTool() string { return "fold.chai1" }
func (chai1Adapter) Recipe() string    { return "chai1" }

// chai1Request mirrors fold.boltz2's request shape: chain id → sequence plus
// an optional workspace-relative save_as path.
type chai1Request struct {
	Sequences map[string]string `json:"sequences"`
	SaveAs    string            `json:"save_as"`
}

// writeChai1FASTA renders a multi-chain FASTA file. Header format follows
// chai-lab's convention: `>protein|name=chain_<id>` so the upstream parser
// keys each record by chain id without confusing it for a UniProt accession.
func writeChai1FASTA(path string, seqs map[string]string) error {
	keys := make([]string, 0, len(seqs))
	for k := range seqs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, id := range keys {
		fmt.Fprintf(&b, ">protein|name=chain_%s\n%s\n", id, seqs[id])
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

// parseChai1Output collects every CIF and PDB under outDir, returning one
// designOut per file. Chai-1 emits CIFs by default; we accept both to keep
// the parser tolerant of an upstream flag flip. Sequence and scores are
// empty — Chai's confidence JSON sidecar isn't surfaced in v0.7.
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
	if len(req.Sequences) == 0 {
		return nil, fmt.Errorf("fold.chai1: at least one chain is required in \"sequences\"")
	}
	for id, seq := range req.Sequences {
		if strings.TrimSpace(seq) == "" {
			return nil, fmt.Errorf("fold.chai1: chain %q has an empty sequence", id)
		}
	}
	if env.Registry == nil {
		return nil, fmt.Errorf("fold.chai1: adapter registry unavailable")
	}
	if env.Recipe.ImageTag == "" {
		return nil, fmt.Errorf("fold.chai1: container image is not configured (run /install chai1)")
	}

	inputFASTA := filepath.Join(env.WorkDir, "in.fasta")
	if err := writeChai1FASTA(inputFASTA, req.Sequences); err != nil {
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
	// CHAI_DOWNLOADS_DIR is baked into the image at /models; Chai-1 downloads
	// its weights into the bind-mounted cache at runtime, so an empty
	// directory is the correct pre-state. Create it if absent rather than
	// failing.
	modelsCache := ModelsRoot(env.Registry.Home(), "chai1")
	if err := os.MkdirAll(modelsCache, 0o755); err != nil {
		return nil, fmt.Errorf("fold.chai1: create weights cache %s: %w", modelsCache, err)
	}
	mounts = append(mounts, Mount{HostPath: modelsCache, ContainerPath: "/models"})

	// ENTRYPOINT is ["chai-lab"]; the subcommand `fold` plus the FASTA and
	// output dir are passed as positional args.
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

	// Optional: copy the first structure to the workspace-side path the
	// caller requested. env.WorkDir is removed when RunDesign returns.
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
