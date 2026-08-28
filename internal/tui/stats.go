package tui

import (
	"fmt"
	"sort"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"bash-teacher/internal/content"
)

// statsScreen reports on the library today and on the learner's progress once
// the store exists. Until then it shows content coverage, which is also the
// authoring view: it makes commands with no cards or no exercises obvious.
type statsScreen struct{ lib *content.Library }

func newStats(lib *content.Library) screen { return &statsScreen{lib: lib} }

func (s *statsScreen) Capturing() bool { return false }

func (s *statsScreen) Help() []key.Binding {
	return []key.Binding{Keys.Back, Keys.Help, Keys.Quit}
}

func (s *statsScreen) Update(a *App, msg tea.Msg) (screen, tea.Cmd) { return s, nil }

func (s *statsScreen) Body(a *App, width, height int) string {
	t := a.Theme

	var uncovered []string
	for _, c := range s.lib.Commands {
		if len(s.lib.CardsFor(c.ID)) == 0 && len(s.lib.ExercisesUsing(c.ID)) == 0 {
			uncovered = append(uncovered, c.Name)
		}
	}
	sort.Strings(uncovered)

	var levels [6]int
	for _, e := range s.lib.Exercises {
		if e.Level >= 1 && e.Level <= 5 {
			levels[e.Level]++
		}
	}

	coverage := []string{t.PanelTitle.Render("Coverage"), ""}
	for _, cat := range content.Categories {
		n := len(s.lib.CommandsByCategory(cat))
		if n == 0 {
			continue
		}
		coverage = append(coverage, row(t, 22, content.CategoryTitles[cat], fmt.Sprintf("%d", n)))
	}
	coverage = append(coverage, "",
		row(t, 22, "Drilled", fmt.Sprintf("%d/%d commands", len(s.lib.Commands)-len(uncovered), len(s.lib.Commands))))
	if len(uncovered) > 0 {
		coverage = append(coverage, t.Faint.Render("no card or exercise: "+strings.Join(uncovered, ", ")))
	}

	ladder := []string{t.PanelTitle.Render("Exercise ladder"), ""}
	for lvl := 1; lvl <= 5; lvl++ {
		bar := strings.Repeat("█", levels[lvl])
		ladder = append(ladder, t.Dim.Render(fmt.Sprintf("level %d ", lvl))+t.Accent.Render(bar)+
			t.Faint.Render(fmt.Sprintf(" %d", levels[lvl])))
	}

	progress := strings.Join([]string{
		t.PanelTitle.Render("Your progress"),
		"",
		t.Dim.Render("Retention, streaks, and mastery need"),
		t.Dim.Render("the progress store, due in M5–M6."),
	}, "\n")

	boxWidth := (width - 8) / 2
	if boxWidth < 24 {
		boxWidth = 24
	}
	left := t.Panel.Width(boxWidth).Render(strings.Join(coverage, "\n"))
	right := t.Panel.Width(boxWidth).Render(strings.Join(ladder, "\n") + "\n\n" + progress)
	return "\n" + indent(lipgloss.JoinHorizontal(lipgloss.Top, left, "  ", right), 2)
}
