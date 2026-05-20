// Command proteus is the Proteus protein-design TUI and CLI.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/alvarogonjim/proteus/internal/agent"
	"github.com/alvarogonjim/proteus/internal/llm"
	"github.com/alvarogonjim/proteus/internal/skills"
	"github.com/alvarogonjim/proteus/internal/tools"
	"github.com/alvarogonjim/proteus/internal/tools/fold"
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

// runTUI builds the registry, model registry, and starts the Bubble Tea app.
func runTUI() error {
	workspace, err := defaultWorkspace()
	if err != nil {
		return err
	}

	registry := tools.NewRegistry()
	for _, t := range tools.NewFSTools(workspace) {
		registry.Register(t)
	}
	registry.Register(fold.NewESMFold(workspace))
	loader := skills.NewLoader()
	registry.Register(loader.ListTool())
	registry.Register(loader.ReadTool())

	models := llm.NewModelRegistry()
	app := tui.New(registry, models, agent.SystemPrompt)

	p := tea.NewProgram(app, tea.WithAltScreen())
	_, err = p.Run()
	return err
}

// defaultWorkspace returns $PROTEUS_HOME/projects/default, creating it.
func defaultWorkspace() (string, error) {
	home := os.Getenv("PROTEUS_HOME")
	if home == "" {
		uh, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		home = filepath.Join(uh, "proteus")
	}
	ws := filepath.Join(home, "projects", "default")
	if err := os.MkdirAll(filepath.Join(ws, "designs"), 0o755); err != nil {
		return "", err
	}
	return ws, nil
}
