package store_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"bash-teacher/internal/srs"
	"bash-teacher/internal/store"
)

// fill writes one row into each of the four progress tables, with values
// chosen so that a field dropped on the way through an archive shows up as a
// difference rather than as a zero that happened to match.
func fill(t *testing.T, s *store.Store) {
	t.Helper()
	if err := s.SaveCard(srs.State{
		CardID:     "cut-fields",
		Stability:  4.5,
		Difficulty: 6.25,
		Due:        day.Add(72 * time.Hour),
		Reps:       3,
		Lapses:     1,
		LastReview: day,
		FirstSeen:  day.Add(-96 * time.Hour),
	}); err != nil {
		t.Fatalf("save card: %v", err)
	}
	if err := s.AppendReview(srs.Review{
		CardID:  "cut-fields",
		At:      day,
		Rating:  srs.Good,
		Elapsed: 4200 * time.Millisecond,
		Source:  srs.SourceReview,
	}); err != nil {
		t.Fatalf("append review: %v", err)
	}
	if err := s.AppendReview(srs.Review{
		CardID:  "cut-fields",
		At:      day.Add(time.Minute),
		Rating:  srs.Hard,
		Elapsed: 0,
		Source:  srs.SourcePractice,
	}); err != nil {
		t.Fatalf("append review: %v", err)
	}
	if err := s.SaveExercise(store.Exercise{
		ID:            "count-users-by-shell",
		FirstPassed:   day,
		Best:          9 * time.Second,
		Attempts:      4,
		Hints:         2,
		SolutionShown: true,
	}); err != nil {
		t.Fatalf("save exercise: %v", err)
	}
	if err := s.AppendAttempt(store.Attempt{
		ExerciseID: "count-users-by-shell",
		At:         day,
		Input:      "cut -d: -f7 /etc/passwd | sort | uniq -c",
		Passed:     true,
		Hints:      2,
		Took:       9 * time.Second,
	}); err != nil {
		t.Fatalf("append attempt: %v", err)
	}
}

// dump snapshots a store and encodes it, which is what `bt export` does.
func dump(t *testing.T, s *store.Store) []byte {
	t.Helper()
	a, err := s.Snapshot("bash-teacher test")
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	var buf bytes.Buffer
	if err := a.Write(&buf); err != nil {
		t.Fatalf("write: %v", err)
	}
	return buf.Bytes()
}

// load reads an archive and restores it, which is what `bt import` does.
func load(t *testing.T, s *store.Store, encoded []byte, force bool) store.Counts {
	t.Helper()
	a, err := store.ReadArchive(bytes.NewReader(encoded))
	if err != nil {
		t.Fatalf("read archive: %v", err)
	}
	c, err := s.Restore(a, force)
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	return c
}

// The whole point of the format: everything that went in comes back out. The
// comparison is against what the store's own loaders return rather than
// against the archive, because what has to survive is the state the screens
// read, not the shape of the file in between.
func TestExportImportRoundTrip(t *testing.T) {
	src := open(t)
	fill(t, src)

	wantCards, err := src.LoadCards()
	if err != nil {
		t.Fatalf("load cards: %v", err)
	}
	wantReviews, err := src.LoadReviews()
	if err != nil {
		t.Fatalf("load reviews: %v", err)
	}
	wantExercises, err := src.LoadExercises()
	if err != nil {
		t.Fatalf("load exercises: %v", err)
	}
	wantAttempts, err := src.LoadAttempts()
	if err != nil {
		t.Fatalf("load attempts: %v", err)
	}

	dst := open(t)
	got := load(t, dst, dump(t, src), false)
	if want := (store.Counts{Cards: 1, Reviews: 2, Exercises: 1, Attempts: 1}); got != want {
		t.Fatalf("imported %v; want %v", got, want)
	}

	gotCards, err := dst.LoadCards()
	if err != nil {
		t.Fatalf("load cards: %v", err)
	}
	if len(gotCards) != len(wantCards) || gotCards[0] != wantCards[0] {
		t.Errorf("cards after import = %+v; want %+v", gotCards, wantCards)
	}
	gotReviews, err := dst.LoadReviews()
	if err != nil {
		t.Fatalf("load reviews: %v", err)
	}
	if len(gotReviews) != len(wantReviews) {
		t.Fatalf("reviews after import = %d; want %d", len(gotReviews), len(wantReviews))
	}
	for i := range gotReviews {
		if gotReviews[i] != wantReviews[i] {
			t.Errorf("review %d = %+v; want %+v", i, gotReviews[i], wantReviews[i])
		}
	}
	gotExercises, err := dst.LoadExercises()
	if err != nil {
		t.Fatalf("load exercises: %v", err)
	}
	if len(gotExercises) != 1 || gotExercises[0] != wantExercises[0] {
		t.Errorf("exercises after import = %+v; want %+v", gotExercises, wantExercises)
	}
	gotAttempts, err := dst.LoadAttempts()
	if err != nil {
		t.Fatalf("load attempts: %v", err)
	}
	if len(gotAttempts) != 1 || gotAttempts[0] != wantAttempts[0] {
		t.Errorf("attempts after import = %+v; want %+v", gotAttempts, wantAttempts)
	}
}

// A restore is a replacement, so the rows the archive does not mention must be
// gone afterwards. Anything less would make an import a merge by accident.
func TestImportReplacesWhatWasThere(t *testing.T) {
	src := open(t)
	fill(t, src)
	encoded := dump(t, src)

	dst := open(t)
	if err := dst.SaveCard(srs.State{CardID: "somebody-elses-card", Reps: 9, Due: day}); err != nil {
		t.Fatalf("save card: %v", err)
	}
	if err := dst.SaveExercise(store.Exercise{ID: "somebody-elses-exercise", Attempts: 3}); err != nil {
		t.Fatalf("save exercise: %v", err)
	}
	load(t, dst, encoded, true)

	cards, err := dst.LoadCards()
	if err != nil {
		t.Fatalf("load cards: %v", err)
	}
	if len(cards) != 1 || cards[0].CardID != "cut-fields" {
		t.Errorf("cards after import = %+v; want only the imported one", cards)
	}
	exercises, err := dst.LoadExercises()
	if err != nil {
		t.Fatalf("load exercises: %v", err)
	}
	if len(exercises) != 1 || exercises[0].ID != "count-users-by-shell" {
		t.Errorf("exercises after import = %+v; want only the imported one", exercises)
	}
}

// Replacing a learner's history is the one destructive thing `bt` can do, so
// it takes an explicit --force and the refusal says what would have been lost.
func TestImportRefusesToClobberWithoutForce(t *testing.T) {
	src := open(t)
	fill(t, src)
	encoded := dump(t, src)

	dst := open(t)
	fill(t, dst)

	a, err := store.ReadArchive(bytes.NewReader(encoded))
	if err != nil {
		t.Fatalf("read archive: %v", err)
	}
	_, err = dst.Restore(a, false)
	if !errors.Is(err, store.ErrNotEmpty) {
		t.Fatalf("restore into a full database = %v; want ErrNotEmpty", err)
	}
	if !strings.Contains(err.Error(), "1 cards, 2 reviews") {
		t.Errorf("refusal %q does not say what is already stored", err)
	}
}

// An empty database takes an import without being asked twice: there is
// nothing to lose, and making a fresh machine pass --force would train the
// habit of passing it.
func TestImportIntoAnEmptyDatabaseNeedsNoForce(t *testing.T) {
	src := open(t)
	fill(t, src)
	dst := open(t)
	if got := load(t, dst, dump(t, src), false); got.Cards != 1 {
		t.Fatalf("imported %v; want the card", got)
	}
}

// A file the import cannot finish must leave the database exactly as it was:
// the replacement is one transaction, and half a history is worse than the old
// one. The forged duplicate is a row the validator would normally catch, which
// is why it is planted after validation, straight into the encoded file.
func TestFailedImportLeavesProgressAlone(t *testing.T) {
	src := open(t)
	fill(t, src)

	a, err := store.ReadArchive(bytes.NewReader(dump(t, src)))
	if err != nil {
		t.Fatalf("read archive: %v", err)
	}
	a.Cards = append(a.Cards, a.Cards[0]) // the primary key will refuse this

	dst := open(t)
	if saveErr := dst.SaveCard(srs.State{CardID: "kept", Reps: 2, Due: day}); saveErr != nil {
		t.Fatalf("save card: %v", saveErr)
	}
	if _, err = dst.Restore(a, true); err == nil {
		t.Fatal("restore of a duplicated primary key succeeded; want an error")
	}
	cards, err := dst.LoadCards()
	if err != nil {
		t.Fatalf("load cards: %v", err)
	}
	if len(cards) != 1 || cards[0].CardID != "kept" {
		t.Errorf("cards after a failed import = %+v; want the original one untouched", cards)
	}
}

// The archive mirrors the SQL schema, so it is versioned by it: a file from a
// build that has migrated further is refused rather than half-read, the same
// way a database file from the future is.
func TestImportRefusesANewerArchive(t *testing.T) {
	src := open(t)
	fill(t, src)
	encoded := bytes.Replace(dump(t, src),
		[]byte(`"schema": 1,`), []byte(`"schema": 99,`), 1)

	_, err := store.ReadArchive(bytes.NewReader(encoded))
	if !errors.Is(err, store.ErrNewerArchive) {
		t.Fatalf("read of a schema-99 archive = %v; want ErrNewerArchive", err)
	}
}

func TestImportRefusesAFileThatIsNotAnArchive(t *testing.T) {
	for _, tc := range []struct {
		name, body, want string
	}{
		{"not json", "cut -d: -f1 /etc/passwd\n", "read archive"},
		{"some other json", `{"hello": "world"}`, "not a bash-teacher progress archive"},
		{"a key nothing recognises", `{"format": "bash-teacher/progress", "schema": 1, "cardz": []}`, "read archive"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := store.ReadArchive(strings.NewReader(tc.body))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("read = %v; want an error mentioning %q", err, tc.want)
			}
		})
	}
}

// Validation reports everything wrong at once, like the content linter and the
// settings file, and it covers the values that would poison the scheduler
// rather than merely the ones SQLite would notice.
func TestArchiveValidationReportsEveryProblem(t *testing.T) {
	body := `{
	  "format": "bash-teacher/progress",
	  "schema": 1,
	  "exported": "2026-03-14T09:30:00Z",
	  "cards": [
	    {"id": "", "stability": 1, "difficulty": 5, "due": 0, "reps": 0, "lapses": 0, "last_review": 0, "first_seen": 0},
	    {"id": "dup", "stability": 1, "difficulty": 5, "due": 0, "reps": -3, "lapses": 0, "last_review": 0, "first_seen": 0},
	    {"id": "dup", "stability": 1, "difficulty": 5, "due": 0, "reps": 0, "lapses": 0, "last_review": 0, "first_seen": 0}
	  ],
	  "reviews": [
	    {"card_id": "dup", "ts": 1, "rating": 7, "elapsed_ms": 0, "source": "review"},
	    {"card_id": "dup", "ts": 1, "rating": 3, "elapsed_ms": 0, "source": "telepathy"}
	  ],
	  "exercises": [{"id": "", "first_passed": 0, "best_ms": 0, "attempts": 0, "hints": 0, "solution_shown": false}],
	  "attempts": [{"exercise_id": "", "ts": 1, "input": "ls", "passed": false, "hints_used": 0, "ms": 0}]
	}`
	_, err := store.ReadArchive(strings.NewReader(body))
	var ae *store.ArchiveError
	if !errors.As(err, &ae) {
		t.Fatalf("read = %v; want an *store.ArchiveError", err)
	}
	for _, want := range []string{
		"cards[0]: no id",
		"cards[2]: dup appears twice",
		"cards[1]: dup has a negative rep or lapse count",
		"reviews[0]: rating 7 is not one of 1–4",
		`reviews[1]: source "telepathy"`,
		"exercises[0]: no id",
		"attempts[0]: no exercise id",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("validation did not report %q; it said:\n%s", want, err)
		}
	}
}

// A stability of NaN survives JSON only as a literal the decoder rejects, but
// an infinity can be written by hand, and either would make every interval the
// scheduler computes from it meaningless.
func TestArchiveRefusesNumbersThatAreNotNumbers(t *testing.T) {
	src := open(t)
	fill(t, src)
	a, err := store.ReadArchive(bytes.NewReader(dump(t, src)))
	if err != nil {
		t.Fatalf("read archive: %v", err)
	}
	a.Cards[0].Stability = math.Inf(1)

	var buf bytes.Buffer
	if err := a.Write(&buf); err == nil {
		t.Fatal("encoding an infinite stability succeeded; want an error")
	}
}

// The envelope is what tells a reader, and a future build, what they are
// holding. It is checked here because nothing else in the round trip would
// notice if it stopped being written.
func TestExportedEnvelopeNamesItself(t *testing.T) {
	s := open(t)
	fill(t, s)
	var envelope struct {
		Format    string    `json:"format"`
		Schema    int       `json:"schema"`
		Generator string    `json:"generator"`
		Exported  time.Time `json:"exported"`
	}
	if err := json.Unmarshal(dump(t, s), &envelope); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if envelope.Format != store.Format {
		t.Errorf("format = %q; want %q", envelope.Format, store.Format)
	}
	if envelope.Schema != store.SchemaVersion {
		t.Errorf("schema = %d; want %d", envelope.Schema, store.SchemaVersion)
	}
	if envelope.Generator != "bash-teacher test" {
		t.Errorf("generator = %q; want the string Snapshot was given", envelope.Generator)
	}
	if envelope.Exported.IsZero() {
		t.Error("exported stamp is zero")
	}
}

// An empty database exports an archive that imports back into an empty
// database, rather than a file that cannot be read at all.
func TestExportOfAnEmptyDatabase(t *testing.T) {
	src := open(t)
	dst := open(t)
	if got := load(t, dst, dump(t, src), false); !got.Empty() {
		t.Fatalf("imported %v from an empty database; want nothing", got)
	}
}
