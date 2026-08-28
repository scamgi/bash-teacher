package answer

import (
	"testing"

	embedded "bash-teacher/content"
	"bash-teacher/internal/content"
)

func newGrader(t *testing.T) *Grader {
	t.Helper()
	lib, err := content.Load(embedded.FS)
	if err != nil {
		t.Fatalf("load content: %v", err)
	}
	return New(lib)
}

// TestEquivalence is SPEC §10's table of answers that must compare equal, and
// — just as important — the near misses that must not.
func TestEquivalence(t *testing.T) {
	g := newGrader(t)

	same := []struct{ a, b, why string }{
		{"ls -la", "ls -al", "a cluster is a set, not a sequence"},
		{"ls -la", "ls -l -a", "clustered and spelled out are the same flags"},
		{"ls -lth", "ls -h -t -l", "three switches in any arrangement"},
		{"sort -rn counts.txt", "sort -nr counts.txt", "the canonical top-N tail either way round"},
		{"sort -rn counts.txt", "sort -r -n counts.txt", "and unclustered"},
		{"grep -v '^$' notes.txt", `grep -v "^$" notes.txt`, "quote style is not meaning"},
		{"grep -v '^$' notes.txt", "grep -v ^$ notes.txt", "nor is quoting at all, when the value is the same"},
		{"cut -d, -f3 data.csv", "cut -f3 -d, data.csv", "flags carrying values still commute"},
		{"cut -d, -f3 data.csv", "cut -d , -f 3 data.csv", "attached or detached value"},
		{"cut -d: -f1 /etc/passwd", "cut -f1 -d: /etc/passwd", "the delimiter is a value, not a cluster"},
		{"grep -i error app.log", "grep --ignore-case error app.log", "short and long, as the dictionary documents them"},
		{"grep -rn TODO .", "grep --recursive --line-number TODO .", "two long forms at once"},
		{"sort -u names.txt", "sort --unique names.txt", "sort's long forms too"},
		{"find . -name '*.log' -type f", "find . -type f -name '*.log'", "find's word-length options commute"},
		{"tar -xzvf archive.tar.gz", "tar -xzvf   archive.tar.gz", "runs of whitespace collapse"},
		{"cat a.txt b.txt > whole.txt", "cat a.txt b.txt >whole.txt", "the space after a redirection is noise"},
		{"grep ERROR app.log | wc -l", "grep ERROR app.log|wc -l", "and around a pipe"},
	}
	for _, tc := range same {
		if !g.Equivalent(tc.a, tc.b) {
			t.Errorf("%q and %q graded as different, but %s\n  %s\n  %s",
				tc.a, tc.b, tc.why, show(g, tc.a), show(g, tc.b))
		}
	}

	different := []struct{ a, b, why string }{
		{"grep -i error app.log", "grep -I error app.log", "case matters in a flag: -i and -I are different options"},
		{"sort -k1 -k2 data.txt", "sort -k2 -k1 data.txt", "two of the same flag keep their order, because that order is the sort"},
		{"cut -d, -f3 data.csv", "cut -d, -f4 data.csv", "a different field is a different answer"},
		{"cut -d, -f3 data.csv", "cut -d; -f3 data.csv", "so is a different delimiter"},
		{"head -5 log.txt", "head -5 other.txt", "a different file is a different answer"},
		{"sort -rn f | head -5", "sort -rn f | head -3", "and a different count"},
		{"sort f | uniq -c", "sort f | uniq -d", "-c counts, -d filters"},
		{"cat a b > c", "cat a b > d", "a redirection target is part of the answer"},
		{"cat a b > c", "cat a b >> c", "appending is not truncating"},
		{"grep ERROR app.log | wc -l", "wc -l | grep ERROR app.log", "the order of a pipeline is its meaning"},
		{"grep -c ERROR app.log", "grep -v ERROR app.log", "different switches entirely"},
		{"ls -la", "ls -la /tmp", "an extra operand changes what is listed"},
	}
	for _, tc := range different {
		if g.Equivalent(tc.a, tc.b) {
			t.Errorf("%q and %q graded as the same, but %s\n  %s\n  %s",
				tc.a, tc.b, tc.why, show(g, tc.a), show(g, tc.b))
		}
	}
}

// TestQuotedScriptsFallBackToSelfGrading holds SPEC §2.3's rule that anything
// ambiguous is handed to the learner rather than marked wrong. Two regexes or
// two sed scripts that differ are exactly the case where this package should
// not be the judge.
func TestQuotedScriptsFallBackToSelfGrading(t *testing.T) {
	g := newGrader(t)
	card := &content.Card{
		ID: "x", Type: content.CardRecall,
		Front: "Replace old with new throughout.",
		Back:  "sed 's/old/new/g' config.txt",
	}
	cases := []struct {
		typed string
		want  Verdict
	}{
		{"sed 's/old/new/g' config.txt", Correct},
		{`sed "s/old/new/g" config.txt`, Correct},
		{"sed 's/old/new/' config.txt", Unsure},
		{"sed 's/old/new/g' other.txt", Wrong},
		{"awk 's/old/new/g' config.txt", Wrong},
		{"", Wrong},
	}
	for _, tc := range cases {
		if got := g.Grade(card, tc.typed).Verdict; got != tc.want {
			t.Errorf("Grade(%q) = %v, want %v", tc.typed, got, tc.want)
		}
	}
}

// TestGradeAcceptsTheAlternatives checks that a card's accepts list is tried
// alongside its back, since a shell task usually has more than one right
// answer and the card is where that is recorded.
func TestGradeAcceptsTheAlternatives(t *testing.T) {
	g := newGrader(t)
	card := &content.Card{
		ID: "x", Type: content.CardRecall,
		Front:   "Remove the blank lines from notes.txt.",
		Back:    "grep -v '^$' notes.txt",
		Accepts: []string{"sed '/^$/d' notes.txt", "grep . notes.txt"},
	}
	for _, typed := range []string{"grep -v '^$' notes.txt", "sed '/^$/d' notes.txt", "grep . notes.txt"} {
		if got := g.Grade(card, typed).Verdict; got != Correct {
			t.Errorf("Grade(%q) = %v, want correct", typed, got)
		}
	}
	if got := g.Grade(card, "cat notes.txt").Verdict; got != Wrong {
		t.Errorf("Grade(cat notes.txt) = %v, want wrong", got)
	}
}

// TestFlagCardsAreGradedAgainstTheirCommand checks the flag-card shape: the
// answer is an argument list with no command in front of it, and expanding
// "-xzvf" needs to know it belongs to tar.
func TestFlagCardsAreGradedAgainstTheirCommand(t *testing.T) {
	g := newGrader(t)
	card := &content.Card{
		ID: "x", Type: content.CardFlag,
		Front: "tar: extract a gzipped archive, verbosely.", Back: "-xzvf",
		Commands: []string{"tar"},
	}
	for _, typed := range []string{"-xzvf", "-x -z -v -f", "-zxvf"} {
		if got := g.Grade(card, typed).Verdict; got != Correct {
			t.Errorf("Grade(%q) = %v, want correct", typed, got)
		}
	}
	if got := g.Grade(card, "-xzf").Verdict; got != Wrong {
		t.Errorf("Grade(-xzf) = %v, want wrong: the card asked for verbose", got)
	}
}

// TestIdentifyCardsAreAlwaysSelfGraded holds the rule that a card asking for
// prose is never string-matched.
func TestIdentifyCardsAreAlwaysSelfGraded(t *testing.T) {
	g := newGrader(t)
	card := &content.Card{
		ID: "x", Type: content.CardIdentify,
		Front: "sort -t: -k3 -n /etc/passwd", Back: "Sort by numeric UID.",
	}
	if got := g.Grade(card, "Sort by numeric UID.").Verdict; got != Unsure {
		t.Errorf("Grade of an identify card = %v, want unsure", got)
	}
}

// TestUnparseableAnswersAreNotMarkedWrong checks that input the parser refuses
// — a subshell, an unterminated quote — becomes a question rather than a
// false negative. Refusing to run it is the runner's job; refusing to grade
// it is this package's.
func TestUnparseableAnswersAreNotMarkedWrong(t *testing.T) {
	g := newGrader(t)
	card := &content.Card{ID: "x", Type: content.CardRecall, Front: "f", Back: "wc -l < f"}
	for _, typed := range []string{"wc -l < $(echo f)", "wc -l < 'f"} {
		got := g.Grade(card, typed)
		if got.Verdict != Unsure {
			t.Errorf("Grade(%q) = %v, want unsure", typed, got.Verdict)
		}
		if got.Reason == "" {
			t.Errorf("Grade(%q) gave no reason for an unsure verdict", typed)
		}
	}
}

// TestEveryTypedCardAcceptsItsOwnAnswer walks the whole library: a card whose
// own back does not grade as correct is a card no learner can pass.
func TestEveryTypedCardAcceptsItsOwnAnswer(t *testing.T) {
	lib, err := content.Load(embedded.FS)
	if err != nil {
		t.Fatalf("load content: %v", err)
	}
	g := New(lib)
	for _, c := range lib.Cards {
		if c.SelfGraded() {
			continue
		}
		for _, want := range append([]string{c.Back}, c.Accepts...) {
			if got := g.Grade(c, want).Verdict; got != Correct {
				t.Errorf("card %s: its own answer %q grades as %v", c.ID, want, got)
			}
		}
	}
}

// show renders how the grader read one answer, for a failing table row.
func show(g *Grader, s string) string {
	n, err := g.normalize(s)
	if err != nil {
		return s + " -> " + err.Error()
	}
	out := ""
	for _, p := range n.pipelines {
		for _, st := range p {
			out += "[" + st.String() + "]"
		}
	}
	return s + " -> " + out
}
