package tui

import (
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	embedded "bash-teacher/content"
	"bash-teacher/internal/content"
	"bash-teacher/internal/runner"
	"bash-teacher/internal/srs"
	"bash-teacher/internal/store"
	"bash-teacher/internal/theme"
)

// newStoredApp builds the root model over a real progress database, which is
// what a launched `bt` does.
func newStoredApp(t *testing.T, path string, w, h int) *App {
	t.Helper()
	lib, err := content.Load(embedded.FS)
	if err != nil {
		t.Fatalf("load content: %v", err)
	}
	db, err := store.Open(path)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	a := New(lib, theme.Resolve(theme.None), runner.New(lib), "test", ScreenHome, WithStore(db))
	a.Update(tea.WindowSizeMsg{Width: w, Height: h})
	if err := a.StoreError(); err != nil {
		t.Fatalf("store failed during startup: %v", err)
	}
	return a
}

// TestProgressSurvivesRestart is the round trip the store exists for: answer a
// card and solve an exercise in one process, and find both waiting in the
// next. It goes through the key flow rather than the store's API, because what
// has to survive is what the screens actually record.
func TestProgressSurvivesRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "progress.db")

	first := newStoredApp(t, path, 100, 30)
	card := firstCardOfType(t, first, content.CardRecall)
	f := reviewOne(first, card)
	typeIn(first, "", card.Back)
	press(first, "enter")
	if f.phase != phaseGrade {
		t.Fatalf("the card did not reach the rating step")
	}
	press(first, "g")

	ex := openExerciseIn(t, first, "first-lines")
	typeIn(first, "", ex.ReferenceSolution)
	runNow(t, first)
	if !practiceOf(first).prog.Passed(ex.ID) {
		t.Fatal("the reference solution did not pass, so there is nothing to persist")
	}
	if err := first.Store.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	second := newStoredApp(t, path, 100, 30)
	st, ok := second.SRS.State(card.ID)
	if !ok || !st.Seen() {
		t.Errorf("card %s came back unseen after a restart", card.ID)
	}
	if st.Due.IsZero() {
		t.Errorf("card %s came back with no due date", card.ID)
	}
	if !practiceOf(second).prog.Passed(ex.ID) {
		t.Errorf("exercise %s came back unsolved after a restart", ex.ID)
	}
	if len(second.SRS.Log()) == 0 {
		t.Error("the review log came back empty")
	}
}

// The scheduler is what decides; the store only writes down what it decided.
// A state that came back different from the one that was saved would mean the
// next interval is computed from something the learner never earned.
func TestRestoredStateMatchesWhatWasScheduled(t *testing.T) {
	path := filepath.Join(t.TempDir(), "progress.db")

	first := newStoredApp(t, path, 100, 30)
	card := firstCardOfType(t, first, content.CardRecall)
	first.GradeCard(card.ID, srs.Hard, 0)
	want, _ := first.SRS.State(card.ID)
	if err := first.Store.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	second := newStoredApp(t, path, 100, 30)
	got, ok := second.SRS.State(card.ID)
	if !ok {
		t.Fatalf("card %s has no state after a restart", card.ID)
	}
	if got.Stability != want.Stability || got.Difficulty != want.Difficulty ||
		got.Reps != want.Reps || got.Lapses != want.Lapses {
		t.Errorf("restored %+v, scheduled %+v", got, want)
	}
	// Due is compared in whole seconds, which is the resolution the store
	// keeps: the scheduler works in fractions of a day, so nothing below a
	// second was ever meaningful.
	if got.Due.Unix() != want.Due.Unix() {
		t.Errorf("restored due %v, scheduled %v", got.Due, want.Due)
	}
}

// Hints already spent are part of what an exercise remembers: uncovering one,
// leaving, and coming back must not hide it again.
func TestHintsSurviveRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "progress.db")

	first := newStoredApp(t, path, 100, 30)
	openExerciseIn(t, first, "first-lines")
	pressKey(first, chord('h'))
	pressKey(first, chord('h'))
	if got := practiceOf(first).hints; got != 2 {
		t.Fatalf("uncovered %d hints, want 2", got)
	}
	if err := first.Store.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	second := newStoredApp(t, path, 100, 30)
	openExerciseIn(t, second, "first-lines")
	if got := practiceOf(second).hints; got != 2 {
		t.Errorf("after a restart the workspace shows %d hints, want 2", got)
	}
}

// Every run is logged, failures included: the attempt log is the raw record
// SPEC §8 says everything else can be rebuilt from.
func TestFailedAttemptsAreLogged(t *testing.T) {
	path := filepath.Join(t.TempDir(), "progress.db")
	a := newStoredApp(t, path, 100, 30)

	openExerciseIn(t, a, "first-lines")
	typeIn(a, "", "head -5 access.log")
	runNow(t, a)

	got, err := a.Store.Attempts("first-lines")
	if err != nil {
		t.Fatalf("read attempts: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("logged %d attempts, want 1", len(got))
	}
	if got[0].Passed {
		t.Error("a wrong pipeline was logged as a pass")
	}
	if got[0].Input != "head -5 access.log" {
		t.Errorf("logged input %q, want the pipeline that was typed", got[0].Input)
	}
}

// Without a store the app behaves as it did before M6 and says so, rather than
// showing a streak that would not survive the process.
func TestNoStoreSaysSo(t *testing.T) {
	a := newTestApp(t, 100, 30)
	if a.Persisting() {
		t.Fatal("an app with no store reports that it is persisting")
	}
	press(a, "4")
	if v := stripANSI(view(a)); !strings.Contains(v, "no progress store") {
		t.Errorf("Stats should say nothing is being saved:\n%s", v)
	}
}

// A store attached and working must not carry the disclaimer.
func TestStoreReportsWhereItSaves(t *testing.T) {
	path := filepath.Join(t.TempDir(), "progress.db")
	a := newStoredApp(t, path, 100, 30)
	press(a, "4")
	v := stripANSI(view(a))
	if strings.Contains(v, "no progress store") {
		t.Errorf("Stats disclaims a store it has:\n%s", v)
	}
	if !strings.Contains(v, "Saved to") {
		t.Errorf("Stats should name where progress is saved:\n%s", v)
	}
}
