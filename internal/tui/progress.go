package tui

import (
	"bash-teacher/internal/content"
	"bash-teacher/internal/store"
)

// unlockShare is the fraction of a track that must pass before the next one
// opens, per SPEC §2.2.
const unlockShare = 0.8

// progress is what the app knows about every exercise.
//
// It holds store.Exercise values directly rather than a parallel struct of its
// own: the summary the screen reads and the row the database keeps are the
// same six facts, and giving them one type means a save can never drop a field
// the screen was relying on.
//
// When no store is attached the map is simply never loaded and never written,
// and the session's progress dies with the process as it did before M6.
type progress struct {
	byExercise map[string]*store.Exercise
}

func newProgress() *progress {
	return &progress{byExercise: map[string]*store.Exercise{}}
}

// Restore loads persisted summaries, replacing whatever is held.
func (p *progress) Restore(rows []store.Exercise) {
	p.byExercise = make(map[string]*store.Exercise, len(rows))
	for _, e := range rows {
		p.byExercise[e.ID] = &e
	}
}

// state returns the record for an exercise, creating it on first use.
func (p *progress) state(id string) *store.Exercise {
	st, ok := p.byExercise[id]
	if !ok {
		st = &store.Exercise{ID: id}
		p.byExercise[id] = st
	}
	return st
}

// summary returns what is known about an exercise without recording an
// interest in it. The rendering path uses this rather than state: opening an
// exercise and reading the prompt is not progress, and would otherwise leave a
// blank row behind in the store.
func (p *progress) summary(id string) store.Exercise {
	if st, ok := p.byExercise[id]; ok {
		return *st
	}
	return store.Exercise{ID: id}
}

// Passed reports whether the exercise has ever been solved.
func (p *progress) Passed(id string) bool {
	st, ok := p.byExercise[id]
	return ok && st.Passed()
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
// The practice screen shows this but does not enforce it: the dictionary's
// jump-to-an-exercise shortcut addresses any exercise in the library, and a
// hard gate would have to refuse it. Now that progress is durable the gate has
// something to remember, so enforcing it is a decision the browser can make
// rather than one the storage forbids.
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
