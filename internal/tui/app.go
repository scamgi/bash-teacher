// Package tui implements the Bubble Tea interface: one root model that routes
// between screens, each of which is an independently testable sub-model.
package tui

import (
	"fmt"
	"strings"

	"bash-teacher/internal/content"
	"bash-teacher/internal/theme"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// minWidth and minHeight are the smallest terminal the layouts are designed
// for. Below either, the app draws a single-pane warning instead of a broken
// layout.
const (
	minWidth  = 80
	minHeight = 24
)

// Screen identifies a top-level destination.
type Screen int

// The top-level screens, in the order their number keys select them.
const (
	ScreenHome Screen = iota
	ScreenDictionary
	ScreenPractice
	ScreenFlashcards
	ScreenStats
)

func (s Screen) String() string {
	switch s {
	case ScreenDictionary:
		return "Dictionary"
	case ScreenPractice:
		return "Practice"
	case ScreenFlashcards:
		return "Flashcards"
	case ScreenStats:
		return "Stats"
	default:
		return "Home"
	}
}

// screen is what every sub-model implements. Sub-models never quit the program
// or switch screens themselves; they emit a navigateMsg and let the root model
// decide.
type screen interface {
	Update(*App, tea.Msg) (screen, tea.Cmd)
	// Body renders the screen's content into the given interior size, which
	// excludes the app's header and footer.
	Body(a *App, width, height int) string
	// Help returns the bindings this screen wants in the footer legend.
	Help() []key.Binding
	// Capturing reports whether the screen is consuming raw text input, in
	// which case the root model must not treat digits or "q" as navigation.
	Capturing() bool
}

// navigateMsg asks the root model to switch screens.
type navigateMsg struct{ to Screen }

// Navigate returns a command that switches to the given screen.
func Navigate(to Screen) tea.Cmd {
	return func() tea.Msg { return navigateMsg{to: to} }
}

// openExerciseMsg asks Practice to switch to a particular exercise. Screens
// address each other through the root model rather than holding references to
// one another, so the routing stays in one place.
type openExerciseMsg struct{ id string }

// openExercise returns a command that opens an exercise in Practice.
func openExercise(id string) tea.Cmd {
	return func() tea.Msg { return openExerciseMsg{id: id} }
}

// showCardsMsg asks Flashcards to show only one command's cards.
type showCardsMsg struct{ commandID string }

// showCards returns a command that filters the deck to one command.
func showCards(commandID string) tea.Cmd {
	return func() tea.Msg { return showCardsMsg{commandID: commandID} }
}

// flashMsg carries a transient line for the footer: what a key just did, or
// why it did nothing.
type flashMsg struct{ text string }

// flash returns a command that shows a transient footer message.
func flash(text string) tea.Cmd {
	return func() tea.Msg { return flashMsg{text: text} }
}

// exerciseOpener is implemented by the Practice screen.
type exerciseOpener interface{ OpenExercise(id string) bool }

// cardFilterer is implemented by the Flashcards screen.
type cardFilterer interface{ ShowCommand(commandID string) bool }

// App is the root model.
type App struct {
	Lib     *content.Library
	Theme   *theme.Theme
	Version string

	width, height int
	current       Screen
	screens       map[Screen]screen
	help          help.Model
	showHelp      bool
	quitting      bool
	// flash is a transient footer message, cleared by the next keystroke.
	flash string
}

// New builds the root model with every screen constructed up front, so that
// switching screens is free and each keeps its own cursor position.
func New(lib *content.Library, th *theme.Theme, version string, start Screen) *App {
	h := help.New()
	h.Styles = help.DefaultStyles(th.IsDark())
	h.Styles.ShortKey = th.Key
	h.Styles.ShortDesc = th.Footer
	h.Styles.ShortSeparator = th.Faint
	h.Styles.FullKey = th.Key
	h.Styles.FullDesc = th.Footer
	h.Styles.FullSeparator = th.Faint

	a := &App{
		Lib:     lib,
		Theme:   th,
		Version: version,
		current: start,
		help:    h,
		screens: map[Screen]screen{
			ScreenHome:       newHome(),
			ScreenDictionary: newDictionary(lib),
			ScreenPractice:   newPractice(lib),
			ScreenFlashcards: newFlashcards(lib),
			ScreenStats:      newStats(lib),
		},
	}
	return a
}

// Init implements tea.Model.
func (a *App) Init() tea.Cmd { return nil }

// Update implements tea.Model.
func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width, a.height = msg.Width, msg.Height
		a.help.SetWidth(msg.Width)
	case navigateMsg:
		a.current = msg.to
		return a, nil
	case openExerciseMsg:
		if p, ok := a.screens[ScreenPractice].(exerciseOpener); ok && p.OpenExercise(msg.id) {
			a.current = ScreenPractice
		}
		return a, nil
	case showCardsMsg:
		if f, ok := a.screens[ScreenFlashcards].(cardFilterer); ok && f.ShowCommand(msg.commandID) {
			a.current = ScreenFlashcards
		}
		return a, nil
	case flashMsg:
		a.flash = msg.text
		return a, nil
	case tea.KeyPressMsg:
		// Any keystroke clears the previous flash, so a message never lingers
		// past the action that produced it.
		a.flash = ""
		if cmd, handled := a.handleGlobalKey(msg); handled {
			return a, cmd
		}
	}

	next, cmd := a.screens[a.current].Update(a, msg)
	a.screens[a.current] = next
	return a, cmd
}

// handleGlobalKey applies the bindings that work everywhere. It defers to the
// active screen whenever that screen is capturing text, so typing "1" into a
// search box does not navigate away.
func (a *App) handleGlobalKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	capturing := a.screens[a.current].Capturing()

	// Ctrl-C always quits, even mid-input.
	if key.Matches(msg, Keys.Cancel) {
		a.quitting = true
		return tea.Quit, true
	}
	if a.showHelp {
		// Any key dismisses the help overlay.
		a.showHelp = false
		return nil, true
	}
	if capturing {
		return nil, false
	}

	switch {
	case key.Matches(msg, Keys.Help):
		a.showHelp = true
		return nil, true
	case key.Matches(msg, Keys.Quit):
		if a.current == ScreenHome {
			a.quitting = true
			return tea.Quit, true
		}
		a.current = ScreenHome
		return nil, true
	case key.Matches(msg, Keys.Back):
		if a.current != ScreenHome {
			a.current = ScreenHome
			return nil, true
		}
		return nil, false
	case key.Matches(msg, Keys.Home):
		a.current = ScreenHome
		return nil, true
	case key.Matches(msg, Keys.Dict):
		a.current = ScreenDictionary
		return nil, true
	case key.Matches(msg, Keys.Prac):
		a.current = ScreenPractice
		return nil, true
	case key.Matches(msg, Keys.Cards):
		a.current = ScreenFlashcards
		return nil, true
	case key.Matches(msg, Keys.Stats):
		a.current = ScreenStats
		return nil, true
	}
	return nil, false
}

// View implements tea.Model. In Bubble Tea v2 the alternate screen is a
// property of the view rather than a program option, so it is set here.
func (a *App) View() tea.View {
	if a.quitting {
		return altView("")
	}
	if a.width == 0 {
		// The first frame arrives before the terminal size does.
		return altView("")
	}
	if a.width < minWidth || a.height < minHeight {
		return altView(a.tooSmall())
	}

	header := a.header()
	footer := a.footer()
	bodyHeight := a.height - lipgloss.Height(header) - lipgloss.Height(footer)
	if bodyHeight < 1 {
		bodyHeight = 1
	}

	var body string
	if a.showHelp {
		body = a.helpOverlay(a.width, bodyHeight)
	} else {
		body = a.screens[a.current].Body(a, a.width, bodyHeight)
	}
	body = fitBlock(body, a.width, bodyHeight)

	return altView(strings.Join([]string{header, body, footer}, "\n"))
}

// altView wraps a rendered frame as a full-screen view.
func altView(s string) tea.View {
	v := tea.NewView(s)
	v.AltScreen = true
	return v
}

func (a *App) header() string {
	t := a.Theme
	left := t.Title.Render("bash-teacher")
	crumb := t.Dim.Render(" › ") + t.Subtitle.Render(a.current.String())
	if a.current == ScreenHome {
		crumb = ""
	}
	right := t.Faint.Render(a.Version)

	gap := a.width - lipgloss.Width(left+crumb) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	line := left + crumb + strings.Repeat(" ", gap) + right
	rule := t.Faint.Render(strings.Repeat("─", a.width))
	return line + "\n" + rule
}

func (a *App) footer() string {
	rule := a.Theme.Faint.Render(strings.Repeat("─", a.width))
	if a.flash != "" {
		return rule + "\n" + truncate(a.Theme.Warn.Render(a.flash), a.width)
	}
	bindings := a.screens[a.current].Help()
	if len(bindings) == 0 {
		bindings = Keys.ShortHelp()
	}
	return rule + "\n" + truncate(a.help.ShortHelpView(bindings), a.width)
}

func (a *App) tooSmall() string {
	msg := fmt.Sprintf("Terminal is %d×%d.\nbash-teacher needs at least %d×%d.\nResize and the layout will return.",
		a.width, a.height, minWidth, minHeight)
	return lipgloss.Place(a.width, a.height, lipgloss.Center, lipgloss.Center, a.Theme.Warn.Render(msg))
}

// helpOverlay renders the expanded key reference over the body area.
func (a *App) helpOverlay(width, height int) string {
	inner := a.help
	inner.SetWidth(width - 4)
	panel := a.Theme.Title.Render("Keys") + "\n\n" + inner.FullHelpView(Keys.FullHelp()) +
		"\n\n" + a.Theme.Faint.Render("press any key to close")
	box := a.Theme.Panel.Render(panel)
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
}

// fitBlock pads or truncates a rendered block to exactly w×h so that the header
// and footer never drift as screens change height.
func fitBlock(s string, w, h int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > h {
		lines = lines[:h]
	}
	for len(lines) < h {
		lines = append(lines, "")
	}
	for i, ln := range lines {
		if lipgloss.Width(ln) > w {
			lines[i] = truncate(ln, w)
		}
	}
	return strings.Join(lines, "\n")
}
