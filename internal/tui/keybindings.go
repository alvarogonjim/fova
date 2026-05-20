package tui

// keyBinding is one row of the keybindings table. The table is the single
// source of truth: every entry has a matching handler in handleKey, and the
// /keys overlay renders directly from it.
type keyBinding struct {
	Key         string // the printable shortcut, e.g. "Ctrl+L", "PgUp", "?"
	Action      string // a stable identifier for the action (used by tests)
	Description string // human-readable hint shown in the overlay
}

// keybindings returns the v0.5 keybinding set. The order is the order the
// /keys overlay renders. SPECS §10.4's seven bindings come first, then the
// SP-A additions.
//
// Note on the newline binding: SPECS §10.4 lists Shift+Enter for inserting a
// newline, but the v0.4 implementation in submitInput actually wires Alt+Enter
// (most terminals do not differentiate Shift+Enter from Enter). The overlay
// shows the binding that actually works; reconciling the spec is a follow-up.
func keybindings() []keyBinding {
	return []keyBinding{
		// SPECS §10.4 — shipped in v0.4.
		{"Ctrl+C", "cancel", "Cancel the running turn"},
		{"Ctrl+D", "quit", "Save and exit"},
		{"Tab", "focus", "Cycle focus between panels"},
		{"Esc", "dismiss", "Close modal / dismiss overlay"},
		{"↑/↓", "navigate", "Navigate within the focused panel"},
		{"Enter", "send", "Send the message / activate"},
		{"Alt+Enter", "newline", "Insert a newline in the input"},

		// SP-A additions (v0.5).
		{"PgUp", "chat.pageup", "Scroll the chat up one page"},
		{"PgDown", "chat.pagedown", "Scroll the chat down one page"},
		{"Home", "chat.top", "Scroll the chat to the top"},
		{"End", "chat.bottom", "Scroll the chat to the bottom"},
		{"Ctrl+L", "clear", "Clear the chat (alias of /clear)"},
		{"Ctrl+R", "reload", "Reload config.toml and models.toml"},
		{"Ctrl+X", "editor", "Open $EDITOR (or $VISUAL) to compose a long message"},
		{"?", "keys", "Show this keybinding overlay"},
	}
}
