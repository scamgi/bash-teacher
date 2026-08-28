// Package answer grades a typed flashcard answer against the one the card
// expects.
//
// The comparison is not string equality. A learner who writes `ls -al` when
// the card says `ls -la`, or `sort -r -n` for `sort -rn`, or double quotes
// where the card used single ones, has answered correctly, and being told
// otherwise teaches nothing but distrust. So both sides are reduced to a
// canonical form first: parsed with internal/shellparse, flags expanded and
// ordered by what the dictionary documents about them, quoting removed.
//
// The dictionary is what makes this possible. Whether `-f3` is a cluster of
// two flags or one flag carrying the value `3` is not a question about shell
// syntax — it is a question about `cut`, and the dictionary already answers
// it. That is also why the grader is built from a Library rather than from a
// table of special cases.
//
// Where the reduction cannot decide, it says so instead of guessing. Two
// answers that differ only inside a quoted script or pattern — `'s/a/b/'`
// against `'s/a/b/g'` — are handed back to the learner to grade, per SPEC
// §2.3: a false negative on a regex is worse than a question.
package answer

import (
	"sort"
	"strconv"
	"strings"

	"bash-teacher/internal/content"
	"bash-teacher/internal/shellparse"
)

// Verdict is the outcome of comparing a typed answer with the expected one.
// The values ascend so that grading against several accepted answers can take
// the best of them.
type Verdict int

// The three outcomes. Unsure is not a failure: it means the comparison is not
// one this package should be making, and the learner grades it.
const (
	// Wrong is a confident mismatch.
	Wrong Verdict = iota
	// Unsure means the answers are close enough that only the learner can
	// say, so the card falls back to self-grading.
	Unsure
	// Correct is a match after normalization.
	Correct
)

func (v Verdict) String() string {
	switch v {
	case Correct:
		return "correct"
	case Unsure:
		return "unsure"
	default:
		return "wrong"
	}
}

// Result is a graded answer: the verdict, and — when the verdict is not a
// plain match — a sentence saying why, for the screen to show.
type Result struct {
	Verdict Verdict
	Reason  string
}

// flagSpec is what the dictionary knows about one documented flag.
type flagSpec struct {
	// takesValue reports whether the flag is followed by an operand, which is
	// what tells `-f3` (cut's field list) from `-la` (two of ls's switches).
	takesValue bool
}

// Grader normalizes and compares answers using what the dictionary documents.
type Grader struct {
	// flags maps a command name to its documented flags. Commands are indexed
	// under both their id and their name, since a card names the id and the
	// answer text names the executable.
	flags map[string]map[string]flagSpec
	// long maps a command's long-form spellings to their short equivalents,
	// so `--recursive` and `-r` compare equal where the dictionary says they
	// are the same flag.
	long map[string]map[string]string
}

// New builds a grader from the content library.
func New(lib *content.Library) *Grader {
	g := &Grader{flags: map[string]map[string]flagSpec{}, long: map[string]map[string]string{}}
	for _, c := range lib.Commands {
		specs := map[string]flagSpec{}
		longs := map[string]string{}
		for _, f := range c.Flags {
			name, takesValue := parseFlagField(f.Flag)
			if name == "" {
				continue
			}
			specs[name] = flagSpec{takesValue: takesValue}
			if f.Long != "" {
				longName, longTakesValue := parseFlagField(f.Long)
				if longName != "" {
					specs[longName] = flagSpec{takesValue: takesValue || longTakesValue}
					longs[longName] = name
				}
			}
		}
		for _, key := range []string{c.ID, c.Name} {
			if key == "" {
				continue
			}
			g.flags[key] = specs
			g.long[key] = longs
		}
	}
	return g
}

// parseFlagField splits a dictionary flag field into the flag itself and
// whether it carries a value. The field is written the way a synopsis writes
// it — "-n", "-k N[,M]", "-exec CMD {} +" — so a second word means a value.
func parseFlagField(field string) (name string, takesValue bool) {
	fields := strings.Fields(field)
	if len(fields) == 0 {
		return "", false
	}
	name = strings.TrimSuffix(fields[0], ",")
	if !strings.HasPrefix(name, "-") {
		return "", false
	}
	return name, len(fields) > 1
}

// Grade compares a typed answer with the card's expected answer and each of
// its accepted alternatives, returning the best result any of them gives.
func (g *Grader) Grade(c *content.Card, typed string) Result {
	if c.SelfGraded() {
		return Result{Verdict: Unsure, Reason: "this card is yours to grade"}
	}
	if strings.TrimSpace(typed) == "" {
		return Result{Verdict: Wrong}
	}

	// A flag card's answer is an argument list with no command in front of
	// it, and the flags cannot be expanded without knowing whose they are.
	prefix := ""
	if c.Type == content.CardFlag && len(c.Commands) > 0 {
		prefix = c.Commands[0] + " "
	}

	mine, err := g.normalize(prefix + typed)
	if err != nil {
		return Result{Verdict: Unsure, Reason: "I could not read that as a command line — " + err.Error()}
	}

	best := Result{Verdict: Wrong}
	for _, want := range append([]string{c.Back}, c.Accepts...) {
		theirs, err := g.normalize(prefix + want)
		if err != nil {
			// The card's own answer is beyond the parser — a self-graded
			// comparison is the honest outcome, not a failure.
			if best.Verdict < Unsure {
				best = Result{Verdict: Unsure, Reason: "this answer is beyond what I can compare — grade it yourself"}
			}
			continue
		}
		switch verdict := compare(mine, theirs); verdict {
		case Correct:
			return Result{Verdict: Correct}
		case Unsure:
			if best.Verdict < Unsure {
				best = Result{
					Verdict: Unsure,
					Reason:  "that differs from the expected answer only inside a quoted pattern — you be the judge",
				}
			}
		}
	}
	return best
}

// Equivalent reports whether two command lines mean the same thing after
// normalization. It is the table-driven entry point the equivalence tests use,
// and it treats an Unsure comparison as not equivalent: a caller asking a
// yes-or-no question gets the strict answer.
func (g *Grader) Equivalent(a, b string) bool {
	na, err := g.normalize(a)
	if err != nil {
		return false
	}
	nb, err := g.normalize(b)
	if err != nil {
		return false
	}
	return compare(na, nb) == Correct
}

// token is one word of an answer, kept with whether it was quoted. Quoting is
// not part of the comparison — `'x'` and `"x"` and `x` all mean x — but it is
// what marks an operand as a program or a pattern, where two different
// spellings may both be right.
type token struct {
	value  string
	quoted bool
}

// stage is one simple command reduced to its meaning.
type stage struct {
	name string
	// switches counts the flags that carry no value, so a cluster and its
	// spelled-out equivalent come out the same.
	switches map[string]int
	// valued keeps each value-carrying flag's values in the order they
	// appeared. Keying by name makes `cut -f3 -d,` and `cut -d, -f3` equal
	// while keeping `sort -k1 -k2` distinct from `sort -k2 -k1`, where the
	// order of two of the same flag is the whole meaning.
	valued   map[string][]token
	operands []token
	redirs   []string
}

// normal is a whole answer: its pipelines, and the operators between them.
type normal struct {
	pipelines [][]stage
	ops       []string
}

// normalize parses one answer and reduces every command in it.
func (g *Grader) normalize(input string) (normal, error) {
	script, err := shellparse.Parse(input)
	if err != nil {
		return normal{}, err
	}
	var n normal
	for _, p := range script.Pipelines {
		stages := make([]stage, 0, len(p.Commands))
		for _, c := range p.Commands {
			stages = append(stages, g.stageOf(c))
		}
		n.pipelines = append(n.pipelines, stages)
	}
	for _, op := range script.Ops {
		n.ops = append(n.ops, string(op))
	}
	return n, nil
}

// stageOf reduces one simple command, consulting the dictionary to tell a
// flag cluster from a flag carrying a value.
func (g *Grader) stageOf(c *shellparse.Command) stage {
	name := c.Name.Value
	s := stage{name: name, switches: map[string]int{}, valued: map[string][]token{}}

	for _, a := range c.Assignments {
		s.operands = append(s.operands, token{value: a.Value, quoted: a.Quoted})
	}
	for _, r := range c.Redirects {
		s.redirs = append(s.redirs, redirKey(r))
	}

	args := c.Args
	literal := false
	for i := 0; i < len(args); i++ {
		w := args[i]
		switch {
		case literal || !isFlag(w):
			s.operands = append(s.operands, token{value: w.Value, quoted: w.Quoted})
		case w.Value == "--":
			literal = true
		default:
			i += g.addFlag(&s, name, w, args[i+1:])
		}
	}
	return s
}

// addFlag records one flag word and reports how many following words it
// swallowed as its value.
func (g *Grader) addFlag(s *stage, cmd string, w shellparse.Word, rest []shellparse.Word) int {
	text := w.Value

	// An explicit --flag=value needs no lookahead and no expansion.
	if strings.HasPrefix(text, "--") {
		name, value, hasValue := strings.Cut(text, "=")
		name = g.canonical(cmd, name)
		if hasValue {
			s.valued[name] = append(s.valued[name], token{value: value, quoted: w.Quoted})
			return 0
		}
		return s.take(name, g.takesValue(cmd, name), rest, w.Quoted)
	}

	// A word that is itself a documented flag is taken whole: `find -name`
	// is one option, not four of them clustered.
	if spec, ok := g.spec(cmd, text); ok {
		return s.take(g.canonical(cmd, text), spec.takesValue, rest, w.Quoted)
	}

	// Otherwise it is a cluster: walk it a letter at a time until one of them
	// turns out to carry the rest as its value.
	body := text[1:]
	for i := 0; i < len(body); i++ {
		name := "-" + string(body[i])
		spec, known := g.spec(cmd, name)
		if !known {
			// An undocumented letter means the dictionary cannot vouch for
			// this reading, so the word is kept exactly as it was typed.
			s.switches["-"+body[i:]]++
			return 0
		}
		if spec.takesValue {
			if value := body[i+1:]; value != "" {
				s.valued[name] = append(s.valued[name], token{value: value, quoted: w.Quoted})
				return 0
			}
			return s.take(name, true, rest, w.Quoted)
		}
		s.switches[name]++
	}
	return 0
}

// take records a flag, pulling its value from the following word when it has
// one, and reports how many words that consumed.
func (s *stage) take(name string, takesValue bool, rest []shellparse.Word, quoted bool) int {
	if !takesValue {
		s.switches[name]++
		return 0
	}
	if len(rest) == 0 {
		// A value-carrying flag with nothing after it: a flag card's answer
		// is often exactly this ("-xzvf"), so record it rather than refuse.
		s.valued[name] = append(s.valued[name], token{quoted: quoted})
		return 0
	}
	s.valued[name] = append(s.valued[name], token{value: rest[0].Value, quoted: rest[0].Quoted})
	return 1
}

// spec looks up what the dictionary documents about a flag of a command.
func (g *Grader) spec(cmd, flag string) (flagSpec, bool) {
	spec, ok := g.flags[cmd][flag]
	return spec, ok
}

func (g *Grader) takesValue(cmd, flag string) bool {
	spec, ok := g.spec(cmd, flag)
	return ok && spec.takesValue
}

// canonical folds a documented long-form flag onto its short spelling.
func (g *Grader) canonical(cmd, flag string) string {
	if short, ok := g.long[cmd][flag]; ok {
		return short
	}
	return flag
}

// isFlag reports whether a word is an option rather than an operand. A lone
// "-" is stdin and a negative number is an argument, so neither counts.
func isFlag(w shellparse.Word) bool {
	if w.Quoted || len(w.Value) < 2 || w.Value[0] != '-' {
		return false
	}
	return w.Value[1] < '0' || w.Value[1] > '9'
}

func redirKey(r shellparse.Redirect) string {
	return strconv.Itoa(r.Fd) + r.Op + r.Target.Value
}

// compare reduces two normalized answers to a verdict. Every difference is
// either hard — a different command, a different flag, a different filename —
// or soft, meaning the two sides differ only inside quoted text, where a
// pattern or a script may have more than one right spelling.
func compare(a, b normal) Verdict {
	if len(a.pipelines) != len(b.pipelines) || !equalStrings(a.ops, b.ops) {
		return Wrong
	}
	soft := false
	for i := range a.pipelines {
		if len(a.pipelines[i]) != len(b.pipelines[i]) {
			return Wrong
		}
		for j := range a.pipelines[i] {
			hard, s := diffStage(a.pipelines[i][j], b.pipelines[i][j])
			if hard {
				return Wrong
			}
			soft = soft || s
		}
	}
	if soft {
		return Unsure
	}
	return Correct
}

// diffStage compares two reduced commands, reporting whether they differ in a
// way that settles the question and whether they differ only in quoted text.
func diffStage(a, b stage) (hard, soft bool) {
	if a.name != b.name || !equalCounts(a.switches, b.switches) || !equalStrings(a.redirs, b.redirs) {
		return true, false
	}
	if len(a.valued) != len(b.valued) {
		return true, false
	}
	for name, mine := range a.valued {
		theirs, ok := b.valued[name]
		if !ok || len(mine) != len(theirs) {
			return true, false
		}
		for i := range mine {
			h, s := diffToken(mine[i], theirs[i])
			if h {
				return true, false
			}
			soft = soft || s
		}
	}
	if len(a.operands) != len(b.operands) {
		return true, false
	}
	for i := range a.operands {
		h, s := diffToken(a.operands[i], b.operands[i])
		if h {
			return true, false
		}
		soft = soft || s
	}
	return false, soft
}

// diffToken compares two words. Quoting is not part of the value, but two
// different quoted values are a regex or a script written two ways, and this
// package does not claim to know which of them is right.
func diffToken(a, b token) (hard, soft bool) {
	if a.value == b.value {
		return false, false
	}
	if a.quoted && b.quoted {
		return false, true
	}
	return true, false
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func equalCounts(a, b map[string]int) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// sortedKeys is used by the tests and by the diagnostic string below to make
// a stage's flags printable in a stable order.
func sortedKeys(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// String renders a reduced command, which is what a failing equivalence test
// prints to show how each side was read.
func (s stage) String() string {
	parts := []string{s.name}
	parts = append(parts, sortedKeys(s.switches)...)
	for _, name := range sortedKeys(countOf(s.valued)) {
		for _, v := range s.valued[name] {
			parts = append(parts, name+"="+v.value)
		}
	}
	for _, o := range s.operands {
		parts = append(parts, o.value)
	}
	parts = append(parts, s.redirs...)
	return strings.Join(parts, " ")
}

func countOf(m map[string][]token) map[string]int {
	out := make(map[string]int, len(m))
	for k, v := range m {
		out[k] = len(v)
	}
	return out
}
