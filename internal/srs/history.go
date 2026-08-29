package srs

import (
	"sort"
	"time"
)

// Day is one calendar day's slice of the review log, which is what the Stats
// screen's history is drawn from.
//
// The two counts are deliberately separate. Answered is every log entry, the
// credit an exercise pass gives included, and is what the streak and the
// activity chart are about: the question there is whether the learner turned
// up. Reviewed counts only cards actually answered in a review session, and is
// the denominator retention is measured against, because half-strength credit
// from a solved pipeline was never a recall test and must not be allowed to
// flatter the curve.
type Day struct {
	Date     time.Time
	Answered int
	Reviewed int
	Recalled int
}

// Retention is the share of the day's reviews recalled — anything but Again.
// The second result is false on a day with no reviews, which a caller must
// draw as a gap rather than as a zero: a day not studied is not a day failed.
func (d Day) Retention() (float64, bool) {
	if d.Reviewed == 0 {
		return 0, false
	}
	return float64(d.Recalled) / float64(d.Reviewed), true
}

// History buckets the log into the given number of days ending today, oldest
// first. Days with nothing in them are present and empty, so the result is a
// fixed-width series a chart can index by column without the caller having to
// reconcile dates.
func (s *Scheduler) History(now time.Time, days int) []Day {
	if days < 1 {
		return nil
	}
	byDay := map[int64]*Day{}
	for _, r := range s.log {
		day := startOfDay(r.At)
		d, ok := byDay[day.Unix()]
		if !ok {
			d = &Day{Date: day}
			byDay[day.Unix()] = d
		}
		d.Answered++
		if r.Source != SourceReview {
			continue
		}
		d.Reviewed++
		if r.Rating != Again {
			d.Recalled++
		}
	}

	out := make([]Day, 0, days)
	// Stepping with AddDate rather than by 24 hours keeps the columns on
	// calendar days across a daylight-saving change, which is the same reason
	// the streak walks backwards that way.
	day := startOfDay(now).AddDate(0, 0, -(days - 1))
	for i := 0; i < days; i++ {
		if d, ok := byDay[day.Unix()]; ok {
			out = append(out, *d)
		} else {
			out = append(out, Day{Date: day})
		}
		day = day.AddDate(0, 0, 1)
	}
	return out
}

// activeDays returns the distinct local days the log has an entry on, oldest
// first.
func (s *Scheduler) activeDays() []time.Time {
	seen := map[int64]bool{}
	var out []time.Time
	for _, r := range s.log {
		day := startOfDay(r.At)
		if seen[day.Unix()] {
			continue
		}
		seen[day.Unix()] = true
		out = append(out, day)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Before(out[j]) })
	return out
}

// ActiveDays counts the days something was answered on. It is what makes a
// total legible: 400 answers over eight days and over eight months describe
// different learners.
func (s *Scheduler) ActiveDays() int { return len(s.activeDays()) }

// LongestStreak is the longest run of consecutive days with an answer on each,
// anywhere in the log. Unlike Streak it takes no clock: the best run is a fact
// about the history and does not change with the hour it is asked at.
func (s *Scheduler) LongestStreak() int {
	days := s.activeDays()
	best, run := 0, 0
	var prev time.Time
	for _, day := range days {
		if !prev.IsZero() && day.Equal(prev.AddDate(0, 0, 1)) {
			run++
		} else {
			run = 1
		}
		if run > best {
			best = run
		}
		prev = day
	}
	return best
}

// FirstAnswer is when the log starts, and whether it starts at all.
func (s *Scheduler) FirstAnswer() (time.Time, bool) {
	if len(s.log) == 0 {
		return time.Time{}, false
	}
	first := s.log[0].At
	for _, r := range s.log[1:] {
		if r.At.Before(first) {
			first = r.At
		}
	}
	return first, true
}

// Totals counts the log by what produced each entry: cards answered in a
// review session, and cards credited by solving an exercise.
func (s *Scheduler) Totals() (reviews, credits int) {
	for _, r := range s.log {
		if r.Source == SourceReview {
			reviews++
			continue
		}
		credits++
	}
	return reviews, credits
}
