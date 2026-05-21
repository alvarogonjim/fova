// Command fova is the fova protein-design TUI and CLI.
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/alvarogonjim/fova/internal/agent"
	"github.com/alvarogonjim/fova/internal/assets"
	"github.com/alvarogonjim/fova/internal/backends"
	"github.com/alvarogonjim/fova/internal/backends/local"
	jobmgr "github.com/alvarogonjim/fova/internal/jobs"
	"github.com/alvarogonjim/fova/internal/llm"
	"github.com/alvarogonjim/fova/internal/safety"
	"github.com/alvarogonjim/fova/internal/skills"
	"github.com/alvarogonjim/fova/internal/store"
	"github.com/alvarogonjim/fova/internal/tools"
	designtools "github.com/alvarogonjim/fova/internal/tools/design"
	"github.com/alvarogonjim/fova/internal/tools/fold"
	jobstools "github.com/alvarogonjim/fova/internal/tools/jobs"
	knowledge "github.com/alvarogonjim/fova/internal/tools/knowledge"
	"github.com/alvarogonjim/fova/internal/tools/lab"
	plantool "github.com/alvarogonjim/fova/internal/tools/plan"
	scoretools "github.com/alvarogonjim/fova/internal/tools/score"
	viztools "github.com/alvarogonjim/fova/internal/tools/viz"
	"github.com/alvarogonjim/fova/internal/tui"
	"github.com/alvarogonjim/fova/internal/version"
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

// newRootCmd builds the cobra command tree. Bare `fova` launches the TUI.
func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "fova",
		Short:         "fova — a TUI agent for de novo protein design",
		Version:       version.String(),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTUI()
		},
	}
	root.SetVersionTemplate("fova {{.Version}}\n")
	root.AddCommand(&cobra.Command{
		Use:   "tui",
		Short: "Launch the fova TUI (default)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTUI()
		},
	})
	root.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print the fova version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Fprintf(cmd.OutOrStdout(), "fova %s\n", version.String())
		},
	})
	root.AddCommand(newExportCmd())
	root.AddCommand(newReplayCmd())
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
	mgr.SetLogDir(filepath.Join(fovaHome(), "logs"))
	bundle, err := assets.Load()
	if err != nil {
		return err
	}
	models := llm.NewModelRegistry(bundle.Models)
	cfg := bundle.Config
	tui.ApplyTheme(cfg.UI.Theme)
	if err := models.SelectDefault(cfg.Defaults); err != nil {
		return err
	}
	skillLoader := skills.NewLoader(bundle.Skills)

	home := fovaHome()
	localReg, err := local.LoadRegistry(home)
	if err != nil {
		return err
	}
	// installer is shared between plan.create (for the Bug 11 install
	// cross-check) and the TUI's /install slash command. We build it here
	// rather than inside buildRegistry so the same Installer instance backs
	// both paths.
	installer := local.NewInstaller(localReg)
	registry := buildRegistry(workspace, st, mgr, models, cfg, installer, skillLoader)

	webhookPort := 0
	if cfg.Webhook.Enabled {
		webhookPort = cfg.Webhook.Port
	}
	guardTable, err := safety.LoadDefaultTable()
	if err != nil {
		return err
	}
	guard := safety.NewGuard(guardTable)
	app := tui.New(tui.Deps{
		Registry:           registry,
		Models:             models,
		SystemPrompt:       agent.BuildSystemPrompt(tui.Commands(), bundle.SystemPrompt),
		Store:              st,
		Jobs:               mgr,
		Local:              localReg,
		FovaHome:           home,
		ConfigDir:          assets.Dir(),
		WebhookPort:        webhookPort,
		WebhookURL:         cfg.Webhook.EffectiveURL(),
		BudgetLimitUSD:     cfg.Budget.SessionSoftLimitUSD,
		InlineGraphicsMode: cfg.UI.InlineGraphics,
		Guard:              guard,
		SkillLoader:        skillLoader,
		AssetReport:        bundle.Report,
	})

	p := tea.NewProgram(app, tea.WithAltScreen())
	_, err = p.Run()
	return err
}

// buildRegistry assembles the tool registry for a TUI session.
func buildRegistry(workspace string, st *store.Store, mgr *jobmgr.Manager, models *llm.ModelRegistry, cfg assets.Config, installer *local.Installer, skillLoader *skills.Loader) *tools.Registry {
	registry := tools.NewRegistry()
	for _, t := range tools.NewFSTools(workspace) {
		registry.Register(t)
	}
	registry.Register(fold.NewESMFold(workspace))
	registry.Register(skillLoader.ListTool())
	registry.Register(skillLoader.ReadTool())

	// estd lets jobs.status / jobs.result surface each tool's advertised
	// EstimatedDuration without taking a hard dependency on the full
	// registry. The closure captures `registry` so estimates work for tools
	// registered later in this function too.
	estd := func(name string) time.Duration {
		if t, ok := registry.Get(name); ok {
			return t.EstimatedDuration(nil)
		}
		return 0
	}
	registry.Register(jobstools.NewListTool(mgr))
	registry.Register(jobstools.NewStatusTool(mgr, estd))
	registry.Register(jobstools.NewCancelTool(mgr))
	registry.Register(jobstools.NewResultTool(mgr, estd))

	// Compute backend: FOVA_COMPUTE_BACKEND overrides config.toml's
	// [defaults].compute_backend (env wins; SP2 design §4).
	be := os.Getenv("FOVA_COMPUTE_BACKEND")
	if be == "" {
		be = cfg.Defaults.ComputeBackend
	}
	backend, err := backends.Select(be, fovaHome())
	if err != nil {
		// An unknown backend name falls back to local rather than crashing the TUI.
		backend, _ = backends.Select("local", fovaHome())
	}
	registry.Register(designtools.NewBindCraftTool(workspace, mgr, backend, st))
	registry.Register(designtools.NewBoltzGenTool(workspace, mgr, backend, st))
	registry.Register(designtools.NewBoltzGenCheckTool(workspace, backend))
	registry.Register(designtools.NewRFdiffusionTool(workspace, mgr, backend, st))
	registry.Register(designtools.NewProteinMPNNTool(workspace, mgr, backend, st))
	registry.Register(designtools.NewRFAntibodyTool(workspace, mgr, backend, st))
	registry.Register(designtools.NewChai2Tool(workspace, mgr, backend, st))
	registry.Register(designtools.NewRFdiffusion2Tool(workspace, mgr, backend, st))
	registry.Register(designtools.NewLigandMPNNTool(workspace, mgr, backend, st))
	registry.Register(fold.NewBoltz2(mgr, backend))
	registry.Register(fold.NewChai1(mgr, backend))
	registry.Register(scoretools.NewFilterTool(st))
	registry.Register(scoretools.NewMetricsTool())
	registry.Register(scoretools.NewIPSAETool())

	// v0.4 Adaptyv wet-lab tools. An empty token is fine here — the tools
	// surface a clear error at call time when no token is configured.
	labToken, _ := lab.Token()
	labClient := lab.NewClient(labToken)
	targetsTool := lab.NewTargetsSearchTool(labClient)
	if tcache, err := lab.OpenTargetsCacheDefault(); err == nil {
		targetsTool.WithCache(tcache)
	}
	registry.Register(targetsTool)
	registry.Register(lab.NewCostEstimateTool(labClient))
	registry.Register(lab.NewExperimentStatusTool(labClient))
	registry.Register(lab.NewResultsTool(labClient))
	registry.Register(lab.NewSubmitExperimentTool(labClient, st, cfg.Webhook.EffectiveURL()))

	// v0.3 knowledge and planning tools.
	results := knowledge.NewResults()
	registry.Register(knowledge.NewEuropePMC(results))
	registry.Register(knowledge.NewOpenAlex(results, cfg.Knowledge.Mailto))
	registry.Register(knowledge.NewS2(results))
	registry.Register(knowledge.NewBioRxiv(results, cfg.Knowledge.BiorxivRecentDays))
	registry.Register(knowledge.NewCrossref(results))
	registry.Register(knowledge.NewUniProt())
	registry.Register(knowledge.NewPDB())
	registry.Register(knowledge.NewInterPro())
	registry.Register(knowledge.NewWebFetch())
	registry.Register(knowledge.NewWebSearch())
	registry.Register(knowledge.NewBLAST())
	// knowledge.corpus registers itself as eight per-action tools
	// (knowledge.corpus_add, ...search, ...grep, ...map, ...reduce,
	// ...read, ...remove, ...add_from_search) — the flat shape OpenAI-style
	// LLMs naturally call. corpus_map needs the jobs.Manager because it runs
	// as an async job (v0.7 Bugs 3 and 4).
	knowledge.NewCorpus(st, results, corpusMapper{models: models}, mgr, filepath.Join(workspace, "corpus.bleve"), cfg.Knowledge.CorpusDefaultMaxPapers).Register(registry)
	registry.Register(knowledge.NewLocalPDFs(
		results,
		corpusMapper{models: models},
		filepath.Join(workspace, "local_pdfs.bleve"),
		cfg.Knowledge.LocalPDFsDir,
	))
	if token := os.Getenv("PAPERCLIP_TOKEN"); token != "" {
		registry.Register(knowledge.NewPaperclip(token, cfg.Knowledge.PaperclipBaseURL))
	}
	planCreate := plantool.NewPlanCreateTool(st, installer)
	planCreate.SetRegistry(registry)
	registry.Register(planCreate)

	registry.Register(viztools.NewMetricPlot(workspace, results))
	registry.Register(viztools.NewContactMap(workspace))
	registry.Register(viztools.NewAsciiStructure(workspace))
	registry.Register(viztools.NewPyMolRender(workspace))

	return registry
}

// fovaHome returns the fova home directory ($FOVA_HOME or ~/fova).
func fovaHome() string {
	if h := os.Getenv("FOVA_HOME"); h != "" {
		return h
	}
	uh, err := os.UserHomeDir()
	if err != nil {
		return "fova"
	}
	return filepath.Join(uh, "fova")
}

// defaultWorkspace returns $FOVA_HOME/projects/default, creating it.
func defaultWorkspace() (string, error) {
	ws := filepath.Join(fovaHome(), "projects", "default")
	if err := os.MkdirAll(filepath.Join(ws, "designs"), 0o755); err != nil {
		return "", err
	}
	return ws, nil
}
