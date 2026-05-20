// Command proteus is the Proteus protein-design TUI and CLI.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/alvarogonjim/proteus/internal/agent"
	"github.com/alvarogonjim/proteus/internal/backends"
	"github.com/alvarogonjim/proteus/internal/backends/local"
	jobmgr "github.com/alvarogonjim/proteus/internal/jobs"
	"github.com/alvarogonjim/proteus/internal/llm"
	"github.com/alvarogonjim/proteus/internal/skills"
	"github.com/alvarogonjim/proteus/internal/store"
	"github.com/alvarogonjim/proteus/internal/tools"
	designtools "github.com/alvarogonjim/proteus/internal/tools/design"
	"github.com/alvarogonjim/proteus/internal/tools/fold"
	jobstools "github.com/alvarogonjim/proteus/internal/tools/jobs"
	scoretools "github.com/alvarogonjim/proteus/internal/tools/score"
	"github.com/alvarogonjim/proteus/internal/tui"
	"github.com/alvarogonjim/proteus/internal/version"
)

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// newRootCmd builds the cobra command tree. Bare `proteus` launches the TUI.
func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "proteus",
		Short:         "Proteus — a TUI agent for de novo protein design",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTUI()
		},
	}
	root.AddCommand(&cobra.Command{
		Use:   "tui",
		Short: "Launch the Proteus TUI (default)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTUI()
		},
	})
	root.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print the Proteus version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Fprintf(cmd.OutOrStdout(), "proteus %s\n", version.String())
		},
	})
	return root
}

// runTUI builds the registry, model registry, store, and starts the app.
func runTUI() error {
	workspace, err := defaultWorkspace()
	if err != nil {
		return err
	}

	st, err := store.Open(filepath.Join(workspace, "workspace.db"))
	if err != nil {
		return err
	}
	defer st.Close()
	if err := st.MarkRunningJobsInterrupted(); err != nil {
		return err
	}

	mgr := jobmgr.NewManager(st, nil)
	registry := buildRegistry(workspace, st, mgr)

	home := proteusHome()
	localReg, err := local.LoadRegistry(home)
	if err != nil {
		return err
	}

	models := llm.NewModelRegistry()
	app := tui.New(tui.Deps{
		Registry:     registry,
		Models:       models,
		SystemPrompt: agent.SystemPrompt,
		Store:        st,
		Jobs:         mgr,
		Local:        localReg,
		ProteusHome:  home,
	})

	p := tea.NewProgram(app, tea.WithAltScreen())
	_, err = p.Run()
	return err
}

// buildRegistry assembles the tool registry for a TUI session.
func buildRegistry(workspace string, st *store.Store, mgr *jobmgr.Manager) *tools.Registry {
	registry := tools.NewRegistry()
	for _, t := range tools.NewFSTools(workspace) {
		registry.Register(t)
	}
	registry.Register(fold.NewESMFold(workspace))
	loader := skills.NewLoader()
	registry.Register(loader.ListTool())
	registry.Register(loader.ReadTool())

	registry.Register(jobstools.NewListTool(mgr))
	registry.Register(jobstools.NewStatusTool(mgr))
	registry.Register(jobstools.NewCancelTool(mgr))
	registry.Register(jobstools.NewResultTool(mgr))

	// Compute backend (env-selectable; defaults to local).
	backend, err := backends.Select(os.Getenv("PROTEUS_COMPUTE_BACKEND"), proteusHome())
	if err != nil {
		// An unknown backend name falls back to local rather than crashing the TUI.
		backend, _ = backends.Select("local", proteusHome())
	}
	registry.Register(designtools.NewBindCraftTool(mgr, backend, st))
	registry.Register(designtools.NewRFdiffusionTool(mgr, backend, st))
	registry.Register(designtools.NewProteinMPNNTool(mgr, backend, st))
	registry.Register(scoretools.NewFilterTool(st))
	registry.Register(scoretools.NewMetricsTool())
	registry.Register(scoretools.NewIPSAETool())

	return registry
}

// proteusHome returns the Proteus home directory ($PROTEUS_HOME or ~/proteus).
func proteusHome() string {
	if h := os.Getenv("PROTEUS_HOME"); h != "" {
		return h
	}
	uh, err := os.UserHomeDir()
	if err != nil {
		return "proteus"
	}
	return filepath.Join(uh, "proteus")
}

// defaultWorkspace returns $PROTEUS_HOME/projects/default, creating it.
func defaultWorkspace() (string, error) {
	ws := filepath.Join(proteusHome(), "projects", "default")
	if err := os.MkdirAll(filepath.Join(ws, "designs"), 0o755); err != nil {
		return "", err
	}
	return ws, nil
}
