package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// typeInto feeds a string to the editor one keystroke at a time.
func typeInto(e *lineEditor, s string) {
	for _, r := range s {
		e.Update(tea.KeyPressMsg{Code: r, Text: string(r)}, nil)
	}
}

// stroke builds a key press with a modifier, since the package already has a
// "key" name from the bubbles binding package.
func stroke(code rune, mod tea.KeyMod) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: code, Mod: mod}
}

func TestEditorInsertsAndMoves(t *testing.T) {
	var e lineEditor
	typeInto(&e, "sort -n f")
	if e.Value() != "sort -n f" {
		t.Fatalf("got %q", e.Value())
	}
	e.Update(stroke(tea.KeyLeft, 0), nil)
	typeInto(&e, "X")
	if e.Value() != "sort -n Xf" {
		t.Errorf("cursor movement is off: %q", e.Value())
	}
}

func TestEditorWordMotionsAndKills(t *testing.T) {
	var e lineEditor
	typeInto(&e, "cut -d, -f1 data.csv")

	e.Update(stroke('w', tea.ModCtrl), nil) // delete the last word
	if e.Value() != "cut -d, -f1 " {
		t.Fatalf("ctrl+w should delete a word: %q", e.Value())
	}
	e.Update(stroke('a', tea.ModCtrl), nil)
	if e.Cursor() != 0 {
		t.Errorf("ctrl+a should go to the start, cursor at %d", e.Cursor())
	}
	e.Update(stroke('f', tea.ModAlt), nil)
	if got := e.Value()[:e.Cursor()]; got != "cut" {
		t.Errorf("alt+f should move one word right, stopped after %q", got)
	}
	e.Update(stroke('e', tea.ModCtrl), nil)
	e.Update(stroke('u', tea.ModCtrl), nil)
	if e.Value() != "" {
		t.Errorf("ctrl+u should clear back to the start: %q", e.Value())
	}
}

func TestEditorHistoryWalksBothWays(t *testing.T) {
	var e lineEditor
	typeInto(&e, "ls")
	e.Accept()
	e.Clear()
	typeInto(&e, "wc -l f")
	e.Accept()
	e.Clear()
	typeInto(&e, "half-typed")

	e.Update(stroke(tea.KeyUp, 0), nil)
	if e.Value() != "wc -l f" {
		t.Fatalf("↑ should recall the last line, got %q", e.Value())
	}
	e.Update(stroke(tea.KeyUp, 0), nil)
	if e.Value() != "ls" {
		t.Fatalf("a second ↑ should go further back, got %q", e.Value())
	}
	e.Update(stroke(tea.KeyDown, 0), nil)
	e.Update(stroke(tea.KeyDown, 0), nil)
	if e.Value() != "half-typed" {
		t.Errorf("walking forward should restore the unfinished line, got %q", e.Value())
	}
}

func TestEditorHistorySkipsRepeats(t *testing.T) {
	var e lineEditor
	typeInto(&e, "ls")
	e.Accept()
	e.Accept()
	if len(e.history) != 1 {
		t.Errorf("the same line twice should be one history entry, got %v", e.history)
	}
}

func TestEditorCompletionCycles(t *testing.T) {
	var e lineEditor
	typeInto(&e, "so")
	candidates := func(string, int) []string { return []string{"sort", "source"} }
	e.Update(stroke(tea.KeyTab, 0), candidates)
	if e.Value() != "sort" {
		t.Fatalf("first tab should take the first candidate, got %q", e.Value())
	}
	e.Update(stroke(tea.KeyTab, 0), candidates)
	if e.Value() != "source" {
		t.Fatalf("a second tab should cycle, got %q", e.Value())
	}
	e.Update(stroke(tea.KeyTab, 0), candidates)
	if e.Value() != "sort" {
		t.Errorf("the cycle should wrap, got %q", e.Value())
	}
}

func TestEditorCompletionKeepsTheRestOfTheLine(t *testing.T) {
	var e lineEditor
	typeInto(&e, "so f.txt")
	for range 6 {
		e.Update(stroke(tea.KeyLeft, 0), nil)
	}
	e.Update(stroke(tea.KeyTab, 0), func(string, int) []string { return []string{"sort"} })
	if e.Value() != "sort f.txt" {
		t.Errorf("completion should splice into the line, got %q", e.Value())
	}
}

func TestEditorWordAtCursor(t *testing.T) {
	cases := []struct {
		line string
		back int // how many times to press left from the end
		want string
	}{
		{"sort -n f", 0, "f"},
		{"cut -d, -f1 data.csv", 9, "-f1"},
		{"ls | wc -l", 5, "wc"},
		{"", 0, ""},
	}
	for _, tc := range cases {
		var e lineEditor
		typeInto(&e, tc.line)
		for range tc.back {
			e.Update(stroke(tea.KeyLeft, 0), nil)
		}
		if got := e.WordAt(); got != tc.want {
			t.Errorf("WordAt(%q, back %d) = %q, want %q", tc.line, tc.back, got, tc.want)
		}
	}
}

func TestEditorBackspaceAndDelete(t *testing.T) {
	var e lineEditor
	typeInto(&e, "wcc")
	e.Update(tea.KeyPressMsg{Code: tea.KeyBackspace}, nil)
	if e.Value() != "wc" {
		t.Fatalf("backspace: %q", e.Value())
	}
	e.Update(stroke(tea.KeyLeft, 0), nil)
	e.Update(tea.KeyPressMsg{Code: tea.KeyDelete}, nil)
	if e.Value() != "w" {
		t.Errorf("delete: %q", e.Value())
	}
}
