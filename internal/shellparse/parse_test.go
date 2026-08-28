package shellparse

import (
	"errors"
	"strings"
	"testing"
)

func TestParsePipeline(t *testing.T) {
	s, err := Parse(`cut -d' ' -f1 access.log | sort | uniq -c | sort -rn | head -5`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(s.Pipelines) != 1 {
		t.Fatalf("got %d pipelines, want 1", len(s.Pipelines))
	}
	got := strings.Join(s.Names(), ",")
	if want := "cut,sort,uniq,sort,head"; got != want {
		t.Errorf("names = %q, want %q", got, want)
	}
	cut := s.Pipelines[0].Commands[0]
	if cut.Args[0].Value != "-d " {
		t.Errorf("quote removal on -d' ' gave %q, want %q", cut.Args[0].Value, "-d ")
	}
	if !cut.Args[0].Quoted {
		t.Errorf("-d' ' should be marked quoted")
	}
}

func TestParseQuoting(t *testing.T) {
	tests := []struct {
		in   string
		want []string // command name then argument values
	}{
		{`awk '{print $1}' f`, []string{"awk", "{print $1}", "f"}},
		{`grep -v '^$' file`, []string{"grep", "-v", "^$", "file"}},
		{`sed "s/a/b/g" f`, []string{"sed", "s/a/b/g", "f"}},
		{`echo a\ b`, []string{"echo", "a b"}},
		{`echo "it's"`, []string{"echo", "it's"}},
		{`echo 'say "hi"'`, []string{"echo", `say "hi"`}},
		{`echo a"b"c`, []string{"echo", "abc"}},
		{`tr -d '\n'`, []string{"tr", "-d", `\n`}},
	}
	for _, tc := range tests {
		s, err := Parse(tc.in)
		if err != nil {
			t.Errorf("Parse(%q): %v", tc.in, err)
			continue
		}
		c := s.Pipelines[0].Commands[0]
		got := []string{c.Name.Value}
		for _, a := range c.Args {
			got = append(got, a.Value)
		}
		if strings.Join(got, "\x00") != strings.Join(tc.want, "\x00") {
			t.Errorf("Parse(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestParseRedirects(t *testing.T) {
	s, err := Parse(`sort f > out.txt 2>&1 < in.txt`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	rs := s.Pipelines[0].Commands[0].Redirects
	if len(rs) != 3 {
		t.Fatalf("got %d redirects, want 3", len(rs))
	}
	if rs[0].Fd != 1 || rs[0].Op != ">" || rs[0].Target.Value != "out.txt" {
		t.Errorf("redirect 0 = %+v", rs[0])
	}
	if rs[1].Fd != 2 || rs[1].Op != ">&" || rs[1].Target.Value != "1" {
		t.Errorf("redirect 1 = %+v", rs[1])
	}
	if rs[2].Fd != 0 || rs[2].Op != "<" || rs[2].Target.Value != "in.txt" {
		t.Errorf("redirect 2 = %+v", rs[2])
	}
}

func TestParseLists(t *testing.T) {
	s, err := Parse(`ls; wc -l f && head -1 f || true`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(s.Pipelines) != 4 || len(s.Ops) != 3 {
		t.Fatalf("got %d pipelines / %d ops, want 4/3", len(s.Pipelines), len(s.Ops))
	}
	if s.Ops[0] != OpSeq || s.Ops[1] != OpAnd || s.Ops[2] != OpOr {
		t.Errorf("ops = %v", s.Ops)
	}
}

func TestParseAssignmentPrefix(t *testing.T) {
	s, err := Parse(`LC_ALL=C sort f`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	c := s.Pipelines[0].Commands[0]
	if len(c.Assignments) != 1 || c.Name.Value != "sort" {
		t.Fatalf("got assignments=%v name=%q", c.Assignments, c.Name.Value)
	}
	if name, ok := IsAssignment(c.Assignments[0]); !ok || name != "LC_ALL" {
		t.Errorf("IsAssignment = %q,%v", name, ok)
	}
}

// TestParseRejects covers the syntax the runner must never see. Each case is
// rejected by the parser rather than by the allowlist, because the allowlist
// only sees commands the parser could name.
func TestParseRejects(t *testing.T) {
	tests := []struct {
		in   string
		want string // substring of the expected message
	}{
		{"cat $(echo f)", "command substitution"},
		{"cat `echo f`", "command substitution"},
		{`echo "$(id)"`, "command substitution"},
		{"diff <(sort a) <(sort b)", "process substitution"},
		{"sleep 100 &", "background jobs"},
		{":(){ :|:& };:", "subshells and function definitions"},
		{"(cd / && ls)", "subshells and function definitions"},
		{"{ ls ; }", "command groups"},
		{"echo 'unterminated", "unterminated single quote"},
		{`echo "unterminated`, "unterminated double quote"},
		{"ls |", "expected a command after |"},
		{"| ls", "expected a command before |"},
		{"ls &&", "&& must be followed by another command"},
		{"", "nothing to run"},
		{"sort >", "expected a filename after >"},
	}
	for _, tc := range tests {
		_, err := Parse(tc.in)
		if err == nil {
			t.Errorf("Parse(%q) succeeded, want error containing %q", tc.in, tc.want)
			continue
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("Parse(%q) = %q, want it to contain %q", tc.in, err, tc.want)
		}
		var pe *Error
		if !errors.As(err, &pe) {
			t.Errorf("Parse(%q) returned %T, want *shellparse.Error", tc.in, err)
		}
	}
}

func TestErrorCaret(t *testing.T) {
	_, err := Parse("cat $(id)")
	var pe *Error
	if !errors.As(err, &pe) {
		t.Fatalf("want *Error, got %T", err)
	}
	want := "cat $(id)\n    ^"
	if got := pe.Caret(); got != want {
		t.Errorf("Caret() = %q, want %q", got, want)
	}
}
