// Package tui implements the Bubble Tea interface: one root model that routes
// between screens, each of which is an independently testable sub-model.
package tui

import (
	"fmt"
	"strings"
	"time"

	"bash-teacher/internal/answer"
	"bash-teacher/internal/content"
	"bash-teacher/internal/runner"
	"bash-teacher/internal/srs"
	"bash-teacher/internal/store"
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

// lookupCommandMsg asks the Dictionary to open one command's entry. It is the
// contextual lookup from the pipeline editor, so the name it carries is a word
// the learner typed and may not be a command at all.
type lookupCommandMsg struct{ name string }

// lookupCommand returns a command that opens a dictionary entry.
func lookupCommand(name string) tea.Cmd {
	return func() tea.Msg { return lookupCommandMsg{name: name} }
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

// sessionStarter is implemented by the Flashcards screen, so that `bt review`
// opens on the day's queue rather than on a menu about it.
type sessionStarter interface{ Start(a *App) }

// exerciseCounter is implemented by the Practice screen, so that Home can
// report the session's progress without holding a reference to it.
type exerciseCounter interface{ PassedCount() int }

// commandSelector is implemented by the Dictionary screen.
type commandSelector interface{ SelectCommand(id string) bool }

// progressRestorer is implemented by the Practice screen, so that the store
// can hand it what it remembers without the root model knowing how the screen
// keeps it.
type progressRestorer interface{ RestoreProgress(rows []store.Exercise) }

// App is the root model.
type App struct {
	Lib   *content.Library
	Theme *theme.Theme
	// Runner executes learner input. It may be nil in tests that never run
	// anything; screens must check before using it.
	Runner *runner.Runner
	// SRS schedules the flashcard deck and holds the review log. It lives on
	// the root model rather than on the flashcards screen because three
	// screens read it: Home reports the day's load, Stats draws the forecast,
	// and Practice credits the cards an exercise teaches.
	//
	// It is loaded from and written through to Store, so a session picks up
	// where the last one stopped.
	SRS *srs.Scheduler
	// Store is the progress database. It may be nil — `--no-store`, and
	// tests that have nothing to remember — in which case everything works
	// exactly as before and the session's progress dies with the process.
	// Screens ask Persisting rather than checking it themselves.
	Store *store.Store
	// Grader normalizes typed flashcard answers against what the dictionary
	// documents.
	Grader  *answer.Grader
	Version string

	// storeErr is the first write that failed. One is enough: a database
	// that cannot be written to will not start working again mid-session,
	// and the learner needs to be told once rather than on every keystroke.
	storeErr error

	// clock is time.Now, indirected so tests can place a session on a known
	// day rather than on whatever day they happen to run.
	clock func() time.Time

	width, height int
	current       Screen
	screens       map[Screen]screen
	help          help.Model
	showHelp      bool
	quitting      bool
	// flash is a transient footer message, cleared by the next keystroke.
	flash string
	// timer reports whether the review screen shows how long an answer took.
	// It defaults to on and is turned off by the settings file.
	timer bool
}

// Timer reports whether the answer timer is shown. It never affects grading:
// SPEC §2.3 makes the soft target a nudge, so the timer is a display setting
// and nothing more.
func (a *App) Timer() bool { return a.timer }

// Option adjusts the root model as it is built. The settings file arrives
// this way too, as values rather than as a *config.Config, so that the TUI
// stays ignorant of the file format and a test can ask for one scheduler
// parameter without writing TOML to say so.
type Option func(*App)

// WithTrack opens Practice on a given track, which is what `bt practice
// <track>` does. An unknown track is ignored; the CLI validates the name
// before it gets here, so that a typo is an error rather than a silent
// landing on the wrong screen.
func WithTrack(name string) Option {
	return func(a *App) {
		if p, ok := a.screens[ScreenPractice].(trackOpener); ok {
			p.OpenTrack(name)
		}
	}
}

// trackOpener is implemented by the Practice screen.
type trackOpener interface{ OpenTrack(name string) bool }

// WithParams sets the scheduler's parameters, which is how the [review] table
// of the settings file reaches the deck. It is order-independent among the
// options: parameters are a preference, so nothing already restored is
// re-derived from them.
func WithParams(p srs.Params) Option {
	return func(a *App) {
		if a.SRS != nil {
			a.SRS.SetParams(p)
		}
	}
}

// WithTimer turns the answer timer on or off. When it is off the review screen
// says nothing about how long an answer took: the timer is a nudge toward
// automaticity, and a learner who does not want to be timed should not be told
// their time and then told it did not count.
func WithTimer(on bool) Option {
	return func(a *App) { a.timer = on }
}

// WithStore attaches the progress database and loads what it holds into the
// scheduler and the practice screen.
//
// It is an option rather than a parameter because a store is optional: the
// TUI is fully usable without one, and every test that does not care about
// persistence should not have to open a database to say so.
func WithStore(db *store.Store) Option {
	return func(a *App) {
		a.Store = db
		a.hydrate()
	}
}

// hydrate restores a session from the store. A failure here is recorded and
// then ignored: an unreadable database should cost the learner their history,
// not their app.
func (a *App) hydrate() {
	if a.Store == nil {
		return
	}
	states, err := a.Store.LoadCards()
	a.record(err)
	log, err := a.Store.LoadReviews()
	a.record(err)
	if a.storeErr == nil && a.SRS != nil {
		a.SRS.Restore(states, log)
	}

	rows, err := a.Store.LoadExercises()
	a.record(err)
	if a.storeErr == nil {
		if p, ok := a.screens[ScreenPractice].(progressRestorer); ok {
			p.RestoreProgress(rows)
		}
	}
}

// record keeps the first store failure. Persisting then reports false for the
// rest of the session, so the screens that would otherwise show a streak or a
// history say plainly that nothing is being saved.
func (a *App) record(err error) {
	if err != nil && a.storeErr == nil {
		a.storeErr = err
	}
}

// Persisting reports whether progress is being saved. Screens ask this before
// describing anything as durable.
func (a *App) Persisting() bool { return a.Store != nil && a.storeErr == nil }

// StoreError returns the failure that stopped progress being saved, if any.
func (a *App) StoreError() error { return a.storeErr }

// New builds the root model with every screen constructed up front, so that
// switching screens is free and each keeps its own cursor position.
func New(lib *content.Library, th *theme.Theme, run *runner.Runner, version string, start Screen, opts ...Option) *App {
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
		Runner:  run,
		SRS:     srs.New(srs.Defaults()),
		Grader:  answer.New(lib),
		Version: version,
		clock:   time.Now,
		timer:   true,
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
	for _, o := range opts {
		o(a)
	}
	// `bt review` asks for the day's cards, not for a menu about them.
	if start == ScreenFlashcards {
		if f, ok := a.screens[ScreenFlashcards].(sessionStarter); ok {
			f.Start(a)
		}
	}
	return a
}

// Now is the app's clock. Everything that schedules or reports on scheduling
// reads the time through it.
func (a *App) Now() time.Time {
	if a.clock == nil {
		return time.Now()
	}
	return a.clock()
}

// Grade compares a typed flashcard answer with what the card expects.
func (a *App) Grade(c *content.Card, typed string) answer.Result {
	if a.Grader == nil {
		return answer.Result{Verdict: answer.Unsure, Reason: "no grader is configured"}
	}
	return a.Grader.Grade(c, typed)
}

// GradeCard applies one answer to a card and saves the result. Grading goes
// through the root model rather than straight to the scheduler so that the
// state and the log entry are written together, by the one place that knows
// whether there is a store at all.
func (a *App) GradeCard(cardID string, r srs.Rating, elapsed time.Duration) {
	if a.SRS == nil {
		return
	}
	a.persistCard(a.SRS.Grade(cardID, r, elapsed, a.Now()))
}

// persistCard writes a card's new state and the log entry that produced it.
// The entry is read back off the scheduler rather than rebuilt here, so what
// is stored is exactly what was scheduled.
func (a *App) persistCard(st srs.State) {
	if a.Store == nil {
		return
	}
	a.record(a.Store.SaveCard(st))
	if r, ok := a.SRS.LastReview(); ok {
		a.record(a.Store.AppendReview(r))
	}
}

// SaveExercise writes one exercise's summary.
func (a *App) SaveExercise(e store.Exercise) {
	if a.Store == nil {
		return
	}
	a.record(a.Store.SaveExercise(e))
}

// LogAttempt appends one run of a learner's pipeline to the attempt log.
func (a *App) LogAttempt(at store.Attempt) {
	if a.Store == nil {
		return
	}
	a.record(a.Store.AppendAttempt(at))
}

// DueCards reports how many cards in the library are due now.
func (a *App) DueCards() int {
	if a.SRS == nil {
		return 0
	}
	return a.SRS.DueCount(cardIDs(a.Lib.Cards), a.Now())
}

// UnseenCards reports how many cards have never been answered.
func (a *App) UnseenCards() int {
	if a.SRS == nil {
		return len(a.Lib.Cards)
	}
	return a.SRS.UnseenCount(cardIDs(a.Lib.Cards))
}

// CreditPractice gives every card an exercise drills the half-strength review
// SPEC §5 grants for solving it: practice reinforces recall without standing
// in for it. It reports how many cards were credited, for the footer line.
func (a *App) CreditPractice(ex *content.Exercise) int {
	if a.SRS == nil {
		return 0
	}
	now := a.Now()
	seen := map[string]bool{}
	for _, id := range ex.Teaches {
		for _, c := range a.Lib.CardsFor(id) {
			if seen[c.ID] {
				continue
			}
			seen[c.ID] = true
			a.persistCard(a.SRS.Credit(c.ID, now))
		}
	}
	return len(seen)
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
	case lookupCommandMsg:
		if d, ok := a.screens[ScreenDictionary].(commandSelector); ok && d.SelectCommand(msg.name) {
			a.current = ScreenDictionary
			return a, nil
		}
		a.flash = msg.name + " is not in the dictionary"
		return a, nil
	case runResultMsg:
		// A run outlives the screen it started on: the learner may be reading
		// a dictionary entry by the time the sandbox answers, and the result
		// still belongs to Practice.
		cmd := a.updateScreen(ScreenPractice, msg)
		return a, cmd
	case spinnerTickMsg:
		cmd := a.updateScreen(ScreenPractice, msg)
		return a, cmd
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

	cmd := a.updateScreen(a.current, msg)
	return a, cmd
}

// PassedExercises reports how many exercises have been solved this session.
func (a *App) PassedExercises() int {
	if p, ok := a.screens[ScreenPractice].(exerciseCounter); ok {
		return p.PassedCount()
	}
	return 0
}

// updateScreen hands a message to one screen and stores whatever it becomes.
func (a *App) updateScreen(s Screen, msg tea.Msg) tea.Cmd {
	next, cmd := a.screens[s].Update(a, msg)
	a.screens[s] = next
	return cmd
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
