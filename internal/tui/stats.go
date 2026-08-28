package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"bash-teacher/internal/content"
)

// statsScreen reports on the library today and on the learner's progress once
// the store exists. Until then it shows content coverage, which is also the
// authoring view: it makes commands with no cards or no exercises obvious.
type statsScreen struct{ lib *content.Library }

func newStats(lib *content.Library) screen { return &statsScreen{lib: lib} }

func (s *statsScreen) Capturing() bool { return false }

func (s *statsScreen) Help() []key.Binding {
	return []key.Binding{Keys.Back, Keys.Help, Keys.Quit}
}

func (s *statsScreen) Update(a *App, msg tea.Msg) (screen, tea.Cmd) { return s, nil }

func (s *statsScreen) Body(a *App, width, height int) string {
	t := a.Theme

	var uncovered []string
	for _, c := range s.lib.Commands {
		if len(s.lib.CardsFor(c.ID)) == 0 && len(s.lib.ExercisesUsing(c.ID)) == 0 {
			uncovered = append(uncovered, c.Name)
		}
	}
	sort.Strings(uncovered)

	var levels [6]int
	for _, e := range s.lib.Exercises {
		if e.Level >= 1 && e.Level <= 5 {
			levels[e.Level]++
		}
	}

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

	boxWidth := boxWidthOf(width)
	ladder := []string{t.PanelTitle.Render("Exercise ladder"), ""}
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
	barRoom := clampInt(boxWidth-18, 4, 40)
	for lvl := 1; lvl <= 5; lvl++ {
		bar := strings.Repeat("█", levels[lvl]*barRoom/fullest)
		ladder = append(ladder, t.Dim.Render(fmt.Sprintf("level %d ", lvl))+t.Accent.Render(bar)+
			t.Faint.Render(fmt.Sprintf(" %d", levels[lvl])))
	}

	progress := strings.Join(s.reviewQueue(a), "\n")

	left := t.Panel.Width(boxWidth).Render(strings.Join(coverage, "\n"))
	right := t.Panel.Width(boxWidth).Render(strings.Join(ladder, "\n") + "\n\n" + progress)
	return "\n" + indent(lipgloss.JoinHorizontal(lipgloss.Top, left, "  ", right), 2)
}

// boxWidthOf is the width of one of the screen's two side-by-side panels.
func boxWidthOf(width int) int {
	if w := (width - 8) / 2; w > 24 {
		return w
	}
	return 24
}

// forecastDays is how far ahead the review outlook reaches, per SPEC §2.4.
const forecastDays = 14

// reviewQueue reports what the scheduler is holding: what is due, how much of
// the deck has been introduced, how well it is being recalled, and the shape
// of the next fortnight.
//
// The figures are this session's. Nothing survives the process until the store
// lands in M6, which the panel says rather than implying a history it does not
// have.
func (s *statsScreen) reviewQueue(a *App) []string {
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
			fmt.Sprintf("%d%% of %s", 100*recalled/answered, plural(answered, "answer", "answers"))))
	}

	out = append(out, "", t.Faint.Render("due over the next fortnight"))
	out = append(out, s.forecastLines(a, ids, now)...)

	out = append(out, "", t.Faint.Render("This session only — the progress store lands in M6."))
	return out
}

// sparkBlocks is the eighth-height ramp the forecast is drawn with. One column
// per day keeps a fortnight inside a panel, where fourteen rows would not fit
// beside the coverage table.
var sparkBlocks = []rune("▁▂▃▄▅▆▇█")

// forecastLines draws the review outlook as a sparkline with a scale under it,
// so the shape of the coming fortnight is one glance rather than fourteen rows.
func (s *statsScreen) forecastLines(a *App, ids []string, now time.Time) []string {
	t := a.Theme
	counts := a.SRS.Forecast(ids, now, forecastDays)
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
		t.Dim.Render(fmt.Sprintf("%s due in the next %d days", plural(total, "card", "cards"), forecastDays)),
	}
}
