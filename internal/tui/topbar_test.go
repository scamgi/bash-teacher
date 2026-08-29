package tui

import (
	"strings"
	"testing"
	"unicode/utf8"

	"charm.land/lipgloss/v2"
)

// headerLines returns the two rows of the top bar: the tabs and the rule.
func headerLines(a *App) (tabs, rule string) {
	lines := strings.Split(view(a), "\n")
	return lines[0], lines[1]
}

// TestTopBarNamesEveryScreenAndItsKey checks the bar is a map of the app: the
// digits that switch screens are on it, next to what they select.
func TestTopBarNamesEveryScreenAndItsKey(t *testing.T) {
	a := newTestApp(t, 120, 30)
	line, _ := headerLines(a)
	for s := ScreenHome; s <= ScreenStats; s++ {
		if !strings.Contains(line, screenKey(s)+" "+s.String()) {
			t.Errorf("the top bar should offer %q, got %q", screenKey(s)+" "+s.String(), line)
		}
	}
	if !strings.Contains(line, brandName) {
		t.Errorf("a wide terminal should still carry the brand, got %q", line)
	}
}

// TestTopBarUnderlinesTheCurrentScreen pins the one signal that is not a
// colour: the rule is heavy under the active tab and faint everywhere else, so
// the current screen is legible under --theme none too.
func TestTopBarUnderlinesTheCurrentScreen(t *testing.T) {
	a := newTestApp(t, 120, 30)
	for _, tc := range []struct {
		key    string
		screen Screen
	}{{"1", ScreenDictionary}, {"2", ScreenPractice}, {"3", ScreenFlashcards}, {"4", ScreenStats}} {
		press(a, tc.key)
		line, rule := headerLines(a)
		want := tabWidth(tc.screen) + 2*tabBleed
		if n := strings.Count(rule, "━"); n != want {
			t.Errorf("%v: underline is %d cells, want %d (%q)", tc.screen, n, want, rule)
		}
		// The mark has to sit under the tab it belongs to, not merely be the
		// right length: the offset is where an off-by-one would show.
		at := utf8.RuneCountInString(rule[:strings.IndexRune(rule, '━')])
		label := strings.Index(line, tc.screen.String())
		if label < 0 {
			t.Fatalf("%v: the top bar does not name it: %q", tc.screen, line)
		}
		if got := lipgloss.Width(line[:label]); at+tabBleed+2 != got {
			t.Errorf("%v: underline starts at %d, tab at %d", tc.screen, at, got)
		}
	}
}

// TestTopBarDropsOrnamentBeforeTabs is the priority the bar is built on: the
// tabs are navigation and stay, the version and then the brand are ornament
// and go, so a long `git describe` version cannot push the tabs off the line.
func TestTopBarDropsOrnamentBeforeTabs(t *testing.T) {
	a := newTestApp(t, minWidth, minHeight)
	a.Version = "v0.1.0-42-gdeadbeef-dirty"
	line, _ := headerLines(a)
	if strings.Contains(line, a.Version) {
		t.Errorf("a version this long should have been dropped at %d cells, got %q", minWidth, line)
	}
	for s := ScreenHome; s <= ScreenStats; s++ {
		if !strings.Contains(line, s.String()) {
			t.Errorf("%v should survive a narrow terminal, got %q", s, line)
		}
	}
}

// TestTopBarNeverOverflows walks every screen at three widths: the bar is
// assembled from independently sized zones and a rule built by arithmetic, so
// an off-by-one in either would widen the frame.
func TestTopBarNeverOverflows(t *testing.T) {
	for _, w := range []int{minWidth, 100, 200} {
		a := newTestApp(t, w, minHeight)
		for s := ScreenHome; s <= ScreenStats; s++ {
			a.current = s
			line, rule := headerLines(a)
			if n := lipgloss.Width(line); n > w {
				t.Errorf("%v at width %d: tab line is %d cells", s, w, n)
			}
			if n := lipgloss.Width(rule); n != w {
				t.Errorf("%v at width %d: rule is %d cells", s, w, n)
			}
		}
	}
}
