package tui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"bash-teacher/internal/content"
	"bash-teacher/internal/theme"
)

// masteryLevel is how well one command is known, as the heat grid paints it.
type masteryLevel int

// The bands, weakest first. "Nothing" is a command the library has no card
// for: an absence in the content rather than in the learner, which is why it
// is a band of its own and not the same as untouched.
const (
	masteryNothing masteryLevel = iota
	masteryUnseen
	masteryLearning
	masteryFamiliar
	masteryStrong
)

// familiarStability and strongStability are where the bands sit, in days of
// stability. Stability is the interval in days at the default retention (the
// identity `internal/srs` pins), so these read as "comes back in a week" and
// "comes back in three weeks" rather than as tuning constants.
const (
	familiarStability = 7.0
	strongStability   = 21.0
)

// glyph is the cell the grid draws. The ramp is a shade ramp rather than four
// colours, because colour is never the only signal.
func (l masteryLevel) glyph() string {
	switch l {
	case masteryUnseen:
		return "░"
	case masteryLearning:
		return "▒"
	case masteryFamiliar:
		return "▓"
	case masteryStrong:
		return "█"
	default:
		return "·"
	}
}

func (l masteryLevel) label() string {
	switch l {
	case masteryUnseen:
		return "not started"
	case masteryLearning:
		return "learning"
	case masteryFamiliar:
		return "familiar"
	case masteryStrong:
		return "strong"
	default:
		return "no cards"
	}
}

// style paints a band by role: what is known is a pass, what is in hand is a
// warning, and what has never been touched is chrome.
func (l masteryLevel) style(t *theme.Theme) lipgloss.Style {
	switch l {
	case masteryUnseen:
		return t.Dim
	case masteryLearning:
		return t.Warn
	case masteryFamiliar:
		return t.Accent
	case masteryStrong:
		return t.Pass
	default:
		return t.Faint
	}
}

// commandMastery is one cell of the grid: a command and what the learner's
// history says about it.
type commandMastery struct {
	cmd        *content.Command
	level      masteryLevel
	cards      int
	introduced int
	due        int
	// strongest is the best stability among the command's cards, in days —
	// the longest the learner has gone and still recalled one of them.
	strongest float64
	exercises int
	passed    int
}

// masteryOf reads one command's standing out of the scheduler and the practice
// summaries.
//
// A command is scored by its cards rather than by its exercises because the
// cards are what test recall; an exercise contributes through the half-strength
// credit SPEC §5 already gives every card it teaches, so practice raises the
// grid without a second, differently-weighted path into it.
func masteryOf(a *App, c *content.Command) commandMastery {
	m := commandMastery{cmd: c}
	now := a.Now()

	total := 0
	for _, card := range a.Lib.CardsFor(c.ID) {
		m.cards++
		if a.SRS == nil {
			continue
		}
		st, _ := a.SRS.State(card.ID)
		if !st.Seen() {
			continue
		}
		m.introduced++
		if !st.Due.After(now) {
			m.due++
		}
		if st.Stability > m.strongest {
			m.strongest = st.Stability
		}
		switch {
		case st.Stability >= strongStability:
			total += 3
		case st.Stability >= familiarStability:
			total += 2
		default:
			total++
		}
	}

	for _, ex := range a.Lib.ExercisesUsing(c.ID) {
		m.exercises++
		if a.ExercisePassed(ex.ID) {
			m.passed++
		}
	}

	switch {
	case m.cards == 0:
		m.level = masteryNothing
	case total == 0:
		m.level = masteryUnseen
	default:
		// The band is the mean card score, floored: a command is as well
		// known as its cards are on average, and one strong card does not
		// carry three weak ones.
		band := clampInt(total/m.cards, 1, 3)
		m.level = masteryLevel(int(masteryLearning) + band - 1)
	}
	return m
}

// masteryRow is one category's cells, in the dictionary's own order so that
// the grid and the dictionary read the same way.
type masteryRow struct {
	title string
	cells []commandMastery
}

// masteryRows builds the whole grid. It is rebuilt every frame rather than
// cached: eighty commands over a few hundred cards is a handful of map lookups,
// and a cache would have to be invalidated by every answered card.
func masteryRows(a *App) []masteryRow {
	var rows []masteryRow
	for _, cat := range content.Categories {
		cmds := a.Lib.CommandsByCategory(cat)
		if len(cmds) == 0 {
			continue
		}
		row := masteryRow{title: content.CategoryTitles[cat]}
		for _, c := range cmds {
			row.cells = append(row.cells, masteryOf(a, c))
		}
		rows = append(rows, row)
	}
	return rows
}

// focusedColumn is where the cursor actually sits on a row of the given
// length. The stored column is a wish rather than a position: the categories
// are different lengths, and a cursor that gave up its column every time it
// crossed a short row would drift left as the learner scanned down the grid.
func (s *statsScreen) focusedColumn(cells int) int { return clampInt(s.col, 0, cells-1) }

// focused returns the cell under the cursor, and whether there is one.
func (s *statsScreen) focused(rows []masteryRow) (commandMastery, bool) {
	if len(rows) == 0 {
		return commandMastery{}, false
	}
	cells := rows[clampInt(s.row, 0, len(rows)-1)].cells
	if len(cells) == 0 {
		return commandMastery{}, false
	}
	return cells[s.focusedColumn(len(cells))], true
}

// moveMastery walks the grid. Enter opens the focused command's dictionary
// entry, which is the question the grid provokes: a cell that is still empty
// is one the learner wants to go and read.
func (s *statsScreen) moveMastery(a *App, km tea.KeyPressMsg) tea.Cmd {
	rows := masteryRows(a)
	if len(rows) == 0 {
		return nil
	}
	s.row = clampInt(s.row, 0, len(rows)-1)

	width := len(rows[s.row].cells)
	switch {
	case key.Matches(km, Keys.Up):
		s.row = clampInt(s.row-1, 0, len(rows)-1)
	case key.Matches(km, Keys.Down):
		s.row = clampInt(s.row+1, 0, len(rows)-1)
	case key.Matches(km, Keys.Left):
		// Sideways motion starts from where the cursor is drawn, not from the
		// column it wishes it were in, so a left press always moves one cell.
		s.col = clampInt(s.focusedColumn(width)-1, 0, width-1)
	case key.Matches(km, Keys.Right):
		s.col = clampInt(s.focusedColumn(width)+1, 0, width-1)
	case key.Matches(km, Keys.Act):
		if cell, ok := s.focused(rows); ok {
			return lookupCommand(cell.cmd.ID)
		}
	}
	return nil
}

// masteryPane draws the per-command heat grid SPEC §2.4 asks for: one cell per
// dictionary entry, grouped by category, with the focused command's figures
// spelled out underneath.
//
// It is one full-width panel rather than the two the other panes use, because
// the widest category holds sixteen commands and a half-width box cannot show
// them all without wrapping a row that is meant to be read as one band.
func (s *statsScreen) masteryPane(a *App, width int) string {
	t := a.Theme
	rows := masteryRows(a)
	box := masteryBoxWidth(width)
	inner := innerWidth(box)

	head := t.PanelTitle.Render("Mastery")
	legend := masteryLegend(t)
	gap := inner - lipgloss.Width(head) - lipgloss.Width(legend)
	if gap < 1 {
		gap = 1
	}
	out := []string{head + strings.Repeat(" ", gap) + legend, ""}

	const labelWidth = 22
	for i, r := range rows {
		var b strings.Builder
		b.WriteString(t.Dim.Render(pad(truncate(r.title, labelWidth-1), labelWidth)))
		focus := s.focusedColumn(len(r.cells))
		for j, cell := range r.cells {
			// The focused cell is bracketed as well as highlighted: on a
			// terminal with no colour the brackets are the whole cursor.
			if i == s.row && j == focus {
				b.WriteString(t.Selected.Render("[" + cell.level.glyph() + "]"))
				continue
			}
			b.WriteString(" " + cell.level.style(t).Render(cell.glyph()) + " ")
		}
		out = append(out, truncate(b.String(), inner))
	}

	out = append(out, "")
	out = append(out, s.masteryDetail(a, rows, inner)...)
	return t.Panel.Width(box).Render(strings.Join(out, "\n"))
}

// glyph is the cell for one command, shared by the grid and its detail line.
func (m commandMastery) glyph() string { return m.level.glyph() }

// masteryLegend names the ramp. Without it the shades are a gradient with no
// units, and a learner cannot tell "seen once" from "known cold".
func masteryLegend(t *theme.Theme) string {
	parts := make([]string, 0, 5)
	for _, l := range []masteryLevel{masteryNothing, masteryUnseen, masteryLearning, masteryFamiliar, masteryStrong} {
		parts = append(parts, l.style(t).Render(l.glyph())+t.Faint.Render(" "+l.label()))
	}
	return strings.Join(parts, t.Faint.Render("  "))
}

// masteryDetail spells out the focused command in words, so the grid never
// asks a learner to read a figure out of a shade.
func (s *statsScreen) masteryDetail(a *App, rows []masteryRow, inner int) []string {
	t := a.Theme
	cell, ok := s.focused(rows)
	if !ok {
		return []string{t.Dim.Render("no commands to show")}
	}

	head := t.Body.Render(cell.cmd.Name) + t.Faint.Render(" · ") +
		cell.level.style(t).Render(cell.level.label()) + t.Faint.Render(" · ") +
		t.Dim.Render(cell.cmd.Summary)

	var facts []string
	if cell.cards == 0 {
		facts = append(facts, "no cards")
	} else {
		facts = append(facts, fmt.Sprintf("%s, %d introduced", plural(cell.cards, "card", "cards"), cell.introduced))
		if cell.due > 0 {
			facts = append(facts, fmt.Sprintf("%d due", cell.due))
		}
		if cell.strongest > 0 {
			facts = append(facts, fmt.Sprintf("strongest %s", intervalWords(cell.strongest)))
		}
	}
	if cell.exercises > 0 {
		facts = append(facts, fmt.Sprintf("exercises %d/%d", cell.passed, cell.exercises))
	}

	return []string{
		truncate(head, inner),
		truncate(t.Faint.Render(strings.Join(facts, " · ")), inner),
	}
}

// intervalWords renders a stability in days the way the review screen renders
// an interval: in whatever unit reads shortest.
func intervalWords(days float64) string {
	switch {
	case days >= 365:
		return fmt.Sprintf("%.1fy", days/365)
	case days >= 30:
		return fmt.Sprintf("%.0fmo", days/30)
	case days >= 1:
		return fmt.Sprintf("%.0fd", days)
	default:
		return fmt.Sprintf("%.0fh", days*24)
	}
}
