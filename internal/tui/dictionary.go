package tui

import (
	"fmt"
	"sort"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"bash-teacher/internal/content"
	"bash-teacher/internal/fuzzy"
)

// dictRow is a flattened list row: either a category heading or a command.
// When a filter is active the headings are dropped and the commands are ranked
// by score instead, which is what a search box is for.
type dictRow struct {
	heading string
	cmd     *content.Command
	// hits are the positions in the command's name that matched the filter.
	hits []int
}

// dictFocus is which pane takes the arrow keys.
type dictFocus int

const (
	focusList dictFocus = iota
	focusDetail
)

// dictItemKind is what pressing enter on a detail item does.
type dictItemKind int

const (
	itemExample  dictItemKind = iota // copy it to the clipboard
	itemRelated                      // jump to that dictionary entry
	itemExercise                     // open it in Practice
)

// dictItem is an actionable line in the detail pane.
type dictItem struct {
	kind dictItemKind
	line int    // its row within the detail body, for scrolling into view
	text string // the example's command line, or a command or exercise id
}

// dictionaryScreen is the two-pane dictionary: a filterable index on the left
// and a scrollable entry on the right whose examples, related commands, and
// exercises are all actionable.
type dictionaryScreen struct {
	lib *content.Library

	rows      []dictRow
	cursor    int
	offset    int
	filter    string
	filtering bool

	focus  dictFocus
	detail viewport.Model
	items  []dictItem
	item   int
	// shownID, shownWidth, and shownItem together decide whether the cached
	// detail render is still valid; the focus caret is part of the content, so
	// moving the item cursor invalidates it too.
	shownID    string
	shownWidth int
	shownItem  int
	shownFocus dictFocus

	// history is the stack of command ids visited by jumping through
	// "plays well with", so backspace can walk back out.
	history []string
}

func newDictionary(lib *content.Library) screen {
	d := &dictionaryScreen{lib: lib, detail: viewport.New()}
	d.rebuild()
	return d
}

func (d *dictionaryScreen) Capturing() bool { return d.filtering }

func (d *dictionaryScreen) Help() []key.Binding {
	switch {
	case d.filtering:
		return []key.Binding{Keys.Choose, Keys.Back}
	case d.focus == focusDetail:
		return []key.Binding{Keys.Up, Keys.Down, Keys.Act, Keys.Copy, Keys.Practise, Keys.Drill, Keys.Left, Keys.Quit}
	default:
		return []key.Binding{Keys.Up, Keys.Down, Keys.Right, Keys.Filter, Keys.Practise, Keys.Drill, Keys.Back, Keys.Quit}
	}
}

// current returns the command under the list cursor.
func (d *dictionaryScreen) current() *content.Command {
	if d.cursor < 0 || d.cursor >= len(d.rows) {
		return nil
	}
	return d.rows[d.cursor].cmd
}

// rebuild recomputes the visible rows. With no filter the list is grouped by
// category in dictionary order; with one it becomes a flat ranked list, because
// ranking is the whole point of a search box.
func (d *dictionaryScreen) rebuild() {
	prev := d.current()
	d.rows = nil
	q := strings.TrimSpace(d.filter)

	if q == "" {
		for _, cat := range content.Categories {
			cmds := d.lib.CommandsByCategory(cat)
			if len(cmds) == 0 {
				continue
			}
			d.rows = append(d.rows, dictRow{heading: content.CategoryTitles[cat]})
			for _, c := range cmds {
				d.rows = append(d.rows, dictRow{cmd: c})
			}
		}
	} else {
		type ranked struct {
			row   dictRow
			score int
		}
		var hits []ranked
		for _, c := range d.lib.Commands {
			m, field, ok := fuzzy.Best(q, c.Name, c.Summary)
			if !ok {
				continue
			}
			row := dictRow{cmd: c}
			if field == 0 {
				row.hits = m.Positions
			}
			hits = append(hits, ranked{row, m.Score})
		}
		sort.SliceStable(hits, func(i, j int) bool {
			if hits[i].score != hits[j].score {
				return hits[i].score > hits[j].score
			}
			return hits[i].row.cmd.Name < hits[j].row.cmd.Name
		})
		for _, h := range hits {
			d.rows = append(d.rows, h.row)
		}
	}

	// While a search is active the cursor always sits on the best match, so
	// that typing more characters narrows towards what you meant. A command
	// can match through its summary alone, so keeping the old selection here
	// would strand the cursor on a barely-relevant entry.
	//
	// With no filter — including just after clearing one — the previously
	// selected command is kept instead, so esc does not lose your place.
	d.cursor = 0
	if q == "" && prev != nil {
		for i, r := range d.rows {
			if r.cmd == prev {
				d.cursor = i
				break
			}
		}
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

// selectCommand moves the list cursor to a command by id, clearing any filter
// that would hide it. It is how a jump from "plays well with" lands.
func (d *dictionaryScreen) selectCommand(id string) bool {
	if _, ok := d.lib.Command(id); !ok {
		return false
	}
	if d.filter != "" {
		d.filter = ""
		d.rebuild()
	}
	for i, r := range d.rows {
		if r.cmd != nil && r.cmd.ID == id {
			d.cursor = i
			d.item = 0
			d.shownID = "" // force the detail pane to rebuild
			return true
		}
	}
	return false
}

func (d *dictionaryScreen) Update(a *App, msg tea.Msg) (screen, tea.Cmd) {
	km, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return d, nil
	}
	if d.filtering {
		d.updateFilter(km)
		return d, nil
	}
	if d.focus == focusDetail {
		cmd := d.updateDetail(km)
		return d, cmd
	}
	cmd := d.updateList(km)
	return d, cmd
}

func (d *dictionaryScreen) updateFilter(km tea.KeyPressMsg) {
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
}

func (d *dictionaryScreen) updateList(km tea.KeyPressMsg) tea.Cmd {
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
	case key.Matches(km, Keys.Right), key.Matches(km, Keys.Choose):
		if len(d.items) > 0 {
			d.focus = focusDetail
			d.item = 0
			d.scrollToItem()
		}
	case key.Matches(km, Keys.Practise):
		return d.openPractice()
	case key.Matches(km, Keys.Drill):
		return d.openCards()
	case key.Matches(km, Keys.Pop):
		return d.popHistory()
	}
	return nil
}

func (d *dictionaryScreen) updateDetail(km tea.KeyPressMsg) tea.Cmd {
	switch {
	case key.Matches(km, Keys.Left), key.Matches(km, Keys.Back):
		d.focus = focusList
	case key.Matches(km, Keys.Up):
		if d.item > 0 {
			d.item--
			d.scrollToItem()
		}
	case key.Matches(km, Keys.Down):
		if d.item < len(d.items)-1 {
			d.item++
			d.scrollToItem()
		}
	case key.Matches(km, Keys.PageUp):
		d.detail.PageUp()
	case key.Matches(km, Keys.PageDown):
		d.detail.PageDown()
	case key.Matches(km, Keys.Choose):
		return d.act()
	case key.Matches(km, Keys.Copy):
		return d.copyExample()
	case key.Matches(km, Keys.Practise):
		return d.openPractice()
	case key.Matches(km, Keys.Drill):
		return d.openCards()
	case key.Matches(km, Keys.Pop):
		return d.popHistory()
	}
	return nil
}

// act performs the obvious thing for the focused detail item.
func (d *dictionaryScreen) act() tea.Cmd {
	if d.item < 0 || d.item >= len(d.items) {
		return nil
	}
	it := d.items[d.item]
	switch it.kind {
	case itemExample:
		return d.copyExample()
	case itemRelated:
		if c := d.current(); c != nil {
			d.history = append(d.history, c.ID)
		}
		if !d.selectCommand(it.text) {
			return nil
		}
		d.focus = focusList
		return flash(fmt.Sprintf("jumped to %s · backspace to go back", it.text))
	case itemExercise:
		return openExercise(it.text)
	}
	return nil
}

func (d *dictionaryScreen) copyExample() tea.Cmd {
	if d.item < 0 || d.item >= len(d.items) || d.items[d.item].kind != itemExample {
		return flash("select an example first to copy it")
	}
	cmd := d.items[d.item].text
	return tea.Batch(tea.SetClipboard(cmd), flash("copied: "+cmd))
}

func (d *dictionaryScreen) openPractice() tea.Cmd {
	c := d.current()
	if c == nil {
		return nil
	}
	exs := d.lib.ExercisesUsing(c.ID)
	if len(exs) == 0 {
		return flash("no exercise uses " + c.Name + " yet")
	}
	return openExercise(exs[0].ID)
}

func (d *dictionaryScreen) openCards() tea.Cmd {
	c := d.current()
	if c == nil {
		return nil
	}
	if len(d.lib.CardsFor(c.ID)) == 0 {
		return flash("no flashcard drills " + c.Name + " yet")
	}
	return showCards(c.ID)
}

func (d *dictionaryScreen) popHistory() tea.Cmd {
	if len(d.history) == 0 {
		return nil
	}
	id := d.history[len(d.history)-1]
	d.history = d.history[:len(d.history)-1]
	d.selectCommand(id)
	return flash("back to " + id)
}

// scrollToItem keeps the focused item inside the viewport.
func (d *dictionaryScreen) scrollToItem() {
	if d.item < 0 || d.item >= len(d.items) {
		return
	}
	d.detail.EnsureVisible(d.items[d.item].line, 0, 0)
}

func (d *dictionaryScreen) Body(a *App, width, height int) string {
	t := a.Theme
	listWidth := clampInt(width/3, 22, 34)
	detailWidth := width - listWidth - 3
	bodyHeight := height - 1

	d.buildDetail(a, detailWidth)
	d.detail.SetWidth(detailWidth)
	d.detail.SetHeight(bodyHeight)

	head := t.Dim.Render("  " + d.filterLine())
	return head + "\n" + lipgloss.JoinHorizontal(lipgloss.Top,
		lipgloss.NewStyle().Width(listWidth).Render(d.listPane(a, listWidth, bodyHeight)),
		t.Faint.Render(verticalRule(bodyHeight)),
		lipgloss.NewStyle().Width(detailWidth).Render(d.detail.View()),
	)
}

func (d *dictionaryScreen) filterLine() string {
	switch {
	case d.filtering:
		return "search: " + d.filter + "▏"
	case d.filter != "":
		return fmt.Sprintf("search: %s   %s   / to edit · esc to clear", d.filter, plural(len(d.rows), "match", "matches"))
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
			marker := " "
			if d.focus == focusList {
				marker = "▸"
			}
			b.WriteString(t.Accent.Render(marker) + t.Selected.Render(pad(" "+r.cmd.Name, width-1)))
		default:
			b.WriteString(" " + t.Body.Render(truncate(" "+highlighted(a, r.cmd.Name, r.hits), width-2)))
		}
		b.WriteString("\n")
	}
	if len(d.rows) == 0 {
		b.WriteString(t.Dim.Render("  no matches"))
	}
	return b.String()
}

// highlighted renders a name with its matched characters picked out.
func highlighted(a *App, s string, hits []int) string {
	if len(hits) == 0 {
		return s
	}
	var b strings.Builder
	for _, h := range fuzzy.Split(s, hits) {
		if h.Matched {
			b.WriteString(a.Theme.Accent.Render(h.Text))
			continue
		}
		b.WriteString(h.Text)
	}
	return b.String()
}

// buildDetail renders the current entry into the viewport and records where
// each actionable item landed. It is a no-op when neither the command nor the
// width has changed, so scrolling does not re-render on every frame.
func (d *dictionaryScreen) buildDetail(a *App, width int) {
	c := d.current()
	if c == nil {
		d.detail.SetContent("")
		d.items = nil
		d.shownID = ""
		return
	}
	if d.shownID == c.ID && d.shownWidth == width && d.shownItem == d.item && d.shownFocus == d.focus {
		return
	}
	d.shownID, d.shownWidth, d.shownItem, d.shownFocus = c.ID, width, d.item, d.focus
	d.items = nil

	t := a.Theme
	w := width - 2
	var lines []string
	add := func(s string) { lines = append(lines, " "+s) }
	item := func(kind dictItemKind, text, rendered string) {
		d.items = append(d.items, dictItem{kind: kind, line: len(lines), text: text})
		lines = append(lines, rendered)
	}
	section := func(name string) {
		add("")
		add(t.Faint.Render(name))
	}

	add(t.Title.Render(c.Name) + "  " + t.Dim.Render(c.Summary))
	add("")
	for _, ln := range strings.Split(wrap(c.Purpose, w), "\n") {
		add(t.Body.Render(ln))
	}

	section("SYNOPSIS")
	add(t.Code.Render(truncate(c.Synopsis, w)))

	if len(c.Flags) > 0 {
		section("FLAGS")
		fw := 0
		for _, f := range c.Flags {
			if n := lipgloss.Width(f.Flag); n > fw {
				fw = n
			}
		}
		fw = clampInt(fw+2, 6, 18)
		for _, f := range c.Flags {
			for i, ln := range strings.Split(wrap(f.Gloss, max(12, w-fw)), "\n") {
				// The flag is styled, so its column is padded from the raw
				// width rather than with pad(), which would count escapes.
				label := strings.Repeat(" ", fw)
				if i == 0 {
					gap := fw - lipgloss.Width(f.Flag)
					if gap > 0 {
						label = t.Accent.Render(f.Flag) + strings.Repeat(" ", gap)
					} else {
						// A flag too wide for the column gets a line of its
						// own, so the glosses stay in one column instead of
						// stair-stepping across the pane.
						add(t.Accent.Render(f.Flag))
					}
				}
				add(label + t.Dim.Render(ln))
			}
		}
	}

	section("EXAMPLES")
	for _, ex := range c.Examples {
		marker := "  "
		item(itemExample, ex.Cmd, " "+marker+t.Code.Render(truncate(ex.Cmd, w-2)))
		for _, ln := range strings.Split(wrap(ex.Caption, w-4), "\n") {
			add("    " + t.Dim.Render(ln))
		}
	}

	if len(c.Gotchas) > 0 {
		section("GOTCHAS")
		for _, g := range c.Gotchas {
			for i, ln := range strings.Split(wrap(g, w-3), "\n") {
				prefix := t.Warn.Render(" · ")
				if i > 0 {
					prefix = "   "
				}
				add(prefix + t.Body.Render(ln))
			}
		}
	}

	if len(c.PlaysWellWith) > 0 {
		section("PLAYS WELL WITH")
		for _, id := range c.PlaysWellWith {
			rel, ok := d.lib.Command(id)
			if !ok {
				continue
			}
			item(itemRelated, id, "   "+t.Accent.Render(pad(rel.Name, 10))+t.Dim.Render(truncate(rel.Summary, w-13)))
		}
	}

	if exs := d.lib.ExercisesUsing(c.ID); len(exs) > 0 {
		section("SEEN IN")
		for _, ex := range exs {
			item(itemExercise, ex.ID, "   "+t.Subtitle.Render(pad(ex.Title, 30))+
				t.Faint.Render(fmt.Sprintf("level %d · %s", ex.Level, ex.Track)))
		}
	}

	if n := len(d.lib.CardsFor(c.ID)); n > 0 {
		section("FLASHCARDS")
		add("   " + t.Body.Render(plural(n, "card", "cards")) + t.Faint.Render("   press f to drill them"))
	}

	if d.item >= len(d.items) {
		d.item = 0
		d.shownItem = 0
	}
	// The focus caret is drawn into the content rather than overlaid, so that
	// the viewport's own scrolling and wrapping see it.
	if d.focus == focusDetail && d.item < len(d.items) {
		ln := d.items[d.item].line
		lines[ln] = a.Theme.Accent.Render("▸") + strings.TrimPrefix(lines[ln], " ")
	}
	d.detail.SetContent(strings.Join(lines, "\n"))
}
