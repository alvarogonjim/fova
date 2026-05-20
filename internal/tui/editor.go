package tui

import (
	"os"
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// editorDoneMsg is delivered after the external editor closes. Contents is
// the saved file's text (trailing whitespace trimmed) when no error occurred;
// when Err is non-nil, Contents is empty and the chat shows a brief error.
// Path is the temp file we created (already removed by the time this fires).
type editorDoneMsg struct {
	Contents string
	Err      error
}

// resolveEditor returns the user's preferred external editor:
// $VISUAL, then $EDITOR, then the POSIX fallback "vi".
func resolveEditor() string {
	if e := strings.TrimSpace(os.Getenv("VISUAL")); e != "" {
		return e
	}
	if e := strings.TrimSpace(os.Getenv("EDITOR")); e != "" {
		return e
	}
	return "vi"
}

// openEditorCmd writes initial to a temp file, hands the TTY to the user's
// $EDITOR via tea.ExecProcess, then reads the saved content back and delivers
// it as an editorDoneMsg. The temp file is removed in every code path.
func openEditorCmd(initial string) tea.Cmd {
	tmp, err := os.CreateTemp("", "fova-msg-*.md")
	if err != nil {
		return func() tea.Msg { return editorDoneMsg{Err: err} }
	}
	path := tmp.Name()
	if _, err := tmp.WriteString(initial); err != nil {
		tmp.Close()
		os.Remove(path)
		return func() tea.Msg { return editorDoneMsg{Err: err} }
	}
	if err := tmp.Close(); err != nil {
		os.Remove(path)
		return func() tea.Msg { return editorDoneMsg{Err: err} }
	}

	// editor + args (handles e.g. EDITOR="emacsclient -nw").
	fields := strings.Fields(resolveEditor())
	args := append(fields[1:], path)
	cmd := exec.Command(fields[0], args...)

	return tea.ExecProcess(cmd, func(execErr error) tea.Msg {
		body, readErr := os.ReadFile(path)
		_ = os.Remove(path)
		if execErr != nil {
			return editorDoneMsg{Err: execErr}
		}
		if readErr != nil {
			return editorDoneMsg{Err: readErr}
		}
		return editorDoneMsg{Contents: strings.TrimRight(string(body), "\n\r\t ")}
	})
}
