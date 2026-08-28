package fuzzy_test

import (
	"sort"
	"testing"

	"bash-teacher/internal/fuzzy"
)

func TestSubsequenceMatching(t *testing.T) {
	for _, tc := range []struct {
		pattern, target string
		want            bool
	}{
		{"grep", "grep", true},
		{"grp", "grep", true},
		{"gp", "grep", true},
		{"prg", "grep", false},
		{"grepp", "grep", false},
		{"", "anything", true},
		{"GREP", "grep", true},
		{"grep", "GREP", true},
	} {
		_, ok := fuzzy.Score(tc.pattern, tc.target)
		if ok != tc.want {
			t.Errorf("Score(%q, %q) matched = %v, want %v", tc.pattern, tc.target, ok, tc.want)
		}
	}
}

// TestRankingPrefersTheObviousMatch is the property that makes the search box
// usable: typing a command's name must put that command first.
func TestRankingPrefersTheObviousMatch(t *testing.T) {
	candidates := []string{"grep", "pgrep", "egrep-like", "printf"}
	type scored struct {
		name  string
		score int
	}
	var got []scored
	for _, c := range candidates {
		if m, ok := fuzzy.Score("grep", c); ok {
			got = append(got, scored{c, m.Score})
		}
	}
	sort.SliceStable(got, func(i, j int) bool { return got[i].score > got[j].score })
	if len(got) == 0 || got[0].name != "grep" {
		t.Fatalf("expected grep to rank first, got %+v", got)
	}
}

func TestShorterTargetWinsTies(t *testing.T) {
	short, _ := fuzzy.Score("tar", "tar")
	long, _ := fuzzy.Score("tar", "tar-with-a-long-name")
	if short.Score <= long.Score {
		t.Errorf("short target scored %d, long scored %d; short should win", short.Score, long.Score)
	}
}

func TestPositionsPointAtTheMatchedCharacters(t *testing.T) {
	m, ok := fuzzy.Score("gp", "grep")
	if !ok {
		t.Fatal("expected a match")
	}
	want := []int{0, 3}
	if len(m.Positions) != len(want) {
		t.Fatalf("positions = %v, want %v", m.Positions, want)
	}
	for i := range want {
		if m.Positions[i] != want[i] {
			t.Fatalf("positions = %v, want %v", m.Positions, want)
		}
	}
}

// TestBestPrefersEarlierFields keeps a name hit ahead of a summary hit, so
// searching "sort" does not surface every command whose description mentions
// sorting before sort itself.
func TestBestPrefersEarlierFields(t *testing.T) {
	_, field, ok := fuzzy.Best("sort", "sort", "Sort lines of text.")
	if !ok || field != 0 {
		t.Errorf("Best matched field %d (ok=%v), want field 0", field, ok)
	}
	_, field, ok = fuzzy.Best("lines", "sort", "Sort lines of text.")
	if !ok || field != 1 {
		t.Errorf("Best matched field %d (ok=%v), want field 1", field, ok)
	}
}

func TestSplitReconstructsTheString(t *testing.T) {
	m, _ := fuzzy.Score("gp", "grep")
	var rebuilt string
	matched := 0
	for _, h := range fuzzy.Split("grep", m.Positions) {
		rebuilt += h.Text
		if h.Matched {
			matched += len(h.Text)
		}
	}
	if rebuilt != "grep" {
		t.Errorf("Split lost characters: %q", rebuilt)
	}
	if matched != 2 {
		t.Errorf("marked %d characters as matched, want 2", matched)
	}
}
