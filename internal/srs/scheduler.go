package srs

import (
	"fmt"
	"sort"
	"time"
)

// Rating is how well a card was recalled, on FSRS's four-point scale.
type Rating int

// The four ratings. A typed answer that matches maps to Good; a wrong one maps
// to Again. Hard and Easy are the learner's own judgement.
const (
	Again Rating = 1
	Hard  Rating = 2
	Good  Rating = 3
	Easy  Rating = 4
)

func (r Rating) String() string {
	switch r {
	case Again:
		return "again"
	case Hard:
		return "hard"
	case Good:
		return "good"
	case Easy:
		return "easy"
	default:
		return fmt.Sprintf("rating(%d)", int(r))
	}
}

// Valid reports whether r is one of the four ratings.
func (r Rating) Valid() bool { return r >= Again && r <= Easy }

// Source records what produced a review, so that credit earned by solving an
// exercise can be told from a card actually answered.
type Source string

// The two things that can grade a card.
const (
	// SourceReview is a card answered in a flashcard session.
	SourceReview Source = "review"
	// SourcePractice is the half-strength credit an exercise pass gives every
	// command it teaches.
	SourcePractice Source = "practice"
)

// Params are the knobs SPEC §5 exposes. They become config in M6; until then
// Defaults is what the app runs on.
type Params struct {
	// DesiredRetention is the recall probability an interval aims for.
	DesiredRetention float64
	// NewPerDay caps how many unseen cards a day may introduce.
	NewPerDay int
	// MaxReviewsPerDay caps how many due cards a day may present.
	MaxReviewsPerDay int
	// SessionSize is how many cards one sitting holds.
	SessionSize int
	// RelearnStep is how soon a failed card comes back.
	RelearnStep time.Duration
	// SoftTarget is the answer time a learner is nudged toward. It never
	// fails a card: automaticity is the goal, but a slow right answer is
	// still a right answer.
	SoftTarget time.Duration
}

// Defaults returns the parameters from SPEC §5.
func Defaults() Params {
	return Params{
		DesiredRetention: 0.90,
		NewPerDay:        15,
		MaxReviewsPerDay: 120,
		SessionSize:      20,
		RelearnStep:      10 * time.Minute,
		SoftTarget:       8 * time.Second,
	}
}

// State is one card's scheduling record: SPEC §8's `cards` row.
type State struct {
	CardID     string
	Stability  float64
	Difficulty float64
	Due        time.Time
	Reps       int
	Lapses     int
	LastReview time.Time
	// FirstSeen is when the card left the new pile, which is what the daily
	// new-card cap is counted against.
	FirstSeen time.Time
}

// Seen reports whether the card has ever been answered.
func (s State) Seen() bool { return s.Reps > 0 }

// Review is one append-only log entry: SPEC §8's `reviews` row.
type Review struct {
	CardID  string
	At      time.Time
	Rating  Rating
	Elapsed time.Duration
	Source  Source
}

// Scheduler holds every card's state and the review log.
//
// It knows nothing about the content library: cards are addressed by id, so
// the scheduler can be simulated over synthetic ids and replayed over a log
// without a library to hand.
type Scheduler struct {
	params Params
	states map[string]*State
	log    []Review
}

// New builds a scheduler with the given parameters.
func New(p Params) *Scheduler {
	return &Scheduler{params: p, states: map[string]*State{}}
}

// Params returns the scheduler's parameters.
func (s *Scheduler) Params() Params { return s.params }

// State returns a card's scheduling record, and whether it has one. A card
// with no record has never been seen.
func (s *Scheduler) State(cardID string) (State, bool) {
	st, ok := s.states[cardID]
	if !ok {
		return State{CardID: cardID}, false
	}
	return *st, true
}

// Log returns the review log, oldest first. The slice is the scheduler's own;
// callers read it and do not modify it.
func (s *Scheduler) Log() []Review { return s.log }

// Grade applies one answer to a card and returns its new state.
//
// A wrong answer (Again) does not push the card away: it sets the relearning
// step so the card comes back inside the same sitting, and counts a lapse.
func (s *Scheduler) Grade(cardID string, r Rating, elapsed time.Duration, now time.Time) State {
	if !r.Valid() {
		r = Good
	}
	st := s.stateFor(cardID)
	*st = s.next(*st, r, now)
	s.log = append(s.log, Review{CardID: cardID, At: now, Rating: r, Elapsed: elapsed, Source: SourceReview})
	return *st
}

// Preview reports how far off a rating would put a card, without recording
// anything. It is what lets the review screen label each rating key with the
// interval it buys, so a learner picking "hard" over "good" can see the price.
func (s *Scheduler) Preview(cardID string, r Rating, now time.Time) time.Duration {
	if !r.Valid() {
		r = Good
	}
	st, _ := s.State(cardID)
	return s.next(st, r, now).Due.Sub(now)
}

// next applies one rating to a state and returns what it becomes. It is pure:
// Grade keeps what it returns and Preview throws it away, so the two can never
// disagree about what a rating is worth.
func (s *Scheduler) next(st State, r Rating, now time.Time) State {
	switch {
	case !st.Seen():
		st.Stability = initialStability[r]
		st.Difficulty = initialDifficulty(r)
		st.FirstSeen = now
	case r == Again:
		st.Stability = lapsedStability(st.Stability)
		st.Difficulty = nextDifficulty(st.Difficulty, r)
	default:
		days := elapsedDays(st.LastReview, now)
		st.Stability = nextStability(st.Stability, st.Difficulty, r, days)
		st.Difficulty = nextDifficulty(st.Difficulty, r)
	}

	st.Reps++
	if r == Again {
		st.Lapses++
		st.Due = now.Add(s.params.RelearnStep)
	} else {
		st.Due = s.dueAfter(st.Stability, now)
	}
	st.LastReview = now
	return st
}

// Credit gives a card the half-strength Good review that SPEC §5 grants every
// command an exercise teaches. Practice reinforces recall, so it should move
// the schedule; it is not a recall test, so it should not move it as far as
// one, and it never pulls a due date forward.
func (s *Scheduler) Credit(cardID string, now time.Time) State {
	st := s.stateFor(cardID)
	was := st.Due

	if !st.Seen() {
		st.Stability = initialStability[Good] / 2
		st.Difficulty = initialDifficulty(Good)
		st.FirstSeen = now
	} else {
		days := elapsedDays(st.LastReview, now)
		ret := retrievability(st.Stability, days)
		growth := 1 + (growthFactor(st.Difficulty, ret, Good)-1)/2
		st.Stability = clampFloat(st.Stability*growth, minStability, maxStability)
	}

	st.Reps++
	st.LastReview = now
	st.Due = s.dueAfter(st.Stability, now)
	if st.Reps > 1 && st.Due.Before(was) {
		st.Due = was
	}

	s.log = append(s.log, Review{CardID: cardID, At: now, Rating: Good, Source: SourcePractice})
	return *st
}

// dueAfter turns a stability into the moment the card falls to the desired
// retention.
func (s *Scheduler) dueAfter(stability float64, now time.Time) time.Time {
	d := intervalDays(stability, s.params.DesiredRetention)
	return now.Add(time.Duration(d * float64(24*time.Hour)))
}

func (s *Scheduler) stateFor(cardID string) *State {
	st, ok := s.states[cardID]
	if !ok {
		st = &State{CardID: cardID}
		s.states[cardID] = st
	}
	return st
}

// elapsedDays is the gap between two reviews in days, floored at zero so that
// a clock that steps backwards cannot produce a negative interval.
func elapsedDays(from, to time.Time) float64 {
	d := to.Sub(from).Hours() / 24
	if d < 0 {
		return 0
	}
	return d
}

// Queue picks the cards for one sitting out of ids, which is the deck in
// library order: everything already due, most overdue first, then unseen
// cards in deck order to fill the rest.
//
// Both daily caps are honoured against what the log already holds for today,
// so two sessions in one day cannot together exceed one day's allowance.
func (s *Scheduler) Queue(ids []string, now time.Time) []string {
	newRoom := s.params.NewPerDay - s.NewToday(now)
	reviewRoom := s.params.MaxReviewsPerDay - s.ReviewsToday(now)

	type dueCard struct {
		id  string
		due time.Time
	}
	var due []dueCard
	var fresh []string
	for _, id := range ids {
		st, ok := s.states[id]
		switch {
		case !ok || !st.Seen():
			fresh = append(fresh, id)
		case !st.Due.After(now):
			due = append(due, dueCard{id: id, due: st.Due})
		}
	}
	// Most overdue first: the card closest to being forgotten is the one
	// worth the next minute.
	sort.SliceStable(due, func(i, j int) bool { return due[i].due.Before(due[j].due) })

	out := make([]string, 0, s.params.SessionSize)
	for _, d := range due {
		if len(out) >= s.params.SessionSize || reviewRoom <= 0 {
			break
		}
		out = append(out, d.id)
		reviewRoom--
	}
	for _, id := range fresh {
		if len(out) >= s.params.SessionSize || newRoom <= 0 || reviewRoom <= 0 {
			break
		}
		out = append(out, id)
		newRoom--
		reviewRoom--
	}
	return out
}

// DueCount reports how many of ids are due at or before now.
func (s *Scheduler) DueCount(ids []string, now time.Time) int {
	n := 0
	for _, id := range ids {
		if st, ok := s.states[id]; ok && st.Seen() && !st.Due.After(now) {
			n++
		}
	}
	return n
}

// UnseenCount reports how many of ids have never been answered.
func (s *Scheduler) UnseenCount(ids []string) int {
	n := 0
	for _, id := range ids {
		if st, ok := s.states[id]; !ok || !st.Seen() {
			n++
		}
	}
	return n
}

// Forecast counts how many of ids come due on each of the next days, starting
// with today. Everything already overdue lands in the first bucket, which is
// what a learner opening the app is actually facing.
func (s *Scheduler) Forecast(ids []string, now time.Time, days int) []int {
	if days < 1 {
		return nil
	}
	out := make([]int, days)
	today := startOfDay(now)
	for _, id := range ids {
		st, ok := s.states[id]
		if !ok || !st.Seen() {
			continue
		}
		bucket := int(startOfDay(st.Due).Sub(today).Hours() / 24)
		if bucket < 0 {
			bucket = 0
		}
		if bucket >= days {
			continue
		}
		out[bucket]++
	}
	return out
}

// NewToday counts the cards introduced today, which is what the daily
// new-card cap is measured against.
func (s *Scheduler) NewToday(now time.Time) int {
	day := startOfDay(now)
	n := 0
	for _, st := range s.states {
		if st.Seen() && startOfDay(st.FirstSeen).Equal(day) {
			n++
		}
	}
	return n
}

// ReviewsToday counts the answers logged today, credit from practice
// included: a card drilled by an exercise has had its turn.
func (s *Scheduler) ReviewsToday(now time.Time) int {
	return s.reviewsOn(startOfDay(now))
}

func (s *Scheduler) reviewsOn(day time.Time) int {
	n := 0
	for _, r := range s.log {
		if startOfDay(r.At).Equal(day) {
			n++
		}
	}
	return n
}

// Accuracy is the share of answered cards recalled — anything but Again —
// over the whole log. It is the retention figure Stats reports.
func (s *Scheduler) Accuracy() (recalled, total int) {
	for _, r := range s.log {
		if r.Source != SourceReview {
			continue
		}
		total++
		if r.Rating != Again {
			recalled++
		}
	}
	return recalled, total
}

// startOfDay truncates to local midnight. Local rather than UTC because the
// daily caps are about the learner's day, not the clock's.
func startOfDay(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

// Streak counts the consecutive days ending today on which something was
// reviewed. A day with no reviews ends it; today having none does not, since
// the day is not over.
func (s *Scheduler) Streak(now time.Time) int {
	day := startOfDay(now)
	if s.reviewsOn(day) == 0 {
		day = day.AddDate(0, 0, -1)
	}
	n := 0
	for s.reviewsOn(day) > 0 {
		n++
		day = day.AddDate(0, 0, -1)
	}
	return n
}

// NextDue reports when the soonest not-yet-due card comes back, so a screen
// with nothing to offer can say how long the wait is rather than just "none".
func (s *Scheduler) NextDue(ids []string, now time.Time) (time.Time, bool) {
	var soonest time.Time
	for _, id := range ids {
		st, ok := s.states[id]
		if !ok || !st.Seen() || !st.Due.After(now) {
			continue
		}
		if soonest.IsZero() || st.Due.Before(soonest) {
			soonest = st.Due
		}
	}
	return soonest, !soonest.IsZero()
}
