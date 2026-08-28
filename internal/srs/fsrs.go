// Package srs schedules flashcard reviews and keeps the review log.
//
// The algorithm is FSRS-lite: the same two-variable model as FSRS — a card's
// stability (how long a memory lasts) and its difficulty (how hard it is to
// keep) — driven by the same power forgetting curve, but with a small set of
// hand-chosen, documented constants instead of nineteen fitted weights. There
// is no training data to fit weights to and no way to collect any without
// telemetry, so a legible model that a reader can predict beats an opaque one
// that is merely more precise on somebody else's corpus.
//
// The review log is append-only. Every scheduling decision is a pure function
// of a card's state and one rating, so a better scheduler can be dropped in
// later and the whole history replayed through it.
package srs

import "math"

// The forgetting curve. Retrievability — the probability of recalling a card —
// decays as a power of elapsed time over stability:
//
//	R(t) = (1 + factor·t/S) ^ decay
//
// These two constants are FSRS's, and they are chosen together so that at the
// default 90% retention the next interval comes out at exactly S days. That
// identity is what makes stability readable: a stability of 12 means "this
// will be due in twelve days".
const (
	decay  = -0.5
	factor = 19.0 / 81.0
)

// The bounds every scheduling value is kept inside.
const (
	minStability = 0.05  // about an hour: the floor after a bad lapse
	maxStability = 365.0 // a year; longer intervals stop being a course
	// maxLapseStability caps what a lapsed card keeps. A card forgotten after
	// six months is not a six-month card, however well it did before.
	maxLapseStability = 2.0
	// lapseRetain is the share of stability that survives a lapse.
	lapseRetain = 0.25
	// meanReversion pulls difficulty back toward the middle on every review,
	// so one bad day cannot mark a card hard forever.
	meanReversion = 0.1
)

// initialStability is the first stability granted to a brand-new card, in
// days, by the rating its first answer earned.
var initialStability = map[Rating]float64{
	Again: 0.4,
	Hard:  1.2,
	Good:  3.0,
	Easy:  8.0,
}

// ease scales how much a successful review grows stability. Again never
// reaches it: a failure lapses instead.
var ease = map[Rating]float64{
	Hard: 0.4,
	Good: 1.0,
	Easy: 1.5,
}

// retrievability is the probability of recalling a card of the given stability
// after elapsed days.
func retrievability(stability, days float64) float64 {
	if stability <= 0 {
		return 0
	}
	if days <= 0 {
		return 1
	}
	return math.Pow(1+factor*days/stability, decay)
}

// intervalDays solves the forgetting curve for the moment retrievability falls
// to the desired retention. At retention 0.9 this returns the stability
// unchanged, which is the identity the two curve constants were picked for.
func intervalDays(stability, retention float64) float64 {
	if stability <= 0 {
		return 0
	}
	d := stability / factor * (math.Pow(retention, 1/decay) - 1)
	return clampFloat(d, 1.0/1440, maxStability)
}

// initialDifficulty is where a new card starts on the 1–10 scale, from the
// rating its first answer earned: Good lands mid-scale and the others move a
// step either side.
func initialDifficulty(r Rating) float64 {
	return clampFloat(5.5-1.2*(float64(r)-3), 1, 10)
}

// nextDifficulty moves difficulty by one step per rating away from Good, then
// reverts a tenth of the way to the middle.
func nextDifficulty(d float64, r Rating) float64 {
	d -= float64(r) - 3
	d += meanReversion * (5 - d)
	return clampFloat(d, 1, 10)
}

// nextStability is the stability of a card that was recalled. Growth is
// largest when the card was nearly forgotten — that is the spacing effect —
// and smallest when it is difficult or the answer came hard.
func nextStability(stability, difficulty float64, r Rating, elapsedDays float64) float64 {
	ret := retrievability(stability, elapsedDays)
	growth := growthFactor(difficulty, ret, r)
	return clampFloat(stability*growth, minStability, maxStability)
}

// growthFactor is the multiplier a successful review applies to stability.
func growthFactor(difficulty, ret float64, r Rating) float64 {
	return 1 + ease[r]*(11-difficulty)/9*math.Exp(1.4*(1-ret))
}

// lapsedStability is what survives a forgotten card: a quarter of what it had,
// and never more than a couple of days.
func lapsedStability(stability float64) float64 {
	return clampFloat(stability*lapseRetain, minStability, maxLapseStability)
}

func clampFloat(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
