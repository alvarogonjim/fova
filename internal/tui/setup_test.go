package tui

import (
	"context"
	"io"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/alvarogonjim/proteus/internal/agent"
	"github.com/alvarogonjim/proteus/internal/backends/local"
	"github.com/alvarogonjim/proteus/internal/domain"
	jobmgr "github.com/alvarogonjim/proteus/internal/jobs"
	"github.com/alvarogonjim/proteus/internal/llm"
	"github.com/alvarogonjim/proteus/internal/store"
	"github.com/alvarogonjim/proteus/internal/tools"
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
		Models:       llm.NewModelRegistry(),
		SystemPrompt: agent.SystemPrompt,
		Store:        st,
		Jobs:         jobmgr.NewManager(st, nil),
		Local:        reg,
		ProteusHome:  home,
	})
}

func TestCmdDoctorPostsReport(t *testing.T) {
	m := newSetupTestModel(t)
	before := len(m.chat.entries)
	m.cmdDoctor()
	if len(m.chat.entries) <= before {
		t.Fatal("cmdDoctor posted nothing to chat")
	}
	if m.chat.entries[len(m.chat.entries)-1].kind != entryAgent {
		t.Error("doctor report should be an agent entry")
	}
}

func TestCmdToolsListsTools(t *testing.T) {
	m := newSetupTestModel(t)
	m.cmdTools()
	last := m.chat.entries[len(m.chat.entries)-1]
	if last.kind != entryAgent || last.text == "" {
		t.Fatalf("cmdTools should post a non-empty agent entry, got %+v", last)
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
	if m.chat.entries[len(m.chat.entries)-1].kind != entryAgent {
		t.Error("--dry-run should post an agent block with the steps")
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
