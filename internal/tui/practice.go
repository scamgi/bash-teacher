package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"bash-teacher/internal/content"
	"bash-teacher/internal/runner"
	"bash-teacher/internal/store"
	"bash-teacher/internal/theme"
)

// runTimeout bounds the whole run command, not just the sandboxed process.
// The runner has its own wall clock; this is the backstop for the machinery
// around it, so a stuck run can never wedge the UI.
const runTimeout = 30 * time.Second

// spinnerDelay is how long a run may take before the screen admits it is
// working. Below it the result usually arrives first and a spinner would only
// flicker.
const spinnerDelay = 150 * time.Millisecond

// The workspace's vertical budget: how many lines the title block may take
// before the prompt is elided, and how many lines of output are held back
// from the fixture preview.
const (
	maxHeadLines   = 8
	minOutputLines = 4
)

// spinnerFrames is the running indicator. It is text, not colour, so it
// survives --theme none.
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// practiceRow is a flattened browser row: a track heading or an exercise.
type practiceRow struct {
	track string
	ex    *content.Exercise
}

// practiceMode is which of the screen's two faces is showing.
type practiceMode int

const (
	// modeBrowse is the track-and-exercise list.
	modeBrowse practiceMode = iota
	// modeWork is the exercise itself: prompt, fixture, editor, output.
	modeWork
)

// runResultMsg carries a finished run back to the screen. It names the
// exercise so that a result arriving after the learner moved on is discarded
// rather than shown against the wrong task.
type runResultMsg struct {
	id      string
	outcome *runner.Outcome
	err     error
}

// spinnerTickMsg advances the running indicator.
type spinnerTickMsg struct{}

func spinnerTick() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg { return spinnerTickMsg{} })
}

// practiceScreen is the exercise browser and, once an exercise is open, the
// workspace: the task, the fixture it runs against, the pipeline editor, and
// whatever the last run produced.
type practiceScreen struct {
	lib  *content.Library
	prog *progress

	rows   []practiceRow
	cursor int
	offset int

	mode   practiceMode
	ex     *content.Exercise
	editor lineEditor

	// hints is how many of the exercise's hints are on screen, and solved
	// records whether the reference solution has been revealed. Both are
	// per-exercise state kept in progress; these are the copies for the
	// exercise currently open.
	hints        int
	showSolution bool
	preview      bool
	fixture      []content.FixtureFile
	fixtureErr   error

	running  bool
	runStart time.Time
	// lastInput is the pipeline the running attempt was started with, kept
	// so the attempt log records what was typed even when the run comes back
	// as an error rather than as an outcome.
	lastInput string
	frame     int
	outcome   *runner.Outcome
	runErr    error
	critique  []string
	outOffset int
}

func newPractice(lib *content.Library) screen {
	p := &practiceScreen{lib: lib, prog: newProgress()}
	for _, t := range lib.Tracks {
		p.rows = append(p.rows, practiceRow{track: t.Name})
		for _, e := range t.Exercises {
			p.rows = append(p.rows, practiceRow{ex: e})
		}
	}
	p.moveToExercise(1)
	return p
}

func (p *practiceScreen) moveToExercise(dir int) {
	for p.cursor >= 0 && p.cursor < len(p.rows) && p.rows[p.cursor].ex == nil {
		p.cursor += dir
	}
	p.cursor = clampInt(p.cursor, 0, max(0, len(p.rows)-1))
}

// OpenExercise moves to a given exercise and opens it, so that a jump from the
// dictionary lands in the workspace rather than at the top of the list. It
// reports false when the id is unknown, in which case the root model does not
// switch screens.
func (p *practiceScreen) OpenExercise(id string) bool {
	for i, r := range p.rows {
		if r.ex != nil && r.ex.ID == id {
			p.cursor = i
			p.open(r.ex)
			return true
		}
	}
	return false
}

// PassedCount reports how many exercises have been solved this session, for
// Home's summary.
func (p *practiceScreen) PassedCount() int {
	n := 0
	for _, st := range p.prog.byExercise {
		if st.Passed() {
			n++
		}
	}
	return n
}

// PassedInTrack reports how many of a track's exercises have been solved, for
// the per-track bars on Stats.
func (p *practiceScreen) PassedInTrack(t *content.Track) int { return p.prog.PassedIn(t) }

// ExercisePassed reports whether one exercise has ever been solved. Stats asks
// it per command, to weigh the drills a command has been used in against the
// cards that name it.
func (p *practiceScreen) ExercisePassed(id string) bool { return p.prog.Passed(id) }

// RestoreProgress loads what the store remembers about every exercise, so the
// library opens on the learner's own history rather than on a blank slate.
func (p *practiceScreen) RestoreProgress(rows []store.Exercise) { p.prog.Restore(rows) }

// OpenTrack puts the browser's cursor on the first exercise of a track. It
// leaves the workspace closed: a learner who asked for a track wants to see
// what is in it.
func (p *practiceScreen) OpenTrack(name string) bool {
	for i, r := range p.rows {
		if r.ex != nil && r.ex.Track == name {
			p.cursor, p.mode = i, modeBrowse
			p.offset = 0
			return true
		}
	}
	return false
}

// open loads an exercise into the workspace, restoring what the learner had
// already seen of it.
func (p *practiceScreen) open(ex *content.Exercise) {
	p.mode, p.ex = modeWork, ex
	st := p.prog.state(ex.ID)
	p.hints, p.showSolution = st.Hints, st.SolutionShown
	p.preview = false
	p.outcome, p.runErr, p.critique, p.outOffset = nil, nil, nil, 0
	p.running = false
	p.editor.Clear()
	p.fixture, p.fixtureErr = p.lib.Fixture(ex.Fixture)
}

// Capturing reports that the pipeline editor owns every printable key, which
// is what keeps typing `2` or `q` into a pipeline from navigating away.
func (p *practiceScreen) Capturing() bool { return p.mode == modeWork }

func (p *practiceScreen) Help() []key.Binding {
	if p.mode == modeBrowse {
		return []key.Binding{Keys.Up, Keys.Down, Keys.Choose, Keys.Back, Keys.Help, Keys.Quit}
	}
	return []key.Binding{Keys.Run, Keys.Hint, Keys.Solution, Keys.NextEx, Keys.Preview, Keys.Lookup, Keys.Back}
}

func (p *practiceScreen) Update(a *App, msg tea.Msg) (screen, tea.Cmd) {
	switch msg := msg.(type) {
	case spinnerTickMsg:
		if !p.running {
			return p, nil
		}
		p.frame++
		tick := spinnerTick()
		return p, tick
	case runResultMsg:
		cmd := p.finishRun(a, msg)
		return p, cmd
	case tea.KeyPressMsg:
		if p.mode == modeBrowse {
			cmd := p.updateBrowse(msg)
			return p, cmd
		}
		cmd := p.updateWork(a, msg)
		return p, cmd
	}
	return p, nil
}

func (p *practiceScreen) updateBrowse(km tea.KeyPressMsg) tea.Cmd {
	switch {
	case key.Matches(km, Keys.Up):
		if p.cursor > 0 {
			p.cursor--
			p.moveToExercise(-1)
		}
	case key.Matches(km, Keys.Down):
		if p.cursor < len(p.rows)-1 {
			p.cursor++
			p.moveToExercise(1)
		}
	case key.Matches(km, Keys.Choose), key.Matches(km, Keys.Right):
		if ex := p.currentExercise(); ex != nil {
			p.open(ex)
			if !p.prog.Unlocked(p.lib, ex.Track) {
				return flash("this track is not unlocked yet — pass 80% of the one before it to open it in order")
			}
		}
	}
	return nil
}

// updateWork handles the workspace. Every action is a chord, because the
// editor is entitled to every printable key.
func (p *practiceScreen) updateWork(a *App, km tea.KeyPressMsg) tea.Cmd {
	switch {
	case key.Matches(km, Keys.Back):
		p.mode = modeBrowse
		return nil
	case key.Matches(km, Keys.Run):
		return p.startRun(a)
	case key.Matches(km, Keys.Hint):
		return p.nextHint(a)
	case key.Matches(km, Keys.Solution):
		return p.revealSolution(a)
	case key.Matches(km, Keys.NextEx):
		return p.step(1)
	case key.Matches(km, Keys.PrevEx):
		return p.step(-1)
	case key.Matches(km, Keys.Preview):
		p.preview = !p.preview
		return nil
	case key.Matches(km, Keys.Lookup):
		return p.lookup()
	case key.Matches(km, Keys.Reset):
		p.editor.Clear()
		return nil
	case key.Matches(km, Keys.OutDown):
		p.outOffset++
		return nil
	case key.Matches(km, Keys.OutUp):
		p.outOffset = max(0, p.outOffset-1)
		return nil
	}
	p.editor.Update(km, p.completions)
	return nil
}

// step moves to the next or previous exercise in the flattened list, skipping
// the track headings.
func (p *practiceScreen) step(dir int) tea.Cmd {
	i := p.cursor + dir
	for i >= 0 && i < len(p.rows) && p.rows[i].ex == nil {
		i += dir
	}
	if i < 0 || i >= len(p.rows) {
		return flash("that is the end of the library")
	}
	p.cursor = i
	p.open(p.rows[i].ex)
	return nil
}

func (p *practiceScreen) currentExercise() *content.Exercise {
	if p.cursor < 0 || p.cursor >= len(p.rows) {
		return nil
	}
	return p.rows[p.cursor].ex
}

// startRun dispatches the sandboxed run as a command, so the UI keeps
// drawing while it happens.
func (p *practiceScreen) startRun(a *App) tea.Cmd {
	if p.ex == nil || p.running {
		return nil
	}
	input := strings.TrimSpace(p.editor.Value())
	if input == "" {
		return flash("type a pipeline first")
	}
	if a.Runner == nil {
		return flash("no runner is configured")
	}
	p.editor.Accept()
	p.lastInput = input
	p.running, p.runStart, p.frame = true, time.Now(), 0
	p.outcome, p.runErr, p.critique, p.outOffset = nil, nil, nil, 0

	ex, run := p.ex, a.Runner
	return tea.Batch(spinnerTick(), func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), runTimeout)
		defer cancel()
		out, err := run.RunExercise(ctx, ex, input)
		return runResultMsg{id: ex.ID, outcome: out, err: err}
	})
}

// finishRun records a result, ignoring one that belongs to an exercise the
// learner has already left.
//
// Every run that comes back is logged, passing or not: the attempt log is the
// raw record, and a failed attempt is the more interesting half of it. The
// summary alongside it is what the browser reads, so both are written before
// anything else is decided.
func (p *practiceScreen) finishRun(a *App, msg runResultMsg) tea.Cmd {
	if p.ex == nil || msg.id != p.ex.ID {
		return nil
	}
	p.running = false
	p.outcome, p.runErr = msg.outcome, msg.err
	took := time.Since(p.runStart)
	passed := msg.err == nil && msg.outcome != nil && msg.outcome.Passed

	st := p.prog.state(p.ex.ID)
	first := passed && !st.Passed()
	st.Attempts++
	st.Hints, st.SolutionShown = p.hints, p.showSolution
	if passed {
		if st.FirstPassed.IsZero() {
			st.FirstPassed = a.Now()
		}
		if st.Best == 0 || took < st.Best {
			st.Best = took
		}
	}
	a.LogAttempt(store.Attempt{
		ExerciseID: p.ex.ID,
		At:         a.Now(),
		Input:      p.lastInput,
		Passed:     passed,
		Hints:      p.hints,
		Took:       took,
	})
	a.SaveExercise(*st)

	if !passed {
		return nil
	}
	p.critique = runner.Critique(msg.outcome.Input, p.ex.ReferenceSolution)
	if !first {
		return nil
	}
	// SPEC §5: solving an exercise reinforces every card it drills, at half
	// the strength of answering the card itself. Only the first pass counts —
	// re-running a solved exercise is not new evidence.
	note := "✓ passed — " + p.ex.Title
	if n := a.CreditPractice(p.ex); n > 0 {
		note += fmt.Sprintf(" · credited %s", plural(n, "flashcard", "flashcards"))
	}
	return flash(note)
}

// nextHint reveals one more hint. Hints cost nothing and are recorded, which
// is what lets Stats tell a solved exercise from a solved-with-help one.
func (p *practiceScreen) nextHint(a *App) tea.Cmd {
	if p.ex == nil {
		return nil
	}
	if p.hints >= len(p.ex.Hints) {
		return flash("that is the last hint — ^S shows the reference solution")
	}
	p.hints++
	st := p.prog.state(p.ex.ID)
	st.Hints = p.hints
	a.SaveExercise(*st)
	return nil
}

func (p *practiceScreen) revealSolution(a *App) tea.Cmd {
	if p.ex == nil {
		return nil
	}
	p.showSolution = true
	st := p.prog.state(p.ex.ID)
	st.SolutionShown = true
	a.SaveExercise(*st)
	return nil
}

// lookup opens the dictionary on the word under the cursor.
func (p *practiceScreen) lookup() tea.Cmd {
	word := p.editor.WordAt()
	if word == "" {
		return flash("put the cursor on a command to look it up")
	}
	return lookupCommand(word)
}

// completions offers the command names that fit where the cursor is, or the
// flags of the command the cursor's word belongs to. Both come from the
// dictionary, so completion can never suggest something the sandbox refuses.
func (p *practiceScreen) completions(line string, at int) []string {
	head := line[:at]
	prefix, word := "", head
	if i := strings.LastIndexAny(head, " \t"); i >= 0 {
		prefix, word = head[:i], head[i+1:]
	}

	var out []string
	if strings.HasPrefix(word, "-") {
		if c, ok := p.lib.Command(commandOf(prefix)); ok {
			for _, f := range c.Flags {
				// A flag entry may be spelled "-k N[,M]"; only the flag
				// itself can be completed.
				name := strings.Fields(f.Flag)[0]
				if strings.HasPrefix(name, word) {
					out = append(out, name)
				}
			}
		}
		return out
	}
	for _, c := range p.lib.Commands {
		if c.CanExecute() && strings.HasPrefix(c.Name, word) {
			out = append(out, c.Name)
		}
	}
	return out
}

// commandOf returns the command name that governs the end of a partial line:
// the first word after the last pipe or separator.
func commandOf(prefix string) string {
	if i := strings.LastIndexAny(prefix, "|;&"); i >= 0 {
		prefix = prefix[i+1:]
	}
	fields := strings.Fields(prefix)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

func (p *practiceScreen) Body(a *App, width, height int) string {
	if len(p.rows) == 0 {
		return "\n  " + a.Theme.Dim.Render("No exercises loaded.")
	}
	if p.mode == modeWork && p.ex != nil {
		return p.workBody(a, width, height)
	}
	return p.browseBody(a, width, height)
}

// browseBody is the library view: every track, every exercise, and what has
// been passed so far.
func (p *practiceScreen) browseBody(a *App, width, height int) string {
	t := a.Theme
	listHeight := max(1, height-6)
	p.offset = scrollTo(p.cursor, p.offset, listHeight)

	var b strings.Builder
	for i := p.offset; i < len(p.rows) && i < p.offset+listHeight; i++ {
		r := p.rows[i]
		if r.ex == nil {
			b.WriteString(truncate("  "+t.Faint.Render(strings.ToUpper(content.TrackTitle(r.track)))+
				"  "+t.Faint.Render(p.trackStatus(r.track)), width))
			b.WriteString("\n")
			continue
		}
		mark := "  "
		if p.prog.Passed(r.ex.ID) {
			mark = t.Pass.Render("✓ ")
		}
		title, dots := pad(r.ex.Title, 42), levelDots(r.ex.Level)
		if i == p.cursor {
			b.WriteString("  " + t.Accent.Render("▸ ") + mark + t.Selected.Render(title) + " " + t.Dim.Render(dots))
		} else {
			b.WriteString("    " + mark + t.Body.Render(title) + " " + t.Faint.Render(dots))
		}
		b.WriteString("\n")
	}

	if ex := p.currentExercise(); ex != nil {
		b.WriteString("\n" + indent(t.Dim.Render(truncate(oneLine(ex.Prompt), width-6)), 4))
		b.WriteString("\n" + indent(t.Faint.Render("enter to open · teaches ")+
			t.Body.Render(strings.Join(ex.Teaches, ", ")), 4))
	}
	return b.String()
}

// trackStatus is the progress line beside a track heading, and says so when a
// track is still ahead of where the learner has got to.
func (p *practiceScreen) trackStatus(name string) string {
	t, ok := p.lib.Track(name)
	if !ok {
		return ""
	}
	status := fmt.Sprintf("%d/%d passed", p.prog.PassedIn(t), len(t.Exercises))
	if !p.prog.Unlocked(p.lib, name) {
		status += " · locked until the previous track is 80% passed"
	}
	return status
}

// workBody renders the exercise workspace.
//
// The screen reads top to bottom as the exercise itself does: what is being
// asked, what data it is asked about, the line where it is answered, and what
// came back. The editor is the only part inside a box, because it is the only
// part the learner acts on.
//
// The height is divided from the bottom up: the editor and a few lines of
// output are reserved first, and the fixture preview gets whatever is left.
// A preview that pushed the output off the frame would hide the answer to the
// question the learner just asked.
func (p *practiceScreen) workBody(a *App, width, height int) string {
	t := a.Theme
	w := width - 4

	head := p.headLines(a, w)
	editor := p.editorBox(a, w)

	// Two section rules, the editor's box, and a floor for the output.
	reserved := 2 + len(editor) + minOutputLines
	fixture := p.fixtureLines(a, w)
	if budget := height - len(head) - reserved; budget < len(fixture) {
		budget = max(budget, 1)
		fixture = append(fixture[:budget-1:budget-1], t.Faint.Render("  …"))
	}

	lines := append([]string{}, head...)
	lines = append(lines, rule(t, "Fixture", p.previewHint(), w))
	lines = append(lines, fixture...)
	lines = append(lines, editor...)

	out := p.outputLines(a, w)
	// One line for the Output rule itself, which is drawn after the window is
	// measured so that it can say whether there is more below it.
	room := max(1, height-len(lines)-1)
	p.outOffset = clampInt(p.outOffset, 0, max(0, len(out)-room))
	shown := window(out, p.outOffset, room)
	more := len(out) > p.outOffset+room

	note := ""
	if more || p.outOffset > 0 {
		note = "pgup/pgdn scrolls"
	}
	lines = append(lines, rule(t, "Output", note, w))
	if more && len(shown) > 0 {
		shown[len(shown)-1] = t.Faint.Render(fmt.Sprintf("… %s below",
			plural(len(out)-p.outOffset-room+1, "line", "lines")))
	}
	lines = append(lines, shown...)

	for i, ln := range lines {
		lines[i] = "  " + ln
	}
	return strings.Join(lines, "\n")
}

// headLines is the workspace's title block: what the exercise is, where it
// sits, and then the task itself, set off by a rule down its left edge so the
// prose reads as the question rather than as more chrome.
func (p *practiceScreen) headLines(a *App, width int) []string {
	t := a.Theme
	out := []string{p.titleLine(a, width), p.metaLine(a, width), ""}

	room := max(1, maxHeadLines-len(out)-1)
	prompt := strings.Split(wrap(p.ex.Prompt, width-2), "\n")
	if len(prompt) > room {
		prompt = append(prompt[:room-1:room-1], "…")
	}
	for _, ln := range prompt {
		out = append(out, t.Accent.Render("▎")+" "+t.Body.Render(ln))
	}
	return append(out, "")
}

// titleLine names the exercise and says where the learner stands on it.
func (p *practiceScreen) titleLine(a *App, width int) string {
	return spread(a.Theme.Title.Render(p.ex.Title), p.standing(a), width)
}

// standing is the right-hand note on the title line: solved, or how many
// attempts have gone in so far. It reads the summary without creating one, so
// merely opening an exercise does not put a row in the store.
func (p *practiceScreen) standing(a *App) string {
	t := a.Theme
	st := p.prog.summary(p.ex.ID)
	switch {
	case st.Passed() && st.Best > 0:
		return t.Pass.Render("✓ solved") + t.Faint.Render(" · best "+shortDuration(st.Best))
	case st.Passed():
		return t.Pass.Render("✓ solved")
	case st.Attempts > 0:
		return t.Faint.Render(plural(st.Attempts, "attempt", "attempts") + " so far")
	}
	return t.Faint.Render("not solved yet")
}

// metaLine is where the exercise sits in the library, how hard it is, and what
// it drills — the same three facts the browser row carries, so opening an
// exercise never loses the context it was chosen from.
func (p *practiceScreen) metaLine(a *App, width int) string {
	t := a.Theme
	n, total := p.position()
	left := t.Subtitle.Render(content.TrackTitle(p.ex.Track)) +
		t.Faint.Render(fmt.Sprintf(" · %d of %d · ", n, total)) + t.Dim.Render(levelDots(p.ex.Level))
	right := ""
	if len(p.ex.Teaches) > 0 {
		right = t.Faint.Render("teaches ") + t.Code.Render(strings.Join(p.ex.Teaches, ", "))
	}
	return spread(left, right, width)
}

// position reports which exercise this is out of the whole library, counting
// in track order.
func (p *practiceScreen) position() (n, total int) {
	for _, r := range p.rows {
		if r.ex == nil {
			continue
		}
		total++
		if r.ex == p.ex {
			n = total
		}
	}
	return n, total
}

// editorBox draws the pipeline editor inside a box of its own. It is the one
// part of the workspace with a border around it: the section rules divide
// things that are read, and this is the thing that is typed into.
func (p *practiceScreen) editorBox(a *App, width int) []string {
	t := a.Theme
	inner := max(width-4, 8)

	label := t.Subtitle.Render("Your pipeline")
	dashes := max(width-5-lipgloss.Width(label), 0)
	line := t.Accent.Render("$ ") + p.editor.View(a, inner-2, true)
	return []string{
		t.Accent.Render("╭─ ") + label + " " + t.Accent.Render(strings.Repeat("─", dashes)+"╮"),
		t.Accent.Render("│ ") + pad(line, inner) + t.Accent.Render(" │"),
		t.Accent.Render("╰" + strings.Repeat("─", max(width-2, 0)) + "╯"),
	}
}

// previewHint is the note on the Fixture rule: what ^B will do next.
func (p *practiceScreen) previewHint() string {
	if p.preview {
		return "^B closes the preview"
	}
	return "^B previews the files"
}

// fixtureLines describes the files the pipeline will see, and shows the head
// of each one while the preview is open.
func (p *practiceScreen) fixtureLines(a *App, width int) []string {
	t := a.Theme
	if p.fixtureErr != nil {
		return []string{t.Fail.Render("✗ " + p.fixtureErr.Error())}
	}
	var names []string
	for _, f := range p.fixture {
		names = append(names, t.Code.Render(f.Name)+
			t.Faint.Render(fmt.Sprintf(" %s, %s", byteSize(f.Size), plural(f.Lines, "line", "lines"))))
	}
	head := truncate(strings.Join(names, t.Faint.Render("  ·  ")), width)
	if !p.preview {
		return []string{head}
	}

	// The preview is a taste of each file, not a pager: two files of five
	// lines each tells a learner what shape the data is in, which is what the
	// question in front of them needs. The gutter marks it as file content
	// rather than as something the screen is saying.
	out := []string{head}
	const previewRows = 5
	for _, f := range p.fixture {
		out = append(out, t.Faint.Render("  "+f.Name))
		for _, ln := range window(f.Preview, 0, previewRows) {
			out = append(out, t.Faint.Render("  │ ")+t.Dim.Render(truncate(ln, width-6)))
		}
		if len(f.Preview) > previewRows || f.Truncated {
			out = append(out, t.Faint.Render("  │ …"))
		}
	}
	return out
}

// outputLines renders whatever the screen has to say about the last run, plus
// any hints and the reference solution once they have been asked for.
func (p *practiceScreen) outputLines(a *App, width int) []string {
	t := a.Theme
	var out []string

	switch {
	case p.running && time.Since(p.runStart) >= spinnerDelay:
		out = append(out, t.Dim.Render(spinnerFrames[p.frame%len(spinnerFrames)]+" running in the sandbox…"))
	case p.runErr != nil:
		out = append(out, t.Fail.Render("✗ the runner could not start: "+p.runErr.Error()))
	case p.outcome != nil:
		out = append(out, p.resultLines(a, width)...)
	default:
		out = append(out, t.Faint.Render("type a pipeline above, then ")+t.Key.Render("^R")+
			t.Faint.Render(" to run it against the fixture"))
	}

	for i := 0; i < p.hints && i < len(p.ex.Hints); i++ {
		out = append(out, "", t.Warn.Render(fmt.Sprintf("▸ hint %d", i+1))+
			t.Faint.Render(fmt.Sprintf(" of %d", len(p.ex.Hints))))
		for _, ln := range strings.Split(wrap(p.ex.Hints[i], width-4), "\n") {
			out = append(out, "    "+t.Body.Render(ln))
		}
	}
	if p.showSolution {
		out = append(out, "",
			t.Warn.Render("▸ reference solution"),
			"    "+t.Accent.Render("$ ")+t.Code.Render(truncate(p.ex.ReferenceSolution, width-6)))
		for _, ln := range strings.Split(wrap(p.ex.SolutionNotes, width-4), "\n") {
			out = append(out, "    "+t.Dim.Render(ln))
		}
	}
	return out
}

// resultLines renders one finished run: a refusal, a diff, or a pass followed
// by the reference solution beside the learner's own.
func (p *practiceScreen) resultLines(a *App, width int) []string {
	t := a.Theme
	o := p.outcome
	var out []string

	if !o.Ran() {
		head := t.Fail.Render("✗ not run")
		if len(o.Violations) > 0 && o.Violations[0].Kind == runner.KindConstraint {
			// A constraint is the exercise's rule, not a safety refusal, and
			// saying so is the difference between a lesson and a scolding.
			head = t.Warn.Render("✗ that breaks the rule this exercise sets")
		}
		out = append(out, head)
		for _, ln := range strings.Split(o.Refusal(), "\n") {
			for _, wrapped := range strings.Split(wrap(ln, width-4), "\n") {
				out = append(out, "    "+t.Dim.Render(wrapped))
			}
		}
		if o.ParseError != nil {
			for _, ln := range strings.Split(o.ParseError.Caret(), "\n") {
				out = append(out, "    "+t.Faint.Render(ln))
			}
		}
		return out
	}

	if o.Passed {
		out = append(out, t.Pass.Render("✓ correct — the output matches"), "",
			t.Faint.Render(pad("yours", 12))+t.Code.Render(truncate(o.Input, width-12)),
			t.Faint.Render(pad("reference", 12))+t.Code.Render(truncate(p.ex.ReferenceSolution, width-12)))
		if len(p.critique) > 0 {
			out = append(out, "")
		}
		for _, n := range p.critique {
			for _, ln := range strings.Split(wrap("· "+n, width-4), "\n") {
				out = append(out, "    "+t.Dim.Render(ln))
			}
		}
		out = append(out, "", t.Key.Render("^N")+t.Faint.Render(" for the next exercise"))
		return out
	}

	if o.Stderr != "" {
		for _, ln := range strings.Split(strings.TrimRight(o.Stderr, "\n"), "\n") {
			out = append(out, t.Fail.Render("  "+truncate(ln, width-2)))
		}
	}
	for i, ln := range strings.Split(o.Diff.String(), "\n") {
		if i == 0 {
			out = append(out, t.Fail.Render(truncate(ln, width)))
			continue
		}
		out = append(out, t.Dim.Render(truncate(ln, width)))
	}
	return out
}

// rule renders a labelled section divider, optionally with a note about the
// section closing it on the right — the key that opens the fixture preview,
// or that there is more output below the fold.
func rule(t *theme.Theme, label, note string, width int) string {
	head := t.Faint.Render("── ") + t.Subtitle.Render(label) + " "
	tail := ""
	if note != "" {
		tail = " " + t.Faint.Render(note+" ──")
	}
	n := width - lipgloss.Width(head) - lipgloss.Width(tail)
	if n < 1 {
		return truncate(head+tail, width)
	}
	return head + t.Faint.Render(strings.Repeat("─", n)) + tail
}

// spread lays a left and a right fragment on one line of the given width. When
// they will not both fit the right one is dropped: it is always the note, and
// the left is always the thing being noted.
func spread(left, right string, width int) string {
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		return truncate(left, width)
	}
	return left + strings.Repeat(" ", gap) + right
}

// shortDuration formats a run time the way a stopwatch would.
func shortDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%.1fs", d.Seconds())
}

// window returns at most n entries of s starting at off.
func window(s []string, off, n int) []string {
	off = clampInt(off, 0, len(s))
	end := clampInt(off+n, off, len(s))
	return s[off:end]
}

// oneLine collapses prose to a single line, for list rows.
func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// byteSize formats a file size the way a listing would.
func byteSize(n int64) string {
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	return fmt.Sprintf("%.1f KB", float64(n)/1024)
}

// levelDots renders the 1–5 difficulty as filled and hollow dots.
func levelDots(level int) string {
	level = clampInt(level, 0, 5)
	return strings.Repeat("●", level) + strings.Repeat("○", 5-level)
}
