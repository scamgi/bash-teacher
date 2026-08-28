package runner

import (
	"fmt"
	"strings"

	"bash-teacher/internal/shellparse"
)

// Critique compares a passing solution with the exercise's reference one and
// returns the remarks worth showing beside them.
//
// It never judges a correct answer wrong: SPEC §2.2 says any pipeline that
// produces the expected output passes, so these are notes about cost and
// idiom, in the register of a colleague reading over your shoulder. An empty
// result means there was nothing useful to say.
func Critique(input, reference string) []string {
	got, err := shellparse.Parse(input)
	if err != nil {
		return nil
	}
	var notes []string
	notes = append(notes, idiomNotes(got)...)

	want, err := shellparse.Parse(reference)
	if err != nil {
		return notes
	}
	mine, theirs := len(got.Commands()), len(want.Commands())
	switch {
	case mine > theirs:
		notes = append(notes, fmt.Sprintf(
			"yours starts %s where the reference starts %s", processes(mine), processes(theirs)))
	case mine < theirs:
		notes = append(notes, fmt.Sprintf(
			"yours does it in %s to the reference's %s — worth remembering",
			processes(mine), processes(theirs)))
	case len(strings.Fields(input)) > len(strings.Fields(reference))+2:
		notes = append(notes, "same number of processes, but the reference says it in fewer words")
	}
	return notes
}

// idiomNotes reports the pipeline shapes that have a shorter spelling. Each
// one is a real pattern learners produce, and the note names the replacement
// rather than only pointing at the problem.
func idiomNotes(s *shellparse.Script) []string {
	var notes []string
	for _, p := range s.Pipelines {
		for i, c := range p.Commands {
			var next *shellparse.Command
			if i+1 < len(p.Commands) {
				next = p.Commands[i+1]
			}
			notes = append(notes, idiomNote(c, next)...)
		}
	}
	return notes
}

func idiomNote(c, next *shellparse.Command) []string {
	var notes []string
	name := c.Name.Value
	switch {
	case name == "cat" && next != nil && len(c.Args) == 1 && !hasFlags(c):
		notes = append(notes, fmt.Sprintf(
			"`cat %s | %s` can be `%s %s` — most commands open a file themselves",
			c.Args[0].Raw, next.Name.Value, next.Name.Value, c.Args[0].Raw))
	case name == "grep" && next != nil && next.Name.Value == "wc" && hasArg(next, "-l"):
		notes = append(notes, "`grep -c` counts its own matches, which would have saved the `wc`")
	case name == "sort" && next != nil && next.Name.Value == "uniq" && !hasFlags(next):
		notes = append(notes, "`sort -u` deduplicates in one pass; `sort | uniq` needs two processes")
	case name == "sort" && next != nil && next.Name.Value == "wc" && hasArg(next, "-l"):
		notes = append(notes, "sorting does not change how many lines there are, so the `sort` is not earning its place")
	}
	return notes
}

func hasFlags(c *shellparse.Command) bool {
	for _, a := range c.Args {
		if strings.HasPrefix(a.Value, "-") && a.Value != "-" {
			return true
		}
	}
	return false
}

func hasArg(c *shellparse.Command, want string) bool {
	for _, a := range c.Args {
		if a.Value == want {
			return true
		}
	}
	return false
}

// processes formats a process count for the notes above.
func processes(n int) string {
	if n == 1 {
		return "1 process"
	}
	return fmt.Sprintf("%d processes", n)
}
