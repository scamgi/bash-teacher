package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"bash-teacher/internal/answer"
	"bash-teacher/internal/content"
	"bash-teacher/internal/srs"
)

// phase is where in one card's life the screen is.
type phase int

const (
	// phaseIdle is between sessions: the summary of the last one and what is
	// waiting, with nothing on the clock.
	phaseIdle phase = iota
	// phaseAsk shows a card's front. For a typed card the answer editor owns
	// every printable key here.
	phaseAsk
	// phaseGrade shows the answer beside what was typed, and waits for a
	// rating.
	phaseGrade
)

// requeueGap is how many cards later a failed card comes back. Far enough
// that it is recalled rather than echoed, near enough to be the same sitting.
const requeueGap = 3

// flashcardsScreen runs a review session: a queue built by the scheduler,
// typed answers graded by the normalizer, and a rating that feeds back into
// the schedule.
type flashcardsScreen struct {
	lib *content.Library

	queue []*content.Card
	pos   int
	phase phase

	editor   lineEditor
	result   answer.Result
	revealed bool
	rating   srs.Rating
	shown    time.Time
	elapsed  time.Duration

	// The session tally, reported when the queue runs out.
	answered, recalled, lapses int
	// note explains an empty or finished session.
	note string

	// filter is the command id the deck is restricted to, empty for the
	// scheduled queue. It is set by a jump from the dictionary, which is a
	// request to drill one command rather than to do the day's work, so a
	// filtered session ignores due dates.
	filter string
}

func newFlashcards(lib *content.Library) screen {
	return &flashcardsScreen{lib: lib}
}

// ShowCommand narrows the deck to the cards drilling one command and starts a
// session on them, whether or not they are due. It reports false when that
// command has no cards, so the caller can say so rather than opening an empty
// deck.
func (f *flashcardsScreen) ShowCommand(commandID string) bool {
	cards := f.lib.CardsFor(commandID)
	if len(cards) == 0 {
		return false
	}
	f.filter = commandID
	f.begin(cards)
	return true
}

// Start builds the day's session from the scheduler's queue. It is what
// entering the screen and finishing a session both call.
func (f *flashcardsScreen) Start(a *App) {
	f.filter = ""
	f.begin(f.scheduled(a))
	if len(f.queue) == 0 {
		f.note = f.emptyReason(a)
	}
}

// emptyReason says why there is nothing to review, since "nothing is due" and
// "you have had today's new cards" are different situations with different
// answers, and a learner who is told only the first will keep pressing enter.
func (f *flashcardsScreen) emptyReason(a *App) string {
	p := a.SRS.Params()
	now := a.Now()
	unseen := a.UnseenCards()

	switch {
	case unseen > 0 && a.SRS.NewToday(now) >= p.NewPerDay:
		return fmt.Sprintf("That is today's %d new cards. The rest of the deck opens tomorrow — "+
			"the cap is what keeps the reviews they generate from piling up a week from now.", p.NewPerDay)
	case a.SRS.ReviewsToday(now) >= p.MaxReviewsPerDay:
		return fmt.Sprintf("That is today's %d reviews. Come back tomorrow.", p.MaxReviewsPerDay)
	case unseen == 0:
		if due, ok := a.SRS.NextDue(cardIDs(f.lib.Cards), now); ok {
			return "The whole deck is scheduled. The next card is due in " +
				humanInterval(due.Sub(now)) + "."
		}
		return "The whole deck is scheduled and nothing is due."
	default:
		return "Nothing is due just now."
	}
}

// scheduled asks the scheduler which cards this sitting should hold.
func (f *flashcardsScreen) scheduled(a *App) []*content.Card {
	if a.SRS == nil {
		return f.lib.Cards
	}
	ids := a.SRS.Queue(cardIDs(f.lib.Cards), a.Now())
	out := make([]*content.Card, 0, len(ids))
	for _, id := range ids {
		if c, ok := f.lib.Card(id); ok {
			out = append(out, c)
		}
	}
	return out
}

// begin opens a session on a set of cards, resetting the tally.
//
// An empty session leaves the previous tally standing: a learner who has just
// finished fifteen cards and pressed enter again should see what they did,
// not a row of zeroes above the reason there is nothing left.
func (f *flashcardsScreen) begin(cards []*content.Card) {
	f.queue, f.pos = cards, 0
	f.note = ""
	if len(cards) == 0 {
		f.phase = phaseIdle
		f.note = "Nothing is due just now."
		return
	}
	f.answered, f.recalled, f.lapses = 0, 0, 0
	f.ask()
}

// ask puts the current card's front up and starts its clock.
func (f *flashcardsScreen) ask() {
	f.phase = phaseAsk
	f.editor.Clear()
	f.result = answer.Result{}
	f.revealed = false
	f.shown = time.Now()
	f.elapsed = 0
}

// card is the one on screen, or nil when the session is over.
func (f *flashcardsScreen) card() *content.Card {
	if f.pos < 0 || f.pos >= len(f.queue) {
		return nil
	}
	return f.queue[f.pos]
}

// Capturing reports that the answer editor owns every printable key, which is
// what lets a learner type `2` or `q` into an answer.
func (f *flashcardsScreen) Capturing() bool {
	c := f.card()
	return f.phase == phaseAsk && c != nil && !c.SelfGraded()
}

func (f *flashcardsScreen) Help() []key.Binding {
	switch {
	case f.phase == phaseIdle:
		return []key.Binding{Keys.Start, Keys.Back, Keys.Help, Keys.Quit}
	case f.phase == phaseGrade:
		b := []key.Binding{Keys.Rate, Keys.Choose, Keys.Harder, Keys.Easier}
		if f.filter != "" {
			b = append(b, Keys.Pop)
		}
		return b
	case f.card() != nil && f.card().SelfGraded():
		return []key.Binding{Keys.Reveal, Keys.Back, Keys.Help, Keys.Quit}
	default:
		return []key.Binding{Keys.Submit, Keys.Solution, Keys.Reset, Keys.Back}
	}
}

func (f *flashcardsScreen) Update(a *App, msg tea.Msg) (screen, tea.Cmd) {
	km, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return f, nil
	}
	switch f.phase {
	case phaseIdle:
		f.updateIdle(a, km)
		return f, nil
	case phaseAsk:
		cmd := f.updateAsk(a, km)
		return f, cmd
	default:
		cmd := f.updateGrade(a, km)
		return f, cmd
	}
}

func (f *flashcardsScreen) updateIdle(a *App, km tea.KeyPressMsg) {
	if key.Matches(km, Keys.Start) {
		f.Start(a)
	}
}

// updateAsk handles the question. A self-graded card waits for a keypress to
// turn over; a typed one hands every printable key to the editor.
func (f *flashcardsScreen) updateAsk(a *App, km tea.KeyPressMsg) tea.Cmd {
	c := f.card()
	if c == nil {
		return nil
	}
	if c.SelfGraded() {
		if key.Matches(km, Keys.Reveal) {
			f.grade(answer.Result{Verdict: answer.Unsure, Reason: "this card is yours to grade"})
		}
		return nil
	}

	switch {
	case key.Matches(km, Keys.Submit):
		f.grade(a.Grade(c, f.editor.Value()))
		return nil
	case key.Matches(km, Keys.Solution):
		// Giving up is an answer too, and the honest rating for it is again.
		f.editor.Clear()
		f.grade(answer.Result{Verdict: answer.Wrong, Reason: "you asked to see it"})
		return nil
	case key.Matches(km, Keys.Back):
		if f.filter != "" {
			f.Start(a)
			return flash("back to the scheduled queue")
		}
		f.finish("session ended early")
		return nil
	case key.Matches(km, Keys.Reset):
		f.editor.Clear()
		return nil
	}
	f.editor.Update(km, nil)
	return nil
}

// grade records the verdict and moves to the rating step, pre-selecting the
// rating the answer earned: a match is good, a miss is again, and anything the
// normalizer would not call is left on good for the learner to move.
func (f *flashcardsScreen) grade(r answer.Result) {
	f.result = r
	f.elapsed = time.Since(f.shown)
	f.phase = phaseGrade
	f.revealed = true
	f.rating = srs.Good
	if r.Verdict == answer.Wrong {
		f.rating = srs.Again
	}
}

func (f *flashcardsScreen) updateGrade(a *App, km tea.KeyPressMsg) tea.Cmd {
	switch {
	case key.Matches(km, Keys.RateAgain):
		return f.rate(a, srs.Again)
	case key.Matches(km, Keys.RateHard):
		return f.rate(a, srs.Hard)
	case key.Matches(km, Keys.RateGood):
		return f.rate(a, srs.Good)
	case key.Matches(km, Keys.RateEasy):
		return f.rate(a, srs.Easy)
	case key.Matches(km, Keys.Harder):
		f.rating = srs.Rating(clampInt(int(f.rating)-1, int(srs.Again), int(srs.Easy)))
	case key.Matches(km, Keys.Easier):
		f.rating = srs.Rating(clampInt(int(f.rating)+1, int(srs.Again), int(srs.Easy)))
	case key.Matches(km, Keys.Choose):
		return f.rate(a, f.rating)
	case key.Matches(km, Keys.Pop):
		if f.filter != "" {
			f.Start(a)
			return flash("back to the scheduled queue")
		}
	}
	return nil
}

// rate applies a rating and moves on. A failed card is put back a few places
// so it comes round again before the sitting ends.
func (f *flashcardsScreen) rate(a *App, r srs.Rating) tea.Cmd {
	c := f.card()
	if c == nil {
		return nil
	}
	if a.SRS != nil {
		a.SRS.Grade(c.ID, r, f.elapsed, a.Now())
	}
	f.answered++
	if r == srs.Again {
		f.lapses++
		f.requeue(c)
	} else {
		f.recalled++
	}

	f.pos++
	if f.card() == nil {
		f.finish("")
		return nil
	}
	f.ask()
	return nil
}

// requeue puts a failed card back into the sitting, a few cards further on.
func (f *flashcardsScreen) requeue(c *content.Card) {
	at := clampInt(f.pos+1+requeueGap, 0, len(f.queue))
	f.queue = append(f.queue[:at], append([]*content.Card{c}, f.queue[at:]...)...)
}

// finish ends the session and shows the tally.
func (f *flashcardsScreen) finish(note string) {
	f.phase = phaseIdle
	f.queue, f.pos = nil, 0
	f.note = note
}

// cardIDs is the deck as the scheduler addresses it.
func cardIDs(cards []*content.Card) []string {
	out := make([]string, 0, len(cards))
	for _, c := range cards {
		out = append(out, c.ID)
	}
	return out
}

func (f *flashcardsScreen) Body(a *App, width, height int) string {
	if f.phase == phaseIdle {
		return f.idleBody(a, width)
	}
	return f.cardBody(a, width, height)
}

// idleBody is the between-sessions view: what the last sitting came to, and
// what is waiting.
func (f *flashcardsScreen) idleBody(a *App, width int) string {
	t := a.Theme
	lines := []string{t.PanelTitle.Render("Flashcards"), ""}

	if f.answered > 0 {
		lines = append(lines,
			row(t, 14, "Answered", fmt.Sprintf("%d", f.answered)),
			row(t, 14, "Recalled", fmt.Sprintf("%d", f.recalled)),
			row(t, 14, "Missed", fmt.Sprintf("%d", f.lapses)),
			"")
	}

	due, unseen := a.DueCards(), a.UnseenCards()
	lines = append(lines,
		row(t, 14, "Due now", fmt.Sprintf("%d", due)),
		row(t, 14, "Not yet seen", fmt.Sprintf("%d of %d", unseen, len(f.lib.Cards))),
		"")
	if f.note != "" {
		lines = append(lines, t.Warn.Render(wrap(f.note, width-12)), "")
	}
	lines = append(lines, t.Faint.Render("enter starts a session of up to "+
		fmt.Sprintf("%d cards", a.SRS.Params().SessionSize)))

	return "\n" + indent(t.Panel.Width(width-8).Render(strings.Join(lines, "\n")), 2)
}

// cardBody renders the card on screen: its front, the answer box, and — once
// answered — the verdict and what each rating would cost.
func (f *flashcardsScreen) cardBody(a *App, width, height int) string {
	t := a.Theme
	c := f.card()
	if c == nil {
		return "\n  " + t.Dim.Render("No cards loaded.")
	}
	w := width - 8

	lines := []string{t.Faint.Render(f.deckLine(a, c)), ""}
	lines = append(lines, strings.Split(wrap(c.Front, w), "\n")...)
	lines = append(lines, "")

	if c.SelfGraded() {
		lines = append(lines, f.identifyLines(a, c, w)...)
	} else {
		lines = append(lines, f.typedLines(a, c, w)...)
	}

	if f.phase == phaseGrade {
		lines = append(lines, "", f.ratingLine(a, c))
	}

	box := indent(t.Panel.Width(width-8).Render(strings.Join(lines, "\n")), 2)
	return "\n" + fitBlock(box, width, max(1, height-1))
}

// deckLine is the header above a card: where the sitting is, what kind of card
// this is, and which commands it drills.
func (f *flashcardsScreen) deckLine(a *App, c *content.Card) string {
	scope := "scheduled"
	if f.filter != "" {
		scope = "drilling " + f.filter
	}
	line := fmt.Sprintf("card %d/%d · %s · %s · %s",
		f.pos+1, len(f.queue), scope, c.Type, strings.Join(c.Commands, ", "))
	if st, ok := a.SRS.State(c.ID); !ok || !st.Seen() {
		return line + " · new"
	}
	return line
}

// identifyLines render a self-graded card: the prose reading, hidden until
// asked for.
func (f *flashcardsScreen) identifyLines(a *App, c *content.Card, w int) []string {
	t := a.Theme
	if !f.revealed {
		return []string{t.Faint.Render("say what this does, then press enter")}
	}
	out := []string{t.Faint.Render("the reading")}
	for _, ln := range strings.Split(wrap(c.Back, w-2), "\n") {
		out = append(out, "  "+t.Body.Render(ln))
	}
	return out
}

// typedLines render an answer box and, once submitted, the verdict beside the
// expected answer.
func (f *flashcardsScreen) typedLines(a *App, c *content.Card, w int) []string {
	t := a.Theme
	out := []string{t.Accent.Render("$ ") + f.editor.View(a, w-2, f.phase == phaseAsk)}
	if f.phase != phaseGrade {
		return out
	}

	out = append(out, "", f.verdictLine(a))
	if f.result.Reason != "" && f.result.Verdict != answer.Correct {
		for _, ln := range strings.Split(wrap(f.result.Reason, w-2), "\n") {
			out = append(out, "  "+t.Dim.Render(ln))
		}
	}
	if f.result.Verdict != answer.Correct {
		out = append(out, t.Faint.Render("expected  ")+t.Code.Render(truncate(c.Back, w-12)))
		if len(c.Accepts) > 0 {
			out = append(out, t.Faint.Render("also      ")+
				t.Dim.Render(truncate(strings.Join(c.Accepts, "  ·  "), w-12)))
		}
	}
	return out
}

// verdictLine states the outcome. Colour is never the only signal: each
// verdict carries its own glyph.
func (f *flashcardsScreen) verdictLine(a *App) string {
	t := a.Theme
	var left string
	switch f.result.Verdict {
	case answer.Correct:
		left = t.Pass.Render("✓ correct")
	case answer.Unsure:
		left = t.Warn.Render("? I cannot call this one")
	default:
		left = t.Fail.Render("✗ not what the card wanted")
	}
	return left + "  " + t.Faint.Render(f.timing(a))
}

// timing reports how long the answer took, and nudges when it ran past the
// soft target. It never fails a card: automaticity is the goal, but a slow
// right answer is still right.
func (f *flashcardsScreen) timing(a *App) string {
	target := a.SRS.Params().SoftTarget
	took := f.elapsed.Truncate(100 * time.Millisecond)
	if f.elapsed > target && f.result.Verdict == answer.Correct {
		return fmt.Sprintf("%s — over the %s you are aiming for", took, target)
	}
	return took.String()
}

// ratingLine offers the four ratings with the interval each one buys, so the
// choice between hard and good is a visible trade rather than a guess.
func (f *flashcardsScreen) ratingLine(a *App, c *content.Card) string {
	t := a.Theme
	labels := []struct {
		keyName string
		rating  srs.Rating
	}{
		{"a", srs.Again}, {"h", srs.Hard}, {"g", srs.Good}, {"e", srs.Easy},
	}
	parts := make([]string, 0, len(labels))
	for _, l := range labels {
		gap := humanInterval(a.SRS.Preview(c.ID, l.rating, a.Now()))
		text := fmt.Sprintf("%s %s %s", l.keyName, l.rating, gap)
		if l.rating == f.rating {
			parts = append(parts, t.Selected.Render(" "+text+" "))
			continue
		}
		parts = append(parts, t.Dim.Render(" "+text+" "))
	}
	return strings.Join(parts, t.Faint.Render("·"))
}

// humanInterval renders a scheduling gap the way a learner reads one: minutes
// close in, then days, then months.
func humanInterval(d time.Duration) string {
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()+0.5))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()+0.5))
	case d < 60*24*time.Hour:
		return fmt.Sprintf("%.0fd", d.Hours()/24)
	default:
		return fmt.Sprintf("%.1fmo", d.Hours()/24/30.4)
	}
}
