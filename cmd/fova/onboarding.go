package main

import (
	"os"
	"path/filepath"

	"golang.org/x/term"

	"github.com/alvarogonjim/fova/internal/config"
	"github.com/alvarogonjim/fova/internal/tui"
)

// isFirstRun reports whether fova has never been configured: config.toml does
// not yet exist in the config directory.
func isFirstRun() bool {
	_, err := os.Stat(filepath.Join(config.ConfigDir(), "config.toml"))
	return os.IsNotExist(err)
}

// interactive reports whether stdin and stdout are both a terminal — the
// wizard only runs in a real interactive session.
func interactive() bool {
	return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))
}

// maybeRunOnboarding runs the first-run wizard when fova has never been
// configured and the session is interactive. It applies the result before
// the main TUI starts. Skips silently on any non-first-run / non-interactive
// path so existing users and tests are unaffected.
func maybeRunOnboarding() error {
	if !isFirstRun() || !interactive() {
		return nil
	}
	cat, err := config.LoadModels()
	if err != nil {
		return err
	}
	result, ok, err := tui.RunOnboarding(cat)
	if err != nil {
		return err
	}
	if !ok {
		return nil // skipped — fall through to embedded defaults
	}
	return tui.ApplyWizardResult(result)
}
