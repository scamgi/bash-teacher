package tui

import (
	"fmt"
	"sort"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"bash-teacher/internal/content"
	"bash-teacher/internal/srs"
	"bash-teacher/internal/theme"
)

// statsPane is one of the four views the screen cycles between with tab, per
// SPEC §7.1. They are separate panes rather than one long page because SPEC
// §2.4 asks for five different reports and the smallest supported terminal is
// twenty-four rows: anything that scrolled would hide the half a learner came
// to see.
type statsPane int

// The panes, in the order tab walks them. Review comes first because it is the
// one with a question in it — what is due — and Library last because it is the
// authoring view rather than the learner's.
const (
	paneReview statsPane = iota
	paneHistory
	paneMastery
	paneLibrary
)

// statsPaneTitles labels the tab bar and orders the cycle.
var statsPaneTitles = []string{"Review", "History", "Mastery", "Library"}

// statsScreen reports what is due, what has been answered, how well each
// command is known, and what the library holds.
//
// Everything it draws about the learner comes from the scheduler and the
// practice summaries the root model already holds, both of which are restored
// from the progress store at startup — so the history spans every session
// rather than this one. A run with no store says so instead of showing a
// history it does not have.
type statsScreen struct {
	lib  *content.Library
	pane statsPane
	// row and col are the mastery grid's cursor. They are held here rather
	// than on the grid because the grid is rebuilt from the scheduler on
	// every frame: only the cursor is state.
	row, col int
}

func newStats(lib *content.Library) screen { return &statsScreen{lib: lib} }

func (s *statsScreen) Capturing() bool { return false }

func (s *statsScreen) Help() []key.Binding {
	if s.pane == paneMastery {
		return []key.Binding{Keys.Up, Keys.Down, Keys.Left, Keys.Right, Keys.Act, Keys.Tab, Keys.Back, Keys.Quit}
	}
	return []key.Binding{Keys.Tab, Keys.Back, Keys.Help, Keys.Quit}
}

func (s *statsScreen) Update(a *App, msg tea.Msg) (screen, tea.Cmd) {
	km, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return s, nil
	}
	switch {
	case key.Matches(km, Keys.Tab):
		s.pane = (s.pane + 1) % statsPane(len(statsPaneTitles))
		return s, nil
	case key.Matches(km, Keys.BackTab):
		s.pane = (s.pane + statsPane(len(statsPaneTitles)) - 1) % statsPane(len(statsPaneTitles))
		return s, nil
	}
	if s.pane == paneMastery {
		cmd := s.moveMastery(a, km)
		return s, cmd
	}
	return s, nil
}

func (s *statsScreen) Body(a *App, width, height int) string {
	var body string
	switch s.pane {
	case paneHistory:
		body = s.historyPane(a, width)
	case paneMastery:
		body = s.masteryPane(a, width)
	case paneLibrary:
		body = s.libraryPane(a, width)
	default:
		body = s.reviewPane(a, width)
	}
	return "\n" + s.tabs(a, width) + "\n" + indent(body, 2)
}

// tabs draws the pane switcher. The active pane is bracketed as well as
// highlighted, so which one is showing survives --theme none.
func (s *statsScreen) tabs(a *App, width int) string {
	t := a.Theme
	labels := make([]string, 0, len(statsPaneTitles))
	for i, name := range statsPaneTitles {
		if statsPane(i) == s.pane {
			labels = append(labels, t.Selected.Render("["+name+"]"))
			continue
		}
		labels = append(labels, t.Dim.Render(" "+name+" "))
	}
	line := "  " + strings.Join(labels, " ")
	hint := t.Faint.Render("tab cycles")
	gap := width - lipgloss.Width(line) - lipgloss.Width(hint) - 2
	if gap < 1 {
		gap = 1
	}
	return truncate(line+strings.Repeat(" ", gap)+hint, width)
}

// panelWidth is the width of one of a two-panel pane's boxes. Two of them plus
// the gap between come to the same width as the mastery pane's single box, so
// the frame does not breathe in and out as tab walks the panes.
func panelWidth(width int) int {
	if w := (width - 6) / 2; w > 24 {
		return w
	}
	return 24
}

// masteryBoxWidth is the width of the mastery pane's one full-width box.
func masteryBoxWidth(width int) int { return max(2*panelWidth(width)+2, 24) }

func innerWidth(boxWidth int) int { return max(boxWidth-4, 8) }

// twoPanels lays a pane out as two boxes side by side, which is the shape
// every screen in the app uses for a summary.
func twoPanels(a *App, width int, left, right string) string {
	t, box := a.Theme, panelWidth(width)
	return lipgloss.JoinHorizontal(lipgloss.Top,
		t.Panel.Width(box).Render(left), "  ", t.Panel.Width(box).Render(right))
}

// reviewPane is the learner's standing position: what the scheduler is holding
// now, and how far through the exercise tracks they are.
func (s *statsScreen) reviewPane(a *App, width int) string {
	inner := innerWidth(panelWidth(width))
	return twoPanels(a, width,
		strings.Join(s.reviewQueue(a, inner), "\n"),
		strings.Join(s.trackProgress(a, inner), "\n"))
}

// historyPane is the half of SPEC §2.4 that reads the review log rather than
// the scheduler's current state: how retention has moved, and how much work
// went in on each of the last few weeks' days.
func (s *statsScreen) historyPane(a *App, width int) string {
	inner := innerWidth(panelWidth(width))
	days := a.SRS.History(a.Now(), clampInt(inner, 7, historyDays))
	return twoPanels(a, width,
		strings.Join(s.retentionPanel(a, days), "\n"),
		strings.Join(s.activityPanel(a, days), "\n"))
}

// libraryPane is the content view: what the library covers and how its
// exercises are distributed. It is the authoring report — a command with no
// card and no exercise is a gap in the content, not in the learner.
func (s *statsScreen) libraryPane(a *App, width int) string {
	t := a.Theme

	var uncovered []string
	for _, c := range s.lib.Commands {
		if len(s.lib.CardsFor(c.ID)) == 0 && len(s.lib.ExercisesUsing(c.ID)) == 0 {
			uncovered = append(uncovered, c.Name)
		}
	}
	sort.Strings(uncovered)

	coverage := []string{t.PanelTitle.Render("Coverage"), ""}
	for _, cat := range content.Categories {
		n := len(s.lib.CommandsByCategory(cat))
		if n == 0 {
			continue
		}
		coverage = append(coverage, row(t, 22, content.CategoryTitles[cat], fmt.Sprintf("%d", n)))
	}
	coverage = append(coverage, "",
		row(t, 22, "Drilled", fmt.Sprintf("%d/%d commands", len(s.lib.Commands)-len(uncovered), len(s.lib.Commands))))
	if len(uncovered) > 0 {
		coverage = append(coverage, t.Faint.Render("no card or exercise: "+strings.Join(uncovered, ", ")))
	}

	return twoPanels(a, width, strings.Join(coverage, "\n"), strings.Join(s.ladder(a, width), "\n"))
}

// ladder draws how many exercises sit at each difficulty level.
func (s *statsScreen) ladder(a *App, width int) []string {
	t := a.Theme
	var levels [6]int
	for _, e := range s.lib.Exercises {
		if e.Level >= 1 && e.Level <= 5 {
			levels[e.Level]++
		}
	}

	out := []string{t.PanelTitle.Render("Exercise ladder"), ""}
	fullest := 1
	for lvl := 1; lvl <= 5; lvl++ {
		if levels[lvl] > fullest {
			fullest = levels[lvl]
		}
	}
	// The bars are scaled to the panel rather than drawn one block per
	// exercise, so a longer library cannot push them through the border. The
	// budget is the panel less its border and padding, the "level N " label,
	// and room for the count on the end.
	barRoom := clampInt(panelWidth(width)-18, 4, 40)
	for lvl := 1; lvl <= 5; lvl++ {
		bar := strings.Repeat("█", levels[lvl]*barRoom/fullest)
		out = append(out, t.Dim.Render(fmt.Sprintf("level %d ", lvl))+t.Accent.Render(bar)+
			t.Faint.Render(fmt.Sprintf(" %d", levels[lvl])))
	}
	return out
}

// forecastDays is how far ahead the review outlook reaches, per SPEC §2.4.
const forecastDays = 14

// historyDays is how far back the history charts reach. A month is long enough
// for a curve to have a shape and short enough to fit one column per day in
// half of an eighty-column terminal.
const historyDays = 30

// reviewQueue reports what the scheduler is holding: what is due, how much of
// the deck has been introduced, how well it is being recalled, and the shape
// of the next fortnight.
//
// The figures come from the scheduler, which is loaded from the progress store
// at startup, so they span every session. A run with no store — `--no-store`,
// or one whose database could not be written — says so rather than implying a
// history it does not have.
func (s *statsScreen) reviewQueue(a *App, inner int) []string {
	t := a.Theme
	ids := cardIDs(s.lib.Cards)
	now := a.Now()
	seen := len(ids) - a.SRS.UnseenCount(ids)

	out := []string{t.PanelTitle.Render("Review queue"), ""}
	out = append(out,
		row(t, 14, "Due now", fmt.Sprintf("%d", a.SRS.DueCount(ids, now))),
		row(t, 14, "Introduced", fmt.Sprintf("%d of %d", seen, len(ids))))

	recalled, answered := a.SRS.Accuracy()
	if answered == 0 {
		out = append(out, row(t, 14, "Recalled", "no answers yet"))
	} else {
		out = append(out, row(t, 14, "Recalled",
			fmt.Sprintf("%d%% of %s", percent(recalled, answered), plural(answered, "answer", "answers"))))
	}

	out = append(out, "", t.Faint.Render("due over the next fortnight"))
	out = append(out, s.forecastLines(a, ids, inner)...)

	out = append(out, "", t.Faint.Render(persistenceNote(a)))
	return out
}

// trackProgress is the per-track half of SPEC §2.4: how many of each track's
// exercises have been solved. It reads the same summaries Practice draws its
// own list from, so the two can never disagree.
func (s *statsScreen) trackProgress(a *App, inner int) []string {
	t := a.Theme
	out := []string{t.PanelTitle.Render("Exercises"), ""}

	// The label and the count are fixed columns so the bars line up; the bar
	// takes whatever the panel has left, and disappears rather than wraps on
	// a panel too narrow to hold one.
	const labelWidth, countWidth = 19, 7
	barRoom := clampInt(inner-labelWidth-countWidth, 0, 24)

	passed := 0
	for _, track := range s.lib.Tracks {
		n := a.PassedInTrack(track)
		passed += n
		label := t.Dim.Render(pad(truncate(track.Title(), labelWidth-1), labelWidth))
		count := t.Body.Render(pad(fmt.Sprintf("%d/%d", n, len(track.Exercises)), countWidth))
		out = append(out, label+count+progressBar(a, barRoom, n, len(track.Exercises)))
	}

	out = append(out, "",
		row(t, labelWidth, "Solved", fmt.Sprintf("%d of %d", passed, len(s.lib.Exercises))))
	if note := shortPersistenceNote(a); note != "" {
		out = append(out, "", t.Faint.Render(note))
	}
	return out
}

// progressBar draws done-out-of-total as a filled bar. The filled part is
// never empty when anything at all is done, so a first pass shows.
func progressBar(a *App, width, done, total int) string {
	if width < 1 || total < 1 {
		return ""
	}
	filled := done * width / total
	if done > 0 && filled == 0 {
		filled = 1
	}
	return a.Theme.Accent.Render(strings.Repeat("█", filled)) +
		a.Theme.Faint.Render(strings.Repeat("░", width-filled))
}

// retentionPanel draws the curve SPEC §2.4 asks for: the share of reviews
// recalled, day by day, against the retention the scheduler is aiming at.
//
// Practice credit is deliberately absent from it. A card credited by a solved
// pipeline was never asked, so counting it would raise the curve without the
// learner having recalled anything.
func (s *statsScreen) retentionPanel(a *App, days []srs.Day) []string {
	t := a.Theme
	out := []string{t.PanelTitle.Render("Retention"), ""}

	recalled, answered := a.SRS.Accuracy()
	if answered == 0 {
		return append(out, t.Dim.Render("Nothing has been reviewed yet."), "",
			t.Faint.Render("This is the share of reviews"),
			t.Faint.Render("recalled, day by day."), "",
			t.Faint.Render(persistenceNote(a)))
	}

	out = append(out, row(t, 13, "Overall",
		fmt.Sprintf("%d%% of %s", percent(recalled, answered), plural(answered, "review", "reviews"))))

	recentRecalled, recentReviewed := 0, 0
	for _, d := range recentDays(days, 7) {
		recentRecalled += d.Recalled
		recentReviewed += d.Reviewed
	}
	if recentReviewed == 0 {
		out = append(out, row(t, 13, "Last 7 days", "nothing reviewed"))
	} else {
		out = append(out, row(t, 13, "Last 7 days",
			fmt.Sprintf("%d%% of %d", percent(recentRecalled, recentReviewed), recentReviewed)))
	}
	out = append(out, row(t, 13, "Target",
		fmt.Sprintf("%d%%", int(a.SRS.Params().DesiredRetention*100+0.5))))

	// The columns are scaled over the whole 0–100% range rather than over the
	// range the learner happens to occupy: a curve that rescaled itself would
	// make a good week and a bad one look identical.
	var spark strings.Builder
	for _, d := range days {
		r, ok := d.Retention()
		if !ok {
			spark.WriteRune(' ')
			continue
		}
		spark.WriteRune(sparkBlocks[clampInt(int(r*float64(len(sparkBlocks))), 0, len(sparkBlocks)-1)])
	}

	out = append(out, "", t.Faint.Render("by day · gap = nothing reviewed"),
		t.Accent.Render(spark.String()), dayScale(t, len(days)))
	return out
}

// activityPanel is the volume half of the history: how much was answered, over
// how many days, and whether the habit is holding.
func (s *statsScreen) activityPanel(a *App, days []srs.Day) []string {
	t := a.Theme
	out := []string{t.PanelTitle.Render("Activity"), ""}

	reviews, credits := a.SRS.Totals()
	active := a.SRS.ActiveDays()
	if reviews+credits == 0 {
		return append(out, t.Dim.Render("No history yet."), "",
			t.Faint.Render("Answer a card or solve an"),
			t.Faint.Render("exercise and it starts here."))
	}

	out = append(out,
		row(t, 13, "Answered", fmt.Sprintf("%d over %s", reviews+credits, plural(active, "day", "days"))),
		row(t, 13, "Reviews", fmt.Sprintf("%d", reviews)),
		row(t, 13, "Practice", plural(credits, "credit", "credits")),
		row(t, 13, "Streak", fmt.Sprintf("%s (best %d)",
			plural(a.SRS.Streak(a.Now()), "day", "days"), a.SRS.LongestStreak())))
	if first, ok := a.SRS.FirstAnswer(); ok {
		out = append(out, row(t, 13, "Since", first.Format("2 Jan 2006")))
	}
	// A history that is not being written is still worth drawing — it is this
	// session's — but it must not be read as one that will be there tomorrow.
	if note := shortPersistenceNote(a); note != "" {
		out = append(out, row(t, 13, "Saved", note))
	}

	peak := 0
	for _, d := range days {
		if d.Answered > peak {
			peak = d.Answered
		}
	}
	out = append(out, "", t.Faint.Render(fmt.Sprintf("answers per day · peak %d", peak)))
	if peak == 0 {
		return append(out, t.Dim.Render("nothing in the last month"))
	}

	var spark strings.Builder
	for _, d := range days {
		if d.Answered == 0 {
			spark.WriteRune(' ')
			continue
		}
		// A day with any work at all gets at least the shortest block, so one
		// answered card never disappears into the baseline.
		spark.WriteRune(sparkBlocks[clampInt((d.Answered*len(sparkBlocks)-1)/peak, 0, len(sparkBlocks)-1)])
	}
	return append(out, t.Accent.Render(spark.String()), dayScale(t, len(days)))
}

// recentDays returns the last n days of a history, or all of it when it is
// shorter.
func recentDays(days []srs.Day, n int) []srs.Day {
	if len(days) <= n {
		return days
	}
	return days[len(days)-n:]
}

// dayScale labels a backward-looking chart at both ends, so a column's
// distance from today is readable without counting.
func dayScale(t *theme.Theme, columns int) string {
	left := fmt.Sprintf("-%dd", columns-1)
	gap := columns - len(left) - len("today")
	if gap < 1 {
		gap = 1
	}
	return t.Faint.Render(left + strings.Repeat(" ", gap) + "today")
}

// percent rounds a ratio to whole percentage points.
func percent(part, whole int) int {
	if whole == 0 {
		return 0
	}
	return (part*200 + whole) / (whole * 2)
}

// sparkBlocks is the eighth-height ramp the charts are drawn with. One column
// per day keeps a fortnight — or a month — inside a panel, where a row each
// would not fit beside its figures.
var sparkBlocks = []rune("▁▂▃▄▅▆▇█")

// forecastLines draws the review outlook as a sparkline with a scale under it,
// so the shape of the coming fortnight is one glance rather than fourteen rows.
func (s *statsScreen) forecastLines(a *App, ids []string, inner int) []string {
	t := a.Theme
	counts := a.SRS.Forecast(ids, a.Now(), forecastDays)
	peak, total := 0, 0
	for _, n := range counts {
		total += n
		if n > peak {
			peak = n
		}
	}
	if peak == 0 {
		return []string{t.Dim.Render("nothing scheduled yet")}
	}

	var spark strings.Builder
	for _, n := range counts {
		if n == 0 {
			spark.WriteRune(' ')
			continue
		}
		// A day with any work at all gets at least the shortest block, so a
		// single card never disappears into the baseline.
		i := clampInt((n*len(sparkBlocks)-1)/peak, 0, len(sparkBlocks)-1)
		spark.WriteRune(sparkBlocks[i])
	}

	return []string{
		t.Accent.Render(spark.String()) + t.Faint.Render(fmt.Sprintf("  peak %d", peak)),
		t.Faint.Render("today" + strings.Repeat(" ", max(1, forecastDays-9)) + "+13d"),
		t.Dim.Render(truncate(fmt.Sprintf("%s due in the next %d days", plural(total, "card", "cards"), forecastDays), inner)),
	}
}
