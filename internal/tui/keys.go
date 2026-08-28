package tui

import "charm.land/bubbles/v2/key"

// KeyMap is the global binding set. Every screen shows the subset it honours in
// its footer legend; nothing is hidden behind an undiscoverable chord.
type KeyMap struct {
	Up     key.Binding
	Down   key.Binding
	Left   key.Binding
	Right  key.Binding
	Choose key.Binding
	Back   key.Binding
	Quit   key.Binding
	Help   key.Binding
	Filter key.Binding
	Tab    key.Binding
	Dict   key.Binding
	Prac   key.Binding
	Cards  key.Binding
	Stats  key.Binding
	Home   key.Binding
	Cancel key.Binding
}

// Keys is the single instance used by the whole app.
var Keys = KeyMap{
	Up:     key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
	Down:   key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
	Left:   key.NewBinding(key.WithKeys("left", "h"), key.WithHelp("←/h", "left")),
	Right:  key.NewBinding(key.WithKeys("right", "l"), key.WithHelp("→/l", "right")),
	Choose: key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "choose")),
	Back:   key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
	Quit:   key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
	Help:   key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
	Filter: key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "search")),
	Tab:    key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "pane")),
	Dict:   key.NewBinding(key.WithKeys("1"), key.WithHelp("1", "dictionary")),
	Prac:   key.NewBinding(key.WithKeys("2"), key.WithHelp("2", "practice")),
	Cards:  key.NewBinding(key.WithKeys("3"), key.WithHelp("3", "flashcards")),
	Stats:  key.NewBinding(key.WithKeys("4"), key.WithHelp("4", "stats")),
	Home:   key.NewBinding(key.WithKeys("0"), key.WithHelp("0", "home")),
	// Cancel is the one binding that fires even while a screen is capturing
	// text input, so there is always a way out.
	Cancel: key.NewBinding(key.WithKeys("ctrl+c"), key.WithHelp("ctrl+c", "quit")),
}

// ShortHelp implements help.KeyMap for the collapsed footer legend.
func (k KeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Up, k.Down, k.Choose, k.Back, k.Help, k.Quit}
}

// FullHelp implements help.KeyMap for the expanded help overlay.
func (k KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Left, k.Right},
		{k.Choose, k.Back, k.Tab, k.Filter},
		{k.Home, k.Dict, k.Prac, k.Cards},
		{k.Stats, k.Help, k.Quit},
	}
}
