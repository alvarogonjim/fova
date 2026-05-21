package main

import (
	"fmt"
	"io"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/alvarogonjim/fova/internal/agent"
	"github.com/alvarogonjim/fova/internal/config"
	"github.com/alvarogonjim/fova/internal/llm"
	"github.com/alvarogonjim/fova/internal/replay"
	"github.com/alvarogonjim/fova/internal/tools"
	"github.com/alvarogonjim/fova/internal/tui"
)

// newReplayCmd builds the `fova replay <session.json>` command.
// Replay is read-only: it makes no LLM calls, runs no tools, and never
// writes to the workspace store. The hidden --dry flag emits a one-line
// summary per event to stdout instead of opening the TUI, and is used by
// the test suite to assert event-stream shape.
func newReplayCmd() *cobra.Command {
	var dry bool
	cmd := &cobra.Command{
		Use:   "replay <session.json>",
		Short: "Replay a recorded session in the TUI (read-only)",
		Long: `Replay a recorded session in the TUI.

Replay mode is fully read-only: no LLM calls are made, no tools are run,
and the workspace store is never written to. Events are fed into the chat
from the JSON document, paced by their original timestamps (capped at
50 ms/event). Space steps forward manually; Esc quits.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if dry {
				return runReplayDry(cmd.OutOrStdout(), args[0])
			}
			return runReplayTUI(args[0])
		},
	}
	cmd.Flags().BoolVar(&dry, "dry", false, "print one line per event and exit (testing only)")
	_ = cmd.Flags().MarkHidden("dry")
	return cmd
}

// runReplayDry prints a one-line summary per event to w. Used by tests and
// behind the hidden --dry flag.
func runReplayDry(w io.Writer, path string) error {
	doc, err := replay.LoadDocument(path)
	if err != nil {
		return err
	}
	for i, ev := range doc.Events {
		line := fmt.Sprintf("[%03d] %-12s %s", i+1, string(ev.Kind), ev.TS.UTC().Format("2006-01-02T15:04:05Z"))
		switch ev.Kind {
		case replay.KindUserMsg, replay.KindAgentText:
			line += " " + ev.Text
		case replay.KindToolStart:
			line += " " + ev.Name
			if len(ev.Input) > 0 {
				line += " " + string(ev.Input)
			}
		case replay.KindToolResult:
			line += " " + ev.Name + " " + ev.Display
			if ev.Err != "" {
				line += " err=" + ev.Err
			}
		}
		if _, err := fmt.Fprintln(w, line); err != nil {
			return err
		}
	}
	return nil
}

// runReplayTUI launches the TUI in read-only replay mode.
var runReplayTUI = func(path string) error {
	doc, err := replay.LoadDocument(path)
	if err != nil {
		return err
	}
	cat, err := config.LoadModels()
	if err != nil {
		return err
	}
	models := llm.NewModelRegistry(cat)
	// Best-effort: select the recorded model so the welcome line names it.
	// A miss is non-fatal — replay never calls the provider.
	_ = models.SetModel(doc.Model)

	app := tui.New(tui.Deps{
		Registry:     tools.NewRegistry(),
		Models:       models,
		SystemPrompt: agent.BuildSystemPrompt(tui.Commands()),
		ReplayEvents: doc.Events,
		ReplayPace:   true,
	})
	p := tea.NewProgram(app, tea.WithAltScreen(), tea.WithMouseCellMotion())
	_, err = p.Run()
	return err
}
