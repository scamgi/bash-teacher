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
	deck     []*content.Card
	index    int
	revealed bool
	// filter is the command id the deck is restricted to, empty for the whole
	// library. It is set by a jump from the dictionary.
	filter string
}

func newFlashcards(lib *content.Library) screen {
	return &flashcardsScreen{lib: lib, deck: lib.Cards}
}

// ShowCommand narrows the deck to the cards drilling one command. It reports
// false when that command has no cards, so the caller can say so rather than
// opening an empty deck.
func (f *flashcardsScreen) ShowCommand(commandID string) bool {
	cards := f.lib.CardsFor(commandID)
	if len(cards) == 0 {
		return false
	}
	f.deck, f.filter, f.index, f.revealed = cards, commandID, 0, false
	return true
}

// clearFilter restores the full deck.
func (f *flashcardsScreen) clearFilter() {
	f.deck, f.filter, f.index, f.revealed = f.lib.Cards, "", 0, false
}

func (f *flashcardsScreen) Capturing() bool { return false }

func (f *flashcardsScreen) Help() []key.Binding {
	b := []key.Binding{Keys.Choose, Keys.Left, Keys.Right, Keys.Back, Keys.Help, Keys.Quit}
	if f.filter != "" {
		b = append([]key.Binding{Keys.Pop}, b...)
	}
	return b
}

// deckLine is the header above a card: where you are in the deck, what kind of
// card it is, and which commands it drills.
func (f *flashcardsScreen) deckLine(c *content.Card) string {
	scope := "all cards"
	if f.filter != "" {
		scope = "filtered to " + f.filter
	}
	return fmt.Sprintf("card %d/%d · %s · %s · %s",
		f.index+1, len(f.deck), scope, c.Type, strings.Join(c.Commands, ", "))
}

func (f *flashcardsScreen) Update(a *App, msg tea.Msg) (screen, tea.Cmd) {
	km, ok := msg.(tea.KeyPressMsg)
	if !ok || len(f.deck) == 0 {
		return f, nil
	}
	switch {
	case key.Matches(km, Keys.Choose):
		f.revealed = !f.revealed
	case key.Matches(km, Keys.Right), key.Matches(km, Keys.Down):
		f.index = (f.index + 1) % len(f.deck)
		f.revealed = false
	case key.Matches(km, Keys.Left), key.Matches(km, Keys.Up):
		f.index = (f.index - 1 + len(f.deck)) % len(f.deck)
		f.revealed = false
	case key.Matches(km, Keys.Pop):
		if f.filter != "" {
			f.clearFilter()
			return f, flash("showing the whole deck again")
		}
	}
	return f, nil
}

func (f *flashcardsScreen) Body(a *App, width, height int) string {
	t := a.Theme
	if len(f.deck) == 0 {
		return "\n  " + t.Dim.Render("No cards loaded.")
	}
	c := f.deck[f.index]

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
		t.Faint.Render(f.deckLine(c)),
		"",
		wrap(c.Front, width-12),
		"",
		back,
	}, "\n")

	box := indent(t.Panel.Width(width-8).Render(card), 2)
	note := indent(t.Warn.Render("Scheduling and typed-answer grading arrive in M5; this is a plain deck walk."), 2)
	return "\n" + box + "\n\n" + note
}
