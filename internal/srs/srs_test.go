package srs

import (
	"fmt"
	"math"
	"math/rand"
	"testing"
	"time"
)

var origin = time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)

// TestIntervalEqualsStabilityAtDefaultRetention pins the identity the two
// forgetting-curve constants were chosen for: at 90% retention the next
// interval is the stability itself. Every other number in this package is
// readable only because of it — a stability of 12 means "due in twelve days".
func TestIntervalEqualsStabilityAtDefaultRetention(t *testing.T) {
	for _, s := range []float64{0.5, 1, 3, 12, 90, 300} {
		if got := intervalDays(s, 0.9); math.Abs(got-s) > 1e-9 {
			t.Errorf("intervalDays(%v, 0.9) = %v, want %v", s, got, s)
		}
	}
}

// TestRetentionMovesTheInterval checks the knob does what it says: asking for
// more retention shortens intervals, asking for less lengthens them.
func TestRetentionMovesTheInterval(t *testing.T) {
	strict := intervalDays(10, 0.95)
	relaxed := intervalDays(10, 0.80)
	if !(strict < 10 && 10 < relaxed) {
		t.Fatalf("intervals not ordered by retention: 0.95 -> %v, 0.90 -> 10, 0.80 -> %v", strict, relaxed)
	}
}

// TestScheduleMatchesExpectedIntervals is the M5 exit criterion: a learner who
// answers a card the same way every time gets a fixed, known ladder of
// intervals. The numbers are days between reviews, rounded to two places.
func TestScheduleMatchesExpectedIntervals(t *testing.T) {
	cases := []struct {
		rating Rating
		want   []float64
	}{
		{Good, []float64{3.00, 5.11, 8.73, 14.98, 25.77, 44.44, 76.85, 133.17}},
		{Easy, []float64{8.00, 18.28, 44.66, 115.52, 313.72, 365.00, 365.00, 365.00}},
		{Hard, []float64{1.20, 1.46, 1.73, 1.99, 2.22, 2.43, 2.59, 2.72}},
	}
	for _, tc := range cases {
		t.Run(tc.rating.String(), func(t *testing.T) {
			s := New(Defaults())
			now := origin
			for i, want := range tc.want {
				st := s.Grade("card", tc.rating, time.Second, now)
				got := round2(st.Due.Sub(now).Hours() / 24)
				if got != want {
					t.Fatalf("review %d: interval %.2f days, want %.2f", i+1, got, want)
				}
				now = st.Due
			}
		})
	}
}

// TestAgainRelearnsInsideTheSession checks that a forgotten card comes back in
// ten minutes rather than being pushed away, and that the stability it kept is
// small enough that the next interval is a day or two, not a month.
func TestAgainRelearnsInsideTheSession(t *testing.T) {
	s := New(Defaults())
	now := origin
	for i := 0; i < 4; i++ {
		st := s.Grade("card", Good, time.Second, now)
		now = st.Due
	}
	before, _ := s.State("card")
	st := s.Grade("card", Again, time.Second, now)

	if gap := st.Due.Sub(now); gap != Defaults().RelearnStep {
		t.Errorf("relearning step %v, want %v", gap, Defaults().RelearnStep)
	}
	if st.Lapses != 1 {
		t.Errorf("lapses = %d, want 1", st.Lapses)
	}
	if st.Stability >= before.Stability || st.Stability > maxLapseStability {
		t.Errorf("stability after a lapse = %.2f, want under both %.2f and the %.1f-day cap",
			st.Stability, before.Stability, maxLapseStability)
	}
}

// TestPracticeCreditIsHalfStrength holds SPEC §5's rule that passing an
// exercise reinforces every card it teaches without counting as a full recall
// test: the interval moves, but not as far as answering the card would.
func TestPracticeCreditIsHalfStrength(t *testing.T) {
	now := origin

	graded := New(Defaults())
	graded.Grade("card", Good, time.Second, now)
	full, _ := graded.State("card")

	credited := New(Defaults())
	credited.Credit("card", now)
	half, _ := credited.State("card")

	if !(half.Stability > 0 && half.Stability < full.Stability) {
		t.Fatalf("credit stability %.2f, want between 0 and the full %.2f", half.Stability, full.Stability)
	}
	if log := credited.Log(); len(log) != 1 || log[0].Source != SourcePractice {
		t.Fatalf("credit log = %+v, want one entry from practice", log)
	}
}

// TestCreditNeverPullsADueDateForward guards the case where a card is already
// scheduled far out: solving an exercise that touches it must not shorten what
// the card earned by being recalled.
func TestCreditNeverPullsADueDateForward(t *testing.T) {
	s := New(Defaults())
	now := origin
	for i := 0; i < 5; i++ {
		st := s.Grade("card", Easy, time.Second, now)
		now = st.Due
	}
	before, _ := s.State("card")
	after := s.Credit("card", origin.Add(24*time.Hour))
	if after.Due.Before(before.Due) {
		t.Errorf("credit moved the due date from %v forward to %v", before.Due, after.Due)
	}
}

// TestQueueTakesDueCardsBeforeNewOnes checks the sitting is assembled the way
// SPEC §2.3 describes, and that the daily new-card cap holds across two
// sessions in the same day rather than resetting with each one.
func TestQueueTakesDueCardsBeforeNewOnes(t *testing.T) {
	p := Defaults()
	p.NewPerDay, p.SessionSize = 3, 5
	s := New(p)

	ids := []string{"a", "b", "c", "d", "e", "f", "g", "h"}
	first := s.Queue(ids, origin)
	if len(first) != 3 {
		t.Fatalf("first sitting = %v, want three new cards", first)
	}
	for _, id := range first {
		s.Grade(id, Again, time.Second, origin)
	}

	later := origin.Add(30 * time.Minute)
	second := s.Queue(ids, later)
	if len(second) != 3 {
		t.Fatalf("second sitting = %v, want the three relearning cards and no new ones", second)
	}
	for i, id := range second {
		if id != first[i] {
			t.Fatalf("second sitting = %v, want the relearning cards %v", second, first)
		}
	}

	tomorrow := origin.Add(24 * time.Hour)
	if got := s.Queue(ids, tomorrow); len(got) != 5 {
		t.Fatalf("next day's sitting = %v, want a full session of %d", got, p.SessionSize)
	}
}

// TestForecastBucketsOverdueIntoToday checks the 14-day outlook Stats draws:
// a card two days late is work for today, not for the day it was due.
func TestForecastBucketsOverdueIntoToday(t *testing.T) {
	s := New(Defaults())
	s.Grade("late", Good, time.Second, origin.Add(-10*24*time.Hour))
	s.Grade("soon", Good, time.Second, origin)

	f := s.Forecast([]string{"late", "soon"}, origin, 14)
	if f[0] != 1 {
		t.Errorf("forecast[0] = %d, want the overdue card", f[0])
	}
	if f[3] != 1 {
		t.Errorf("forecast[3] = %d, want the card due in three days; got %v", f[3], f)
	}
}

// learner is a simulated student whose recall is exactly as good as the model
// predicts: a card is recalled with the probability its own forgetting curve
// gives at the moment it is shown. That makes the simulation a test of the
// scheduler rather than of an invented student.
type learner struct {
	sched *Scheduler
	rng   *rand.Rand
	// last is each card's previous rating. A card answered ten minutes after
	// failing is a relearning step, not a test of retention, so those answers
	// are left out of the retention figure the way Anki's "true retention"
	// leaves them out.
	last               map[string]Rating
	recalled, answered int
}

func newLearner(s *Scheduler, seed int64) *learner {
	// A fixed seed, not entropy: the point of a simulation is that it gives
	// the same answer on every machine and every run.
	return &learner{sched: s, rng: rand.New(rand.NewSource(seed)), last: map[string]Rating{}}
}

func (l *learner) answer(id string, now time.Time) {
	st, _ := l.sched.State(id)
	rating := Good
	if st.Seen() && l.rng.Float64() > retrievability(st.Stability, elapsedDays(st.LastReview, now)) {
		rating = Again
	}
	if st.Seen() && l.last[id] != Again {
		l.answered++
		if rating != Again {
			l.recalled++
		}
	}
	l.last[id] = rating
	l.sched.Grade(id, rating, 4*time.Second, now)
}

func (l *learner) retention() float64 { return float64(l.recalled) / float64(l.answered) }

// deck builds n synthetic card ids.
func deck(n int) []string {
	ids := make([]string, n)
	for i := range ids {
		ids[i] = fmt.Sprintf("card-%03d", i)
	}
	return ids
}

// TestPunctualReviewsConvergeOnTheTarget is the retention half of SPEC §10's
// simulation. A learner who answers every card at the exact moment it comes
// due should get the retention the intervals were computed for — that is what
// desired_retention means, and it is the claim the whole forgetting curve
// makes.
func TestPunctualReviewsConvergeOnTheTarget(t *testing.T) {
	p := Defaults()
	s := New(p)
	l := newLearner(s, 20260101)
	ids := deck(250)
	deadline := origin.Add(365 * 24 * time.Hour)

	introduced := 0
	now := origin
	for {
		// Whichever card is due soonest is the next thing that happens.
		next, at := "", time.Time{}
		for _, id := range ids {
			st, ok := s.State(id)
			if !ok || !st.Seen() {
				continue
			}
			if next == "" || st.Due.Before(at) {
				next, at = id, st.Due
			}
		}
		// Keep introducing new cards at the daily allowance until the deck
		// is exhausted, so the sample spans every stage of learning.
		allowed := int(now.Sub(origin).Hours()/24)*p.NewPerDay + p.NewPerDay
		if introduced < len(ids) && introduced < allowed {
			l.answer(ids[introduced], now)
			introduced++
			continue
		}
		if next == "" || at.After(deadline) {
			break
		}
		now = at
		l.answer(next, now)
	}

	if introduced != len(ids) {
		t.Fatalf("introduced %d of %d cards", introduced, len(ids))
	}
	if r := l.retention(); math.Abs(r-p.DesiredRetention) > 0.02 {
		t.Errorf("measured retention %.3f over %d reviews, want %.2f ± 0.02",
			r, l.answered, p.DesiredRetention)
	}
}

// TestSimulatedYearStaysWithinTheCaps is the review-load half of SPEC §10's
// simulation: a learner sits down once a day for a year and clears whatever
// the scheduler offers. The daily caps must hold every day, the whole deck
// must get introduced, and the load must settle rather than snowball.
//
// Retention comes out about ten points under the target here, and should: a
// once-a-day learner answers a card up to a day after it fell due, and on a
// three-day interval that lateness is a third of the card's whole life. The
// punctual test above is where the model's own accuracy is checked; this one
// bounds what daily granularity costs on top of it.
func TestSimulatedYearStaysWithinTheCaps(t *testing.T) {
	const days = 365
	p := Defaults()
	s := New(p)
	l := newLearner(s, 20260101)
	ids := deck(250)

	peakLoad, lastWeek := 0, 0
	for day := 0; day < days; day++ {
		now := origin.Add(time.Duration(day) * 24 * time.Hour)
		newBefore, load := s.NewToday(now), 0

		// One sitting after another until the day's queue is empty, which is
		// how a learner clearing the day's work actually behaves.
		for at := now; ; {
			queue := s.Queue(ids, at)
			if len(queue) == 0 {
				break
			}
			for i, id := range queue {
				l.answer(id, at.Add(time.Duration(i)*time.Minute))
				load++
			}
			at = at.Add(time.Duration(len(queue)+11) * time.Minute)
		}

		if load > p.MaxReviewsPerDay {
			t.Fatalf("day %d: %d reviews, over the %d cap", day, load, p.MaxReviewsPerDay)
		}
		if introduced := s.NewToday(now) - newBefore; introduced > p.NewPerDay {
			t.Fatalf("day %d: %d new cards, over the %d cap", day, introduced, p.NewPerDay)
		}
		if load > peakLoad {
			peakLoad = load
		}
		if day >= days-7 {
			lastWeek += load
		}
	}

	if unseen := s.UnseenCount(ids); unseen != 0 {
		t.Errorf("%d of %d cards never introduced in a year", unseen, len(ids))
	}
	if r := l.retention(); r < 0.78 || r > p.DesiredRetention {
		t.Errorf("measured retention %.3f over %d reviews, want between 0.78 and %.2f",
			r, l.answered, p.DesiredRetention)
	}
	// The load has to fall away once the deck is learned, or spaced
	// repetition is not buying anything.
	if lastWeek/7 >= peakLoad {
		t.Errorf("steady-state load %d/day never fell below the peak of %d", lastWeek/7, peakLoad)
	}

	// A year of steady work should leave the deck spaced out, not churning:
	// most cards should be sitting on intervals of over a month.
	long := 0
	for _, id := range ids {
		if st, _ := s.State(id); st.Stability > 30 {
			long++
		}
	}
	if long < len(ids)/2 {
		t.Errorf("only %d of %d cards reached a month's stability", long, len(ids))
	}
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }
