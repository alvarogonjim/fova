package tui

import (
	"context"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alvarogonjim/fova/internal/assets"
	"github.com/alvarogonjim/fova/internal/backends/local"
	"github.com/alvarogonjim/fova/internal/config"
	"github.com/alvarogonjim/fova/internal/domain"
	jobmgr "github.com/alvarogonjim/fova/internal/jobs"
	"github.com/alvarogonjim/fova/internal/llm"
	"github.com/alvarogonjim/fova/internal/store"
	"github.com/alvarogonjim/fova/internal/tools"
)

// newSetupTestModel builds a Model wired with a local registry and a job
// manager backed by a temp store — enough to exercise the setup commands.
func newSetupTestModel(t *testing.T) *Model {
	t.Helper()
	home := t.TempDir()
	reg, err := local.LoadRegistry(home)
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	st, err := store.Open(filepath.Join(t.TempDir(), "workspace.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return New(Deps{
		Registry:     tools.NewRegistry(),
		Models:       llm.NewModelRegistry(config.DefaultCatalog()),
		SystemPrompt: assets.DefaultSystemPrompt(),
		Store:        st,
		Jobs:         jobmgr.NewManager(st, nil),
		Local:        reg,
		FovaHome:     home,
	})
}

func TestCmdDoctorPostsReport(t *testing.T) {
	m := newSetupTestModel(t)
	before := len(m.chat.entries)
	m.cmdDoctor()
	if len(m.chat.entries) <= before {
		t.Fatal("cmdDoctor posted nothing to chat")
	}
	last := m.chat.entries[len(m.chat.entries)-1]
	if last.kind != entrySlash {
		t.Errorf("doctor report should be a slash-output entry, got kind=%d", last.kind)
	}
	// The posted text must be multi-line (one labelled row per line), and the
	// rendered chat must keep those newlines — guards spec Bug 7.
	if strings.Count(last.text, "\n") < 3 {
		t.Errorf("doctor report should contain >=3 newlines, got %d:\n%s",
			strings.Count(last.text, "\n"), last.text)
	}
	rendered := m.chat.renderEntries()
	if !strings.Contains(rendered, "System") {
		t.Errorf("rendered chat missing System header:\n%s", rendered)
	}
	if !strings.Contains(rendered, "Local protein tools") {
		t.Errorf("rendered chat missing Local protein tools header:\n%s", rendered)
	}
}

func TestCmdToolsListsTools(t *testing.T) {
	m := newSetupTestModel(t)
	m.cmdTools()
	last := m.chat.entries[len(m.chat.entries)-1]
	if last.kind != entrySlash || last.text == "" {
		t.Fatalf("cmdTools should post a non-empty slash-output entry, got %+v", last)
	}
}

// waitJobDone blocks until a job reaches a terminal state, so the job's
// background goroutine cannot write to the store after the test closes it.
func waitJobDone(t *testing.T, mgr *jobmgr.Manager, id domain.JobID) {
	t.Helper()
	for i := 0; i < 200; i++ {
		j, err := mgr.Status(id)
		if err != nil {
			t.Fatalf("Status: %v", err)
		}
		switch j.Status {
		case domain.JobSucceeded, domain.JobFailed, domain.JobCancelled:
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("job did not finish in time")
}

func TestCmdInstallSubmitsJob(t *testing.T) {
	m := newSetupTestModel(t)
	m.installFn = func(ctx context.Context, name string, log io.Writer) error {
		io.WriteString(log, "stub install\n")
		return nil
	}
	m.cmdInstall("ipsae")
	jobs, err := m.jobMgr.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 {
		t.Fatalf("expected one job, got %d", len(jobs))
	}
	if jobs[0].Kind != domain.JobSetup || jobs[0].Tool != "install:ipsae" {
		t.Errorf("job = kind %q tool %q, want setup / install:ipsae", jobs[0].Kind, jobs[0].Tool)
	}
	waitJobDone(t, m.jobMgr, jobs[0].ID)
}

func TestCmdInstallUnknownToolSubmitsNothing(t *testing.T) {
	m := newSetupTestModel(t)
	m.cmdInstall("nonesuch")
	jobs, _ := m.jobMgr.List()
	if len(jobs) != 0 {
		t.Fatalf("unknown tool must not submit a job, got %d", len(jobs))
	}
	if m.chat.entries[len(m.chat.entries)-1].kind != entryError {
		t.Error("unknown tool should post an error entry")
	}
}

func TestCmdInstallDryRunSubmitsNothing(t *testing.T) {
	m := newSetupTestModel(t)
	m.cmdInstall("--dry-run ipsae")
	jobs, _ := m.jobMgr.List()
	if len(jobs) != 0 {
		t.Fatalf("--dry-run must not submit a job, got %d", len(jobs))
	}
	if m.chat.entries[len(m.chat.entries)-1].kind != entrySlash {
		t.Error("--dry-run should post a slash-output block with the steps")
	}
}

func TestCmdModalDeployWithoutCLIPostsHint(t *testing.T) {
	// This test covers the absent-CLI path. Skip it when `modal` is installed,
	// so it never triggers a real deploy on a developer's machine.
	if _, err := exec.LookPath("modal"); err == nil {
		t.Skip("modal CLI is installed; skipping the absent-CLI path test")
	}
	m := newSetupTestModel(t)
	m.cmdModalDeploy("deploy")
	jobs, _ := m.jobMgr.List()
	if len(jobs) != 0 {
		t.Fatalf("modal deploy without the CLI must submit no job, got %d", len(jobs))
	}
	if m.chat.entries[len(m.chat.entries)-1].text == "" {
		t.Error("modal deploy should post a hint")
	}
}

func TestCmdUninstallSubmitsJob(t *testing.T) {
	m := newSetupTestModel(t)
	m.cmdUninstall("ipsae")
	jobs, err := m.jobMgr.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 {
		t.Fatalf("expected one job, got %d", len(jobs))
	}
	if jobs[0].Kind != domain.JobSetup || jobs[0].Tool != "uninstall:ipsae" {
		t.Errorf("job = kind %q tool %q, want setup / uninstall:ipsae", jobs[0].Kind, jobs[0].Tool)
	}
	waitJobDone(t, m.jobMgr, jobs[0].ID)
}

func TestCmdUninstallUnknownToolSubmitsNothing(t *testing.T) {
	m := newSetupTestModel(t)
	m.cmdUninstall("nonesuch")
	jobs, _ := m.jobMgr.List()
	if len(jobs) != 0 {
		t.Fatalf("unknown tool must not submit a job, got %d", len(jobs))
	}
	if m.chat.entries[len(m.chat.entries)-1].kind != entryError {
		t.Error("unknown tool should post an error entry")
	}
}
