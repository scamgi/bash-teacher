package runner

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"bash-teacher/internal/content"
)

// maxDiffRows bounds how much of a failure is shown. A learner needs the first
// place their pipeline diverged, not every consequence of it.
const maxDiffRows = 10

// DiffRow is one line of a side-by-side comparison. Either side may be empty
// when one output ran out before the other.
type DiffRow struct {
	N        int // 1-based line number
	Expected string
	Actual   string
	Same     bool
}

// Diff is the result of comparing an exercise's expected output with what the
// learner's pipeline actually printed.
type Diff struct {
	Mode  content.MatchMode
	Equal bool
	Rows  []DiffRow
	// Line and Col locate the first divergence, 1-based. Col is 0 when the
	// lines differ only by one being absent.
	Line int
	Col  int
	// Hidden counts differing rows beyond the ones in Rows.
	Hidden int
	// Note explains a whole-output mismatch that no row can show, such as a
	// regex that did not match or an unordered comparison's missing lines.
	Note string
}

// Compare diffs actual against expected under the exercise's match mode.
func Compare(expected, actual string, mode content.MatchMode) *Diff {
	d := &Diff{Mode: mode}
	switch mode {
	case content.MatchRegex:
		compareRegex(d, expected, actual)
	case content.MatchUnordered:
		compareUnordered(d, expected, actual)
	case content.MatchTrimmed:
		compareLines(d, trimTrailing(splitLines(expected)), trimTrailing(splitLines(actual)))
	default: // content.MatchExact
		compareLines(d, splitLines(expected), splitLines(actual))
	}
	return d
}

// splitLines normalizes the trailing newline away, so a pipeline that ends
// with one and an expected file that does not are still equal.
func splitLines(s string) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.TrimSuffix(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

func trimTrailing(lines []string) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = strings.TrimRight(l, " \t")
	}
	return out
}

func compareLines(d *Diff, want, got []string) {
	n := len(want)
	if len(got) > n {
		n = len(got)
	}
	for i := range n {
		w, g := at(want, i), at(got, i)
		same := i < len(want) && i < len(got) && w == g
		if same {
			continue
		}
		if d.Line == 0 {
			d.Line = i + 1
			d.Col = firstDiffColumn(w, g)
		}
		if len(d.Rows) < maxDiffRows {
			d.Rows = append(d.Rows, DiffRow{N: i + 1, Expected: w, Actual: g})
			continue
		}
		d.Hidden++
	}
	d.Equal = d.Line == 0
}

func at(lines []string, i int) string {
	if i < len(lines) {
		return lines[i]
	}
	return ""
}

// firstDiffColumn returns the 1-based column of the first differing character,
// or 0 when one of the lines is simply absent.
func firstDiffColumn(want, got string) int {
	if want == "" || got == "" {
		return 0
	}
	wr, gr := []rune(want), []rune(got)
	for i := range min(len(wr), len(gr)) {
		if wr[i] != gr[i] {
			return i + 1
		}
	}
	return min(len(wr), len(gr)) + 1
}

func compareRegex(d *Diff, pattern, actual string) {
	// The expected file holds one regex, anchored so a partial match is not
	// mistaken for a pass. Trailing whitespace in the file is not part of it.
	pat := "(?s)^" + strings.TrimRight(pattern, "\n \t") + "$"
	re, err := regexp.Compile(pat)
	if err != nil {
		d.Note = "the exercise's expected-output regex does not compile: " + err.Error()
		return
	}
	trimmed := strings.TrimSuffix(actual, "\n")
	if re.MatchString(trimmed) {
		d.Equal = true
		return
	}
	d.Note = "output did not match the expected pattern"
	for i, l := range splitLines(actual) {
		if len(d.Rows) >= maxDiffRows {
			d.Hidden++
			continue
		}
		d.Rows = append(d.Rows, DiffRow{N: i + 1, Actual: l})
	}
	d.Line = 1
}

// compareUnordered treats both outputs as multisets of lines, for tasks whose
// output order is genuinely unspecified.
func compareUnordered(d *Diff, expected, actual string) {
	want, got := count(splitLines(expected)), count(splitLines(actual))
	var missing, extra []string
	for line, n := range want {
		if diff := n - got[line]; diff > 0 {
			missing = append(missing, fmt.Sprintf("%s (×%d)", line, diff))
		}
	}
	for line, n := range got {
		if diff := n - want[line]; diff > 0 {
			extra = append(extra, fmt.Sprintf("%s (×%d)", line, diff))
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	if len(missing) == 0 && len(extra) == 0 {
		d.Equal = true
		return
	}
	d.Line = 1
	d.Note = "the lines are compared without regard to order"
	for i := range max(len(missing), len(extra)) {
		if len(d.Rows) >= maxDiffRows {
			d.Hidden++
			continue
		}
		d.Rows = append(d.Rows, DiffRow{N: i + 1, Expected: at(missing, i), Actual: at(extra, i)})
	}
}

func count(lines []string) map[string]int {
	m := map[string]int{}
	for _, l := range lines {
		m[l]++
	}
	return m
}

// String renders the diff as the two-column block the practice screen and the
// CLI both show, with a caret under the first differing character.
func (d *Diff) String() string {
	if d.Equal {
		return "✓ matches expected"
	}
	var b strings.Builder
	head := "✗ output differs"
	if d.Line > 0 {
		head += fmt.Sprintf(" at line %d", d.Line)
		if d.Col > 0 {
			head += fmt.Sprintf(", column %d", d.Col)
		}
	}
	b.WriteString(head)
	if d.Note != "" {
		b.WriteString("\n" + d.Note)
	}
	if len(d.Rows) == 0 {
		return b.String()
	}

	label := [2]string{"expected", "actual"}
	if d.Mode == content.MatchUnordered {
		label = [2]string{"missing", "unexpected"}
	}
	width := len(label[0])
	for _, r := range d.Rows {
		width = max(width, len([]rune(r.Expected)))
	}
	width = min(width, 40)

	fmt.Fprintf(&b, "\n     %-*s  %s\n", width, label[0], label[1])
	for _, r := range d.Rows {
		fmt.Fprintf(&b, "%4d %-*s  %s\n", r.N, width, clipRunes(r.Expected, width), clipRunes(r.Actual, width))
		if r.N == d.Line && d.Col > 0 {
			b.WriteString(strings.Repeat(" ", 5+d.Col-1) + "^\n")
		}
	}
	if d.Hidden > 0 {
		fmt.Fprintf(&b, "     … %d more differing line(s)\n", d.Hidden)
	}
	return strings.TrimRight(b.String(), "\n")
}

func clipRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 1 {
		return "…"
	}
	return string(r[:n-1]) + "…"
}
