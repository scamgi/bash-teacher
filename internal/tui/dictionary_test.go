package tui

import (
	"strings"
	"testing"
)

// TestEveryCommandEntryRenders is the M2 exit criterion in test form: walking
// the whole dictionary must render every entry, at a narrow width as well as a
// wide one, with its mandatory sections present and nothing overflowing.
func TestEveryCommandEntryRenders(t *testing.T) {
	for _, size := range []struct{ w, h int }{{80, 24}, {120, 40}} {
		a := newTestApp(t, size.w, size.h)
		a.current = ScreenDictionary
		d := dictOf(a)

		seen := 0
		for i, row := range d.rows {
			if row.cmd == nil {
				continue
			}
			d.cursor = i
			d.shownID = "" // invalidate the cache so each entry is built
			frame := view(a)
			seen++

			body := d.detail.GetContent()
			for _, want := range []string{"SYNOPSIS", "EXAMPLES"} {
				if !strings.Contains(body, want) {
					t.Errorf("%dx%d: %s is missing a %s section", size.w, size.h, row.cmd.Name, want)
				}
			}
			if !strings.Contains(body, row.cmd.Name) {
				t.Errorf("%dx%d: %s does not name itself", size.w, size.h, row.cmd.Name)
			}
			if len(d.items) == 0 {
				t.Errorf("%dx%d: %s has no actionable items", size.w, size.h, row.cmd.Name)
			}
			for n, line := range strings.Split(frame, "\n") {
				if got := len([]rune(stripANSI(line))); got > size.w {
					t.Fatalf("%dx%d: %s overflows on line %d: %d cells", size.w, size.h, row.cmd.Name, n, got)
				}
			}
		}
		if seen != len(a.Lib.Commands) {
			t.Errorf("%dx%d: rendered %d entries, library has %d", size.w, size.h, seen, len(a.Lib.Commands))
		}
	}
}

// TestDetailItemsAreActionable checks that every recorded item points at a line
// that exists, since the caret and the scroll-into-view both index by line.
func TestDetailItemsAreActionable(t *testing.T) {
	a := newTestApp(t, 100, 30)
	a.current = ScreenDictionary
	d := dictOf(a)
	for i, row := range d.rows {
		if row.cmd == nil {
			continue
		}
		d.cursor = i
		d.shownID = ""
		_ = view(a)
		lines := strings.Split(d.detail.GetContent(), "\n")
		for _, it := range d.items {
			if it.line < 0 || it.line >= len(lines) {
				t.Fatalf("%s: item %q points at line %d of %d", row.cmd.Name, it.text, it.line, len(lines))
			}
			if it.kind == itemRelated {
				if _, ok := a.Lib.Command(it.text); !ok {
					t.Errorf("%s: related item %q is not a command", row.cmd.Name, it.text)
				}
			}
			if it.kind == itemExercise {
				if _, ok := a.Lib.Exercise(it.text); !ok {
					t.Errorf("%s: exercise item %q is not an exercise", row.cmd.Name, it.text)
				}
			}
		}
	}
}
