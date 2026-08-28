package tui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"bash-teacher/internal/content"
)

// practiceRow is a flattened row: a track heading or an exercise.
type practiceRow struct {
	track string
	ex    *content.Exercise
}

// practiceScreen lists the exercise library by track and shows the task for the
// selected exercise. The sandboxed runner behind it is built (M3); the pipeline
// editor that feeds it arrives with M4, so for now this screen is the exercise
// browser.
type practiceScreen struct {
	lib    *content.Library
	rows   []practiceRow
	cursor int
	offset int
}

func newPractice(lib *content.Library) screen {
	p := &practiceScreen{lib: lib}
	for _, t := range lib.Tracks {
		p.rows = append(p.rows, practiceRow{track: t.Name})
		for _, e := range t.Exercises {
			p.rows = append(p.rows, practiceRow{ex: e})
		}
	}
	p.cursor = 0
	p.moveToExercise(1)
	return p
}

func (p *practiceScreen) moveToExercise(dir int) {
	for p.cursor >= 0 && p.cursor < len(p.rows) && p.rows[p.cursor].ex == nil {
		p.cursor += dir
	}
	if p.cursor >= len(p.rows) {
		p.cursor = len(p.rows) - 1
	}
	if p.cursor < 0 {
		p.cursor = 0
	}
}

// OpenExercise moves the cursor to a given exercise, so that a jump from the
// dictionary lands on the task rather than at the top of the list. It reports
// false when the id is unknown, in which case the root model does not switch
// screens.
func (p *practiceScreen) OpenExercise(id string) bool {
	for i, r := range p.rows {
		if r.ex != nil && r.ex.ID == id {
			p.cursor = i
			return true
		}
	}
	return false
}

func (p *practiceScreen) Capturing() bool { return false }

func (p *practiceScreen) Help() []key.Binding {
	return []key.Binding{Keys.Up, Keys.Down, Keys.Back, Keys.Help, Keys.Quit}
}

func (p *practiceScreen) Update(a *App, msg tea.Msg) (screen, tea.Cmd) {
	km, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return p, nil
	}
	switch {
	case key.Matches(km, Keys.Up):
		if p.cursor > 0 {
			p.cursor--
			p.moveToExercise(-1)
		}
	case key.Matches(km, Keys.Down):
		if p.cursor < len(p.rows)-1 {
			p.cursor++
			p.moveToExercise(1)
		}
	}
	return p, nil
}

func (p *practiceScreen) Body(a *App, width, height int) string {
	t := a.Theme
	if len(p.rows) == 0 {
		return "\n  " + t.Dim.Render("No exercises loaded.")
	}

	listHeight := height / 2
	p.offset = scrollTo(p.cursor, p.offset, listHeight)

	var b strings.Builder
	b.WriteString("\n")
	for i := p.offset; i < len(p.rows) && i < p.offset+listHeight; i++ {
		r := p.rows[i]
		switch {
		case r.ex == nil:
			b.WriteString("  " + t.Faint.Render(strings.ToUpper(r.track)))
		case i == p.cursor:
			b.WriteString("  " + t.Accent.Render("▸ ") + t.Selected.Render(pad(r.ex.Title, 40)) + " " + t.Dim.Render(levelDots(r.ex.Level)))
		default:
			b.WriteString("    " + t.Body.Render(pad(r.ex.Title, 40)) + " " + t.Faint.Render(levelDots(r.ex.Level)))
		}
		b.WriteString("\n")
	}

	if p.cursor < len(p.rows) && p.rows[p.cursor].ex != nil {
		b.WriteString("\n" + p.detail(a, p.rows[p.cursor].ex, width-4))
	}
	return b.String()
}

func (p *practiceScreen) detail(a *App, e *content.Exercise, width int) string {
	t := a.Theme
	body := strings.Join([]string{
		t.PanelTitle.Render(e.Title) + "  " + t.Faint.Render(fmt.Sprintf("level %d · %s", e.Level, e.Track)),
		"",
		wrap(e.Prompt, width-4),
		"",
		t.Faint.Render("fixture ") + t.Code.Render(e.Fixture) +
			t.Faint.Render("   teaches ") + t.Body.Render(strings.Join(e.Teaches, ", ")),
		t.Warn.Render("The pipeline editor arrives in M4; the sandbox that will run it is ready."),
	}, "\n")
	return indent(t.Panel.Width(width).Render(body), 2)
}

// levelDots renders the 1–5 difficulty as filled and hollow dots.
func levelDots(level int) string {
	if level < 0 {
		level = 0
	}
	if level > 5 {
		level = 5
	}
	return strings.Repeat("●", level) + strings.Repeat("○", 5-level)
}
