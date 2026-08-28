// Package fuzzy implements the subsequence matching used by the dictionary's
// search box.
//
// A pattern matches a target when its characters appear in the target in order,
// though not necessarily adjacently: "grp" finds "grep", "wcl" finds nothing in
// "grep" but does find "wc -l". Matches are scored so that the most obvious
// interpretation sorts first — a prefix beats a word boundary, which beats a
// hit buried mid-word — and the matched positions are returned so the caller can
// highlight them.
package fuzzy

import "strings"

// Scoring weights. They only have to be consistent relative to each other; the
// absolute numbers carry no meaning.
const (
	scoreMatch      = 16 // a character matched at all
	bonusPrefix     = 24 // the match starts at the beginning of the target
	bonusBoundary   = 12 // the match starts a word, after a space, dash or slash
	bonusConsecutiv = 10 // this match immediately follows the previous one
	penaltyGap      = 2  // per character skipped between matches
	penaltyLength   = 1  // per character of target length, so short names win ties
)

// Match is one scored result.
type Match struct {
	// Score is higher for better matches.
	Score int
	// Positions are the indexes in the target that the pattern matched, in
	// order, for highlighting.
	Positions []int
}

// isBoundary reports whether index i in s starts a word.
func isBoundary(s string, i int) bool {
	if i == 0 {
		return true
	}
	switch s[i-1] {
	case ' ', '-', '_', '.', '/', ':', '\t':
		return true
	}
	return false
}

// Score matches pattern against target, case-insensitively. It reports ok=false
// when the pattern is not a subsequence of the target. An empty pattern matches
// everything with a score of zero, so an empty search box leaves the list alone.
func Score(pattern, target string) (Match, bool) {
	if pattern == "" {
		return Match{}, true
	}
	p := strings.ToLower(pattern)
	t := strings.ToLower(target)

	positions := make([]int, 0, len(p))
	score := 0
	ti := 0
	prev := -2 // so the first match is never counted as consecutive

	for pi := 0; pi < len(p); pi++ {
		found := -1
		for ; ti < len(t); ti++ {
			if t[ti] == p[pi] {
				found = ti
				break
			}
		}
		if found < 0 {
			return Match{}, false
		}
		score += scoreMatch
		switch {
		case found == prev+1:
			score += bonusConsecutiv
		case isBoundary(target, found):
			score += bonusBoundary
		default:
			score -= penaltyGap
		}
		if pi == 0 && found == 0 {
			score += bonusPrefix
		}
		positions = append(positions, found)
		prev = found
		ti = found + 1
	}
	score -= len(target) * penaltyLength
	return Match{Score: score, Positions: positions}, true
}

// Best returns the higher-scoring of two matches against different fields of the
// same item — a command's name and its summary, say — so a name hit outranks a
// description hit. The second result reports whether either matched.
func Best(pattern string, targets ...string) (Match, int, bool) {
	best := Match{Score: -1 << 30}
	which := -1
	for i, t := range targets {
		m, ok := Score(pattern, t)
		if !ok {
			continue
		}
		// Later fields are less important, so each costs a small handicap.
		m.Score -= i * 40
		if which < 0 || m.Score > best.Score {
			best, which = m, i
		}
	}
	if which < 0 {
		return Match{}, -1, false
	}
	return best, which, true
}

// Highlight splits s into runs, marking which are part of the match, so a
// renderer can style them without knowing anything about scoring.
type Highlight struct {
	Text    string
	Matched bool
}

// Split turns a target and its matched positions into alternating runs.
func Split(s string, positions []int) []Highlight {
	if len(positions) == 0 {
		return []Highlight{{Text: s}}
	}
	in := make(map[int]bool, len(positions))
	for _, p := range positions {
		in[p] = true
	}
	var out []Highlight
	var cur strings.Builder
	curMatched := in[0]
	for i := 0; i < len(s); i++ {
		if in[i] != curMatched {
			out = append(out, Highlight{Text: cur.String(), Matched: curMatched})
			cur.Reset()
			curMatched = in[i]
		}
		cur.WriteByte(s[i])
	}
	if cur.Len() > 0 {
		out = append(out, Highlight{Text: cur.String(), Matched: curMatched})
	}
	return out
}
