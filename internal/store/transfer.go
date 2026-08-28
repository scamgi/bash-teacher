package store

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
	"time"

	"bash-teacher/internal/srs"
)

// Format is the value of an archive's "format" key. It exists so that a file
// handed to `bt import` by mistake is refused by name rather than by the first
// field that fails to decode.
const Format = "bash-teacher/progress"

// Archive is SPEC §7.3's JSON dump of the progress database: what `bt export`
// writes and `bt import` reads.
//
// The rows mirror the SQL schema of SPEC §8 column for column — same names,
// same units, so timestamps are Unix seconds with 0 for "never" and durations
// are milliseconds. That is deliberate. A dump shaped like the schema is one a
// reader can check against the table definitions, and it makes the round trip
// lossless by construction rather than by care. The envelope around them is
// for the reader instead: Exported is an RFC 3339 stamp because nobody should
// have to decode an epoch to see how old their backup is.
//
// Mirroring the schema also versions the archive by it. A file from an older
// schema decodes with the fields it predates left at zero, which is exactly
// what the migration that added those columns would have given them; a file
// from a newer one is refused, for the same reason ErrNewerSchema exists.
//
// The `meta` table is not carried. It holds facts about one installation
// rather than about the learner, and restoring it into a different machine
// would be importing someone else's bookkeeping.
type Archive struct {
	Format    string             `json:"format"`
	Schema    int                `json:"schema"`
	Generator string             `json:"generator,omitempty"`
	Exported  time.Time          `json:"exported"`
	Cards     []ArchivedCard     `json:"cards"`
	Reviews   []ArchivedReview   `json:"reviews"`
	Exercises []ArchivedExercise `json:"exercises"`
	Attempts  []ArchivedAttempt  `json:"attempts"`
}

// ArchivedCard is one `cards` row.
type ArchivedCard struct {
	ID         string  `json:"id"`
	Stability  float64 `json:"stability"`
	Difficulty float64 `json:"difficulty"`
	Due        int64   `json:"due"`
	Reps       int     `json:"reps"`
	Lapses     int     `json:"lapses"`
	LastReview int64   `json:"last_review"`
	FirstSeen  int64   `json:"first_seen"`
}

// ArchivedReview is one `reviews` row. The row id is left behind: the log is
// append-only and ordered by timestamp, so the number is a fact about one
// database file rather than part of the history it records.
type ArchivedReview struct {
	CardID  string `json:"card_id"`
	At      int64  `json:"ts"`
	Rating  int    `json:"rating"`
	Elapsed int64  `json:"elapsed_ms"`
	Source  string `json:"source"`
}

// ArchivedExercise is one `exercises` row.
type ArchivedExercise struct {
	ID            string `json:"id"`
	FirstPassed   int64  `json:"first_passed"`
	Best          int64  `json:"best_ms"`
	Attempts      int    `json:"attempts"`
	Hints         int    `json:"hints"`
	SolutionShown bool   `json:"solution_shown"`
}

// ArchivedAttempt is one `attempts` row.
type ArchivedAttempt struct {
	ExerciseID string `json:"exercise_id"`
	At         int64  `json:"ts"`
	Input      string `json:"input"`
	Passed     bool   `json:"passed"`
	Hints      int    `json:"hints_used"`
	Took       int64  `json:"ms"`
}

// Counts is how much a database or an archive holds, one number per table.
type Counts struct {
	Cards     int
	Reviews   int
	Exercises int
	Attempts  int
}

// Empty reports whether there is no progress at all.
func (c Counts) Empty() bool { return c == Counts{} }

func (c Counts) String() string {
	return fmt.Sprintf("%d cards, %d reviews, %d exercises, %d attempts",
		c.Cards, c.Reviews, c.Exercises, c.Attempts)
}

// Counts reports how much the database holds.
func (s *Store) Counts() (Counts, error) {
	var c Counts
	// The queries are literals in this file; nothing here is assembled from
	// input.
	for _, q := range []struct {
		query string
		into  *int
	}{
		{`SELECT count(*) FROM cards`, &c.Cards},
		{`SELECT count(*) FROM reviews`, &c.Reviews},
		{`SELECT count(*) FROM exercises`, &c.Exercises},
		{`SELECT count(*) FROM attempts`, &c.Attempts},
	} {
		if err := s.db.QueryRow(q.query).Scan(q.into); err != nil {
			return Counts{}, fmt.Errorf("count progress: %w", err)
		}
	}
	return c, nil
}

// Counts reports how much the archive holds.
func (a *Archive) Counts() Counts {
	return Counts{
		Cards:     len(a.Cards),
		Reviews:   len(a.Reviews),
		Exercises: len(a.Exercises),
		Attempts:  len(a.Attempts),
	}
}

// Snapshot reads the whole database into an Archive.
//
// generator is recorded for provenance — `bt export` passes its own version —
// and is never read back on import: what a file says about the build that
// wrote it is for a human reading the file, not an input to restoring it.
func (s *Store) Snapshot(generator string) (*Archive, error) {
	a := &Archive{
		Format:    Format,
		Schema:    SchemaVersion,
		Generator: generator,
		Exported:  time.Now().UTC().Truncate(time.Second),
	}

	cards, err := s.LoadCards()
	if err != nil {
		return nil, err
	}
	a.Cards = make([]ArchivedCard, 0, len(cards))
	for _, c := range cards {
		a.Cards = append(a.Cards, ArchivedCard{
			ID: c.CardID, Stability: c.Stability, Difficulty: c.Difficulty,
			Due: unix(c.Due), Reps: c.Reps, Lapses: c.Lapses,
			LastReview: unix(c.LastReview), FirstSeen: unix(c.FirstSeen),
		})
	}

	reviews, err := s.LoadReviews()
	if err != nil {
		return nil, err
	}
	a.Reviews = make([]ArchivedReview, 0, len(reviews))
	for _, r := range reviews {
		a.Reviews = append(a.Reviews, ArchivedReview{
			CardID: r.CardID, At: unix(r.At), Rating: int(r.Rating),
			Elapsed: r.Elapsed.Milliseconds(), Source: string(r.Source),
		})
	}

	exercises, err := s.LoadExercises()
	if err != nil {
		return nil, err
	}
	a.Exercises = make([]ArchivedExercise, 0, len(exercises))
	for _, e := range exercises {
		a.Exercises = append(a.Exercises, ArchivedExercise{
			ID: e.ID, FirstPassed: unix(e.FirstPassed), Best: e.Best.Milliseconds(),
			Attempts: e.Attempts, Hints: e.Hints, SolutionShown: e.SolutionShown,
		})
	}

	attempts, err := s.LoadAttempts()
	if err != nil {
		return nil, err
	}
	a.Attempts = make([]ArchivedAttempt, 0, len(attempts))
	for _, at := range attempts {
		a.Attempts = append(a.Attempts, ArchivedAttempt{
			ExerciseID: at.ExerciseID, At: unix(at.At), Input: at.Input,
			Passed: at.Passed, Hints: at.Hints, Took: at.Took.Milliseconds(),
		})
	}
	return a, nil
}

// Write encodes the archive as indented JSON. It is indented because a backup
// nobody can read is one nobody checks, and because a diff between two of them
// is worth having.
func (a *Archive) Write(w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(a); err != nil {
		return fmt.Errorf("write archive: %w", err)
	}
	return nil
}

// ErrNewerArchive is returned when the archive came from a build with a newer
// schema. Migrations are forward-only and an archive is versioned by the
// schema it mirrors, so, as with a database file from the future, there is
// nothing to do but say so.
var ErrNewerArchive = errors.New("progress archive was written by a newer bash-teacher")

// ErrNotEmpty is returned when Restore is asked to replace progress that is
// already stored and was not told to.
var ErrNotEmpty = errors.New("the progress database already holds progress")

// ArchiveError lists everything wrong with an archive. As with the content
// linter and the settings file, every problem is reported at once: a file
// fixed one complaint at a time takes as many runs as it has faults.
type ArchiveError struct {
	Problems []string
}

func (e *ArchiveError) Error() string {
	return fmt.Sprintf("%d problem(s) in the progress archive:\n  %s",
		len(e.Problems), strings.Join(e.Problems, "\n  "))
}

// ReadArchive decodes an archive and checks it over before any of it reaches
// the database.
//
// The envelope is read first, from a permissive pass, so that a file which is
// not an export at all is told so by name rather than by whichever of its keys
// happened to decode badly. The rows are then read strictly: unknown fields
// are refused rather than dropped, because the only thing that writes this
// format is `bt export`, so a key nothing recognises is a hand-edit that has
// gone wrong, and quietly ignoring it would restore something other than what
// the file says.
func ReadArchive(r io.Reader) (*Archive, error) {
	// An archive is a few hundred kilobytes at most — the whole point is that
	// it fits in a backup — so reading it twice costs nothing worth saving.
	raw, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("read archive: %w", err)
	}
	var envelope struct {
		Format string `json:"format"`
		Schema int    `json:"schema"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("read archive: %w", err)
	}
	if envelope.Format != Format {
		return nil, fmt.Errorf("not a bash-teacher progress archive: format is %q, want %q",
			envelope.Format, Format)
	}
	if envelope.Schema > SchemaVersion {
		return nil, fmt.Errorf("%w (archive schema %d, this build knows %d)",
			ErrNewerArchive, envelope.Schema, SchemaVersion)
	}

	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var a Archive
	if err := dec.Decode(&a); err != nil {
		return nil, fmt.Errorf("read archive: %w", err)
	}
	if err := a.validate(); err != nil {
		return nil, err
	}
	return &a, nil
}

// validate rejects what the tables cannot hold and what the scheduler cannot
// read back. It checks shape, not history: an archive naming a card this
// build's content library has never heard of is perfectly well formed, and
// whether that matters is a question for whoever has the library to hand.
func (a *Archive) validate() error {
	var p []string
	note := func(format string, args ...any) { p = append(p, fmt.Sprintf(format, args...)) }

	seenCard := map[string]bool{}
	for i, c := range a.Cards {
		switch {
		case c.ID == "":
			note("cards[%d]: no id", i)
		case seenCard[c.ID]:
			note("cards[%d]: %s appears twice", i, c.ID)
		}
		seenCard[c.ID] = true
		if !finite(c.Stability) || !finite(c.Difficulty) {
			note("cards[%d]: %s has a stability or difficulty that is not a number", i, c.ID)
		}
		if c.Reps < 0 || c.Lapses < 0 {
			note("cards[%d]: %s has a negative rep or lapse count", i, c.ID)
		}
	}
	for i, r := range a.Reviews {
		if r.CardID == "" {
			note("reviews[%d]: no card id", i)
		}
		if !srs.Rating(r.Rating).Valid() {
			note("reviews[%d]: rating %d is not one of 1–4", i, r.Rating)
		}
		if srs.Source(r.Source) != srs.SourceReview && srs.Source(r.Source) != srs.SourcePractice {
			note("reviews[%d]: source %q is neither %q nor %q",
				i, r.Source, srs.SourceReview, srs.SourcePractice)
		}
	}
	seenExercise := map[string]bool{}
	for i, e := range a.Exercises {
		switch {
		case e.ID == "":
			note("exercises[%d]: no id", i)
		case seenExercise[e.ID]:
			note("exercises[%d]: %s appears twice", i, e.ID)
		}
		seenExercise[e.ID] = true
		if e.Attempts < 0 || e.Hints < 0 {
			note("exercises[%d]: %s has a negative attempt or hint count", i, e.ID)
		}
	}
	for i, at := range a.Attempts {
		if at.ExerciseID == "" {
			note("attempts[%d]: no exercise id", i)
		}
	}
	if len(p) > 0 {
		return &ArchiveError{Problems: p}
	}
	return nil
}

func finite(f float64) bool { return !math.IsNaN(f) && !math.IsInf(f, 0) }

// Restore replaces the four progress tables with the archive's contents and
// reports what it wrote. It is a restore, not a merge: what the database holds
// afterwards is what the export found, which is the only reading under which a
// backup means anything.
//
// Because that throws away whatever was there, a database that already holds
// progress is refused with ErrNotEmpty unless force says otherwise. The whole
// replacement runs in one transaction, so an archive that fails halfway leaves
// the learner's history exactly as it was.
func (s *Store) Restore(a *Archive, force bool) (Counts, error) {
	have, err := s.Counts()
	if err != nil {
		return Counts{}, err
	}
	if !have.Empty() && !force {
		return Counts{}, fmt.Errorf("%w (%s)", ErrNotEmpty, have)
	}

	tx, err := s.db.Begin()
	if err != nil {
		return Counts{}, err
	}
	if err := replace(tx, a); err != nil {
		return Counts{}, errors.Join(err, tx.Rollback())
	}
	if err := tx.Commit(); err != nil {
		return Counts{}, fmt.Errorf("import: %w", err)
	}
	return a.Counts(), nil
}

// replace does the work of Restore inside its transaction.
func replace(tx *sql.Tx, a *Archive) error {
	for _, stmt := range []string{
		`DELETE FROM cards`, `DELETE FROM reviews`,
		`DELETE FROM exercises`, `DELETE FROM attempts`,
	} {
		if _, err := tx.Exec(stmt); err != nil {
			return fmt.Errorf("clear progress: %w", err)
		}
	}

	if err := insertAll(tx,
		`INSERT INTO cards (id, stability, difficulty, due, reps, lapses, last_review, first_seen)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		len(a.Cards), func(i int) []any {
			c := a.Cards[i]
			return []any{c.ID, c.Stability, c.Difficulty, c.Due,
				c.Reps, c.Lapses, c.LastReview, c.FirstSeen}
		}); err != nil {
		return fmt.Errorf("import cards: %w", err)
	}
	if err := insertAll(tx,
		`INSERT INTO reviews (card_id, ts, rating, elapsed_ms, source) VALUES (?, ?, ?, ?, ?)`,
		len(a.Reviews), func(i int) []any {
			r := a.Reviews[i]
			return []any{r.CardID, r.At, r.Rating, r.Elapsed, r.Source}
		}); err != nil {
		return fmt.Errorf("import reviews: %w", err)
	}
	if err := insertAll(tx,
		`INSERT INTO exercises (id, first_passed, best_ms, attempts, hints, solution_shown)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		len(a.Exercises), func(i int) []any {
			e := a.Exercises[i]
			return []any{e.ID, e.FirstPassed, e.Best, e.Attempts, e.Hints, e.SolutionShown}
		}); err != nil {
		return fmt.Errorf("import exercises: %w", err)
	}
	if err := insertAll(tx,
		`INSERT INTO attempts (exercise_id, ts, input, passed, hints_used, ms)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		len(a.Attempts), func(i int) []any {
			at := a.Attempts[i]
			return []any{at.ExerciseID, at.At, at.Input, at.Passed, at.Hints, at.Took}
		}); err != nil {
		return fmt.Errorf("import attempts: %w", err)
	}
	return nil
}

// insertAll runs one prepared statement over n rows, asking args for each
// row's values in turn.
func insertAll(tx *sql.Tx, query string, n int, args func(i int) []any) error {
	if n == 0 {
		return nil
	}
	st, err := tx.Prepare(query)
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()
	for i := range n {
		if _, err := st.Exec(args(i)...); err != nil {
			return fmt.Errorf("row %d: %w", i, err)
		}
	}
	return nil
}
