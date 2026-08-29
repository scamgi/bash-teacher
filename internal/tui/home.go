package tui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"bash-teacher/internal/theme"
)

// homeItem is one entry in the main menu.
type homeItem struct {
	label  string
	hint   string
	target Screen
	digit  string
}

var homeItems = []homeItem{
	{"Dictionary", "browse every command, its flags, and what it pipes into", ScreenDictionary, "1"},
	{"Practice", "build pipelines against real files and run them", ScreenPractice, "2"},
	{"Flashcards", "short recall drills, scheduled for retention", ScreenFlashcards, "3"},
	{"Stats", "what you have learned and what is due", ScreenStats, "4"},
}

type homeScreen struct{ cursor int }

func newHome() screen { return &homeScreen{} }

func (h *homeScreen) Capturing() bool { return false }

func (h *homeScreen) Help() []key.Binding {
	return []key.Binding{Keys.Up, Keys.Down, Keys.Choose, Keys.Help, Keys.Quit}
}

func (h *homeScreen) Update(a *App, msg tea.Msg) (screen, tea.Cmd) {
	km, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return h, nil
	}
	switch {
	case key.Matches(km, Keys.Up):
		if h.cursor > 0 {
			h.cursor--
		}
	case key.Matches(km, Keys.Down):
		if h.cursor < len(homeItems)-1 {
			h.cursor++
		}
	case key.Matches(km, Keys.Choose):
		return h, Navigate(homeItems[h.cursor].target)
	}
	return h, nil
}

func (h *homeScreen) Body(a *App, width, height int) string {
	t := a.Theme
	var b strings.Builder

	b.WriteString("\n")
	b.WriteString("  " + t.Subtitle.Render("Learn the commands. Learn to compose them.") + "\n\n")

	for i, it := range homeItems {
		marker := "  "
		label := t.Body.Render(pad(it.label, 13))
		if i == h.cursor {
			marker = t.Accent.Render("▸ ")
			label = t.Selected.Render(pad(it.label, 13))
		}
		line := "  " + marker + t.Faint.Render(it.digit+" ") + label + " " + t.Dim.Render(it.hint)
		b.WriteString(truncate(line, width) + "\n")
	}

	b.WriteString("\n")
	b.WriteString(h.summary(a, width))
	b.WriteString("\n\n  " + truncate(sandboxNotice(a), width-2))
	return b.String()
}

// sandboxNotice states how much confinement exercises will get. SPEC §6.2
// asks for a one-time banner when there is none; this line is shown on Home
// every time instead, because there is nowhere to record "already seen" until
// the store lands, and a standing line is harder to miss than a banner that
// shows once. Colour is never the only signal: the unconfined cases carry a
// ⚠ as well.
func sandboxNotice(a *App) string {
	t := a.Theme
	if a.Runner == nil {
		return t.Faint.Render("sandbox   not configured")
	}
	switch {
	case a.Runner.NoExec():
		return t.Warn.Render("⚠ --no-exec: pipelines are checked but never executed.")
	case !a.Runner.Sandbox().Confines():
		return t.Warn.Render("⚠ Running without an OS sandbox — exercises execute with your normal user permissions.")
	}
	return t.Faint.Render("sandbox   ") + t.Dim.Render(a.Runner.Sandbox().Describe())
}

// summary shows today's review load beside what the loaded library contains.
//
// The load is durable: the scheduler is restored from the progress store at
// startup, so the streak is a real count of days. When there is no store the
// streak is qualified as this session's rather than shown as a number that
// would quietly be a lie.
func (h *homeScreen) summary(a *App, width int) string {
	t := a.Theme
	p := a.SRS.Params()
	due, unseen := a.DueCards(), a.UnseenCards()
	newRoom := min(unseen, max(0, p.NewPerDay-a.SRS.NewToday(a.Now())))

	streak := plural(a.SRS.Streak(a.Now()), "day", "days")
	switch {
	case a.SRS.Streak(a.Now()) == 0:
		streak = "not started today"
	case !a.Persisting():
		streak += " (this session)"
	}

	today := strings.Join([]string{
		t.PanelTitle.Render("Today"),
		"",
		row(t, 12, "Cards due", fmt.Sprintf("%d", due)),
		row(t, 12, "New cards", fmt.Sprintf("%d of %d unseen", newRoom, unseen)),
		row(t, 12, "Reviewed", fmt.Sprintf("%d", a.SRS.ReviewsToday(a.Now()))),
		row(t, 12, "Streak", streak),
		row(t, 12, "Passed", passedLine(a)),
	}, "\n")

	library := strings.Join([]string{
		t.PanelTitle.Render("Library"),
		"",
		row(t, 12, "Commands", fmt.Sprintf("%d", len(a.Lib.Commands))),
		row(t, 12, "Exercises", fmt.Sprintf("%d in %d tracks", len(a.Lib.Exercises), len(a.Lib.Tracks))),
		row(t, 12, "Flashcards", fmt.Sprintf("%d", len(a.Lib.Cards))),
	}, "\n")

	boxWidth := (width - 8) / 2
	if boxWidth < 20 {
		boxWidth = 20
	}
	left := t.Panel.Width(boxWidth).Render(today)
	right := t.Panel.Width(boxWidth).Render(library)
	return indent(lipgloss.JoinHorizontal(lipgloss.Top, left, "  ", right), 2)
}

// row formats a label/value pair, padding the label to a fixed column so the
// values line up down a panel.
func row(t *theme.Theme, width int, label, value string) string {
	return t.Dim.Render(pad(label, width)) + t.Body.Render(value)
}

// passedLine reports the exercise count, qualified as this session's when
// nothing is being saved.
func passedLine(a *App) string {
	n := a.PassedExercises()
	if a.Persisting() {
		return fmt.Sprintf("%d of %d exercises", n, len(a.Lib.Exercises))
	}
	return fmt.Sprintf("%d this session", n)
}

// shortPersistenceNote is the one-phrase form of persistenceNote, for a panel
// too narrow for the sentence. It is empty when progress is being saved, since
// the ordinary case needs no footnote at all.
func shortPersistenceNote(a *App) string {
	switch {
	case a.StoreError() != nil:
		return "not saving progress"
	case a.Store == nil:
		return "this session only"
	default:
		return ""
	}
}

// persistenceNote is the footnote the summary screens carry about whether any
// of this is being remembered. It names the failure when there was one: a
// learner whose history stopped being written should hear it from the app
// rather than discover it on the next launch.
func persistenceNote(a *App) string {
	if err := a.StoreError(); err != nil {
		return "Not saving progress: " + err.Error()
	}
	if a.Store == nil {
		return "This session only — no progress store is attached."
	}
	return "Saved to " + a.Store.Path()
}
