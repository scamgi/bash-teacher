package tui

import (
	"strings"
	"testing"
	"time"

	"bash-teacher/internal/content"
	"bash-teacher/internal/srs"
)

// statsOf returns the Stats screen's own model, so a test can put it on a
// pane without pressing tab a known number of times.
func statsOf(a *App) *statsScreen { return a.screens[ScreenStats].(*statsScreen) }

// seedHistory plays a scripted three weeks into the app's scheduler and pins
// the clock to the day it ends on, so every figure the history draws is a
// known number rather than whatever the day the test runs on produces.
//
// The script is twenty-one days ending today with days 5 and 6 back missed:
// five reviews a day, and the first review of every third day failed. That
// gives 95 reviews, 6 of them lapses, a current streak of five days and a best
// of fourteen — the numbers the assertions below name.
func seedHistory(a *App, now time.Time) {
	a.clock = func() time.Time { return now }
	ids := cardIDs(a.Lib.Cards)
	for back := 20; back >= 0; back-- {
		if back == 5 || back == 6 {
			continue
		}
		day := now.AddDate(0, 0, -back)
		for i := 0; i < 5; i++ {
			rating := srs.Good
			if i == 0 && back%3 == 0 {
				rating = srs.Again
			}
			a.SRS.Grade(ids[(back*5+i)%len(ids)], rating, time.Second, day)
		}
	}
}

// studyDay is the day seeded histories end on. Any fixed date does; this one
// is a Saturday, which nothing depends on but which makes a dumped frame
// readable.
var studyDay = time.Date(2026, 8, 29, 10, 0, 0, 0, time.Local)

func TestStatsPanesCycleWithTab(t *testing.T) {
	a := newTestApp(t, 100, 30)
	press(a, "4")

	want := []struct {
		pane statsPane
		text string
	}{
		{paneReview, "Review queue"},
		{paneHistory, "Retention"},
		{paneMastery, "Mastery"},
		{paneLibrary, "Coverage"},
	}
	for _, w := range want {
		if got := statsOf(a).pane; got != w.pane {
			t.Fatalf("pane is %v, want %v", got, w.pane)
		}
		if v := view(a); !strings.Contains(v, w.text) {
			t.Errorf("pane %v does not show %q:\n%s", w.pane, w.text, v)
		}
		press(a, "tab")
	}
	if got := statsOf(a).pane; got != paneReview {
		t.Errorf("tab did not wrap round to the first pane, landed on %v", got)
	}

	pressKey(a, keyMsg("tab"))
	pressKey(a, shiftTab())
	if got := statsOf(a).pane; got != paneReview {
		t.Errorf("shift+tab did not step back, landed on %v", got)
	}
	pressKey(a, shiftTab())
	if got := statsOf(a).pane; got != paneLibrary {
		t.Errorf("shift+tab from the first pane should wrap to the last, landed on %v", got)
	}
}

func TestHistoryPaneReadsTheReviewLog(t *testing.T) {
	a := newTestApp(t, 100, 30)
	seedHistory(a, studyDay)
	a.current = ScreenStats
	statsOf(a).pane = paneHistory

	v := stripANSI(view(a))
	for _, want := range []string{
		"94% of 95 reviews",      // 89 of 95 recalled, rounded
		"95 over 19 days",        // the log's own volume and span
		"5 days (best 14)",       // the streak the two missed days cut
		"Practice     0 credits", // nothing was solved, so nothing was credited
	} {
		if !strings.Contains(v, want) {
			t.Errorf("history pane is missing %q:\n%s", want, v)
		}
	}
	// The curve is drawn, and its gap is the two days nothing was answered on.
	if !strings.Contains(v, "by day · gap = nothing reviewed") {
		t.Errorf("history pane does not draw a retention curve:\n%s", v)
	}
	if !strings.ContainsAny(v, string(sparkBlocks)) {
		t.Errorf("history pane has no chart columns:\n%s", v)
	}
}

// TestHistoryPaneSaysWhenThereIsNothingYet covers the first launch: the pane
// has to render before anything has been answered, and say why it is empty
// rather than draw a flat line that would read as a run of failures.
func TestHistoryPaneSaysWhenThereIsNothingYet(t *testing.T) {
	a := newTestApp(t, 100, 30)
	a.current = ScreenStats
	statsOf(a).pane = paneHistory

	v := stripANSI(view(a))
	for _, want := range []string{"Nothing has been reviewed yet.", "No history yet."} {
		if !strings.Contains(v, want) {
			t.Errorf("empty history pane is missing %q:\n%s", want, v)
		}
	}
}

// TestStatsSaysWhenNothingIsBeingSaved is the same promise Home makes: a
// session with no store shows its figures but never implies they will be
// there tomorrow.
func TestStatsSaysWhenNothingIsBeingSaved(t *testing.T) {
	a := newTestApp(t, 100, 30)
	seedHistory(a, studyDay)
	a.current = ScreenStats

	statsOf(a).pane = paneHistory
	if v := stripANSI(view(a)); !strings.Contains(v, "this session only") {
		t.Errorf("history pane does not qualify an unsaved history:\n%s", v)
	}
	statsOf(a).pane = paneReview
	if v := stripANSI(view(a)); !strings.Contains(v, "this session only") {
		t.Errorf("review pane does not qualify unsaved practice progress:\n%s", v)
	}
}

// TestMasteryBandsFollowStability pins the grid's reading of the scheduler: a
// command is as well known as its cards are on average, and the bands sit
// where SPEC's intervals put them.
func TestMasteryBandsFollowStability(t *testing.T) {
	a := newTestApp(t, 100, 30)
	a.clock = func() time.Time { return studyDay }

	cmd, ok := a.Lib.Command("cut")
	if !ok {
		t.Fatal("the dictionary has no cut entry")
	}
	cards := a.Lib.CardsFor(cmd.ID)
	if len(cards) < 2 {
		t.Fatalf("cut has %d cards, this test needs at least 2", len(cards))
	}

	restore := func(stability float64) {
		states := make([]srs.State, 0, len(cards))
		for _, c := range cards {
			states = append(states, srs.State{
				CardID: c.ID, Stability: stability, Difficulty: 5, Reps: 1,
				FirstSeen: studyDay.AddDate(0, 0, -1), LastReview: studyDay.AddDate(0, 0, -1),
				Due: studyDay.AddDate(0, 0, 1),
			})
		}
		a.SRS.Restore(states, nil)
	}

	for _, tc := range []struct {
		stability float64
		want      masteryLevel
	}{
		{2, masteryLearning},
		{familiarStability, masteryFamiliar},
		{strongStability, masteryStrong},
		{strongStability * 4, masteryStrong},
	} {
		restore(tc.stability)
		if got := masteryOf(a, cmd).level; got != tc.want {
			t.Errorf("stability %v graded %v, want %v", tc.stability, got.label(), tc.want.label())
		}
	}

	// An untouched command is a band of its own, and so is one the library has
	// no card for: an absence in the content is not a gap in the learner.
	a.SRS.Restore(nil, nil)
	if got := masteryOf(a, cmd).level; got != masteryUnseen {
		t.Errorf("an unanswered command graded %v, want %v", got.label(), masteryUnseen.label())
	}
	empty := &content.Command{ID: "not-in-the-library", Name: "nil"}
	if got := masteryOf(a, empty); got.level != masteryNothing || got.cards != 0 {
		t.Errorf("a command with no cards graded %v", got.level.label())
	}
}

// TestMasteryCursorOpensTheDictionary covers the grid's one action: a cell
// that is still empty is a command the learner wants to go and read, so enter
// takes them there.
func TestMasteryCursorOpensTheDictionary(t *testing.T) {
	a := newTestApp(t, 100, 30)
	press(a, "4")
	press(a, "tab")
	press(a, "tab")
	if statsOf(a).pane != paneMastery {
		t.Fatalf("expected the mastery pane, got %v", statsOf(a).pane)
	}

	press(a, "down")
	press(a, "right")
	press(a, "right")
	cell, ok := statsOf(a).focused(masteryRows(a))
	if !ok {
		t.Fatal("the grid has no focused cell")
	}
	if !strings.Contains(stripANSI(view(a)), cell.cmd.Name+" · ") {
		t.Errorf("the detail line does not name the focused command %q:\n%s", cell.cmd.Name, view(a))
	}

	pressCmd(a, "enter")
	if a.current != ScreenDictionary {
		t.Fatalf("enter on a cell went to %v, want the dictionary", a.current)
	}
	if v := view(a); !strings.Contains(stripANSI(v), cell.cmd.Summary) {
		t.Errorf("the dictionary did not open %q:\n%s", cell.cmd.Name, v)
	}
}

// TestMasteryCursorStaysInsideTheGrid holds the cursor to the shape of the
// content: the rows are different lengths, and a column past the end of a
// shorter one must land on its last cell rather than off it.
func TestMasteryCursorStaysInsideTheGrid(t *testing.T) {
	a := newTestApp(t, 100, 30)
	a.current = ScreenStats
	s := statsOf(a)
	s.pane = paneMastery

	rows := masteryRows(a)
	for i := 0; i < 40; i++ {
		s.moveMastery(a, keyMsg("right"))
	}
	for i := 0; i < 40; i++ {
		s.moveMastery(a, keyMsg("down"))
	}
	if s.row != len(rows)-1 {
		t.Errorf("cursor row is %d, want the last row %d", s.row, len(rows)-1)
	}
	last := rows[s.row].cells[len(rows[s.row].cells)-1]
	if cell, ok := s.focused(rows); !ok || cell.cmd.ID != last.cmd.ID {
		t.Errorf("cursor rests on %v, want the last cell %q of the row", cell.cmd, last.cmd.Name)
	}

	// Crossing a short category narrows where the cursor is drawn but not
	// where it wants to be: back on a row wide enough it is out at the column
	// it walked to, rather than at the shortest row's end.
	walked := len(rows[0].cells) - 1
	for i, r := range rows {
		if len(r.cells) <= walked {
			continue
		}
		s.row = i
		if got := s.focusedColumn(len(r.cells)); got != walked {
			t.Errorf("on a wide row the cursor is at column %d, want %d", got, walked)
		}
		break
	}

	for i := 0; i < 40; i++ {
		s.moveMastery(a, keyMsg("up"))
		s.moveMastery(a, keyMsg("left"))
	}
	if s.row != 0 || s.col != 0 {
		t.Errorf("cursor came to rest at %d,%d, want 0,0", s.row, s.col)
	}
}

// TestStatsPanesFitTheSmallestTerminal is the 80×24 promise SPEC §7.2 makes,
// checked pane by pane: every box closes inside the frame, so nothing the
// learner came for was cut off by the fit.
func TestStatsPanesFitTheSmallestTerminal(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {120, 40}} {
		a := newTestApp(t, size[0], size[1])
		seedHistory(a, studyDay)
		a.current = ScreenStats
		for pane := paneReview; pane <= paneLibrary; pane++ {
			statsOf(a).pane = pane
			lines := strings.Split(view(a), "\n")
			if len(lines) != size[1] {
				t.Errorf("%dx%d pane %v rendered %d lines", size[0], size[1], pane, len(lines))
			}
			for i, ln := range lines {
				if n := len([]rune(stripANSI(ln))); n > size[0] {
					t.Errorf("%dx%d pane %v line %d is %d cells wide", size[0], size[1], pane, i, n)
				}
			}
			if !strings.Contains(stripANSI(view(a)), "╰") {
				t.Errorf("%dx%d pane %v was cut off before its panel closed:\n%s",
					size[0], size[1], pane, view(a))
			}
		}
	}
}
