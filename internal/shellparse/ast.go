// Package shellparse turns learner-typed shell text into a small syntax tree.
//
// It is deliberately not a shell: it understands the sliver of POSIX syntax
// that pipeline exercises need — words, quoting, pipes, redirections, and the
// `;`, `&&`, `||` separators — and reports a friendly error for everything
// else. Anything it cannot represent is something the sandbox would rather not
// run, so a parse failure is a safety feature and not a limitation to work
// around. The static allowlist in internal/runner is applied to the tree this
// package produces.
package shellparse

import (
	"strconv"
	"strings"
)

// Word is one command name, argument, or redirection target, recorded both as
// the literal value the shell would hand to the command and as it was typed.
// Both are needed: the value is what a path check must reason about, and the
// raw text is what an error message should quote back to the learner.
type Word struct {
	Value  string // after quote removal
	Raw    string // exactly as typed
	Quoted bool   // some part of the word was inside quotes
	Pos    int    // byte offset of the first character
}

// Redirect is one redirection attached to a command.
type Redirect struct {
	Fd     int    // the descriptor being redirected: 0 for "<", 1 for ">"
	Op     string // "<", ">", ">>", ">&", "<&"
	Target Word
	Pos    int
}

// Command is a simple command: leading VAR=value assignments, a name, its
// arguments, and its redirections.
type Command struct {
	Assignments []Word
	Name        Word
	Args        []Word
	Redirects   []Redirect
	Pos         int
}

// Words returns the command's name followed by its arguments.
func (c *Command) Words() []Word {
	out := make([]Word, 0, len(c.Args)+1)
	out = append(out, c.Name)
	out = append(out, c.Args...)
	return out
}

// Pipeline is a run of commands joined by "|".
type Pipeline struct{ Commands []*Command }

// Op joins two pipelines within a script.
type Op string

// The list separators the parser accepts.
const (
	OpSeq Op = ";"
	OpAnd Op = "&&"
	OpOr  Op = "||"
)

// Script is one line of learner input: pipelines joined by operators, with
// len(Ops) always one less than len(Pipelines).
type Script struct {
	Pipelines []*Pipeline
	Ops       []Op
}

// Commands returns every simple command in the script, in source order.
func (s *Script) Commands() []*Command {
	var out []*Command
	for _, p := range s.Pipelines {
		out = append(out, p.Commands...)
	}
	return out
}

// Names returns the name of every command in the script, in source order.
func (s *Script) Names() []string {
	cmds := s.Commands()
	out := make([]string, 0, len(cmds))
	for _, c := range cmds {
		out = append(out, c.Name.Value)
	}
	return out
}

// String re-renders the script from the raw text of its words. It is used in
// tests and in the "you wrote" line of a failed run, not to build anything
// that gets executed — the sandbox always runs the learner's original text.
func (s *Script) String() string {
	var b strings.Builder
	for i, p := range s.Pipelines {
		if i > 0 {
			b.WriteString(" " + string(s.Ops[i-1]) + " ")
		}
		for j, c := range p.Commands {
			if j > 0 {
				b.WriteString(" | ")
			}
			b.WriteString(c.String())
		}
	}
	return b.String()
}

// String re-renders one command from the raw text of its words.
func (c *Command) String() string {
	parts := make([]string, 0, len(c.Assignments)+len(c.Args)+1+len(c.Redirects))
	for _, a := range c.Assignments {
		parts = append(parts, a.Raw)
	}
	parts = append(parts, c.Name.Raw)
	for _, a := range c.Args {
		parts = append(parts, a.Raw)
	}
	for _, r := range c.Redirects {
		// A bare "> file" or "< file" already implies its descriptor;
		// anything else has to spell the number out.
		implied := (r.Fd == 1 && (r.Op == ">" || r.Op == ">>")) || (r.Fd == 0 && r.Op == "<")
		prefix := ""
		if !implied {
			prefix = strconv.Itoa(r.Fd)
		}
		parts = append(parts, prefix+r.Op+r.Target.Raw)
	}
	return strings.Join(parts, " ")
}
