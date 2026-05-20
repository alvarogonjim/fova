package local

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestInstallerDryRun(t *testing.T) {
	reg, _ := LoadRegistry(t.TempDir())
	inst := NewInstaller(reg)
	steps, err := inst.DryRun("legacy_fixture")
	if err != nil {
		t.Fatalf("DryRun: %v", err)
	}
	if len(steps) == 0 {
		t.Fatal("DryRun returned no steps")
	}
	if !strings.Contains(steps[0], "git clone") {
		t.Errorf("first step = %q", steps[0])
	}
}

func TestInstallerInstallRunsStepsAndWritesLock(t *testing.T) {
	home := t.TempDir()
	reg, _ := LoadRegistry(home)
	inst := NewInstaller(reg)

	var ran []string
	inst.run = func(ctx context.Context, dir, command string, log io.Writer) (string, error) {
		ran = append(ran, command)
		return "ok", nil
	}
	inst.ensureUV = func(ctx context.Context) error { return nil } // skip real uv

	if err := inst.Install(context.Background(), "legacy_fixture"); err != nil {
		t.Fatalf("Install: %v", err)
	}
	if len(ran) < 1 {
		t.Fatalf("expected at least 1 step run, got %d: %v", len(ran), ran)
	}
	st := inst.Status("legacy_fixture")
	if !st.Installed {
		t.Error("Status should report ipsae installed after Install")
	}
	if st.Version == "" {
		t.Errorf("Status version is empty")
	}
}

func TestInstallerInstallFailureNamesStep(t *testing.T) {
	home := t.TempDir()
	reg, _ := LoadRegistry(home)
	inst := NewInstaller(reg)
	inst.ensureUV = func(ctx context.Context) error { return nil }
	inst.run = func(ctx context.Context, dir, command string, log io.Writer) (string, error) {
		if strings.Contains(command, "uv venv") {
			return "boom", errors.New("venv failed")
		}
		return "ok", nil
	}
	err := inst.Install(context.Background(), "legacy_fixture")
	if err == nil {
		t.Fatal("expected install to fail")
	}
	if !strings.Contains(err.Error(), "step 2") {
		t.Errorf("error should name the failing step, got: %v", err)
	}
	if inst.Status("legacy_fixture").Installed {
		t.Error("a failed install must not be marked installed")
	}
}

func TestInstallerRemove(t *testing.T) {
	home := t.TempDir()
	reg, _ := LoadRegistry(home)
	inst := NewInstaller(reg)
	inst.ensureUV = func(ctx context.Context) error { return nil }
	inst.run = func(ctx context.Context, dir, command string, log io.Writer) (string, error) {
		return "", nil
	}
	if err := inst.Install(context.Background(), "legacy_fixture"); err != nil {
		t.Fatal(err)
	}
	if err := inst.Remove(context.Background(), "legacy_fixture"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if inst.Status("legacy_fixture").Installed {
		t.Error("Status should report ipsae not installed after Remove")
	}
}

func TestUVPath(t *testing.T) {
	// UVPath must not panic and returns ok=false cleanly when uv is absent.
	_, _ = UVPath()
}

func TestInstallerInstallLoggedWritesStepOutput(t *testing.T) {
	home := t.TempDir()
	reg, _ := LoadRegistry(home)
	inst := NewInstaller(reg)
	inst.ensureUV = func(ctx context.Context) error { return nil }
	inst.run = func(ctx context.Context, dir, command string, log io.Writer) (string, error) {
		// The new bashRunner contract: it tees stdout+stderr to log AND
		// returns the same content as a string. The stub mirrors that.
		_, _ = log.Write([]byte("step-output-here"))
		return "step-output-here", nil
	}

	var log bytes.Buffer
	if err := inst.InstallLogged(context.Background(), "legacy_fixture", &log); err != nil {
		t.Fatalf("InstallLogged: %v", err)
	}
	if !strings.Contains(log.String(), "step-output-here") {
		t.Errorf("log missing step output, got: %q", log.String())
	}
	if !inst.Status("legacy_fixture").Installed {
		t.Error("InstallLogged should mark the tool installed")
	}
}

// TestBashRunnerTeesOutputAndReturnsString is the Bug 2 contract test for the
// production runner: stdout+stderr must reach log live AND match the returned
// string.
func TestBashRunnerTeesOutputAndReturnsString(t *testing.T) {
	var buf bytes.Buffer
	out, err := bashRunner(context.Background(), "",
		`printf 'one\n'; printf 'two\n' 1>&2`, &buf)
	if err != nil {
		t.Fatalf("bashRunner: %v", err)
	}
	if !strings.Contains(out, "one") || !strings.Contains(out, "two") {
		t.Errorf("returned string missing stdout/stderr: %q", out)
	}
	if !strings.Contains(buf.String(), "one") || !strings.Contains(buf.String(), "two") {
		t.Errorf("log buffer missing stdout/stderr: %q", buf.String())
	}
	if buf.String() != out {
		t.Errorf("log buffer (%q) and returned string (%q) should match", buf.String(), out)
	}
}

// TestBashRunnerNilLogIsDiscarded confirms a nil log writer is treated as
// io.Discard so adapters that don't care about logs don't have to set one.
func TestBashRunnerNilLogIsDiscarded(t *testing.T) {
	out, err := bashRunner(context.Background(), "", `echo hello`, nil)
	if err != nil {
		t.Fatalf("bashRunner with nil log: %v", err)
	}
	if !strings.Contains(out, "hello") {
		t.Errorf("returned string should still contain stdout: %q", out)
	}
}

// TestBashRunnerStreamsBeforeProcessExits proves Bug 2's headline fix: log
// receives stdout WHILE the process is still running, not only after it exits.
// We launch `echo first; sleep 0.4; echo second` and check the log holds
// "first" before the process is done.
func TestBashRunnerStreamsBeforeProcessExits(t *testing.T) {
	first := make(chan struct{}, 1)
	w := writerFunc(func(p []byte) (int, error) {
		if strings.Contains(string(p), "first") {
			select {
			case first <- struct{}{}:
			default:
			}
		}
		return len(p), nil
	})
	done := make(chan error, 1)
	go func() {
		_, err := bashRunner(context.Background(), "",
			`echo first; sleep 0.4; echo second`, w)
		done <- err
	}()
	select {
	case <-first:
		// got "first" while the process is still asleep — proof of live streaming.
	case err := <-done:
		t.Fatalf("process exited before any streamed line was observed: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("bashRunner: %v", err)
	}
}

// writerFunc adapts a function to io.Writer.
type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }

// TestBashRunnerCancellationKillsChildProcessGroup proves Bug 7's fix: when
// the context backing bashRunner is cancelled, not only the bash leader but
// also any descendants (here a `sleep 30 &` child) terminate promptly,
// instead of being reparented to PID 1 and lingering.
//
// We spawn `sleep 30 & echo $!; wait`, capture the sleep PID from the live
// log stream, cancel, then poll `kill -0 <pid>` for up to 6 s. If process
// groups aren't being signalled, the sleep survives the full 30 s and the
// poll keeps succeeding past the deadline — the test fails.
func TestBashRunnerCancellationKillsChildProcessGroup(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Buffered channel + custom writer so we can react to the first line
	// (the sleep PID) without racing the runner's own bytes.Buffer.
	pidCh := make(chan int, 1)
	logW := writerFunc(func(p []byte) (int, error) {
		for _, line := range strings.Split(string(p), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			if n, err := strconv.Atoi(line); err == nil {
				select {
				case pidCh <- n:
				default:
				}
			}
		}
		return len(p), nil
	})

	type runResult struct {
		out string
		err error
	}
	done := make(chan runResult, 1)
	go func() {
		// `sleep 30 & echo $!; wait` prints the sleep PID, then waits.
		// `wait` (no args) blocks until all background jobs finish, so
		// the bash leader doesn't exit on its own before we cancel.
		out, err := bashRunner(ctx, "", `sleep 30 & echo $!; wait`, logW)
		done <- runResult{out: out, err: err}
	}()

	// Wait for the sleep PID to appear in the log (it should be within
	// milliseconds — `echo` runs immediately after backgrounding sleep).
	var sleepPID int
	select {
	case sleepPID = <-pidCh:
	case <-time.After(3 * time.Second):
		cancel()
		<-done
		t.Fatal("never observed sleep PID on the log stream")
	}
	if sleepPID <= 1 {
		t.Fatalf("nonsense sleep PID %d captured from log", sleepPID)
	}

	// Sanity check: the sleep process IS alive right now.
	if err := signalAlive(sleepPID); err != nil {
		t.Fatalf("sleep pid %d not alive before cancel: %v", sleepPID, err)
	}

	// Pull the trigger.
	cancel()

	// Wait up to 6 s for the sleep child to vanish. Without Bug 7's fix
	// this poll would still find the process alive (reparented to PID 1
	// and running its remaining ~28 s sleep).
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		if err := signalAlive(sleepPID); err != nil {
			break // process is gone
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err := signalAlive(sleepPID); err == nil {
		t.Fatalf("sleep pid %d still alive 6s after cancel — process group not killed", sleepPID)
	}

	// And the runner itself should have returned an error reflecting the
	// cancellation (context.Canceled, or exec's "signal: terminated" once
	// SIGTERM lands — either is fine; the contract is non-nil err).
	select {
	case res := <-done:
		if res.err == nil {
			t.Fatal("bashRunner returned nil err after context cancellation; expected an error")
		}
	case <-time.After(6 * time.Second):
		t.Fatal("bashRunner did not return within 6s of cancellation")
	}
}

// installerWithContainerRecipe returns an installer whose registry has one
// container-mode tool overlaid on the default legacy registry. testRec is
// the recipe; testName is its key.
func installerWithContainerRecipe(t *testing.T, testName string, testRec ToolRecipe) *Installer {
	t.Helper()
	home := t.TempDir()
	reg, err := LoadRegistry(home)
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	testRec.Name = testName
	reg.tools[testName] = testRec
	inst := NewInstaller(reg)
	inst.runtime = Runtime{Bin: "/usr/bin/podman", Kind: RuntimePodman}
	return inst
}

func TestInstallerContainerModeBuildsImage(t *testing.T) {
	rec := ToolRecipe{
		ImageTag:      "fova/proteinmpnn:v1.0.1",
		Containerfile: "proteinmpnn.Containerfile",
		Entrypoint:    "python /opt/proteinmpnn/run.py",
		GPU:           true,
	}
	inst := installerWithContainerRecipe(t, "proteinmpnn-c", rec)

	// Pretend the Containerfile is embedded by stubbing the loader.
	oldLoad := loadContainerfile
	defer func() { loadContainerfile = oldLoad }()
	loadContainerfile = func(name string) ([]byte, error) {
		return []byte("FROM " + BaseImage + "\n"), nil
	}

	var calls [][]string
	oldRun := runCmd
	defer func() { runCmd = oldRun }()
	runCmd = func(cmd *exec.Cmd) error {
		calls = append(calls, append([]string(nil), cmd.Args...))
		return nil
	}
	oldOut := runCmdOutput
	defer func() { runCmdOutput = oldOut }()
	// First check: base image NOT present → triggers Pull.
	imageStates := map[string]bool{}
	runCmdOutput = func(cmd *exec.Cmd) ([]byte, error) {
		// `<bin> image inspect <image>` lookup.
		image := cmd.Args[len(cmd.Args)-1]
		if imageStates[image] {
			return []byte("[{}]\n"), nil
		}
		return nil, &exec.ExitError{}
	}

	if err := inst.Install(context.Background(), "proteinmpnn-c"); err != nil {
		t.Fatalf("Install: %v", err)
	}

	// Expect: pull BaseImage, then build the tool image.
	var sawPull, sawBuild bool
	for _, a := range calls {
		joined := strings.Join(a, " ")
		if strings.Contains(joined, "pull "+BaseImage) {
			sawPull = true
		}
		if strings.Contains(joined, "build -t fova/proteinmpnn:v1.0.1") {
			sawBuild = true
		}
	}
	if !sawPull {
		t.Errorf("expected pull of BaseImage, got calls: %v", calls)
	}
	if !sawBuild {
		t.Errorf("expected build of tool image, got calls: %v", calls)
	}

	// Status now branches on image presence; flip the stub to claim it's there.
	imageStates["fova/proteinmpnn:v1.0.1"] = true
	st := inst.Status("proteinmpnn-c")
	if !st.Installed {
		t.Error("Status should report container-mode tool as Installed when image is present")
	}
	if st.Image != "fova/proteinmpnn:v1.0.1" {
		t.Errorf("Status.Image = %q, want fova/proteinmpnn:v1.0.1", st.Image)
	}
}

func TestInstallerContainerModeSkipsPullWhenBaseCached(t *testing.T) {
	rec := ToolRecipe{
		ImageTag:      "fova/x:v1",
		Containerfile: "x.Containerfile",
		Entrypoint:    "python /opt/x/run.py",
	}
	inst := installerWithContainerRecipe(t, "xtool", rec)
	oldLoad := loadContainerfile
	defer func() { loadContainerfile = oldLoad }()
	loadContainerfile = func(name string) ([]byte, error) {
		return []byte("FROM " + BaseImage + "\n"), nil
	}
	var calls [][]string
	oldRun := runCmd
	defer func() { runCmd = oldRun }()
	runCmd = func(cmd *exec.Cmd) error {
		calls = append(calls, append([]string(nil), cmd.Args...))
		return nil
	}
	oldOut := runCmdOutput
	defer func() { runCmdOutput = oldOut }()
	runCmdOutput = func(cmd *exec.Cmd) ([]byte, error) {
		image := cmd.Args[len(cmd.Args)-1]
		// Base image cached; tool image absent.
		if image == BaseImage {
			return []byte("[{}]\n"), nil
		}
		return nil, &exec.ExitError{}
	}
	if err := inst.Install(context.Background(), "xtool"); err != nil {
		t.Fatalf("Install: %v", err)
	}
	for _, a := range calls {
		if strings.Contains(strings.Join(a, " "), "pull "+BaseImage) {
			t.Errorf("base image already cached — pull should be skipped: %v", calls)
		}
	}
}

func TestInstallerLegacyModeStillUsesInstallSteps(t *testing.T) {
	// The legacy ipsae recipe has no ImageTag, so Install must take the bash path.
	home := t.TempDir()
	reg, _ := LoadRegistry(home)
	inst := NewInstaller(reg)
	inst.ensureUV = func(ctx context.Context) error { return nil }
	var ran []string
	inst.run = func(ctx context.Context, dir, command string, log io.Writer) (string, error) {
		ran = append(ran, command)
		return "ok", nil
	}
	if err := inst.Install(context.Background(), "legacy_fixture"); err != nil {
		t.Fatalf("Install: %v", err)
	}
	if len(ran) == 0 {
		t.Error("legacy mode should have invoked bash steps")
	}
}

func TestInstallerUninstallContainerModeRemovesImageButNotWeights(t *testing.T) {
	rec := ToolRecipe{
		ImageTag:      "fova/rfd:v1",
		Containerfile: "rfdiffusion.Containerfile",
		Entrypoint:    "python /opt/rfdiffusion/run.py",
		WeightsPaths:  []string{"/models/rfdiffusion"},
	}
	inst := installerWithContainerRecipe(t, "rfdtool", rec)

	// Pre-create the per-tool models dir so we can assert it's left alone.
	weightsDir := ModelsRoot(inst.registry.Home(), "rfdtool")
	if err := os.MkdirAll(weightsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(weightsDir, "Base_ckpt.pt"), []byte("weights"), 0o644); err != nil {
		t.Fatal(err)
	}

	var calls [][]string
	oldRun := runCmd
	defer func() { runCmd = oldRun }()
	runCmd = func(cmd *exec.Cmd) error {
		calls = append(calls, append([]string(nil), cmd.Args...))
		return nil
	}

	if err := inst.Uninstall(context.Background(), "rfdtool"); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}

	var sawRmi bool
	for _, a := range calls {
		if strings.Join(a[1:], " ") == "rmi fova/rfd:v1" {
			sawRmi = true
		}
	}
	if !sawRmi {
		t.Errorf("expected `rmi fova/rfd:v1`, got calls: %v", calls)
	}

	// Weights dir survives.
	if _, err := os.Stat(filepath.Join(weightsDir, "Base_ckpt.pt")); err != nil {
		t.Errorf("uninstall must not delete the weights dir: %v", err)
	}
}

func TestInstallerUninstallContainerModeIgnoresRmiFailure(t *testing.T) {
	rec := ToolRecipe{
		ImageTag:      "fova/x:v1",
		Containerfile: "x.Containerfile",
		Entrypoint:    "x",
	}
	inst := installerWithContainerRecipe(t, "xtool", rec)
	oldRun := runCmd
	defer func() { runCmd = oldRun }()
	runCmd = func(cmd *exec.Cmd) error {
		return errors.New("rmi failed")
	}
	// Uninstall is best-effort for the rmi step.
	if err := inst.Uninstall(context.Background(), "xtool"); err != nil {
		t.Errorf("Uninstall should swallow rmi errors, got: %v", err)
	}
}

// TestBindcraftContainerfileEmbedded asserts the embedded Containerfile for
// bindcraft is reachable through the real loadContainerfile seam (no stub),
// covers the locked pip list from upstream install_bindcraft.sh, and bakes
// in PyRosetta at build time. This is the build-success smoke required by
// Bug 14: an actual podman build is too heavy for CI, but verifying the
// Containerfile body is wired into the install path through the registry
// catches regressions cheaply.
func TestBindcraftContainerfileEmbedded(t *testing.T) {
	body, err := loadContainerfile("bindcraft.Containerfile")
	if err != nil {
		t.Fatalf("embedded bindcraft.Containerfile missing: %v", err)
	}
	s := string(body)

	// FROMs the shared NGC base.
	if !strings.Contains(s, "FROM "+BaseImage) {
		t.Errorf("Containerfile must FROM %q; got:\n%s", BaseImage, s)
	}
	// Clones upstream BindCraft.
	if !strings.Contains(s, "github.com/martinpacesa/BindCraft") {
		t.Error("Containerfile must clone martinpacesa/BindCraft")
	}
	// PyRosetta baked in at build time (Bug 8 → Bug 14 pivot).
	if !strings.Contains(s, "pyrosetta_installer.install_pyrosetta") {
		t.Error("Containerfile must bake PyRosetta in via pyrosetta-installer")
	}
	// Locked pip list — every package the upstream install_bindcraft.sh
	// installs, translated to a pip name. Drift here means the recipe has
	// diverged from upstream; re-verify against the live install script.
	required := []string{
		"numpy<2.0.0", "pandas", "matplotlib", "biopython", "scipy",
		"pdbfixer", "seaborn", "tqdm", "fsspec", "py3Dmol",
		"chex", "dm-haiku", "flax<0.10.0", "dm-tree", "joblib",
		"ml-collections", "immutabledict", "optax",
		"jax[cuda12]>=0.4,<=0.6.0",
	}
	for _, pkg := range required {
		if !strings.Contains(s, pkg) {
			t.Errorf("Containerfile missing locked pip dep %q", pkg)
		}
	}
	// ColabDesign from git, --no-deps (matches upstream behaviour).
	if !strings.Contains(s, "github.com/sokrypton/ColabDesign") {
		t.Error("Containerfile must install ColabDesign from git")
	}
	if !strings.Contains(s, "--no-deps") {
		t.Error("ColabDesign should be installed with --no-deps so its " +
			"deps don't override the pinned jax/flax above")
	}
	// Entrypoint targets bindcraft.py (NOT the spec's hypothetical
	// run_bindcraft.py — upstream's CLI is bindcraft.py with --settings).
	if !strings.Contains(s, "bindcraft.py") {
		t.Error("ENTRYPOINT must invoke /opt/bindcraft/bindcraft.py")
	}
}

// TestBindcraftRecipeContainerShape locks the tools.toml entry: bindcraft
// MUST be a container-mode recipe with the right image_tag, containerfile,
// gpu flag, and empty weights_paths (AlphaFold weights are a separate
// data asset; PyRosetta is baked in).
func TestBindcraftRecipeContainerShape(t *testing.T) {
	reg, err := LoadRegistry("/home/u/fova")
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	rec, ok := reg.Tool("bindcraft")
	if !ok {
		t.Fatal("bindcraft missing from registry")
	}
	if rec.Containerfile != "bindcraft.Containerfile" {
		t.Errorf("Containerfile = %q, want bindcraft.Containerfile", rec.Containerfile)
	}
	if rec.ImageTag == "" {
		t.Error("ImageTag empty — bindcraft must be container-mode")
	}
	if !strings.HasPrefix(rec.ImageTag, "fova/bindcraft:") {
		t.Errorf("ImageTag = %q, want fova/bindcraft:* tag", rec.ImageTag)
	}
	if !rec.GPU {
		t.Error("GPU must be true — BindCraft requires CUDA for JAX/AF2")
	}
	if len(rec.WeightsPaths) != 0 {
		t.Errorf("WeightsPaths = %v, want []; AF2 weights ride on extra_data, "+
			"PyRosetta is baked in", rec.WeightsPaths)
	}
	if rec.Entrypoint == "" {
		t.Error("Entrypoint empty — runner needs the bindcraft.py invocation")
	}
	if !strings.Contains(rec.Entrypoint, "bindcraft.py") {
		t.Errorf("Entrypoint = %q, want it to invoke bindcraft.py", rec.Entrypoint)
	}
	// Legacy install_steps and run_command must be absent — Bug 8's failed
	// requirements.txt path is fully gone.
	if len(rec.InstallSteps) != 0 {
		t.Errorf("InstallSteps must be empty in container mode, got: %v", rec.InstallSteps)
	}
	if rec.RunCommand != "" {
		t.Errorf("RunCommand must be empty in container mode, got: %q", rec.RunCommand)
	}
	if rec.SmokeTest == "" {
		t.Error("SmokeTest empty — should be `bindcraft.py --help` plus GPU assertion")
	}
	if !strings.Contains(rec.SmokeTest, "bindcraft.py --help") {
		t.Errorf("SmokeTest = %q, want it to invoke `bindcraft.py --help`", rec.SmokeTest)
	}
	// AlphaFold params still required at run time (mounted, not baked).
	hasAF := false
	for _, d := range rec.ExtraData {
		if d == "alphafold_params" {
			hasAF = true
		}
	}
	if !hasAF {
		t.Error("ExtraData must include alphafold_params — AF2 weights aren't baked in")
	}
}

// TestBindcraftInstallExercisesContainerBuild drives Install through the
// runtime exec seam with the REAL embedded bindcraft.Containerfile loader
// (no loadContainerfile stub). This is the "stubbed runtime exec seam"
// build-success smoke from the task brief: we don't run podman build for
// real, but we verify the installer hands the right Containerfile body
// to a `<runtime> build` invocation tagged fova/bindcraft:*.
func TestBindcraftInstallExercisesContainerBuild(t *testing.T) {
	home := t.TempDir()
	reg, err := LoadRegistry(home)
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	inst := NewInstaller(reg)
	inst.runtime = Runtime{Bin: "/usr/bin/podman", Kind: RuntimePodman}

	var buildCalls [][]string
	oldRun := runCmd
	defer func() { runCmd = oldRun }()
	runCmd = func(cmd *exec.Cmd) error {
		// Capture for later assertions and validate the Containerfile passed
		// on disk has the expected body — this is the strongest "the file is
		// actually wired in" assertion we can make without a real build.
		buildCalls = append(buildCalls, append([]string(nil), cmd.Args...))
		joined := strings.Join(cmd.Args, " ")
		if strings.Contains(joined, "build -t fova/bindcraft:") {
			// The -f flag's value is the Containerfile path; read it back
			// and confirm its body matches the embedded one.
			for i, a := range cmd.Args {
				if a == "-f" && i+1 < len(cmd.Args) {
					body, err := os.ReadFile(cmd.Args[i+1])
					if err != nil {
						return err
					}
					if !strings.Contains(string(body), "github.com/martinpacesa/BindCraft") {
						t.Errorf("build invoked with wrong Containerfile body:\n%s", body)
					}
					if !strings.Contains(string(body), "pyrosetta_installer.install_pyrosetta") {
						t.Error("build Containerfile missing pyrosetta_installer bake-in")
					}
				}
			}
		}
		return nil
	}
	oldOut := runCmdOutput
	defer func() { runCmdOutput = oldOut }()
	// Pretend the base image is already cached so we don't also need a Pull.
	runCmdOutput = func(cmd *exec.Cmd) ([]byte, error) {
		return []byte("[{}]\n"), nil
	}

	if err := inst.Install(context.Background(), "bindcraft"); err != nil {
		t.Fatalf("Install bindcraft: %v", err)
	}

	var sawBuild bool
	for _, a := range buildCalls {
		joined := strings.Join(a, " ")
		if strings.Contains(joined, "build -t fova/bindcraft:") {
			sawBuild = true
		}
	}
	if !sawBuild {
		t.Errorf("expected `<runtime> build -t fova/bindcraft:*`, got calls: %v", buildCalls)
	}
}

// signalAlive returns nil if the process with pid is still alive (i.e. a
// kill(pid, 0) succeeds). On Linux, os.FindProcess never errors, so the
// liveness check is the subsequent Signal(0) call.
func signalAlive(pid int) error {
	p, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return p.Signal(syscall.Signal(0))
}
