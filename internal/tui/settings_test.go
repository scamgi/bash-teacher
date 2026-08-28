package tui

import (
	"strings"
	"testing"
	"time"

	"bash-teacher/internal/content"
	"bash-teacher/internal/srs"
)

// TestSessionSizeFollowsTheSettings checks that the [review] table reaches the
// deck: a smaller session size is what a sitting is cut to, not a number that
// is merely reported.
func TestSessionSizeFollowsTheSettings(t *testing.T) {
	p := srs.Defaults()
	p.SessionSize = 3
	a := newTestApp(t, 100, 30, WithParams(p))

	press(a, "3")
	press(a, "enter")

	if got := len(cardsOf(a).queue); got != 3 {
		t.Errorf("session holds %d cards, want the configured 3", got)
	}
}

// answerLate walks one card to its verdict with a known elapsed time, which is
// how these tests get a number on the screen that does not depend on how fast
// the test ran.
func answerLate(t *testing.T, a *App, took time.Duration) string {
	t.Helper()
	c := firstCardOfType(t, a, content.CardRecall)
	f := reviewOne(a, c)
	f.shown = time.Now().Add(-took)
	typeIn(a, "", c.Back)
	press(a, "enter")
	if f.phase != phaseGrade {
		t.Fatalf("enter should submit the answer, phase = %v", f.phase)
	}
	return view(a)
}

// TestTimerNudgesWhenItIsOn is the default: a correct answer that ran past the
// soft target says so, without the card being marked down for it.
func TestTimerNudgesWhenItIsOn(t *testing.T) {
	a := newTestApp(t, 100, 30)
	out := answerLate(t, a, 12*time.Second)

	if !strings.Contains(out, "12s") {
		t.Errorf("the answer's time should be shown:\n%s", out)
	}
	if !strings.Contains(out, "you are aiming for") {
		t.Errorf("an answer past the soft target should be nudged:\n%s", out)
	}
	if !strings.Contains(out, "✓ correct") {
		t.Errorf("a slow right answer is still right:\n%s", out)
	}
}

// TestTimerCanBeTurnedOff checks that a learner who does not want to be timed
// is not shown a time at all. Being told the number and then told it did not
// count is the outcome the setting exists to avoid.
func TestTimerCanBeTurnedOff(t *testing.T) {
	a := newTestApp(t, 100, 30, WithTimer(false))
	out := answerLate(t, a, 12*time.Second)

	if strings.Contains(out, "12s") || strings.Contains(out, "you are aiming for") {
		t.Errorf("the timer is off, so nothing about timing should be on screen:\n%s", out)
	}
	if !strings.Contains(out, "✓ correct") {
		t.Errorf("the verdict is still shown with the timer off:\n%s", out)
	}
}

// TestParamsDoNotDependOnOptionOrder pins what makes WithParams safe to pass
// alongside WithStore: parameters are a preference, so applying them before or
// after a restore has to land in the same place.
func TestParamsDoNotDependOnOptionOrder(t *testing.T) {
	p := srs.Defaults()
	p.NewPerDay = 2

	first := newTestApp(t, 100, 30, WithParams(p), WithTimer(false))
	second := newTestApp(t, 100, 30, WithTimer(false), WithParams(p))

	if first.SRS.Params() != second.SRS.Params() {
		t.Errorf("option order changed the parameters:\n %+v\n %+v",
			first.SRS.Params(), second.SRS.Params())
	}
}
