package tui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"bash-teacher/internal/content"
)

// flashcardsScreen previews the card deck. Answer normalization and the
// FSRS-lite scheduler arrive in M5; for now it proves the deck loads and lets a
// learner page through cards front-then-back.
type flashcardsScreen struct {
	lib      *content.Library
	index    int
	revealed bool
}

func newFlashcards(lib *content.Library) screen { return &flashcardsScreen{lib: lib} }

func (f *flashcardsScreen) Capturing() bool { return false }

func (f *flashcardsScreen) Help() []key.Binding {
	return []key.Binding{Keys.Choose, Keys.Left, Keys.Right, Keys.Back, Keys.Help, Keys.Quit}
}

func (f *flashcardsScreen) Update(a *App, msg tea.Msg) (screen, tea.Cmd) {
	km, ok := msg.(tea.KeyPressMsg)
	if !ok || len(f.lib.Cards) == 0 {
		return f, nil
	}
	switch {
	case key.Matches(km, Keys.Choose):
		f.revealed = !f.revealed
	case key.Matches(km, Keys.Right), key.Matches(km, Keys.Down):
		f.index = (f.index + 1) % len(f.lib.Cards)
		f.revealed = false
	case key.Matches(km, Keys.Left), key.Matches(km, Keys.Up):
		f.index = (f.index - 1 + len(f.lib.Cards)) % len(f.lib.Cards)
		f.revealed = false
	}
	return f, nil
}

func (f *flashcardsScreen) Body(a *App, width, height int) string {
	t := a.Theme
	if len(f.lib.Cards) == 0 {
		return "\n  " + t.Dim.Render("No cards loaded.")
	}
	c := f.lib.Cards[f.index]

	back := t.Faint.Render("enter to reveal")
	if f.revealed {
		back = t.Code.Render(c.Back)
		if c.SelfGraded() {
			back = t.Body.Render(c.Back)
		}
		if len(c.Accepts) > 0 {
			back += "\n" + t.Faint.Render("also accepted: "+strings.Join(c.Accepts, " · "))
		}
	}

	card := strings.Join([]string{
		t.Faint.Render(fmt.Sprintf("card %d/%d · %s · %s",
			f.index+1, len(f.lib.Cards), c.Type, strings.Join(c.Commands, ", "))),
		"",
		wrap(c.Front, width-12),
		"",
		back,
	}, "\n")

	box := indent(t.Panel.Width(width-8).Render(card), 2)
	note := indent(t.Warn.Render("Scheduling and typed-answer grading arrive in M5; this is a plain deck walk."), 2)
	return "\n" + box + "\n\n" + note
}
