package store

import (
	"fmt"
	"time"

	"bash-teacher/internal/srs"
)

// SaveCard writes one card's scheduling record. The scheduler hands back the
// state it just decided on, so this is an upsert of a value that was already
// computed rather than a place where scheduling happens.
func (s *Store) SaveCard(st srs.State) error {
	_, err := s.db.Exec(
		`INSERT INTO cards (id, stability, difficulty, due, reps, lapses, last_review, first_seen)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
			stability   = excluded.stability,
			difficulty  = excluded.difficulty,
			due         = excluded.due,
			reps        = excluded.reps,
			lapses      = excluded.lapses,
			last_review = excluded.last_review,
			first_seen  = excluded.first_seen`,
		st.CardID, st.Stability, st.Difficulty, unix(st.Due),
		st.Reps, st.Lapses, unix(st.LastReview), unix(st.FirstSeen))
	if err != nil {
		return fmt.Errorf("save card %s: %w", st.CardID, err)
	}
	return nil
}

// LoadCards reads every card's scheduling record.
func (s *Store) LoadCards() ([]srs.State, error) {
	rows, err := s.db.Query(
		`SELECT id, stability, difficulty, due, reps, lapses, last_review, first_seen
		 FROM cards ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("load cards: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []srs.State
	for rows.Next() {
		var st srs.State
		var due, last, first int64
		if err := rows.Scan(&st.CardID, &st.Stability, &st.Difficulty, &due,
			&st.Reps, &st.Lapses, &last, &first); err != nil {
			return nil, fmt.Errorf("load cards: %w", err)
		}
		st.Due, st.LastReview, st.FirstSeen = at(due), at(last), at(first)
		out = append(out, st)
	}
	return out, rows.Err()
}

// AppendReview logs one answer. The log is append-only: it is the record a
// replay could rebuild every card's state from, so nothing here is ever
// updated or deleted.
func (s *Store) AppendReview(r srs.Review) error {
	_, err := s.db.Exec(
		`INSERT INTO reviews (card_id, ts, rating, elapsed_ms, source) VALUES (?, ?, ?, ?, ?)`,
		r.CardID, unix(r.At), int(r.Rating), r.Elapsed.Milliseconds(), string(r.Source))
	if err != nil {
		return fmt.Errorf("log review of %s: %w", r.CardID, err)
	}
	return nil
}

// LoadReviews reads the whole review log, oldest first, which is the order the
// scheduler keeps it in.
//
// It is read in full because everything built on it — the streak, retention,
// today's count — is a scan of the whole history, and a deck of a few hundred
// cards produces a log small enough that paging it would cost more than it
// saved.
func (s *Store) LoadReviews() ([]srs.Review, error) {
	rows, err := s.db.Query(
		`SELECT card_id, ts, rating, elapsed_ms, source FROM reviews ORDER BY ts, id`)
	if err != nil {
		return nil, fmt.Errorf("load reviews: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []srs.Review
	for rows.Next() {
		var r srs.Review
		var ts, elapsed int64
		var rating int
		var source string
		if err := rows.Scan(&r.CardID, &ts, &rating, &elapsed, &source); err != nil {
			return nil, fmt.Errorf("load reviews: %w", err)
		}
		r.At, r.Rating = at(ts), srs.Rating(rating)
		r.Elapsed, r.Source = time.Duration(elapsed)*time.Millisecond, srs.Source(source)
		out = append(out, r)
	}
	return out, rows.Err()
}
