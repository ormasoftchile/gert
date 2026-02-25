package tui

import "github.com/charmbracelet/bubbles/key"

// keyMap holds all TUI key bindings.
type keyMap struct {
	Advance key.Binding
	Up      key.Binding
	Down    key.Binding
	Retry   key.Binding
	Skip    key.Binding
	Vars    key.Binding
	Search  key.Binding
	Save    key.Binding
	Quit    key.Binding
	Help    key.Binding
	PgUp    key.Binding
	PgDown  key.Binding
}

var keys = keyMap{
	Advance: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "advance"),
	),
	Up: key.NewBinding(
		key.WithKeys("up", "k"),
		key.WithHelp("↑/k", "browse up"),
	),
	Down: key.NewBinding(
		key.WithKeys("down", "j"),
		key.WithHelp("↓/j", "browse down"),
	),
	Retry: key.NewBinding(
		key.WithKeys("r"),
		key.WithHelp("r", "retry"),
	),
	Skip: key.NewBinding(
		key.WithKeys("s"),
		key.WithHelp("s", "skip"),
	),
	Vars: key.NewBinding(
		key.WithKeys("v"),
		key.WithHelp("v", "vars"),
	),
	Search: key.NewBinding(
		key.WithKeys("/"),
		key.WithHelp("/", "search"),
	),
	Save: key.NewBinding(
		key.WithKeys("s"),
		key.WithHelp("s", "save scenario"),
	),
	Quit: key.NewBinding(
		key.WithKeys("q", "ctrl+c"),
		key.WithHelp("q", "quit"),
	),
	Help: key.NewBinding(
		key.WithKeys("?"),
		key.WithHelp("?", "help"),
	),
	PgUp: key.NewBinding(
		key.WithKeys("pgup"),
		key.WithHelp("PgUp", "scroll up"),
	),
	PgDown: key.NewBinding(
		key.WithKeys("pgdown"),
		key.WithHelp("PgDn", "scroll down"),
	),
}

// keyBarText renders the context-sensitive key hint string.
func keyBarText(running bool, completed bool, overlay overlayKind) string {
	// Overlay-specific hints
	switch overlay {
	case overlayEvidence:
		return keyStyle.Render("Enter") + keyDescStyle.Render(":submit") + "  " +
			keyStyle.Render("Esc") + keyDescStyle.Render(":cancel") + "  " +
			keyStyle.Render("q") + keyDescStyle.Render(":quit")
	case overlayChoice:
		return keyStyle.Render("↑↓") + keyDescStyle.Render(":select") + "  " +
			keyStyle.Render("Enter") + keyDescStyle.Render(":choose") + "  " +
			keyStyle.Render("1-9") + keyDescStyle.Render(":quick") + "  " +
			keyStyle.Render("q") + keyDescStyle.Render(":quit")
	case overlayVars:
		return keyStyle.Render("Esc") + keyDescStyle.Render(":close") + "  " +
			keyStyle.Render("q") + keyDescStyle.Render(":quit")
	case overlaySummary:
		return keyStyle.Render("s") + keyDescStyle.Render(":save scenario") + "  " +
			keyStyle.Render("v") + keyDescStyle.Render(":vars") + "  " +
			keyStyle.Render("Esc") + keyDescStyle.Render(":close") + "  " +
			keyStyle.Render("q") + keyDescStyle.Render(":quit")
	}

	if completed {
		return keyStyle.Render("s") + keyDescStyle.Render(":summary") + "  " +
			keyStyle.Render("v") + keyDescStyle.Render(":vars") + "  " +
			keyStyle.Render("/") + keyDescStyle.Render(":search") + "  " +
			keyStyle.Render("q") + keyDescStyle.Render(":quit")
	}
	if running {
		return keyStyle.Render("↑↓") + keyDescStyle.Render(":browse") + "  " +
			keyStyle.Render("PgUp/Dn") + keyDescStyle.Render(":scroll") + "  " +
			keyStyle.Render("/") + keyDescStyle.Render(":search")
	}
	return keyStyle.Render("enter") + keyDescStyle.Render(":advance") + "  " +
		keyStyle.Render("↑↓") + keyDescStyle.Render(":browse") + "  " +
		keyStyle.Render("r") + keyDescStyle.Render(":retry") + "  " +
		keyStyle.Render("v") + keyDescStyle.Render(":vars") + "  " +
		keyStyle.Render("/") + keyDescStyle.Render(":search") + "  " +
		keyStyle.Render("q") + keyDescStyle.Render(":quit") + "  " +
		keyStyle.Render("?") + keyDescStyle.Render(":help")
}
