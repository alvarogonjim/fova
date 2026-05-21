package local

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// init registers the BoltzGen spec-check adapter with the local backend.
func init() { registerAdapter(boltzGenCheckAdapter{}) }

// boltzGenCheckAdapter wires design.boltzgen_check to the installed BoltzGen
// tool. It runs `boltzgen check` — a cheap, GPU-free spec validation — and
// shares the boltzgen recipe (and image) with the design.boltzgen adapter.
type boltzGenCheckAdapter struct{}

func (boltzGenCheckAdapter) AgentTool() string { return "design.boltzgen_check" }
func (boltzGenCheckAdapter) Recipe() string    { return "boltzgen" }

// boltzGenCheckRequest mirrors design.boltzgen_check's boltzGenCheckInput: the
// (already workspace-resolved) absolute path to the spec YAML to validate.
type boltzGenCheckRequest struct {
	SpecPath string `json:"spec_path"`
}

// boltzGenCheckOutput is the structured result the adapter returns — the
// pinned {valid, errors, visualization_path} contract.
type boltzGenCheckOutput struct {
	Valid             bool     `json:"valid"`
	Errors            []string `json:"errors"`
	VisualizationPath string   `json:"visualization_path"`
}

// Invoke validates one agent-authored spec: it stages the spec plus every
// structure file the spec references into the container workdir (the same
// staging design.boltzgen uses, so check sees exactly what a run would), runs
// `boltzgen check /work/in.yaml` inside the container, and parses the exit
// status + output into {valid, errors, visualization_path}. A non-zero exit
// means an invalid spec — that is an expected outcome, not an adapter error,
// so it is reported as valid:false rather than returned as an error.
func (boltzGenCheckAdapter) Invoke(ctx context.Context, env AdapterEnv, request []byte) ([]byte, error) {
	var req boltzGenCheckRequest
	if err := json.Unmarshal(request, &req); err != nil {
		return nil, fmt.Errorf("design.boltzgen_check: invalid request: %w", err)
	}
	specPath := strings.TrimSpace(req.SpecPath)
	if specPath == "" {
		return nil, fmt.Errorf("design.boltzgen_check: spec_path is required (the BoltzGen specification YAML to validate)")
	}
	if info, err := os.Stat(specPath); err != nil || info.IsDir() {
		return nil, fmt.Errorf(
			"design.boltzgen_check: spec %q not found. Author the BoltzGen "+
				"specification YAML (see the boltzgen-spec skill) and pass its "+
				"workspace path.",
			specPath)
	}
	if env.Registry == nil {
		return nil, fmt.Errorf("design.boltzgen_check: adapter registry unavailable")
	}
	if env.Recipe.ImageTag == "" {
		// BoltzGen is container-only — there is no legacy venv-mode path.
		return nil, fmt.Errorf("design.boltzgen_check: boltzgen is not installed (run /install boltzgen)")
	}

	// Stage the spec + every structure file it references into env.WorkDir,
	// preserving the relative layout: BoltzGen resolves entities[].file.path
	// relative to the spec file's directory. Reuses the design.boltzgen helper.
	if err := stageBoltzGenSpec(specPath, env.WorkDir); err != nil {
		return nil, fmt.Errorf("design.boltzgen_check: stage spec: %w", err)
	}
	env.Tick(0.2) // spec + referenced files staged

	rt := Detect()
	if !rt.Available() {
		return nil, fmt.Errorf("design.boltzgen_check: no container runtime — install podman or docker")
	}
	if ok, _ := rt.ImageExists(env.Recipe.ImageTag); !ok {
		return nil, fmt.Errorf(
			"design.boltzgen_check: image %s is missing — run /install boltzgen",
			env.Recipe.ImageTag)
	}

	// Capture the container's stdout+stderr so the check result can be parsed
	// from it, while still teeing it to the job log.
	var captured bytes.Buffer
	logSink := io.MultiWriter(&captured, env.LogWriter())

	// `boltzgen check` validates the spec and renders a visualization mmCIF.
	// fova fixes the spec path at /work/in.yaml.
	runErr := func() error {
		_, err := rt.RunContainer(ctx, ContainerRunArgs{
			Image:   env.Recipe.ImageTag,
			Cmd:     []string{"check", "/work/in.yaml"},
			Mounts:  []Mount{{HostPath: env.WorkDir, ContainerPath: "/work"}},
			Workdir: "/work",
			Log:     logSink,
		})
		return err
	}()
	env.Tick(0.9) // boltzgen check done

	// A non-zero exit (runErr != nil) is BoltzGen reporting an invalid spec —
	// an expected outcome, surfaced as valid:false. Distinguish it from a real
	// failure to launch the container (no such image, runtime error): those
	// have no captured output and must propagate as adapter errors.
	result := boltzGenCheckOutput{
		Valid:             runErr == nil,
		Errors:            parseBoltzGenCheckErrors(captured.String()),
		VisualizationPath: findBoltzGenCheckVisualization(env.WorkDir, specPath),
	}
	if runErr != nil && captured.Len() == 0 {
		return nil, fmt.Errorf("design.boltzgen_check: container run failed: %w", runErr)
	}
	if !result.Valid && len(result.Errors) == 0 {
		// The check failed but emitted nothing parseable — still report it as
		// invalid with the raw exit context so the caller is not left blind.
		result.Errors = []string{fmt.Sprintf("boltzgen check failed: %v", runErr)}
	}
	if result.Errors == nil {
		result.Errors = []string{}
	}
	return json.Marshal(result)
}

// parseBoltzGenCheckErrors extracts error/warning lines from `boltzgen check`
// output. BoltzGen prints validation problems prefixed with markers like
// "Error:", "ERROR", "Traceback", or "ValidationError"; this collects any line
// that carries such a marker so the agent gets actionable feedback. When the
// spec is valid the output carries no such lines and an empty slice results.
func parseBoltzGenCheckErrors(out string) []string {
	var errs []string
	scanner := bufio.NewScanner(strings.NewReader(out))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		lower := strings.ToLower(line)
		if strings.Contains(lower, "error") ||
			strings.Contains(lower, "invalid") ||
			strings.Contains(lower, "traceback") ||
			strings.Contains(lower, "exception") ||
			strings.HasPrefix(lower, "failed") {
			errs = append(errs, line)
		}
	}
	return errs
}

// findBoltzGenCheckVisualization locates the mmCIF `boltzgen check` renders of
// the parsed spec. BoltzGen writes it next to the spec it is given (/work in
// the container, env.WorkDir on the host); the input spec is staged as
// in.yaml, so any other .cif appearing in WorkDir after the run that was not a
// staged input is the visualization. Returns "" if none is found.
func findBoltzGenCheckVisualization(workDir, specPath string) string {
	cifs, err := filepath.Glob(filepath.Join(workDir, "*.cif"))
	if err != nil || len(cifs) == 0 {
		return ""
	}
	// Collect the basenames of structure files staged from the spec so they
	// are not mistaken for the rendered visualization.
	staged := map[string]bool{}
	if body, rerr := os.ReadFile(specPath); rerr == nil {
		var spec boltzGenSpec
		if yerr := yaml.Unmarshal(body, &spec); yerr == nil {
			for _, ent := range spec.Entities {
				if ent.File == nil {
					continue
				}
				p := strings.TrimSpace(ent.File.Path)
				if p != "" {
					staged[filepath.Base(p)] = true
				}
			}
		}
	}
	// Prefer a file whose name hints at the visualization; otherwise take the
	// first non-staged .cif.
	var fallback string
	for _, cif := range cifs {
		base := filepath.Base(cif)
		if staged[base] {
			continue
		}
		name := strings.ToLower(base)
		if strings.Contains(name, "viz") ||
			strings.Contains(name, "check") ||
			strings.Contains(name, "in") {
			return cif
		}
		if fallback == "" {
			fallback = cif
		}
	}
	return fallback
}
