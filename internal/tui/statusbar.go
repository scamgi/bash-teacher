package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
)

// The status bar is one line under a rule: the current screen's key legend on
// the left, the session's live counters on the right. Both zones are built at
// full length and then fitted, because what has to give when the terminal is
// narrow is not the same on the two sides — a key the learner cannot find is
// worse than a count they can also read on Home, so the counters are dropped
// one at a time before the legend is touched at all.
const (
	// statusGap is the minimum run of spaces between the legend and the
	// counters, so the two zones never read as one sentence.
	statusGap = 3
	// chipSep separates the counters.
	chipSep = " │ "
	// keySep separates one legend entry from the next.
	keySep = " · "
)

func (a *App) footer() string {
	t := a.Theme
	rule := t.Faint.Render(strings.Repeat("─", a.width))

	left := a.legend()
	if a.flash != "" {
		// A flash replaces the legend rather than the whole bar: what a key
		// just did is news, but the counters are still true.
		left = t.Warn.Render("▸ ") + t.Body.Render(a.flash)
	}

	sep := t.Faint.Render(chipSep)
	chips := a.statusChips()
	room := a.width - lipgloss.Width(left) - statusGap
	for len(chips) > 0 && lipgloss.Width(strings.Join(chips, sep)) > room {
		// The leftmost chip is the least important one; see statusChips.
		chips = chips[1:]
	}
	right := strings.Join(chips, sep)

	if right == "" {
		// Nothing fits beside the legend; do not pad the line out with
		// spaces nobody can see.
		return rule + "\n" + truncate(left, a.width)
	}
	gap := a.width - lipgloss.Width(left) - lipgloss.Width(right)
	return rule + "\n" + left + strings.Repeat(" ", gap) + right
}

// legend renders the current screen's bindings as `key description` pairs, the
// key in the accent and the description dim, so the eye can find the keys
// without reading the words. Entries are dropped whole rather than sliced, so
// a narrow terminal never shows half a binding.
func (a *App) legend() string {
	bindings := a.screens[a.current].Help()
	if len(bindings) == 0 {
		bindings = Keys.ShortHelp()
	}

	t := a.Theme
	sep := t.Faint.Render(keySep)
	var out []string
	width := 0
	for _, b := range bindings {
		if !b.Enabled() {
			continue
		}
		h := b.Help()
		entry := t.Key.Render(h.Key) + " " + t.Dim.Render(h.Desc)
		w := lipgloss.Width(entry)
		if len(out) > 0 {
			w += lipgloss.Width(sep)
		}
		// Leave a cell for the ellipsis that says entries were dropped.
		if width+w > a.width-1 {
			return strings.Join(out, sep) + t.Faint.Render("…")
		}
		out = append(out, entry)
		width += w
	}
	return strings.Join(out, sep)
}

// statusChips are the session counters, ordered least important first: the bar
// drops them from the left, so the two that matter mid-session — what is due
// and whether anything is being saved — are the last to go.
func (a *App) statusChips() []string {
	t := a.Theme
	var chips []string

	if a.Lib != nil {
		if total := len(a.Lib.Exercises); total > 0 {
			chips = append(chips, t.Pass.Render(fmt.Sprintf("%d/%d", a.PassedExercises(), total))+
				" "+t.Dim.Render("solved"))
		}
	}
	if a.SRS != nil {
		if streak := a.SRS.Streak(a.Now()); streak > 0 {
			chips = append(chips, t.Warn.Render(fmt.Sprintf("%d", streak))+
				" "+t.Dim.Render("day streak"))
		}
		if due := a.DueCards(); due > 0 {
			chips = append(chips, t.Accent.Render(fmt.Sprintf("%d", due))+" "+t.Dim.Render("due"))
		}
	}
	// Colour is never the only signal: the warning carries its glyph too.
	if !a.Persisting() {
		chips = append(chips, t.Warn.Render("⚠ not saving"))
	}
	return chips
}
