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
	// BackTab walks a screen's panes the other way, so a cycle of four is
	// never four presses from where the learner just was.
	BackTab key.Binding
	Dict    key.Binding
	Prac    key.Binding
	Cards   key.Binding
	Stats   key.Binding
	Home    key.Binding
	Cancel  key.Binding

	// Detail-pane and cross-screen actions.
	Act      key.Binding
	Copy     key.Binding
	Practise key.Binding
	Drill    key.Binding
	Pop      key.Binding
	PageUp   key.Binding
	PageDown key.Binding

	// Practice actions. They are all chords because the pipeline editor is
	// capturing text: every printable key belongs to the learner's line.
	Run      key.Binding
	Hint     key.Binding
	Solution key.Binding
	NextEx   key.Binding
	PrevEx   key.Binding
	Preview  key.Binding
	Lookup   key.Binding
	Reset    key.Binding

	// Flashcard review. The ratings are letters rather than the digits SPEC
	// §5 numbers them with, because 1-4 are the global screen switches and a
	// learner mid-session must not be thrown to the dictionary by rating a
	// card "again".
	Reveal    key.Binding
	Submit    key.Binding
	Rate      key.Binding
	RateAgain key.Binding
	RateHard  key.Binding
	RateGood  key.Binding
	RateEasy  key.Binding
	Harder    key.Binding
	Easier    key.Binding
	Start     key.Binding

	// Editing motions inside the pipeline editor. CharLeft and CharRight are
	// separate from Left and Right because those carry vim's h and l, which a
	// learner typing a pipeline needs as letters.
	CharLeft    key.Binding
	CharRight   key.Binding
	OutUp       key.Binding
	OutDown     key.Binding
	Complete    key.Binding
	WordLeft    key.Binding
	WordRight   key.Binding
	LineStart   key.Binding
	LineEnd     key.Binding
	DeleteWord  key.Binding
	KillToStart key.Binding
	KillToEnd   key.Binding
	HistPrev    key.Binding
	HistNext    key.Binding
}

// Keys is the single instance used by the whole app.
var Keys = KeyMap{
	Up:      key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
	Down:    key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
	Left:    key.NewBinding(key.WithKeys("left", "h"), key.WithHelp("←/h", "left")),
	Right:   key.NewBinding(key.WithKeys("right", "l"), key.WithHelp("→/l", "right")),
	Choose:  key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "choose")),
	Back:    key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
	Quit:    key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
	Help:    key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
	Filter:  key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "search")),
	Tab:     key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "pane")),
	BackTab: key.NewBinding(key.WithKeys("shift+tab"), key.WithHelp("shift+tab", "previous pane")),
	Dict:    key.NewBinding(key.WithKeys("1"), key.WithHelp("1", "dictionary")),
	Prac:    key.NewBinding(key.WithKeys("2"), key.WithHelp("2", "practice")),
	Cards:   key.NewBinding(key.WithKeys("3"), key.WithHelp("3", "flashcards")),
	Stats:   key.NewBinding(key.WithKeys("4"), key.WithHelp("4", "stats")),
	Home:    key.NewBinding(key.WithKeys("0"), key.WithHelp("0", "home")),
	// Cancel is the one binding that fires even while a screen is capturing
	// text input, so there is always a way out.
	Cancel: key.NewBinding(key.WithKeys("ctrl+c"), key.WithHelp("ctrl+c", "quit")),

	// Detail-pane actions. Act is enter under a different name, so a screen can
	// advertise what enter will actually do where "choose" would be vague.
	Act:      key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open/copy")),
	Copy:     key.NewBinding(key.WithKeys("y"), key.WithHelp("y", "copy example")),
	Practise: key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "practise")),
	Drill:    key.NewBinding(key.WithKeys("f"), key.WithHelp("f", "flashcards")),
	Pop:      key.NewBinding(key.WithKeys("backspace"), key.WithHelp("bksp", "back")),
	PageUp:   key.NewBinding(key.WithKeys("pgup", "ctrl+u"), key.WithHelp("pgup", "page up")),
	PageDown: key.NewBinding(key.WithKeys("pgdown", "ctrl+d"), key.WithHelp("pgdn", "page down")),

	// Practice actions.
	Run:      key.NewBinding(key.WithKeys("ctrl+r"), key.WithHelp("^R", "run")),
	Hint:     key.NewBinding(key.WithKeys("ctrl+h"), key.WithHelp("^H", "hint")),
	Solution: key.NewBinding(key.WithKeys("ctrl+s"), key.WithHelp("^S", "solution")),
	NextEx:   key.NewBinding(key.WithKeys("ctrl+n"), key.WithHelp("^N", "next")),
	PrevEx:   key.NewBinding(key.WithKeys("ctrl+p"), key.WithHelp("^P", "previous")),
	Preview:  key.NewBinding(key.WithKeys("ctrl+b"), key.WithHelp("^B", "fixture")),
	// The global lookup key is ?, but ? is a shell metacharacter the learner
	// must be able to type, so inside the editor the same action is ^G.
	Lookup: key.NewBinding(key.WithKeys("ctrl+g"), key.WithHelp("^G", "lookup")),
	Reset:  key.NewBinding(key.WithKeys("ctrl+x"), key.WithHelp("^X", "clear")),

	// Flashcard review.
	Reveal: key.NewBinding(key.WithKeys("enter", " "), key.WithHelp("enter", "reveal")),
	Submit: key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "answer")),
	// Rate is the legend for the four rating keys, which the footer shows as
	// one entry rather than four.
	Rate:      key.NewBinding(key.WithKeys("a", "h", "g", "e"), key.WithHelp("a/h/g/e", "again·hard·good·easy")),
	RateAgain: key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "again")),
	RateHard:  key.NewBinding(key.WithKeys("h"), key.WithHelp("h", "hard")),
	RateGood:  key.NewBinding(key.WithKeys("g"), key.WithHelp("g", "good")),
	RateEasy:  key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "easy")),
	// SPEC §5 reaches hard and easy with j and k before advancing; they nudge
	// the rating the answer earned rather than replacing it.
	Harder: key.NewBinding(key.WithKeys("j", "down"), key.WithHelp("j", "harder")),
	Easier: key.NewBinding(key.WithKeys("k", "up"), key.WithHelp("k", "easier")),
	Start:  key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "start reviewing")),

	// Editing motions. ctrl+a/e, ctrl+w and ctrl+u/k are readline's, because
	// that is what a shell user's fingers already know.
	CharLeft:  key.NewBinding(key.WithKeys("left"), key.WithHelp("←", "left")),
	CharRight: key.NewBinding(key.WithKeys("right"), key.WithHelp("→", "right")),
	// The output pane scrolls with the page keys alone: ctrl+u and ctrl+d
	// belong to the editor here.
	OutUp:       key.NewBinding(key.WithKeys("pgup"), key.WithHelp("pgup", "scroll output up")),
	OutDown:     key.NewBinding(key.WithKeys("pgdown"), key.WithHelp("pgdn", "scroll output down")),
	Complete:    key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "complete")),
	WordLeft:    key.NewBinding(key.WithKeys("alt+left", "alt+b"), key.WithHelp("alt+←", "word left")),
	WordRight:   key.NewBinding(key.WithKeys("alt+right", "alt+f"), key.WithHelp("alt+→", "word right")),
	LineStart:   key.NewBinding(key.WithKeys("ctrl+a", "home"), key.WithHelp("^A", "line start")),
	LineEnd:     key.NewBinding(key.WithKeys("ctrl+e", "end"), key.WithHelp("^E", "line end")),
	DeleteWord:  key.NewBinding(key.WithKeys("ctrl+w"), key.WithHelp("^W", "delete word")),
	KillToStart: key.NewBinding(key.WithKeys("ctrl+u"), key.WithHelp("^U", "clear to start")),
	KillToEnd:   key.NewBinding(key.WithKeys("ctrl+k"), key.WithHelp("^K", "clear to end")),
	HistPrev:    key.NewBinding(key.WithKeys("up"), key.WithHelp("↑", "previous line")),
	HistNext:    key.NewBinding(key.WithKeys("down"), key.WithHelp("↓", "next line")),
}

// ShortHelp implements help.KeyMap for the collapsed footer legend. The
// receiver is a pointer because a KeyMap is a few kilobytes of bindings.
func (k *KeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Up, k.Down, k.Choose, k.Back, k.Help, k.Quit}
}

// FullHelp implements help.KeyMap for the expanded help overlay.
func (k *KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Left, k.Right},
		{k.Choose, k.Back, k.Tab, k.BackTab},
		{k.Filter, k.Help, k.Quit},
		{k.Home, k.Dict, k.Prac, k.Cards},
		{k.Stats, k.Help, k.Quit},
		{k.Act, k.Copy, k.Practise, k.Drill},
		{k.Pop, k.PageUp, k.PageDown},
		{k.Run, k.Hint, k.Solution, k.NextEx},
		{k.Preview, k.Lookup, k.Reset, k.Complete},
		{k.LineStart, k.LineEnd, k.DeleteWord, k.KillToEnd},
		{k.Reveal, k.Rate, k.Harder, k.Easier},
	}
}
