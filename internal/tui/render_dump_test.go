package tui

import (
	"os"
	"testing"

	"bash-teacher/internal/content"
)

// TestDumpFrames is a development aid: BT_DUMP=1 go test ./internal/tui -run DumpFrames
func TestDumpFrames(t *testing.T) {
	if os.Getenv("BT_DUMP") == "" {
		t.Skip("set BT_DUMP=1 to print frames")
	}
	a := newTestApp(t, 96, 28)
	for _, s := range []Screen{ScreenHome, ScreenDictionary, ScreenPractice, ScreenFlashcards, ScreenStats} {
		a.current = s
		t.Logf("\n=== %v ===\n%s", s, view(a))
	}

	// A searched, detail-focused entry, scrolled to its related commands.
	b := newTestApp(t, 96, 28)
	press(b, "1")
	typeIn(b, "/", "sort")
	press(b, "enter")
	press(b, "right")
	d := dictOf(b)
	for i, it := range d.items {
		if it.kind == itemRelated {
			d.item = i
			d.scrollToItem()
			break
		}
	}
	t.Logf("\n=== Dictionary (searched, detail focused) ===\n%s", view(b))

	// The practice workspace, with a wrong answer and its diff.
	c := newTestApp(t, 96, 28)
	openExerciseIn(t, c, "top-talkers")
	typeIn(c, "", "cut -d' ' -f1 access.log | sort | uniq -c")
	runNow(t, c)
	t.Logf("\n=== Practice (after a failed run) ===\n%s", view(c))

	// The same exercise solved, in the smallest supported terminal.
	small := newTestApp(t, 80, 24)
	ex := openExerciseIn(t, small, "top-talkers")
	typeIn(small, "", ex.ReferenceSolution)
	runNow(t, small)
	t.Logf("\n=== Practice (passed, 80x24) ===\n%s", view(small))

	// The exercise browser, showing tracks and progress.
	list := newTestApp(t, 96, 28)
	press(list, "2")
	t.Logf("\n=== Practice (browser) ===\n%s", view(list))

	// A review session: a card asked, and the same card graded.
	rev := newTestApp(t, 96, 28)
	press(rev, "3")
	press(rev, "enter")
	t.Logf("\n=== Flashcards (asking) ===\n%s", view(rev))
	card := cardsOf(rev).card()
	typeIn(rev, "", card.Back)
	press(rev, "enter")
	t.Logf("\n=== Flashcards (graded) ===\n%s", view(rev))

	miss := newTestApp(t, 80, 24)
	press(miss, "3")
	press(miss, "enter")
	typeIn(miss, "", "cat nope.txt")
	press(miss, "enter")
	t.Logf("\n=== Flashcards (missed, 80x24) ===\n%s", view(miss))

	ident := newTestApp(t, 96, 28)
	reviewOne(ident, firstCardOfType(t, ident, content.CardIdentify))
	press(ident, "enter")
	t.Logf("\n=== Flashcards (self-graded) ===\n%s", view(ident))
}
