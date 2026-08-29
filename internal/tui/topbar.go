package tui

import (
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
)

// The top bar is one line over a rule, and the rule is part of it: the five
// screens are drawn as tabs in the order their number keys select them, and
// the segment of the rule under the current screen is drawn heavy while the
// rest stays faint. That is what makes the bar worth its two rows — the digits
// that switch screens are otherwise written down only on Home and under `?`,
// and the underline says where you are without spending a colour on it, so the
// bar still reads under `--theme none`.
//
// The zones are sized like the status bar's: the tabs are navigation and are
// never dropped, while the brand and the version are ornament and give way, in
// that order, on a terminal too narrow to hold them.
const (
	// tabGap is the run of spaces between two tabs.
	tabGap = 2
	// brandGap separates the app's name from the first tab.
	brandGap = 3
	// versionGap is the least space left between the last tab and the version.
	versionGap = 2
	// tabBleed is how far the active tab's underline runs past its label on
	// each side, so the mark reads as a tab and not as an underscore.
	tabBleed = 1
)

const brandName = "bash-teacher"

func (a *App) header() string {
	t := a.Theme

	// The brand is the first thing to go: the tabs name the app's parts, and
	// the title bar of a terminal already names the app.
	offset := 0
	line := ""
	if brandWidth := lipgloss.Width(brandName) + brandGap; brandWidth+navWidth() <= a.width {
		line = t.Title.Render(brandName) + strings.Repeat(" ", brandGap)
		offset = brandWidth
	}

	strip, from, to := a.navStrip(offset)
	line += strip
	used := offset + navWidth()

	if a.Version != "" {
		if w := lipgloss.Width(a.Version); used+versionGap+w <= a.width {
			line += strings.Repeat(" ", a.width-used-w) + t.Faint.Render(a.Version)
		}
	}
	return truncate(line, a.width) + "\n" + a.headerRule(from, to)
}

// navStrip renders the screen tabs starting at column offset and reports the
// columns the current screen's tab spans, which is what the rule underneath
// marks. Every tab carries its number key, since that is the whole point of
// drawing them.
func (a *App) navStrip(offset int) (strip string, from, to int) {
	t := a.Theme
	var b strings.Builder
	col := offset
	for s := ScreenHome; s <= ScreenStats; s++ {
		if s > ScreenHome {
			b.WriteString(strings.Repeat(" ", tabGap))
			col += tabGap
		}
		key, name := screenKey(s), s.String()
		if s == a.current {
			b.WriteString(t.Key.Render(key) + " " + t.Title.Render(name))
			from, to = col, col+tabWidth(s)
		} else {
			b.WriteString(t.Faint.Render(key) + " " + t.Dim.Render(name))
		}
		col += tabWidth(s)
	}
	return b.String(), from, to
}

// headerRule draws the rule under the bar, heavy across the active tab. The
// glyph carries the signal rather than the colour, so the current screen is
// still marked with colour switched off.
func (a *App) headerRule(from, to int) string {
	t := a.Theme
	from, to = clampSpan(from-tabBleed, to+tabBleed, a.width)
	if from >= to {
		return t.Faint.Render(strings.Repeat("─", a.width))
	}
	return t.Faint.Render(strings.Repeat("─", from)) +
		t.Accent.Render(strings.Repeat("━", to-from)) +
		t.Faint.Render(strings.Repeat("─", a.width-to))
}

// clampSpan trims a span to the line, returning an empty span if none of it
// lands on one.
func clampSpan(from, to, width int) (lo, hi int) {
	if from < 0 {
		from = 0
	}
	if to > width {
		to = width
	}
	if from > to {
		from = to
	}
	return from, to
}

// screenKey is the digit that selects a screen; the constants are declared in
// that order, so the key is the screen's own value.
func screenKey(s Screen) string { return strconv.Itoa(int(s)) }

// tabWidth is the width of one tab's label, key included.
func tabWidth(s Screen) int { return 2 + lipgloss.Width(s.String()) }

// navWidth is the width of the whole strip, which is fixed: the tabs are the
// same five on every screen.
func navWidth() int {
	w := 0
	for s := ScreenHome; s <= ScreenStats; s++ {
		if s > ScreenHome {
			w += tabGap
		}
		w += tabWidth(s)
	}
	return w
}
