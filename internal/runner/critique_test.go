package runner

import (
	"strings"
	"testing"
)

func TestCritiqueNamesTheCheaperIdiom(t *testing.T) {
	cases := []struct {
		name  string
		input string
		ref   string
		want  string
	}{
		{"grep into wc", "grep ERROR app.log | wc -l", "grep -c ERROR app.log", "grep -c"},
		{"useless cat", "cat f.txt | sort", "sort f.txt", "most commands open a file themselves"},
		{"sort into uniq", "sort f.txt | uniq", "sort -u f.txt", "sort -u"},
		{"pointless sort", "sort f.txt | wc -l", "wc -l f.txt", "not earning its place"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			notes := strings.Join(Critique(tc.input, tc.ref), "\n")
			if !strings.Contains(notes, tc.want) {
				t.Errorf("Critique(%q) = %q, want a note mentioning %q", tc.input, notes, tc.want)
			}
		})
	}
}

func TestCritiqueCountsProcesses(t *testing.T) {
	notes := strings.Join(Critique(
		"cut -d' ' -f1 access.log | sort | uniq -c | sort -rn | head -5",
		"awk '{print $1}' access.log | sort | uniq -c | sort -rn | head -5"), "\n")
	if strings.Contains(notes, "starts") {
		t.Errorf("equal-length pipelines need no process note, got %q", notes)
	}

	notes = strings.Join(Critique("sort f | uniq -c | sort -rn", "sort f | uniq -c"), "\n")
	if !strings.Contains(notes, "3 processes") || !strings.Contains(notes, "2 processes") {
		t.Errorf("a longer pipeline should be counted, got %q", notes)
	}

	notes = strings.Join(Critique("grep -c x f", "grep x f | wc -l"), "\n")
	if !strings.Contains(notes, "worth remembering") {
		t.Errorf("a shorter pipeline than the reference deserves credit, got %q", notes)
	}
}

// TestCritiqueSaysNothingUseless keeps the notes from nagging about a solution
// that matches the reference.
func TestCritiqueSaysNothingUseless(t *testing.T) {
	ref := "cut -d' ' -f1 access.log | sort | uniq -c | sort -rn | head -5"
	if notes := Critique(ref, ref); len(notes) != 0 {
		t.Errorf("the reference solution should draw no remarks, got %v", notes)
	}
}

// TestCritiqueIgnoresInputItCannotParse keeps it from panicking on the text a
// learner is halfway through typing.
func TestCritiqueIgnoresInputItCannotParse(t *testing.T) {
	if notes := Critique("sort $(ls)", "ls | sort"); len(notes) != 0 {
		t.Errorf("unparseable input should produce no notes, got %v", notes)
	}
}
