package runner

import (
	"strings"
	"testing"

	"bash-teacher/internal/content"
)

func TestCompareExact(t *testing.T) {
	tests := []struct {
		name          string
		expected, got string
		equal         bool
		line, col     int
	}{
		{"identical", "a\nb\n", "a\nb\n", true, 0, 0},
		{"trailing newline missing", "a\nb\n", "a\nb", true, 0, 0},
		{"crlf normalized", "a\nb\n", "a\r\nb\r\n", true, 0, 0},
		{"one character off", "abc\n", "abd\n", false, 1, 3},
		{"second line differs", "a\nbcd\n", "a\nbxd\n", false, 2, 2},
		{"actual is short", "a\nb\n", "a\n", false, 2, 0},
		{"actual is long", "a\n", "a\nb\n", false, 2, 0},
		{"trailing space matters", "a\n", "a \n", false, 1, 2},
		{"empty against empty", "", "", true, 0, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := Compare(tc.expected, tc.got, content.MatchExact)
			if d.Equal != tc.equal {
				t.Fatalf("Equal = %v, want %v (%s)", d.Equal, tc.equal, d)
			}
			if d.Line != tc.line || d.Col != tc.col {
				t.Errorf("first difference at line %d col %d, want %d/%d", d.Line, d.Col, tc.line, tc.col)
			}
		})
	}
}

func TestCompareTrimmedIgnoresTrailingWhitespace(t *testing.T) {
	if d := Compare("a\nb\n", "a  \nb\t\n", content.MatchTrimmed); !d.Equal {
		t.Errorf("trimmed comparison failed: %s", d)
	}
	if d := Compare("a\n", "  a\n", content.MatchTrimmed); d.Equal {
		t.Error("trimmed comparison ignored leading whitespace, which it must not")
	}
}

func TestCompareUnordered(t *testing.T) {
	if d := Compare("a\nb\nc\n", "c\na\nb\n", content.MatchUnordered); !d.Equal {
		t.Errorf("reordered lines should match: %s", d)
	}
	if d := Compare("a\na\nb\n", "a\nb\n", content.MatchUnordered); d.Equal {
		t.Error("unordered comparison is a multiset, so a repeated line must count")
	}
	d := Compare("a\nb\n", "a\nc\n", content.MatchUnordered)
	if d.Equal {
		t.Fatal("differing multisets matched")
	}
	if !strings.Contains(d.String(), "missing") || !strings.Contains(d.String(), "unexpected") {
		t.Errorf("unordered diff should label the columns missing/unexpected:\n%s", d)
	}
}

func TestCompareRegex(t *testing.T) {
	if d := Compare(`total \d+\n[-rwx]{10}.*`, "total 42\n-rw-r--r-- 1 me 3 f\n", content.MatchRegex); !d.Equal {
		t.Errorf("regex should match: %s", d)
	}
	if d := Compare(`total \d+`, "total x", content.MatchRegex); d.Equal {
		t.Error("regex matched output it should not have")
	}
	// The pattern is anchored, so a partial match is not a pass.
	if d := Compare(`abc`, "abcdef", content.MatchRegex); d.Equal {
		t.Error("unanchored partial match was accepted")
	}
	if d := Compare(`(`, "x", content.MatchRegex); d.Equal || !strings.Contains(d.Note, "does not compile") {
		t.Errorf("a broken pattern should say so, got note %q", d.Note)
	}
}

func TestDiffStringShowsCaretAndCaps(t *testing.T) {
	d := Compare("abcd\n", "abxd\n", content.MatchExact)
	out := d.String()
	if !strings.Contains(out, "line 1, column 3") {
		t.Errorf("header should locate the difference:\n%s", out)
	}
	caret := ""
	for _, l := range strings.Split(out, "\n") {
		if strings.TrimSpace(l) == "^" {
			caret = l
		}
	}
	if caret == "" {
		t.Fatalf("no caret line:\n%s", out)
	}
	if got := strings.Index(caret, "^"); got != 5+3-1 {
		t.Errorf("caret at column %d, want %d:\n%s", got, 5+3-1, out)
	}

	var want, got strings.Builder
	for i := range 30 {
		want.WriteString("line\n")
		got.WriteString("nile\n")
		_ = i
	}
	big := Compare(want.String(), got.String(), content.MatchExact)
	if len(big.Rows) != maxDiffRows {
		t.Errorf("showed %d rows, want at most %d", len(big.Rows), maxDiffRows)
	}
	if big.Hidden != 30-maxDiffRows {
		t.Errorf("Hidden = %d, want %d", big.Hidden, 30-maxDiffRows)
	}
}
