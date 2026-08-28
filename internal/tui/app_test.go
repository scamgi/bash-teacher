package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	embedded "bash-teacher/content"
	"bash-teacher/internal/content"
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
	a := New(lib, theme.Resolve(theme.None), "test", ScreenHome)
	a.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return a
}

// press feeds a printable key, the way the terminal would.
func press(a *App, s string) {
	var msg tea.KeyPressMsg
	switch s {
	case "enter":
		msg = tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		msg = tea.KeyPressMsg{Code: tea.KeyEscape}
	case "down":
		msg = tea.KeyPressMsg{Code: tea.KeyDown}
	case "up":
		msg = tea.KeyPressMsg{Code: tea.KeyUp}
	default:
		msg = tea.KeyPressMsg{Code: rune(s[0]), Text: s}
	}
	a.Update(msg)
}

func view(a *App) string { return a.View().Content }

func TestHomeRendersLibraryCounts(t *testing.T) {
	a := newTestApp(t, 100, 30)
	v := view(a)
	for _, want := range []string{"bash-teacher", "Dictionary", "Practice", "Flashcards", "Stats", "Library"} {
		if !strings.Contains(v, want) {
			t.Errorf("home view is missing %q\n%s", want, v)
		}
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
	if !strings.Contains(view(a), "grep") {
		t.Fatalf("dictionary should list grep:\n%s", view(a))
	}

	press(a, "/")
	for _, r := range "uniq" {
		press(a, string(r))
	}
	v := view(a)
	if !strings.Contains(v, "uniq") {
		t.Errorf("filtered dictionary should still show uniq:\n%s", v)
	}
	if strings.Contains(v, "\n  grep") {
		t.Errorf("filtered dictionary should not list grep:\n%s", v)
	}
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

func TestFlashcardsRevealAndAdvance(t *testing.T) {
	a := newTestApp(t, 100, 30)
	press(a, "3")
	first := view(a)
	if !strings.Contains(first, "enter to reveal") {
		t.Fatalf("a card should start face down:\n%s", first)
	}
	press(a, "enter")
	if strings.Contains(view(a), "enter to reveal") {
		t.Error("enter should reveal the back of the card")
	}
	press(a, "down")
	if !strings.Contains(view(a), "enter to reveal") {
		t.Error("advancing should turn the next card face down")
	}
}
