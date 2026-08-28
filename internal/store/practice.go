package store

import (
	"fmt"
	"time"
)

// Exercise is what the store remembers about one exercise: SPEC §8's
// `exercises` row. It is a summary of the attempts, kept alongside them so
// that opening the library does not mean scanning the whole log.
type Exercise struct {
	ID string
	// FirstPassed is when the exercise was first solved, zero if never.
	FirstPassed time.Time
	// Best is the shortest run that passed, zero if never solved.
	Best time.Duration
	// Attempts counts every run, passing or not.
	Attempts int
	// Hints is how many hints have been uncovered, and SolutionShown
	// whether the reference solution has been read. Both are restored when
	// the exercise is reopened: a hint already spent should not be hidden
	// again on the next launch, and Stats tells a solved exercise from a
	// solved-with-help one.
	Hints         int
	SolutionShown bool
}

// Passed reports whether the exercise has ever been solved.
func (e Exercise) Passed() bool { return !e.FirstPassed.IsZero() }

// SaveExercise writes one exercise's summary.
func (s *Store) SaveExercise(e Exercise) error {
	_, err := s.db.Exec(
		`INSERT INTO exercises (id, first_passed, best_ms, attempts, hints, solution_shown)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
			first_passed   = excluded.first_passed,
			best_ms        = excluded.best_ms,
			attempts       = excluded.attempts,
			hints          = excluded.hints,
			solution_shown = excluded.solution_shown`,
		e.ID, unix(e.FirstPassed), e.Best.Milliseconds(), e.Attempts, e.Hints, e.SolutionShown)
	if err != nil {
		return fmt.Errorf("save exercise %s: %w", e.ID, err)
	}
	return nil
}

// LoadExercises reads every exercise summary.
func (s *Store) LoadExercises() ([]Exercise, error) {
	rows, err := s.db.Query(
		`SELECT id, first_passed, best_ms, attempts, hints, solution_shown
		 FROM exercises ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("load exercises: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []Exercise
	for rows.Next() {
		var e Exercise
		var passed, best int64
		if err := rows.Scan(&e.ID, &passed, &best, &e.Attempts, &e.Hints, &e.SolutionShown); err != nil {
			return nil, fmt.Errorf("load exercises: %w", err)
		}
		e.FirstPassed, e.Best = at(passed), time.Duration(best)*time.Millisecond
		out = append(out, e)
	}
	return out, rows.Err()
}

// Attempt is one run of a learner's pipeline: SPEC §8's `attempts` row, and
// the raw record everything else about practice can be rebuilt from.
type Attempt struct {
	ExerciseID string
	At         time.Time
	Input      string
	Passed     bool
	Hints      int
	Took       time.Duration
}

// AppendAttempt logs one run. Like the review log it is append-only.
func (s *Store) AppendAttempt(a Attempt) error {
	_, err := s.db.Exec(
		`INSERT INTO attempts (exercise_id, ts, input, passed, hints_used, ms)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		a.ExerciseID, unix(a.At), a.Input, a.Passed, a.Hints, a.Took.Milliseconds())
	if err != nil {
		return fmt.Errorf("log attempt at %s: %w", a.ExerciseID, err)
	}
	return nil
}

// Attempts reads the log for one exercise, oldest first.
func (s *Store) Attempts(exerciseID string) ([]Attempt, error) {
	rows, err := s.db.Query(
		`SELECT exercise_id, ts, input, passed, hints_used, ms
		 FROM attempts WHERE exercise_id = ? ORDER BY ts, id`, exerciseID)
	if err != nil {
		return nil, fmt.Errorf("load attempts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []Attempt
	for rows.Next() {
		var a Attempt
		var ts, ms int64
		if err := rows.Scan(&a.ExerciseID, &ts, &a.Input, &a.Passed, &a.Hints, &ms); err != nil {
			return nil, fmt.Errorf("load attempts: %w", err)
		}
		a.At, a.Took = at(ts), time.Duration(ms)*time.Millisecond
		out = append(out, a)
	}
	return out, rows.Err()
}
