package local

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// IPSAEDefaults are the cutoffs recommended by the IPSAE README's AF3/Boltz1
// examples (10 Å PAE cutoff, 10 Å distance cutoff). These are the same values
// the AlphaFold-binder paper uses and match the in-container default; the
// score adapter has no reason to surface them to the agent at this point.
const (
	IPSAEPAECutoff  = "10"
	IPSAEDistCutoff = "10"
)

// RunIPSAE runs the installed ipsae container against an AF2/AF3/Boltz scores
// file and a predicted structure, returning the captured stdout.
//
// It bypasses (*Runner).Run entirely. The ipsae entrypoint takes positional
// args (`pae_file pdb_file pae_cutoff dist_cutoff`) — there is no clean way
// to express those as placeholders against the Runner's mount-the-workspace
// model, and the two input files live anywhere on the host. This helper:
//
//  1. Stages scoresJSON + structureFile into a fresh temp dir on the host
//     (preserving each file's extension — ipsae.py dispatches on .json vs
//     .npz for the PAE file, and .pdb vs .cif for the structure).
//  2. Bind-mounts that temp dir at /work inside the container.
//  3. Builds argv `python /opt/ipsae/ipsae.py /work/pae.<ext>
//     /work/structure.<ext> 10 10` and runs it with GPU disabled.
//  4. Captures the container's stdout+stderr (tee'd to `log` if non-nil) and
//     returns the captured text so the caller can parse the ipSAE score.
//
// The temp dir is removed before returning so callers don't accumulate state.
func RunIPSAE(ctx context.Context, scoresJSON, structureFile string, rec ToolRecipe, log io.Writer) (string, error) {
	if rec.ImageTag == "" {
		return "", fmt.Errorf("ipsae: recipe has no image_tag (was it installed via /install ipsae?)")
	}
	rt := Detect()
	if !rt.Available() {
		return "", fmt.Errorf("ipsae: no container runtime — install podman or docker")
	}

	workDir, err := os.MkdirTemp("", "fova-ipsae-*")
	if err != nil {
		return "", fmt.Errorf("ipsae: create work dir: %w", err)
	}
	defer os.RemoveAll(workDir)

	paeName := "pae" + extOrDefault(scoresJSON, ".json")
	structName := "structure" + extOrDefault(structureFile, ".pdb")
	if err := copyFile(scoresJSON, filepath.Join(workDir, paeName)); err != nil {
		return "", fmt.Errorf("ipsae: stage scores file %q: %w", scoresJSON, err)
	}
	if err := copyFile(structureFile, filepath.Join(workDir, structName)); err != nil {
		return "", fmt.Errorf("ipsae: stage structure file %q: %w", structureFile, err)
	}

	var buf bytes.Buffer
	var sink io.Writer = &buf
	if log != nil {
		sink = io.MultiWriter(&buf, log)
	}

	args := ContainerRunArgs{
		Name:  ipsaeContainerName(),
		Image: rec.ImageTag,
		Cmd: []string{
			"python", "/opt/ipsae/ipsae.py",
			"/work/" + paeName,
			"/work/" + structName,
			IPSAEPAECutoff,
			IPSAEDistCutoff,
		},
		Mounts:  []Mount{{HostPath: workDir, ContainerPath: "/work"}},
		GPU:     false, // ipsae is CPU-only; never pass --gpus all.
		Workdir: "/work",
		Log:     sink,
	}
	if _, err := rt.RunContainer(ctx, args); err != nil {
		return buf.String(), fmt.Errorf("ipsae: container run failed: %w", err)
	}
	return buf.String(), nil
}

// extOrDefault returns the lowercased extension of path (including the dot),
// or fallback if path has no extension. Used so the staged file inside /work
// keeps a meaningful suffix even when the caller passed a name without one.
func extOrDefault(path, fallback string) string {
	if e := strings.ToLower(filepath.Ext(path)); e != "" {
		return e
	}
	return fallback
}

// ipsaeContainerName generates a unique --name for the run. Mirrors the
// scheme used by Runner.runContainer's generateContainerName so cancellation
// telemetry stays consistent across both code paths.
func ipsaeContainerName() string {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return "fova_ipsae_" + hex.EncodeToString(b[:])
}
