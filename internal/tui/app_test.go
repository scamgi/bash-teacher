package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	embedded "bash-teacher/content"
	"bash-teacher/internal/content"
	"bash-teacher/internal/runner"
	"bash-teacher/internal/srs"
	"bash-teacher/internal/theme"
)

// newTestApp builds the root model against the real library at a known size,
// with colour off so assertions compare plain text.
func newTestApp(t *testing.T, w, h int) *App {
	t.Helper()
	lib, err := content.Load(embedded.FS)
	if err != nil {
		t.Fatalf("load content: %v", err)
	}
	a := New(lib, theme.Resolve(theme.None), runner.New(lib), "test", ScreenHome)
	a.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return a
}

// namedKeys maps the key names used in tests to their v2 key codes. Anything
// not listed is treated as printable text, so press("y") types a y.
var namedKeys = map[string]rune{
	"enter":     tea.KeyEnter,
	"esc":       tea.KeyEscape,
	"up":        tea.KeyUp,
	"down":      tea.KeyDown,
	"left":      tea.KeyLeft,
	"right":     tea.KeyRight,
	"backspace": tea.KeyBackspace,
	"tab":       tea.KeyTab,
}

// keyMsg builds the message a terminal would send for a key name.
func keyMsg(s string) tea.KeyPressMsg {
	if code, ok := namedKeys[s]; ok {
		return tea.KeyPressMsg{Code: code}
	}
	return tea.KeyPressMsg{Code: rune(s[0]), Text: s}
}

// press feeds a key and renders, the way the event loop does. Rendering matters:
// the dictionary builds its detail pane during View, so a test that never
// renders would see an empty item list.
func press(a *App, s string) {
	a.Update(keyMsg(s))
	_ = a.View()
}

func view(a *App) string { return a.View().Content }

// pressKey feeds a key message built by the caller, for the chords the name
// table does not cover.
func pressKey(a *App, km tea.KeyPressMsg) {
	a.Update(km)
	_ = a.View()
}

// runNow presses ^R and drives the run command to completion, which is how a
// headless test observes the async sandbox flow the event loop would.
func runNow(t *testing.T, a *App) {
	t.Helper()
	_, cmd := a.Update(tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("^R produced no command")
	}
	deliver(a, cmd)
	_ = a.View()
}

// deliver runs a command and feeds back every message it produces, following
// batches, until the run result has been applied.
func deliver(a *App, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	msg := cmd()
	switch m := msg.(type) {
	case nil:
	case tea.BatchMsg:
		for _, c := range m {
			deliver(a, c)
		}
	case spinnerTickMsg:
		// Ticking here would loop forever; the run result is what matters.
	default:
		_, next := a.Update(m)
		deliver(a, next)
	}
}

// pressCmd feeds a key and then runs whatever command it produced, which is how
// the cross-screen shortcuts take effect in a headless test.
func pressCmd(a *App, s string) {
	_, cmd := a.Update(keyMsg(s))
	_ = a.View()
	for cmd != nil {
		out := cmd()
		if out == nil {
			break
		}
		if batch, ok := out.(tea.BatchMsg); ok {
			cmd = nil
			for _, c := range batch {
				if m := c(); m != nil {
					_, cmd = a.Update(m)
				}
			}
			continue
		}
		_, cmd = a.Update(out)
	}
}

func TestHomeRendersLibraryCounts(t *testing.T) {
	a := newTestApp(t, 100, 30)
	v := view(a)
	for _, want := range []string{"bash-teacher", "Dictionary", "Practice", "Flashcards", "Stats", "Library"} {
		if !strings.Contains(v, want) {
			t.Errorf("home view is missing %q\n%s", want, v)
		}
	}
}

// TestHomeReportsSandboxBackend covers the notice SPEC §6.2 requires: Home
// always says how much confinement a run will get, and says so with a glyph
// and not only with colour when there is none.
func TestHomeReportsSandboxBackend(t *testing.T) {
	lib, err := content.Load(embedded.FS)
	if err != nil {
		t.Fatalf("load content: %v", err)
	}
	cases := []struct {
		name string
		run  *runner.Runner
		want string
	}{
		{"confined", runner.New(lib), runner.DetectSandbox().Describe()},
		{"no OS sandbox", runner.New(lib, runner.WithSandbox(runner.BareSandbox())),
			"⚠ Running without an OS sandbox"},
		{"execution disabled", runner.New(lib, runner.WithNoExec(true)), "⚠ --no-exec"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := New(lib, theme.Resolve(theme.None), tc.run, "test", ScreenHome)
			a.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
			if got := view(a); !strings.Contains(got, tc.want) {
				t.Errorf("Home does not mention %q:\n%s", tc.want, got)
			}
		})
	}
}

func TestNumberKeysNavigate(t *testing.T) {
	a := newTestApp(t, 100, 30)
	for _, tc := range []struct {
		key    string
		screen Screen
	}{{"1", ScreenDictionary}, {"2", ScreenPractice}, {"3", ScreenFlashcards}, {"4", ScreenStats}} {
		press(a, tc.key)
		if a.current != tc.screen {
			t.Fatalf("key %q went to %v, want %v", tc.key, a.current, tc.screen)
		}
		if !strings.Contains(view(a), tc.screen.String()) {
			t.Errorf("%v view does not name itself in the header", tc.screen)
		}
	}
	press(a, "esc")
	if a.current != ScreenHome {
		t.Errorf("esc should return to home, got %v", a.current)
	}
}

func TestEnterOpensSelectedMenuItem(t *testing.T) {
	a := newTestApp(t, 100, 30)
	press(a, "down")
	_, cmd := a.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter on a menu item should return a navigation command")
	}
	a.Update(cmd())
	if a.current != ScreenPractice {
		t.Errorf("expected Practice after down+enter, got %v", a.current)
	}
}

func TestDictionaryShowsAndFiltersCommands(t *testing.T) {
	a := newTestApp(t, 100, 30)
	press(a, "1")
	if !strings.Contains(view(a), "FILES & NAVIGATION") {
		t.Fatalf("the unfiltered dictionary should be grouped by category:\n%s", view(a))
	}

	typeIn(a, "/", "uniq")
	v := view(a)
	if !strings.Contains(v, "uniq") {
		t.Errorf("filtered dictionary should show uniq:\n%s", v)
	}
	if strings.Contains(v, "FILES & NAVIGATION") {
		t.Errorf("filtering should drop the category headings in favour of ranking:\n%s", v)
	}
}

// typeIn opens an input with the given key — or, with an empty key, types into
// whatever is already capturing — then types a string into it.
func typeIn(a *App, open, text string) {
	if open != "" {
		press(a, open)
	}
	for _, r := range text {
		press(a, string(r))
	}
}

// dictOf reaches into the dictionary sub-model for assertions that would be
// fragile to make against rendered text.
func dictOf(a *App) *dictionaryScreen { return a.screens[ScreenDictionary].(*dictionaryScreen) }

// TestFuzzySearchRanksTheNamedCommandFirst is the property that makes the
// search box worth having: typing a name selects that command, even though the
// pattern is a subsequence of many others.
func TestFuzzySearchRanksTheNamedCommandFirst(t *testing.T) {
	a := newTestApp(t, 100, 30)
	press(a, "1")
	typeIn(a, "/", "grep")
	d := dictOf(a)
	if got := d.current(); got == nil || got.Name != "grep" {
		t.Fatalf("expected grep to be selected, got %v", got)
	}
}

func TestFuzzySearchMatchesSubsequences(t *testing.T) {
	a := newTestApp(t, 100, 30)
	press(a, "1")
	typeIn(a, "/", "usr")
	d := dictOf(a)
	var names []string
	for _, r := range d.rows {
		if r.cmd != nil {
			names = append(names, r.cmd.Name)
		}
	}
	if len(names) == 0 {
		t.Fatal("a subsequence search should match something")
	}
	// "usr" is not a substring of any command name, so every hit proves the
	// matcher is doing subsequence work rather than strings.Contains.
	for _, n := range names {
		if strings.Contains(n, "usr") {
			t.Fatalf("%q contains the pattern literally; pick a better probe", n)
		}
	}
}

// TestDictionaryJumpsAndComesBack covers the "plays well with" navigation and
// the history stack that makes it safe to follow.
func TestDictionaryJumpsAndComesBack(t *testing.T) {
	a := newTestApp(t, 120, 40)
	press(a, "1")
	typeIn(a, "/", "sort")
	press(a, "enter") // leave the filter, keeping sort selected
	d := dictOf(a)
	if d.current().Name != "sort" {
		t.Fatalf("expected sort, got %s", d.current().Name)
	}

	press(a, "right") // focus the detail pane
	if d.focus != focusDetail {
		t.Fatal("right should move focus into the detail pane")
	}
	// Walk to the first related command and follow it.
	target := ""
	for i, it := range d.items {
		if it.kind == itemRelated {
			d.item = i
			target = it.text
			break
		}
	}
	if target == "" {
		t.Fatal("sort should list related commands")
	}
	press(a, "enter")
	if got := d.current(); got == nil || got.ID != target {
		t.Fatalf("enter on a related command should jump to %q, got %v", target, got)
	}
	press(a, "backspace")
	if got := d.current(); got == nil || got.Name != "sort" {
		t.Fatalf("backspace should return to sort, got %v", got)
	}
}

// TestDictionaryOpensPractice checks the p shortcut hands off to the Practice
// screen on an exercise that actually teaches the command.
func TestDictionaryOpensPractice(t *testing.T) {
	a := newTestApp(t, 100, 30)
	press(a, "1")
	typeIn(a, "/", "uniq")
	press(a, "enter")
	pressCmd(a, "p")
	if a.current != ScreenPractice {
		t.Fatalf("p should open Practice, got %v", a.current)
	}
	ex := a.screens[ScreenPractice].(*practiceScreen)
	teaches := ex.rows[ex.cursor].ex.Teaches
	if !containsString(teaches, "uniq") {
		t.Errorf("landed on an exercise that does not teach uniq: %v", teaches)
	}
}

// TestDictionaryFiltersFlashcards checks the f shortcut narrows the deck.
func TestDictionaryFiltersFlashcards(t *testing.T) {
	a := newTestApp(t, 100, 30)
	press(a, "1")
	typeIn(a, "/", "grep")
	press(a, "enter")
	pressCmd(a, "f")
	if a.current != ScreenFlashcards {
		t.Fatalf("f should open Flashcards, got %v", a.current)
	}
	f := a.screens[ScreenFlashcards].(*flashcardsScreen)
	if f.filter != "grep" {
		t.Fatalf("deck should be filtered to grep, got %q", f.filter)
	}
	if len(f.queue) == 0 {
		t.Fatal("a filtered session should open on the command's cards")
	}
	for _, c := range f.queue {
		if !containsString(c.Commands, "grep") {
			t.Errorf("card %s is not a grep card", c.ID)
		}
	}
	// esc leaves the drill for the scheduled queue rather than the app: a
	// learner who came here from the dictionary is still mid-session.
	press(a, "esc")
	if f.filter != "" {
		t.Error("esc should leave the filtered drill")
	}
}

// TestCopyExampleFlashes checks the y shortcut reports what it copied; the
// clipboard write itself is an escape sequence the harness cannot observe.
func TestCopyExampleFlashes(t *testing.T) {
	a := newTestApp(t, 100, 30)
	press(a, "1")
	typeIn(a, "/", "grep")
	press(a, "enter")
	press(a, "right")
	pressCmd(a, "y")
	if !strings.Contains(a.flash, "copied:") {
		t.Fatalf("y should report what it copied, got flash %q", a.flash)
	}
	if !strings.Contains(view(a), "copied:") {
		t.Error("the flash should be visible in the footer")
	}
}

// TestFlashClearsOnNextKey keeps a message from outliving the action.
func TestFlashClearsOnNextKey(t *testing.T) {
	a := newTestApp(t, 100, 30)
	press(a, "1")
	press(a, "right")
	pressCmd(a, "y")
	if a.flash == "" {
		t.Fatal("expected a flash")
	}
	press(a, "down")
	if a.flash != "" {
		t.Errorf("the flash should clear on the next keystroke, got %q", a.flash)
	}
}

func containsString(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}

// TestFilterSwallowsNavigationKeys guards the rule that a screen capturing text
// keeps the global digit shortcuts from firing mid-word.
func TestFilterSwallowsNavigationKeys(t *testing.T) {
	a := newTestApp(t, 100, 30)
	press(a, "1")
	press(a, "/")
	press(a, "2")
	if a.current != ScreenDictionary {
		t.Errorf("typing into the filter must not navigate, got %v", a.current)
	}
	press(a, "q")
	if a.current != ScreenDictionary {
		t.Errorf("typing q into the filter must not quit, got %v", a.current)
	}
}

func TestHelpOverlayTogglesWithAnyKey(t *testing.T) {
	a := newTestApp(t, 100, 30)
	press(a, "?")
	if !strings.Contains(view(a), "Keys") {
		t.Fatalf("? should open the help overlay:\n%s", view(a))
	}
	press(a, "x")
	if strings.Contains(view(a), "press any key to close") {
		t.Error("any key should dismiss the help overlay")
	}
}

func TestSmallTerminalShowsResizeNotice(t *testing.T) {
	a := newTestApp(t, 60, 20)
	if !strings.Contains(view(a), "at least") {
		t.Errorf("a small terminal should explain the minimum size:\n%s", view(a))
	}
}

// TestFrameFillsTerminal keeps the header and footer pinned: every screen must
// render exactly the terminal's height and stay within its width.
func TestFrameFillsTerminal(t *testing.T) {
	const w, h = 100, 30
	a := newTestApp(t, w, h)
	for _, s := range []Screen{ScreenHome, ScreenDictionary, ScreenPractice, ScreenFlashcards, ScreenStats} {
		a.current = s
		lines := strings.Split(view(a), "\n")
		if len(lines) != h {
			t.Errorf("%v rendered %d lines, want %d", s, len(lines), h)
		}
		for i, ln := range lines {
			if n := len([]rune(stripANSI(ln))); n > w {
				t.Errorf("%v line %d is %d cells wide, want <= %d", s, i, n, w)
			}
		}
	}
}

// stripANSI removes escape sequences so widths can be measured in tests even
// when a theme with colour is used.
func stripANSI(s string) string {
	var b strings.Builder
	inEsc := false
	for _, r := range s {
		switch {
		case r == '\x1b':
			inEsc = true
		case inEsc && (r == 'm' || r == 'K'):
			inEsc = false
		case !inEsc:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// cardsOf reaches into the flashcards sub-model, the way dictOf does for the
// dictionary.
func cardsOf(a *App) *flashcardsScreen { return a.screens[ScreenFlashcards].(*flashcardsScreen) }

// firstCardOfType finds a card of a given kind in the real library, so the
// review tests do not pin themselves to one card's id.
func firstCardOfType(t *testing.T, a *App, kind content.CardType) *content.Card {
	t.Helper()
	for _, c := range a.Lib.Cards {
		if c.Type == kind {
			return c
		}
	}
	t.Fatalf("no %s card in the library", kind)
	return nil
}

// reviewOne puts the screen into a one-card session, which is how these tests
// get a known card in front of them without answering their way to it.
func reviewOne(a *App, c *content.Card) *flashcardsScreen {
	f := cardsOf(a)
	a.current = ScreenFlashcards
	f.filter = ""
	f.begin([]*content.Card{c})
	_ = a.View()
	return f
}

// TestReviewSessionStartsFromTheScheduler checks that entering the screen
// offers the day's queue and that starting it takes cards up to the session
// size rather than the whole deck.
func TestReviewSessionStartsFromTheScheduler(t *testing.T) {
	a := newTestApp(t, 100, 30)
	press(a, "3")
	if !strings.Contains(view(a), "enter starts a session") {
		t.Fatalf("the flashcards screen should offer a session:\n%s", view(a))
	}
	press(a, "enter")

	f := cardsOf(a)
	size := a.SRS.Params().SessionSize
	if len(f.queue) == 0 || len(f.queue) > size {
		t.Fatalf("session holds %d cards, want between 1 and %d", len(f.queue), size)
	}
	if f.phase != phaseAsk {
		t.Errorf("phase = %v, want the first card to be up", f.phase)
	}
}

// TestTypedAnswerIsGradedAndScheduled walks one recall card end to end: type
// the answer the card wants, see it accepted, rate it, and find the scheduler
// holding a state for it afterwards.
func TestTypedAnswerIsGradedAndScheduled(t *testing.T) {
	a := newTestApp(t, 100, 30)
	c := firstCardOfType(t, a, content.CardRecall)
	f := reviewOne(a, c)

	typeIn(a, "", c.Back)
	press(a, "enter")

	if f.phase != phaseGrade {
		t.Fatalf("enter should submit the answer, phase = %v", f.phase)
	}
	if got := view(a); !strings.Contains(got, "✓ correct") {
		t.Fatalf("the card's own answer should be accepted:\n%s", got)
	}
	if f.rating != srs.Good {
		t.Errorf("a correct answer should preselect good, got %v", f.rating)
	}
	if !strings.Contains(view(a), "g good") {
		t.Error("the rating keys and the interval each one buys should be on screen")
	}

	press(a, "g")
	st, ok := a.SRS.State(c.ID)
	if !ok || !st.Seen() {
		t.Fatalf("rating a card should schedule it, state = %+v", st)
	}
	if st.Due.Before(a.Now()) {
		t.Errorf("a card rated good should be due in the future, got %v", st.Due)
	}
}

// TestNormalizedAnswerIsAccepted is the point of the whole grader: an answer
// that is right but spelled differently must not be marked wrong.
func TestNormalizedAnswerIsAccepted(t *testing.T) {
	a := newTestApp(t, 100, 30)
	c := &content.Card{
		ID: "test-card", Type: content.CardRecall,
		Front: "Count the ERROR lines in app.log.", Back: "grep -c ERROR app.log",
		Commands: []string{"grep"},
	}
	reviewOne(a, c)
	typeIn(a, "", "grep --count ERROR app.log")
	press(a, "enter")
	if got := view(a); !strings.Contains(got, "✓ correct") {
		t.Fatalf("the documented long form should be accepted:\n%s", got)
	}
}

// TestWrongAnswerRequeuesTheCard holds SPEC §2.3's rule that a miss comes back
// inside the same sitting rather than being pushed into the future.
func TestWrongAnswerRequeuesTheCard(t *testing.T) {
	a := newTestApp(t, 100, 30)
	c := firstCardOfType(t, a, content.CardRecall)
	f := reviewOne(a, c)

	typeIn(a, "", "cat nothing.txt")
	press(a, "enter")
	if f.rating != srs.Again {
		t.Errorf("a wrong answer should preselect again, got %v", f.rating)
	}
	if got := view(a); !strings.Contains(got, "✗") || !strings.Contains(got, c.Back) {
		t.Fatalf("a miss should say so and show the expected answer:\n%s", got)
	}

	press(a, "enter") // accept the preselected rating
	if len(f.queue) != 2 || f.card() != c {
		t.Fatalf("a missed card should come back in the same session, queue = %d", len(f.queue))
	}
	if st, _ := a.SRS.State(c.ID); st.Lapses != 1 {
		t.Errorf("lapses = %d, want 1", st.Lapses)
	}
}

// TestAnswerEditorOwnsEveryPrintableKey is the Capturing() contract: a learner
// typing a digit or a q into an answer must not be navigated away.
func TestAnswerEditorOwnsEveryPrintableKey(t *testing.T) {
	a := newTestApp(t, 100, 30)
	f := reviewOne(a, firstCardOfType(t, a, content.CardRecall))
	typeIn(a, "", "cut -d2 -f1 q")
	if a.current != ScreenFlashcards {
		t.Fatalf("typing an answer navigated to %v", a.current)
	}
	if got := f.editor.Value(); got != "cut -d2 -f1 q" {
		t.Errorf("editor holds %q, want the whole answer", got)
	}
}

// TestIdentifyCardIsSelfGraded checks the card type that has no typed answer:
// it turns over on a keypress and the learner rates their own reading.
func TestIdentifyCardIsSelfGraded(t *testing.T) {
	a := newTestApp(t, 100, 30)
	c := firstCardOfType(t, a, content.CardIdentify)
	f := reviewOne(a, c)

	if f.Capturing() {
		t.Error("a self-graded card has no answer box to capture keys")
	}
	if !strings.Contains(view(a), "say what this does") {
		t.Fatalf("an identify card should start face down:\n%s", view(a))
	}
	press(a, "enter")
	if f.phase != phaseGrade {
		t.Fatalf("enter should turn the card over, phase = %v", f.phase)
	}
	if !strings.Contains(stripANSI(view(a)), oneLine(c.Back)[:20]) {
		t.Errorf("the reading should be on screen:\n%s", view(a))
	}
}

// TestPassingAnExerciseCreditsItsCards holds SPEC §5's rule that practice
// reinforces recall: solving an exercise moves every card it teaches.
func TestPassingAnExerciseCreditsItsCards(t *testing.T) {
	a := newTestApp(t, 100, 30)
	var ex *content.Exercise
	for _, e := range a.Lib.Exercises {
		for _, id := range e.Teaches {
			if len(a.Lib.CardsFor(id)) > 0 {
				ex = e
				break
			}
		}
		if ex != nil {
			break
		}
	}
	if ex == nil {
		t.Skip("no exercise in the library teaches a command that has cards")
	}

	if n := a.CreditPractice(ex); n == 0 {
		t.Fatal("CreditPractice credited nothing")
	}
	credited := 0
	for _, id := range ex.Teaches {
		for _, c := range a.Lib.CardsFor(id) {
			st, ok := a.SRS.State(c.ID)
			if ok && st.Reps > 0 {
				credited++
			}
		}
	}
	if credited == 0 {
		t.Fatal("no card was scheduled by the exercise pass")
	}
	for _, r := range a.SRS.Log() {
		if r.Source != srs.SourcePractice {
			t.Errorf("credit logged as %q, want practice", r.Source)
		}
	}
}

// TestASessionCanBeWorkedToTheEnd walks a whole sitting: answer every card
// with what it asks for, rate each one, and end on the tally. It is the guard
// against a session that cannot be finished — a card that never leaves the
// grading phase, or a queue that never empties.
func TestASessionCanBeWorkedToTheEnd(t *testing.T) {
	a := newTestApp(t, 100, 30)
	press(a, "3")
	press(a, "enter")
	f := cardsOf(a)
	started := len(f.queue)
	if started == 0 {
		t.Fatal("the session opened empty")
	}

	for i := 0; i < started+5 && f.phase != phaseIdle; i++ {
		c := f.card()
		if c == nil {
			break
		}
		if !c.SelfGraded() {
			typeIn(a, "", c.Back)
		}
		press(a, "enter")
		if f.phase != phaseGrade {
			t.Fatalf("card %s did not reach the rating step", c.ID)
		}
		press(a, "g")
	}

	if f.phase != phaseIdle {
		t.Fatalf("the session did not finish, %d cards in", f.answered)
	}
	if f.answered != started {
		t.Errorf("answered %d of the %d cards the session held", f.answered, started)
	}
	if got := stripANSI(view(a)); !strings.Contains(got, "Answered") {
		t.Errorf("the tally should be on screen:\n%s", got)
	}

	// A second sitting on the same day is capped, and says why rather than
	// throwing away what the first one came to.
	press(a, "enter")
	got := stripANSI(view(a))
	if !strings.Contains(got, "new cards") {
		t.Errorf("an empty session should explain the daily cap:\n%s", got)
	}
	if !strings.Contains(got, "Answered") {
		t.Error("an empty session should not wipe the tally it is reporting")
	}
}

// TestHomeReportsTheDayLoad checks that Home answers the question it exists to
// answer: what is waiting today.
func TestHomeReportsTheDayLoad(t *testing.T) {
	a := newTestApp(t, 100, 30)
	got := stripANSI(view(a))
	for _, want := range []string{"Cards due", "New cards", "Streak"} {
		if !strings.Contains(got, want) {
			t.Errorf("Home does not report %q:\n%s", want, got)
		}
	}
	if !strings.Contains(got, fmt.Sprintf("%d", len(a.Lib.Cards))) {
		t.Error("Home should say how many cards are waiting to be seen")
	}
}
