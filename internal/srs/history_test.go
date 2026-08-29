package srs

import (
	"testing"
	"time"
)

// grade answers one card on a given day, so a history test reads as the
// sequence of sittings it is describing rather than as log surgery.
func gradeOn(s *Scheduler, id string, r Rating, day time.Time) {
	s.Grade(id, r, time.Second, day)
}

// TestHistoryBucketsTheLogByDay covers what the Stats curve indexes by: a
// fixed-width series ending today, with the empty days present rather than
// skipped, and practice credit kept out of the retention denominator.
func TestHistoryBucketsTheLogByDay(t *testing.T) {
	s := New(Defaults())
	now := origin.AddDate(0, 0, 4)

	gradeOn(s, "a", Good, origin)
	gradeOn(s, "b", Again, origin)
	// Nothing on days 1 and 2.
	gradeOn(s, "c", Good, origin.AddDate(0, 0, 3))
	s.Credit("d", origin.AddDate(0, 0, 3))
	gradeOn(s, "e", Easy, now)

	days := s.History(now, 5)
	if len(days) != 5 {
		t.Fatalf("History returned %d days, want 5", len(days))
	}
	want := []Day{
		{Answered: 2, Reviewed: 2, Recalled: 1},
		{},
		{},
		{Answered: 2, Reviewed: 1, Recalled: 1},
		{Answered: 1, Reviewed: 1, Recalled: 1},
	}
	for i, w := range want {
		got := days[i]
		if got.Answered != w.Answered || got.Reviewed != w.Reviewed || got.Recalled != w.Recalled {
			t.Errorf("day %d = %+v, want answered %d reviewed %d recalled %d",
				i, got, w.Answered, w.Reviewed, w.Recalled)
		}
		if wantDate := startOfDay(origin.AddDate(0, 0, i)); !got.Date.Equal(wantDate) {
			t.Errorf("day %d dated %v, want %v", i, got.Date, wantDate)
		}
	}

	if r, ok := days[0].Retention(); !ok || r != 0.5 {
		t.Errorf("day 0 retention = %v (ok %v), want 0.5", r, ok)
	}
	// A day with only practice credit has no retention to report: nothing was
	// recalled from memory, so drawing it as 100% or as 0% would both be lies.
	if _, ok := days[1].Retention(); ok {
		t.Error("an empty day reported a retention")
	}
}

// TestLongestStreakSpansTheWholeLog checks the best run is found in the
// middle of a history, not only at its end, and that it does not depend on
// when it is asked.
func TestLongestStreakSpansTheWholeLog(t *testing.T) {
	s := New(Defaults())
	for _, offset := range []int{0, 1, 2, 3, 5, 9, 10} {
		gradeOn(s, "a", Good, origin.AddDate(0, 0, offset))
	}
	if got := s.LongestStreak(); got != 4 {
		t.Errorf("LongestStreak = %d, want 4", got)
	}
	if got := s.ActiveDays(); got != 7 {
		t.Errorf("ActiveDays = %d, want 7", got)
	}
	// The current streak ends with the log; the longest one does not.
	if got := s.Streak(origin.AddDate(0, 0, 10)); got != 2 {
		t.Errorf("Streak = %d, want 2", got)
	}
}

// TestTotalsSeparatePracticeCredit pins the split the activity panel reports,
// and that credit is counted as turning up without being counted as recall.
func TestTotalsSeparatePracticeCredit(t *testing.T) {
	s := New(Defaults())
	gradeOn(s, "a", Good, origin)
	gradeOn(s, "b", Again, origin)
	s.Credit("c", origin)

	reviews, credits := s.Totals()
	if reviews != 2 || credits != 1 {
		t.Errorf("Totals = (%d, %d), want (2, 1)", reviews, credits)
	}
	if recalled, total := s.Accuracy(); recalled != 1 || total != 2 {
		t.Errorf("Accuracy = (%d, %d), want (1, 2)", recalled, total)
	}
	if first, ok := s.FirstAnswer(); !ok || !first.Equal(origin) {
		t.Errorf("FirstAnswer = %v (ok %v), want %v", first, ok, origin)
	}
}

// TestHistoryOfAnEmptyLog is the first-launch case: every screen that draws
// the history has to render before anything has been answered.
func TestHistoryOfAnEmptyLog(t *testing.T) {
	s := New(Defaults())
	for _, d := range s.History(origin, 30) {
		if d.Answered != 0 {
			t.Fatalf("empty log produced %+v", d)
		}
	}
	if s.LongestStreak() != 0 || s.ActiveDays() != 0 {
		t.Error("empty log has a streak")
	}
	if _, ok := s.FirstAnswer(); ok {
		t.Error("empty log reported a first answer")
	}
	if got := s.History(origin, 0); got != nil {
		t.Errorf("History(_, 0) = %v, want nil", got)
	}
}
