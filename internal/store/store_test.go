package store_test

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"bash-teacher/internal/srs"
	"bash-teacher/internal/store"
)

func open(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "nested", "progress.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// day is a fixed instant, truncated to the second because that is the
// resolution the store keeps.
var day = time.Date(2026, 3, 14, 9, 30, 0, 0, time.Local)

func TestOpenCreatesTheFileAndItsDirectory(t *testing.T) {
	s := open(t)
	if _, _, err := s.Meta("anything"); err != nil {
		t.Fatalf("query on a fresh database: %v", err)
	}
}

// A second Open of the same file must find the schema already there and leave
// the data alone: migrations are forward-only and run once.
func TestReopenKeepsData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "progress.db")
	first, err := store.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if setErr := first.SetMeta("version", "1.2.3"); setErr != nil {
		t.Fatalf("set meta: %v", setErr)
	}
	if closeErr := first.Close(); closeErr != nil {
		t.Fatalf("close: %v", closeErr)
	}

	second, err := store.Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = second.Close() }()

	v, ok, err := second.Meta("version")
	if err != nil || !ok || v != "1.2.3" {
		t.Fatalf("meta after reopen = %q, %v, %v; want 1.2.3", v, ok, err)
	}
}

func TestCardRoundTrip(t *testing.T) {
	s := open(t)
	want := srs.State{
		CardID:     "cut-fields",
		Stability:  4.5,
		Difficulty: 6.25,
		Due:        day.Add(72 * time.Hour),
		Reps:       3,
		Lapses:     1,
		LastReview: day,
		FirstSeen:  day.Add(-96 * time.Hour),
	}
	if err := s.SaveCard(want); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := s.LoadCards()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("loaded %d cards, want 1", len(got))
	}
	if !got[0].Due.Equal(want.Due) || !got[0].FirstSeen.Equal(want.FirstSeen) {
		t.Errorf("times round-tripped as %v/%v, want %v/%v",
			got[0].Due, got[0].FirstSeen, want.Due, want.FirstSeen)
	}
	got[0].Due, got[0].LastReview, got[0].FirstSeen = want.Due, want.LastReview, want.FirstSeen
	if got[0] != want {
		t.Errorf("card round-tripped as %+v, want %+v", got[0], want)
	}
}

// Saving the same card twice is an update, not a second row: `cards` is the
// current state, and `reviews` is the history.
func TestSaveCardUpserts(t *testing.T) {
	s := open(t)
	st := srs.State{CardID: "sort-numeric", Stability: 1, Reps: 1, Due: day}
	if err := s.SaveCard(st); err != nil {
		t.Fatalf("save: %v", err)
	}
	st.Stability, st.Reps = 9, 2
	if err := s.SaveCard(st); err != nil {
		t.Fatalf("resave: %v", err)
	}
	got, err := s.LoadCards()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("loaded %d rows, want 1", len(got))
	}
	if got[0].Stability != 9 || got[0].Reps != 2 {
		t.Errorf("second save did not replace the first: %+v", got[0])
	}
}

// A card that has never been answered has a zero LastReview, and it has to
// come back as a zero time rather than as the Unix epoch, because Seen and the
// elapsed-days arithmetic both read it.
func TestZeroTimesRoundTripAsZero(t *testing.T) {
	s := open(t)
	if err := s.SaveCard(srs.State{CardID: "never-seen"}); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := s.LoadCards()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !got[0].LastReview.IsZero() || !got[0].FirstSeen.IsZero() || !got[0].Due.IsZero() {
		t.Errorf("zero times came back as %+v", got[0])
	}
}

func TestReviewLogIsAppendOnlyAndOrdered(t *testing.T) {
	s := open(t)
	in := []srs.Review{
		{CardID: "a", At: day, Rating: srs.Good, Elapsed: 1500 * time.Millisecond, Source: srs.SourceReview},
		{CardID: "b", At: day.Add(time.Minute), Rating: srs.Again, Source: srs.SourceReview},
		{CardID: "a", At: day.Add(2 * time.Minute), Rating: srs.Good, Source: srs.SourcePractice},
	}
	for _, r := range in {
		if err := s.AppendReview(r); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	got, err := s.LoadReviews()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(got) != len(in) {
		t.Fatalf("loaded %d reviews, want %d", len(got), len(in))
	}
	for i := range in {
		if got[i].CardID != in[i].CardID || got[i].Rating != in[i].Rating ||
			got[i].Source != in[i].Source || !got[i].At.Equal(in[i].At) {
			t.Errorf("review %d round-tripped as %+v, want %+v", i, got[i], in[i])
		}
	}
	if got[0].Elapsed != 1500*time.Millisecond {
		t.Errorf("elapsed round-tripped as %v, want 1.5s", got[0].Elapsed)
	}
}

func TestExerciseRoundTrip(t *testing.T) {
	s := open(t)
	want := store.Exercise{
		ID:            "count-logins",
		FirstPassed:   day,
		Best:          2400 * time.Millisecond,
		Attempts:      5,
		Hints:         2,
		SolutionShown: true,
	}
	if err := s.SaveExercise(want); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := s.LoadExercises()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("loaded %d exercises, want 1", len(got))
	}
	if !got[0].FirstPassed.Equal(want.FirstPassed) {
		t.Errorf("first pass round-tripped as %v, want %v", got[0].FirstPassed, want.FirstPassed)
	}
	got[0].FirstPassed = want.FirstPassed
	if got[0] != want {
		t.Errorf("exercise round-tripped as %+v, want %+v", got[0], want)
	}
	if !got[0].Passed() {
		t.Error("an exercise with a first-pass time should report Passed")
	}
}

// An exercise that has been attempted but never solved must not read as
// passed: the browser's tick and the track gate both hang off that.
func TestUnsolvedExerciseIsNotPassed(t *testing.T) {
	s := open(t)
	if err := s.SaveExercise(store.Exercise{ID: "hard-one", Attempts: 9}); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := s.LoadExercises()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got[0].Passed() {
		t.Error("an exercise with no first-pass time reports Passed")
	}
}

func TestAttemptsRoundTripPerExercise(t *testing.T) {
	s := open(t)
	in := []store.Attempt{
		{ExerciseID: "count-logins", At: day, Input: "cut -d: -f1 /etc/passwd", Passed: false, Hints: 1, Took: 30 * time.Millisecond},
		{ExerciseID: "count-logins", At: day.Add(time.Minute), Input: "sort names | uniq -c", Passed: true, Hints: 1, Took: 42 * time.Millisecond},
		{ExerciseID: "other", At: day, Input: "wc -l", Passed: true},
	}
	for _, a := range in {
		if err := s.AppendAttempt(a); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	got, err := s.Attempts("count-logins")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("loaded %d attempts for one exercise, want 2", len(got))
	}
	if got[0].Input != in[0].Input || got[0].Passed || !got[1].Passed {
		t.Errorf("attempts round-tripped as %+v", got)
	}
	if got[1].Took != 42*time.Millisecond {
		t.Errorf("duration round-tripped as %v, want 42ms", got[1].Took)
	}
}

// Migrations are forward-only, so a file from a newer build is refused with a
// message rather than opened and half-understood.
func TestNewerSchemaIsRefused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "progress.db")
	s, err := store.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := s.SetMeta("k", "v"); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	bumpSchema(t, path, store.SchemaVersion+1)

	if _, err := store.Open(path); !errors.Is(err, store.ErrNewerSchema) {
		t.Fatalf("opening a newer database returned %v, want ErrNewerSchema", err)
	}
}
