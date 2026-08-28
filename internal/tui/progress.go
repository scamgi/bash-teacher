package tui

import "bash-teacher/internal/content"

// unlockShare is the fraction of a track that must pass before the next one
// opens, per SPEC §2.2.
const unlockShare = 0.8

// exerciseState is what this session knows about one exercise. The store in
// M6 will persist exactly this, plus timings.
type exerciseState struct {
	passed        bool
	attempts      int
	hints         int
	solutionShown bool
}

// progress is the in-memory record of a practice session.
//
// Nothing here survives the process yet: SPEC §8 puts progress in SQLite and
// that arrives with M6. Until then track locking is advisory — see Unlocked —
// because a hard lock backed by memory alone would re-lock the whole library
// on every launch.
type progress struct {
	byExercise map[string]*exerciseState
}

func newProgress() *progress {
	return &progress{byExercise: map[string]*exerciseState{}}
}

// state returns the record for an exercise, creating it on first use.
func (p *progress) state(id string) *exerciseState {
	st, ok := p.byExercise[id]
	if !ok {
		st = &exerciseState{}
		p.byExercise[id] = st
	}
	return st
}

// Passed reports whether the exercise has been solved this session.
func (p *progress) Passed(id string) bool {
	st, ok := p.byExercise[id]
	return ok && st.passed
}

// PassedIn counts the solved exercises in a track.
func (p *progress) PassedIn(t *content.Track) int {
	n := 0
	for _, e := range t.Exercises {
		if p.Passed(e.ID) {
			n++
		}
	}
	return n
}

// Unlocked reports whether the track before this one has been passed to the
// unlock threshold. The first track is always open.
//
// The practice screen shows this but does not enforce it: with progress held
// only in memory, enforcing it would mean every session starts with five of
// the six tracks shut, and would break the dictionary's jump-to-an-exercise
// shortcut, which addresses any exercise in the library. It becomes a real
// gate when M6 gives it something to remember.
func (p *progress) Unlocked(lib *content.Library, track string) bool {
	for i, t := range lib.Tracks {
		if t.Name != track {
			continue
		}
		if i == 0 {
			return true
		}
		prev := lib.Tracks[i-1]
		if len(prev.Exercises) == 0 {
			return true
		}
		return float64(p.PassedIn(prev))/float64(len(prev.Exercises)) >= unlockShare
	}
	return true
}
