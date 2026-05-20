// Command proteus is the Proteus protein-design TUI and CLI.
package main

import (
	"context"
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
	knowledge "github.com/alvarogonjim/proteus/internal/tools/knowledge"
	"github.com/alvarogonjim/proteus/internal/tools/lab"
	plantool "github.com/alvarogonjim/proteus/internal/tools/plan"
	scoretools "github.com/alvarogonjim/proteus/internal/tools/score"
	"github.com/alvarogonjim/proteus/internal/tui"
	"github.com/alvarogonjim/proteus/internal/version"
)

// corpusMapper adapts the LLM model registry to knowledge.Mapper so
// knowledge.corpus can run map/reduce prompts over papers. The provider is
// resolved lazily on each call so a missing API key only fails when
// corpus.map is actually used.
type corpusMapper struct{ models *llm.ModelRegistry }

func (m corpusMapper) Map(ctx context.Context, prompt, text string) (string, error) {
	p, err := m.models.Provider()
	if err != nil {
		return "", err
	}
	resp, err := p.Chat(ctx, llm.ChatRequest{
		Model:    m.models.ActiveModel(),
		System:   prompt,
		Messages: []llm.Message{{Role: "user", Content: text}},
	})
	if err != nil {
		return "", err
	}
	return resp.Text, nil
}

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
	mgr.SetLogDir(filepath.Join(proteusHome(), "logs"))
	models := llm.NewModelRegistry()
	registry := buildRegistry(workspace, st, mgr, models)

	home := proteusHome()
	localReg, err := local.LoadRegistry(home)
	if err != nil {
		return err
	}

	app := tui.New(tui.Deps{
		Registry:     registry,
		Models:       models,
		SystemPrompt: agent.SystemPrompt,
		Store:        st,
		Jobs:         mgr,
		Local:        localReg,
		ProteusHome:  home,
		WebhookPort:  9876,
	})

	p := tea.NewProgram(app, tea.WithAltScreen())
	_, err = p.Run()
	return err
}

// buildRegistry assembles the tool registry for a TUI session.
func buildRegistry(workspace string, st *store.Store, mgr *jobmgr.Manager, models *llm.ModelRegistry) *tools.Registry {
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
	registry.Register(designtools.NewRFAntibodyTool(mgr, backend, st))
	registry.Register(designtools.NewChai2Tool(mgr, backend, st))
	registry.Register(designtools.NewRFdiffusion2Tool(mgr, backend, st))
	registry.Register(designtools.NewLigandMPNNTool(mgr, backend, st))
	registry.Register(fold.NewBoltz2(mgr, backend))
	registry.Register(fold.NewChai1(mgr, backend))
	registry.Register(scoretools.NewFilterTool(st))
	registry.Register(scoretools.NewMetricsTool())
	registry.Register(scoretools.NewIPSAETool())

	// v0.4 Adaptyv wet-lab tools. An empty token is fine here — the tools
	// surface a clear error at call time when no token is configured.
	labToken, _ := lab.Token()
	labClient := lab.NewClient(labToken)
	registry.Register(lab.NewTargetsSearchTool(labClient))
	registry.Register(lab.NewCostEstimateTool(labClient))
	registry.Register(lab.NewExperimentStatusTool(labClient))
	registry.Register(lab.NewResultsTool(labClient))
	registry.Register(lab.NewSubmitExperimentTool(labClient, st))

	// v0.3 knowledge and planning tools.
	results := knowledge.NewResults()
	registry.Register(knowledge.NewEuropePMC(results))
	registry.Register(knowledge.NewOpenAlex(results))
	registry.Register(knowledge.NewS2(results))
	registry.Register(knowledge.NewBioRxiv(results))
	registry.Register(knowledge.NewCrossref(results))
	registry.Register(knowledge.NewUniProt())
	registry.Register(knowledge.NewPDB())
	registry.Register(knowledge.NewInterPro())
	registry.Register(knowledge.NewWebFetch())
	registry.Register(knowledge.NewWebSearch())
	registry.Register(knowledge.NewCorpus(st, results, corpusMapper{models: models}, filepath.Join(workspace, "corpus.bleve")))
	registry.Register(plantool.NewPlanCreateTool(st))

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
