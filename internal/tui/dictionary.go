package tui

import (
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"bash-teacher/internal/content"
)

// dictRow is a flattened list row: either a category heading or a command.
type dictRow struct {
	heading string
	cmd     *content.Command
}

// dictionaryScreen is the M1 dictionary: a navigable, filterable index of every
// loaded command with a summary detail pane. The full entry — flag table,
// worked examples, "plays well with" jumps — lands in M2.
type dictionaryScreen struct {
	lib       *content.Library
	rows      []dictRow
	cursor    int
	offset    int
	filter    string
	filtering bool
}

func newDictionary(lib *content.Library) screen {
	d := &dictionaryScreen{lib: lib}
	d.rebuild()
	return d
}

// rebuild recomputes the visible rows from the current filter and parks the
// cursor on the first selectable command.
func (d *dictionaryScreen) rebuild() {
	d.rows = nil
	q := strings.ToLower(strings.TrimSpace(d.filter))
	for _, cat := range content.Categories {
		cmds := d.lib.CommandsByCategory(cat)
		var kept []*content.Command
		for _, c := range cmds {
			if q == "" || strings.Contains(strings.ToLower(c.Name), q) ||
				strings.Contains(strings.ToLower(c.Summary), q) {
				kept = append(kept, c)
			}
		}
		if len(kept) == 0 {
			continue
		}
		d.rows = append(d.rows, dictRow{heading: content.CategoryTitles[cat]})
		for _, c := range kept {
			d.rows = append(d.rows, dictRow{cmd: c})
		}
	}
	if d.cursor >= len(d.rows) {
		d.cursor = len(d.rows) - 1
	}
	if d.cursor < 0 {
		d.cursor = 0
	}
	d.snapToCommand(1)
}

// snapToCommand moves the cursor off a heading in the given direction, since
// headings are not selectable.
func (d *dictionaryScreen) snapToCommand(dir int) {
	for d.cursor >= 0 && d.cursor < len(d.rows) && d.rows[d.cursor].cmd == nil {
		d.cursor += dir
	}
	if d.cursor >= len(d.rows) {
		d.cursor = len(d.rows) - 1
		for d.cursor >= 0 && d.rows[d.cursor].cmd == nil {
			d.cursor--
		}
	}
	if d.cursor < 0 {
		d.cursor = 0
	}
}

func (d *dictionaryScreen) Capturing() bool { return d.filtering }

func (d *dictionaryScreen) Help() []key.Binding {
	if d.filtering {
		return []key.Binding{Keys.Choose, Keys.Back}
	}
	return []key.Binding{Keys.Up, Keys.Down, Keys.Filter, Keys.Back, Keys.Help, Keys.Quit}
}

func (d *dictionaryScreen) Update(a *App, msg tea.Msg) (screen, tea.Cmd) {
	km, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return d, nil
	}
	if d.filtering {
		switch km.String() {
		case "esc":
			d.filtering, d.filter = false, ""
			d.rebuild()
		case "enter":
			d.filtering = false
		case "backspace":
			if d.filter != "" {
				d.filter = d.filter[:len(d.filter)-1]
				d.rebuild()
			}
		default:
			if km.Text != "" {
				d.filter += km.Text
				d.rebuild()
			}
		}
		return d, nil
	}

	switch {
	case key.Matches(km, Keys.Filter):
		d.filtering = true
	case key.Matches(km, Keys.Up):
		if d.cursor > 0 {
			d.cursor--
			d.snapToCommand(-1)
		}
	case key.Matches(km, Keys.Down):
		if d.cursor < len(d.rows)-1 {
			d.cursor++
			d.snapToCommand(1)
		}
	}
	return d, nil
}

func (d *dictionaryScreen) Body(a *App, width, height int) string {
	t := a.Theme
	listWidth := width / 3
	if listWidth < 20 {
		listWidth = 20
	}
	if listWidth > 32 {
		listWidth = 32
	}

	list := d.listPane(a, listWidth, height-1)
	detail := d.detailPane(a, width-listWidth-4, height-1)

	head := t.Dim.Render("  " + d.filterLine())
	return head + "\n" + lipgloss.JoinHorizontal(lipgloss.Top,
		lipgloss.NewStyle().Width(listWidth).Render(list),
		t.Faint.Render(verticalRule(height-1)),
		lipgloss.NewStyle().Width(width-listWidth-3).Render(detail),
	)
}

func (d *dictionaryScreen) filterLine() string {
	switch {
	case d.filtering:
		return "search: " + d.filter + "▏"
	case d.filter != "":
		return "search: " + d.filter + "   (esc to clear)"
	default:
		return "/ to search · " + plural(len(d.lib.Commands), "command", "commands")
	}
}

func (d *dictionaryScreen) listPane(a *App, width, height int) string {
	t := a.Theme
	d.offset = scrollTo(d.cursor, d.offset, height)

	var b strings.Builder
	for i := d.offset; i < len(d.rows) && i < d.offset+height; i++ {
		r := d.rows[i]
		switch {
		case r.cmd == nil:
			b.WriteString(t.Faint.Render(truncate(" "+strings.ToUpper(r.heading), width)))
		case i == d.cursor:
			b.WriteString(t.Selected.Render(pad(" "+r.cmd.Name, width)))
		default:
			b.WriteString(t.Body.Render(truncate("  "+r.cmd.Name, width)))
		}
		b.WriteString("\n")
	}
	if len(d.rows) == 0 {
		b.WriteString(t.Dim.Render("  no matches"))
	}
	return b.String()
}

func (d *dictionaryScreen) detailPane(a *App, width, height int) string {
	t := a.Theme
	if d.cursor >= len(d.rows) || d.rows[d.cursor].cmd == nil {
		return ""
	}
	// The pane is clipped rather than scrolled for now; M2 gives it its own
	// viewport so long entries are fully reachable.
	c := d.rows[d.cursor].cmd
	w := width - 2

	var b strings.Builder
	b.WriteString(" " + t.Title.Render(c.Name) + "  " + t.Dim.Render(c.Summary) + "\n\n")
	b.WriteString(indent(wrap(c.Purpose, w), 1) + "\n\n")
	b.WriteString(" " + t.Faint.Render("SYNOPSIS") + "\n")
	b.WriteString(" " + t.Code.Render(truncate(c.Synopsis, w)) + "\n\n")

	if len(c.Flags) > 0 {
		b.WriteString(" " + t.Faint.Render("FLAGS") + "\n")
		for _, f := range c.Flags {
			b.WriteString(" " + t.Accent.Render(pad(f.Flag, 10)) + t.Dim.Render(truncate(f.Gloss, w-11)) + "\n")
		}
		b.WriteString("\n")
	}
	if len(c.Examples) > 0 {
		b.WriteString(" " + t.Faint.Render("EXAMPLES") + "\n")
		for _, ex := range c.Examples {
			b.WriteString(" " + t.Code.Render(truncate(ex.Cmd, w)) + "\n")
			b.WriteString(" " + t.Dim.Render(truncate(ex.Caption, w)) + "\n")
		}
		b.WriteString("\n")
	}
	if len(c.PlaysWellWith) > 0 {
		b.WriteString(" " + t.Faint.Render("PLAYS WELL WITH ") + t.Body.Render(strings.Join(c.PlaysWellWith, ", ")) + "\n")
	}
	if n := len(d.lib.CardsFor(c.ID)); n > 0 {
		b.WriteString(" " + t.Faint.Render("CARDS ") + t.Body.Render(plural(n, "card", "cards")) + "\n")
	}
	if n := len(d.lib.ExercisesUsing(c.ID)); n > 0 {
		b.WriteString(" " + t.Faint.Render("SEEN IN ") + t.Body.Render(plural(n, "exercise", "exercises")) + "\n")
	}
	return clip(b.String(), height)
}
