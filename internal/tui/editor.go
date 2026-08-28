package tui

import (
	"strings"
	"unicode"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// lineEditor is the single-line pipeline editor: readline-style motions, a
// history ring, and completion drawn from the dictionary.
//
// It is deliberately not bubbles' textinput. What a learner types here is
// shell text, so the editor has to know where the words are — completion,
// word motions, and the contextual dictionary lookup all ask "which word is
// the cursor in?", which a plain string does not answer.
type lineEditor struct {
	value []rune
	pos   int

	// history holds the accepted lines, oldest first. hpos indexes it while
	// walking back; at len(history) the editor is on the line being typed,
	// which is kept in draft so that walking back and forward again does not
	// lose it.
	history []string
	hpos    int
	draft   string

	// completions is the cycle the last tab press started, and compAt is the
	// rune offset where the completed word begins. A second tab takes the
	// next candidate rather than recomputing from the text tab just wrote.
	completions []string
	compIndex   int
	compAt      int
}

// Value returns the current line.
func (e *lineEditor) Value() string { return string(e.value) }

// Cursor returns the cursor's rune offset into the line.
func (e *lineEditor) Cursor() int { return e.pos }

// SetValue replaces the line and puts the cursor at its end.
func (e *lineEditor) SetValue(s string) {
	e.value = []rune(s)
	e.pos = len(e.value)
	e.resetCompletion()
}

// Clear empties the line without touching the history.
func (e *lineEditor) Clear() { e.SetValue("") }

// Accept records the current line in the history and clears the walk position.
// The line itself stays, since a learner usually wants to edit what they just
// ran rather than retype it.
func (e *lineEditor) Accept() {
	v := strings.TrimSpace(e.Value())
	e.hpos = len(e.history)
	if v == "" {
		return
	}
	if n := len(e.history); n > 0 && e.history[n-1] == v {
		return
	}
	e.history = append(e.history, v)
	e.hpos = len(e.history)
}

// Update applies one keystroke, reporting whether it was the editor's to
// handle. Keys it does not claim — the action chords — fall through to the
// screen.
func (e *lineEditor) Update(km tea.KeyPressMsg, complete func(line string, at int) []string) bool {
	if !key.Matches(km, Keys.Complete) {
		// Any key other than another tab ends the completion cycle.
		e.resetCompletion()
	}
	switch {
	case key.Matches(km, Keys.Complete):
		e.complete(complete)
	case key.Matches(km, Keys.CharLeft):
		e.moveTo(e.pos - 1)
	case key.Matches(km, Keys.CharRight):
		e.moveTo(e.pos + 1)
	case key.Matches(km, Keys.WordLeft):
		e.moveTo(e.wordStart())
	case key.Matches(km, Keys.WordRight):
		e.moveTo(e.wordEnd())
	case key.Matches(km, Keys.LineStart):
		e.pos = 0
	case key.Matches(km, Keys.LineEnd):
		e.pos = len(e.value)
	case key.Matches(km, Keys.HistPrev):
		e.walk(-1)
	case key.Matches(km, Keys.HistNext):
		e.walk(1)
	case key.Matches(km, Keys.DeleteWord):
		e.cut(e.wordStart(), e.pos)
	case key.Matches(km, Keys.KillToStart):
		e.cut(0, e.pos)
	case key.Matches(km, Keys.KillToEnd):
		e.cut(e.pos, len(e.value))
	case km.Code == tea.KeyBackspace:
		if e.pos > 0 {
			e.cut(e.pos-1, e.pos)
		}
	case km.Code == tea.KeyDelete:
		if e.pos < len(e.value) {
			e.cut(e.pos, e.pos+1)
		}
	case km.Text != "":
		e.insert(km.Text)
	default:
		return false
	}
	return true
}

func (e *lineEditor) insert(s string) {
	r := []rune(s)
	e.value = append(e.value[:e.pos], append(r, e.value[e.pos:]...)...)
	e.pos += len(r)
}

func (e *lineEditor) cut(from, to int) {
	from, to = clampInt(from, 0, len(e.value)), clampInt(to, 0, len(e.value))
	if from >= to {
		return
	}
	e.value = append(e.value[:from], e.value[to:]...)
	e.pos = from
}

func (e *lineEditor) moveTo(p int) { e.pos = clampInt(p, 0, len(e.value)) }

// wordStart returns the offset of the start of the word left of the cursor,
// skipping any whitespace directly behind it.
func (e *lineEditor) wordStart() int {
	i := e.pos
	for i > 0 && unicode.IsSpace(e.value[i-1]) {
		i--
	}
	for i > 0 && !unicode.IsSpace(e.value[i-1]) {
		i--
	}
	return i
}

// wordEnd returns the offset just past the word right of the cursor.
func (e *lineEditor) wordEnd() int {
	i := e.pos
	for i < len(e.value) && unicode.IsSpace(e.value[i]) {
		i++
	}
	for i < len(e.value) && !unicode.IsSpace(e.value[i]) {
		i++
	}
	return i
}

// walk moves through the history: -1 towards older lines, +1 towards newer.
func (e *lineEditor) walk(dir int) {
	if len(e.history) == 0 {
		return
	}
	if e.hpos == len(e.history) {
		e.draft = e.Value()
	}
	next := clampInt(e.hpos+dir, 0, len(e.history))
	if next == e.hpos {
		return
	}
	e.hpos = next
	if e.hpos == len(e.history) {
		e.SetValue(e.draft)
		return
	}
	e.SetValue(e.history[e.hpos])
}

// complete fills in the word under the cursor from the candidates the screen
// supplies, cycling through them on repeated presses.
func (e *lineEditor) complete(candidates func(line string, at int) []string) {
	if e.completions == nil {
		e.compAt = e.wordStartHere()
		e.completions = candidates(e.Value(), e.pos)
		e.compIndex = -1
		if len(e.completions) == 0 {
			e.completions = nil
			return
		}
	}
	e.compIndex = (e.compIndex + 1) % len(e.completions)
	pick := []rune(e.completions[e.compIndex])
	tail := append([]rune{}, e.value[e.pos:]...)
	e.value = append(append(append([]rune{}, e.value[:e.compAt]...), pick...), tail...)
	e.pos = e.compAt + len(pick)
}

// wordStartHere is wordStart without the skip-the-whitespace step: completion
// works on the word the cursor is inside, and after a space that word is
// empty and starts where the cursor is.
func (e *lineEditor) wordStartHere() int {
	i := e.pos
	for i > 0 && !unicode.IsSpace(e.value[i-1]) {
		i--
	}
	return i
}

func (e *lineEditor) resetCompletion() {
	e.completions, e.compIndex = nil, 0
}

// WordAt returns the word the cursor is inside, which is what a contextual
// dictionary lookup is about.
func (e *lineEditor) WordAt() string {
	if len(e.value) == 0 {
		return ""
	}
	start := e.wordStartHere()
	end := e.pos
	for end < len(e.value) && !unicode.IsSpace(e.value[end]) {
		end++
	}
	return strings.Trim(string(e.value[start:end]), "|<>;'\"")
}

// View renders the line into width cells with a block cursor, scrolling
// horizontally when the line is longer than the box.
func (e *lineEditor) View(a *App, width int, focused bool) string {
	t := a.Theme
	line := e.value
	// Keep the cursor in view: show the window of width-1 runes ending at it.
	off := 0
	if width > 1 && e.pos > width-1 {
		off = e.pos - (width - 1)
	}
	visible := line[clampInt(off, 0, len(line)):]
	if len(visible) > width {
		visible = visible[:width]
	}
	cur := e.pos - off

	var b strings.Builder
	for i, r := range visible {
		if focused && i == cur {
			b.WriteString(t.Selected.Render(string(r)))
			continue
		}
		b.WriteString(t.Body.Render(string(r)))
	}
	if focused && cur >= len(visible) {
		b.WriteString(t.Selected.Render(" "))
	}
	return b.String()
}
