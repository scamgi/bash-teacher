package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"bash-teacher/internal/content"
)

// practiceOf reaches into the Practice sub-model for assertions that would be
// fragile to make against rendered text.
func practiceOf(a *App) *practiceScreen { return a.screens[ScreenPractice].(*practiceScreen) }

// openExerciseIn puts the workspace on a known exercise, the way the
// dictionary's p shortcut does.
func openExerciseIn(t *testing.T, a *App, id string) *content.Exercise {
	t.Helper()
	a.Update(openExerciseMsg{id: id})
	p := practiceOf(a)
	if p.ex == nil || p.ex.ID != id {
		t.Fatalf("failed to open %s", id)
	}
	_ = a.View()
	return p.ex
}

// chord builds a ctrl-modified key press.
func chord(r rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: r, Mod: tea.ModCtrl} }

// TestReferenceSolutionPassesInTheUI is the round trip the whole milestone is
// about: an exercise opened, a pipeline typed, the sandbox run, and the answer
// judged — with nothing mocked but the terminal.
func TestReferenceSolutionPassesInTheUI(t *testing.T) {
	a := newTestApp(t, 100, 30)
	ex := openExerciseIn(t, a, "first-lines")
	typeIn(a, "", ex.ReferenceSolution)
	runNow(t, a)

	v := view(a)
	if !strings.Contains(v, "✓ correct") {
		t.Fatalf("the reference solution should pass:\n%s", v)
	}
	if !practiceOf(a).prog.Passed(ex.ID) {
		t.Error("a passing run should be recorded in progress")
	}
	if !strings.Contains(v, "reference") {
		t.Errorf("a pass should show the reference solution beside the learner's:\n%s", v)
	}
}

// TestWrongAnswerShowsTheDiff checks that a plausible-but-wrong pipeline comes
// back as a diff rather than a bare failure.
func TestWrongAnswerShowsTheDiff(t *testing.T) {
	a := newTestApp(t, 100, 30)
	openExerciseIn(t, a, "first-lines")
	typeIn(a, "", "head -5 access.log")
	runNow(t, a)

	v := view(a)
	if !strings.Contains(v, "output differs") {
		t.Fatalf("a wrong answer should show a diff:\n%s", v)
	}
	if practiceOf(a).prog.Passed("first-lines") {
		t.Error("a failing run must not be recorded as a pass")
	}
}

// TestAnyCorrectPipelinePasses is SPEC §2.2's promise: the exercise checks the
// output, not the wording of the solution.
func TestAnyCorrectPipelinePasses(t *testing.T) {
	a := newTestApp(t, 100, 30)
	openExerciseIn(t, a, "first-lines")
	typeIn(a, "", "sed -n '1,3p' access.log")
	runNow(t, a)
	if v := view(a); !strings.Contains(v, "✓ correct") {
		t.Fatalf("an alternative solution producing the same output should pass:\n%s", v)
	}
}

// TestUnsafeInputIsRefusedWithAnExplanation keeps the static layers visible in
// the UI: a refusal has to say why, not just decline.
func TestUnsafeInputIsRefusedWithAnExplanation(t *testing.T) {
	a := newTestApp(t, 100, 30)
	openExerciseIn(t, a, "first-lines")
	typeIn(a, "", "cat /etc/passwd")
	runNow(t, a)

	v := view(a)
	if !strings.Contains(v, "not run") || !strings.Contains(v, "absolute path") {
		t.Fatalf("an unsafe pipeline should be refused with a reason:\n%s", v)
	}
}

// TestConstraintViolationReadsAsARule separates the exercise's own rules from
// the sandbox's refusals.
func TestConstraintViolationReadsAsARule(t *testing.T) {
	a := newTestApp(t, 110, 34)
	ex := openExerciseIn(t, a, "status-tally")
	if len(ex.Forbid) == 0 {
		t.Fatal("status-tally is expected to forbid a command")
	}
	typeIn(a, "", "cut -d' ' -f9 access.log")
	runNow(t, a)
	if v := view(a); !strings.Contains(v, "rule this exercise sets") {
		t.Fatalf("a forbidden command should be explained as a rule:\n%s", v)
	}
}

// TestHintsAppearOneAtATime covers the tiered hints and the reference solution
// behind ^S.
func TestHintsAppearOneAtATime(t *testing.T) {
	a := newTestApp(t, 100, 34)
	ex := openExerciseIn(t, a, "first-lines")
	if len(ex.Hints) < 2 {
		t.Fatalf("first-lines should have at least two hints, has %d", len(ex.Hints))
	}

	pressKey(a, chord('h'))
	if v := view(a); !strings.Contains(v, "hint 1") || strings.Contains(v, "hint 2") {
		t.Fatalf("^H should reveal exactly one hint:\n%s", v)
	}
	pressKey(a, chord('h'))
	if v := view(a); !strings.Contains(v, "hint 2") {
		t.Fatalf("a second ^H should reveal the next hint:\n%s", v)
	}
	if got := practiceOf(a).prog.state(ex.ID).Hints; got != 2 {
		t.Errorf("hints used should be recorded, got %d", got)
	}

	pressKey(a, chord('s'))
	if v := view(a); !strings.Contains(v, "reference solution") {
		t.Fatalf("^S should reveal the reference solution:\n%s", v)
	}
}

// TestEditorKeepsPrintableKeys is the Capturing() contract: a pipeline may
// contain digits, q, and vim's motion letters.
func TestEditorKeepsPrintableKeys(t *testing.T) {
	a := newTestApp(t, 100, 30)
	openExerciseIn(t, a, "first-lines")
	typeIn(a, "", "head -2 q3 hjkl")
	if a.current != ScreenPractice {
		t.Fatalf("typing into the editor navigated away, now on %v", a.current)
	}
	if got := practiceOf(a).editor.Value(); got != "head -2 q3 hjkl" {
		t.Errorf("editor swallowed keystrokes: %q", got)
	}
}

// TestEditorHistoryRecallsTheLastRun covers the ↑ history SPEC §2.2 asks for.
func TestEditorHistoryRecallsTheLastRun(t *testing.T) {
	a := newTestApp(t, 100, 30)
	openExerciseIn(t, a, "first-lines")
	typeIn(a, "", "head -9 access.log")
	runNow(t, a)
	pressKey(a, tea.KeyPressMsg{Code: 'x', Text: "x"})
	press(a, "up")
	if got := practiceOf(a).editor.Value(); got != "head -9 access.log" {
		t.Errorf("↑ should recall the last run, got %q", got)
	}
}

// TestCompletionFillsInACommand checks that tab completes from the dictionary.
func TestCompletionFillsInACommand(t *testing.T) {
	a := newTestApp(t, 100, 30)
	openExerciseIn(t, a, "first-lines")
	typeIn(a, "", "uni")
	press(a, "tab")
	if got := practiceOf(a).editor.Value(); got != "uniq" {
		// "uni" is a prefix of nothing else in the library.
		t.Errorf("tab should complete a command name, got %q", got)
	}

	b := newTestApp(t, 100, 30)
	openExerciseIn(t, b, "first-lines")
	typeIn(b, "", "sor")
	press(b, "tab")
	if got := practiceOf(b).editor.Value(); got != "sort" {
		t.Errorf("tab should complete sor to sort, got %q", got)
	}
	typeIn(b, "", " -")
	press(b, "tab")
	if got := practiceOf(b).editor.Value(); !strings.HasPrefix(got, "sort -") || got == "sort -" {
		t.Errorf("tab after a dash should offer sort's flags, got %q", got)
	}
}

// TestLookupOpensTheDictionary covers the contextual lookup: ^G on the word
// under the cursor.
func TestLookupOpensTheDictionary(t *testing.T) {
	a := newTestApp(t, 100, 30)
	openExerciseIn(t, a, "first-lines")
	typeIn(a, "", "sort")
	_, cmd := a.Update(chord('g'))
	deliver(a, cmd)
	if a.current != ScreenDictionary {
		t.Fatalf("^G should open the dictionary, still on %v", a.current)
	}
	if got := dictOf(a).current(); got == nil || got.Name != "sort" {
		t.Errorf("lookup landed on %v, want sort", got)
	}
}

// TestLookupOfANonCommandSaysSo keeps the shortcut from silently doing nothing.
func TestLookupOfANonCommandSaysSo(t *testing.T) {
	a := newTestApp(t, 100, 30)
	openExerciseIn(t, a, "first-lines")
	typeIn(a, "", "wibble")
	_, cmd := a.Update(chord('g'))
	deliver(a, cmd)
	if a.current != ScreenPractice {
		t.Errorf("an unknown word should not navigate, went to %v", a.current)
	}
	if !strings.Contains(a.flash, "not in the dictionary") {
		t.Errorf("expected an explanation, got flash %q", a.flash)
	}
}

// TestNextExerciseMovesOnAndClears checks ^N.
func TestNextExerciseMovesOnAndClears(t *testing.T) {
	a := newTestApp(t, 100, 30)
	p := practiceOf(a)
	openExerciseIn(t, a, "first-lines")
	typeIn(a, "", "head -3 access.log")
	before := p.ex.ID

	pressKey(a, chord('n'))
	if p.ex.ID == before {
		t.Fatal("^N should move to another exercise")
	}
	if p.editor.Value() != "" {
		t.Errorf("a new exercise should start with an empty editor, got %q", p.editor.Value())
	}
}

// TestEscapeReturnsToTheBrowser checks the way back out of the workspace, and
// that the global keys come back with it.
func TestEscapeReturnsToTheBrowser(t *testing.T) {
	a := newTestApp(t, 100, 30)
	openExerciseIn(t, a, "first-lines")
	if !practiceOf(a).Capturing() {
		t.Fatal("the workspace should capture text input")
	}
	press(a, "esc")
	if practiceOf(a).mode != modeBrowse {
		t.Fatal("esc should return to the exercise list")
	}
	if practiceOf(a).Capturing() {
		t.Error("the browser must not capture text input")
	}
	press(a, "1")
	if a.current != ScreenDictionary {
		t.Errorf("the global keys should work again in the browser, got %v", a.current)
	}
}

// TestBrowserListsEveryTrack checks the library view names its tracks and
// counts what has been passed.
func TestBrowserListsEveryTrack(t *testing.T) {
	a := newTestApp(t, 100, 40)
	press(a, "2")
	v := view(a)
	if !strings.Contains(v, strings.ToUpper(content.TrackTitles["files-navigation"])) {
		t.Errorf("the browser should show track headings:\n%s", v)
	}
	if !strings.Contains(v, "passed") {
		t.Errorf("the browser should show per-track progress:\n%s", v)
	}
}

// TestTrackUnlocking covers the progression rule: 80% of a track opens the
// next one.
func TestTrackUnlocking(t *testing.T) {
	a := newTestApp(t, 100, 30)
	p := practiceOf(a)
	first, second := a.Lib.Tracks[0], a.Lib.Tracks[1]

	if !p.prog.Unlocked(a.Lib, first.Name) {
		t.Error("the first track should always be open")
	}
	if p.prog.Unlocked(a.Lib, second.Name) {
		t.Error("the second track should start locked")
	}
	need := (len(first.Exercises)*4 + 4) / 5 // ceil(80%)
	for i := 0; i < need; i++ {
		p.prog.state(first.Exercises[i].ID).FirstPassed = a.Now()
	}
	if !p.prog.Unlocked(a.Lib, second.Name) {
		t.Errorf("passing %d of %d should unlock the next track", need, len(first.Exercises))
	}
}

// TestWorkspaceFillsTerminal extends the frame contract to the workspace,
// including the states that add the most lines.
func TestWorkspaceFillsTerminal(t *testing.T) {
	for _, size := range []struct{ w, h int }{{80, 24}, {120, 40}} {
		a := newTestApp(t, size.w, size.h)
		openExerciseIn(t, a, "top-talkers")
		typeIn(a, "", "cut -d' ' -f1 access.log")
		runNow(t, a)
		pressKey(a, chord('h'))
		pressKey(a, chord('s'))
		pressKey(a, chord('b')) // fixture preview open on top of everything else

		lines := strings.Split(view(a), "\n")
		if len(lines) != size.h {
			t.Errorf("%dx%d: rendered %d lines, want %d", size.w, size.h, len(lines), size.h)
		}
		for i, ln := range lines {
			if n := len([]rune(stripANSI(ln))); n > size.w {
				t.Errorf("%dx%d: line %d is %d cells wide", size.w, size.h, i, n)
			}
		}
	}
}
