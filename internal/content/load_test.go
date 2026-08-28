package content_test

import (
	"strings"
	"testing"
	"testing/fstest"

	embedded "bash-teacher/content"
	"bash-teacher/internal/content"
)

// TestEmbeddedLibraryLoads is the M1 exit criterion in test form: the shipped
// content parses, validates, and indexes.
func TestEmbeddedLibraryLoads(t *testing.T) {
	lib, err := content.Load(embedded.FS)
	if err != nil {
		t.Fatalf("embedded library failed to load: %v", err)
	}
	if len(lib.Commands) == 0 || len(lib.Exercises) == 0 || len(lib.Cards) == 0 {
		t.Fatalf("library is missing content: %d commands, %d exercises, %d cards",
			len(lib.Commands), len(lib.Exercises), len(lib.Cards))
	}
	if _, ok := lib.Command("grep"); !ok {
		t.Error("expected a dictionary entry for grep")
	}
	if len(lib.Tracks) == 0 {
		t.Error("expected exercises to be grouped into tracks")
	}
}

// TestTracksAreOrderedByLevel guards the property practice navigation relies
// on: walking a track means walking up the difficulty ladder.
func TestTracksAreOrderedByLevel(t *testing.T) {
	lib, err := content.Load(embedded.FS)
	if err != nil {
		t.Fatal(err)
	}
	for _, tr := range lib.Tracks {
		for i := 1; i < len(tr.Exercises); i++ {
			if tr.Exercises[i-1].Level > tr.Exercises[i].Level {
				t.Errorf("track %q is out of order at %q", tr.Name, tr.Exercises[i].ID)
			}
		}
	}
}

// TestNetworkCommandsAreNotExecutable checks the rule the M3 sandbox will
// depend on: documented-but-never-run commands stay off the allowlist.
func TestNetworkCommandsAreNotExecutable(t *testing.T) {
	lib, err := content.Load(embedded.FS)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range lib.Allowlist() {
		if name == "curl" || name == "wget" || name == "nc" || name == "ssh" {
			t.Errorf("%s must never be executable", name)
		}
	}
}

func TestLintCatchesBrokenReferences(t *testing.T) {
	src := fstest.MapFS{
		"commands/ls.yaml": &fstest.MapFile{Data: []byte(`
id: ls
name: ls
category: navigation
summary: List directory contents.
purpose: Answers "what is here?".
synopsis: ls [OPTION]...
plays_well_with: [nosuchcommand]
examples:
  - cmd: ls -l
    caption: The everyday listing.
`)},
		"exercises/": &fstest.MapFile{Mode: 0o755},
		"cards/":     &fstest.MapFile{Mode: 0o755},
		"fixtures/":  &fstest.MapFile{Mode: 0o755},
		"expected/":  &fstest.MapFile{Mode: 0o755},
	}
	_, err := content.Load(src)
	if err == nil {
		t.Fatal("expected a lint failure for the dangling reference")
	}
	if !strings.Contains(err.Error(), "nosuchcommand") {
		t.Errorf("error should name the missing command, got: %v", err)
	}
}

func TestLintCatchesMissingFixture(t *testing.T) {
	src := fstest.MapFS{
		"commands/head.yaml": &fstest.MapFile{Data: []byte(`
id: head
name: head
category: inspection
summary: Print the first lines.
purpose: Takes the top of a stream.
synopsis: head [-n COUNT]
examples:
  - cmd: head -5 f
    caption: The first five lines.
`)},
		"exercises/x.yaml": &fstest.MapFile{Data: []byte(`
id: peek
track: inspection
level: 1
title: Peek
prompt: Print the first three lines.
fixture: nowhere
expected_stdout_file: expected/peek.txt
match: exact
teaches: [head]
reference_solution: head -3 f
hints:
  - Use head.
`)},
		"cards/":    &fstest.MapFile{Mode: 0o755},
		"fixtures/": &fstest.MapFile{Mode: 0o755},
		"expected/": &fstest.MapFile{Mode: 0o755},
	}
	_, err := content.Load(src)
	if err == nil {
		t.Fatal("expected a lint failure for the missing fixture")
	}
	for _, want := range []string{"fixture", "expected_stdout_file"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should mention %q, got: %v", want, err)
		}
	}
}

// TestLibraryMeetsTheContentBudget guards the shipped library against
// accidental loss: SPEC §4.5 budgets 80 commands and 120 exercises in 6
// tracks, and M4's exercise set is what makes the practice screen worth
// opening. The assertions are floors, so growing the library is free.
func TestLibraryMeetsTheContentBudget(t *testing.T) {
	lib, err := content.Load(embedded.FS)
	if err != nil {
		t.Fatalf("load content: %v", err)
	}
	if n := len(lib.Commands); n < 80 {
		t.Errorf("library has %d commands, the budget is 80", n)
	}
	if n := len(lib.Exercises); n < 120 {
		t.Errorf("library has %d exercises, the budget is 120", n)
	}
	if n := len(lib.Tracks); n != len(content.TrackOrder) {
		t.Errorf("library has %d tracks, want the %d in TrackOrder", n, len(content.TrackOrder))
	}
	// The ladder is what makes a track teachable: every level has to be
	// represented, or a learner walks from trivial to impossible.
	byLevel := map[int]int{}
	for _, e := range lib.Exercises {
		byLevel[e.Level]++
	}
	for level := 1; level <= 5; level++ {
		if byLevel[level] == 0 {
			t.Errorf("no exercises at level %d", level)
		}
	}
	for _, tr := range lib.Tracks {
		if len(tr.Exercises) == 0 {
			t.Errorf("track %s is empty", tr.Name)
		}
		if content.TrackTitle(tr.Name) == tr.Name {
			t.Errorf("track %s has no title", tr.Name)
		}
	}
}
