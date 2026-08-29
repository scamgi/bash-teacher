package tui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

// statusLine returns the last rendered line, which is the status bar.
func statusLine(a *App) string {
	lines := strings.Split(view(a), "\n")
	return lines[len(lines)-1]
}

// TestStatusBarShowsKeysAndCounters checks the two zones are both there on a
// terminal wide enough for them: the legend on the left, the session counters
// on the right.
func TestStatusBarShowsKeysAndCounters(t *testing.T) {
	a := newTestApp(t, 120, 30)
	line := statusLine(a)
	if !strings.Contains(line, "enter choose") {
		t.Errorf("the legend should name the keys, got %q", line)
	}
	if !strings.Contains(line, "solved") {
		t.Errorf("the counters should report exercise progress, got %q", line)
	}
	// A test app has no store, and the bar has to say so rather than let the
	// learner assume the day is being kept.
	if !strings.Contains(line, "not saving") {
		t.Errorf("a session with no store should say so, got %q", line)
	}
}

// TestStatusBarDropsCountersBeforeKeys pins the priority: a narrow terminal
// keeps the bindings, which are the only place some of them are written down,
// and loses the counters, which Home and Stats also report.
func TestStatusBarDropsCountersBeforeKeys(t *testing.T) {
	a := newTestApp(t, minWidth, minHeight)
	press(a, "1")
	line := statusLine(a)
	if strings.Contains(line, "solved") {
		t.Errorf("the counters should have been dropped at %d cells, got %q", minWidth, line)
	}
	if !strings.Contains(line, "up") || !strings.Contains(line, "search") {
		t.Errorf("the legend should survive a narrow terminal, got %q", line)
	}
}

// TestStatusBarNeverOverflows walks every screen at the smallest supported
// terminal: the bar is built from two independently sized zones, so an
// off-by-one there would push the frame wider than the terminal.
func TestStatusBarNeverOverflows(t *testing.T) {
	for _, w := range []int{minWidth, 100, 200} {
		a := newTestApp(t, w, minHeight)
		for _, s := range []Screen{ScreenHome, ScreenDictionary, ScreenPractice, ScreenFlashcards, ScreenStats} {
			a.current = s
			if n := lipgloss.Width(statusLine(a)); n > w {
				t.Errorf("%v at width %d: status bar is %d cells", s, w, n)
			}
		}
	}
}

// TestFlashKeepsTheCounters checks a transient message takes the legend's
// place and not the whole bar: what a key just did is news, the counters are
// still true.
func TestFlashKeepsTheCounters(t *testing.T) {
	a := newTestApp(t, 120, 30)
	press(a, "1")
	press(a, "right")
	pressCmd(a, "y")
	line := statusLine(a)
	if !strings.Contains(line, "copied:") {
		t.Errorf("the flash should be on the bar, got %q", line)
	}
	if !strings.Contains(line, "solved") {
		t.Errorf("a flash should not take the counters with it, got %q", line)
	}
}
