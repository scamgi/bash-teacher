// Package runner executes learner-typed pipelines against a fixture
// filesystem and compares the output with what an exercise expects.
//
// Execution is layered, and every layer is independently sufficient to stop
// the obvious attacks: the parser (internal/shellparse) refuses syntax it
// cannot reason about, the policy in this file refuses commands and paths the
// tree does contain, the fixture is a throwaway copy in a temp directory, the
// OS sandbox confines what escapes the first two, and rlimits plus a wall
// clock cap bound what the sandbox still allows. Weakening one layer is not
// made safe by the others.
package runner

import (
	"path"
	"regexp"
	"sort"
	"strings"

	"bash-teacher/internal/content"
	"bash-teacher/internal/shellparse"
)

// Builtins are the shell builtins the sandbox permits alongside the commands
// derived from the dictionary. They are listed here rather than in the
// dictionary because they are shell syntax, not programs.
var Builtins = []string{"echo", "printf", "test", "["}

// Kind classifies a static-check failure, so the UI can style constraint
// violations differently from safety refusals.
type Kind string

// The reasons a script can be refused before it ever runs.
const (
	KindUnknownCommand Kind = "unknown-command"
	KindNotExecutable  Kind = "not-executable"
	KindForbiddenPath  Kind = "forbidden-path"
	KindDangerous      Kind = "dangerous"
	KindAssignment     Kind = "assignment"
	KindConstraint     Kind = "constraint"
)

// Violation is one reason a script was refused. A script with any violation is
// never executed.
type Violation struct {
	Kind    Kind
	Subject string // the command or word the violation is about
	Message string // learner-facing explanation
	Pos     int    // byte offset into the input, for a caret
}

func (v Violation) String() string { return v.Message }

// dangerous names commands that must never run even if a future dictionary
// entry documents them. The message matters: a learner who types `sudo` should
// learn why it is pointless here, not just that it was rejected.
var dangerous = map[string]string{
	"sudo":     "there is no privilege to escalate to inside the sandbox",
	"su":       "there is no privilege to escalate to inside the sandbox",
	"doas":     "there is no privilege to escalate to inside the sandbox",
	"mount":    "the sandbox filesystem is fixed for the duration of a run",
	"umount":   "the sandbox filesystem is fixed for the duration of a run",
	"exec":     "exec would replace the sandbox shell itself",
	"eval":     "eval hides from the static check what is about to run",
	"source":   "sourcing a file runs text the static check never saw",
	".":        "sourcing a file runs text the static check never saw",
	"trap":     "trap defers work past the point the sandbox tears down",
	"ulimit":   "the sandbox sets its own resource limits",
	"shutdown": "the sandbox has no machine to shut down",
	"reboot":   "the sandbox has no machine to reboot",
	"crontab":  "nothing scheduled outside a run would survive it",
	"chroot":   "the sandbox root is already chosen for you",
}

// devicePaths are the absolute paths an exercise may name despite the rule
// against absolute paths. Redirecting to /dev/null is a core idiom the
// dictionary teaches, and none of these expose anything the fixture does not.
// /dev/full is deliberately absent: writing to it is a denial-of-service
// pattern, not a technique worth learning.
var devicePaths = map[string]bool{
	"/dev/null":    true,
	"/dev/stdout":  true,
	"/dev/stderr":  true,
	"/dev/stdin":   true,
	"/dev/zero":    true,
	"/dev/urandom": true,
	"/dev/random":  true,
	"/dev/tty":     true,
}

// delimiterFlags are the options whose value is a field separator rather than
// a filename, so that `cut -d / -f2` is not mistaken for a command reaching
// for the root directory.
var delimiterFlags = map[string]bool{
	"-d": true, "--delimiter": true,
	"-t": true, "--field-separator": true,
	"-F": true, "--output-delimiter": true,
}

// setuidRe matches a symbolic chmod mode that sets the setuid or setgid bit.
var setuidRe = regexp.MustCompile(`[+=][rwxXugo]*s`)

// octalSetuidRe matches a four-digit octal mode whose high digit is non-zero,
// which is where setuid, setgid and the sticky bit live.
var octalSetuidRe = regexp.MustCompile(`^[1-7][0-7]{3}$`)

// Policy is the static allowlist. It is derived from the dictionary — a
// command is executable because it is documented and marked executable, never
// because someone added it here by hand.
type Policy struct {
	allowed    map[string]bool
	documented map[string]bool // in the dictionary but never executable
}

// NewPolicy derives the allowlist from a content library.
func NewPolicy(lib *content.Library) *Policy {
	p := &Policy{allowed: map[string]bool{}, documented: map[string]bool{}}
	for _, c := range lib.Commands {
		if c.CanExecute() {
			p.allowed[c.Name] = true
			continue
		}
		p.documented[c.Name] = true
	}
	for _, b := range Builtins {
		p.allowed[b] = true
	}
	return p
}

// Allows reports whether a command name may be executed.
func (p *Policy) Allows(name string) bool { return p.allowed[name] }

// Names returns every executable command name, sorted, for `bt doctor`.
func (p *Policy) Names() []string {
	out := make([]string, 0, len(p.allowed))
	for n := range p.allowed {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// Check applies every static rule to a parsed script and returns all the
// reasons it may not run. Like the content linter, it reports the whole set
// rather than the first failure, so a learner fixing one problem is not
// ambushed by the next.
func (p *Policy) Check(s *shellparse.Script) []Violation {
	var vs []Violation
	for _, c := range s.Commands() {
		vs = append(vs, p.checkCommand(c)...)
	}
	return vs
}

func (p *Policy) checkCommand(c *shellparse.Command) []Violation {
	var vs []Violation
	name := c.Name.Value

	for _, a := range c.Assignments {
		v, _ := shellparse.IsAssignment(a)
		vs = append(vs, Violation{
			Kind: KindAssignment, Subject: v, Pos: a.Pos,
			Message: "variable assignments like " + a.Raw + " are not available in exercises; " +
				"the sandbox runs with a fixed environment",
		})
	}
	if name == "" {
		return vs
	}

	switch {
	case dangerous[name] != "":
		vs = append(vs, Violation{
			Kind: KindDangerous, Subject: name, Pos: c.Name.Pos,
			Message: name + " is never available: " + dangerous[name],
		})
	case p.documented[name]:
		vs = append(vs, Violation{
			Kind: KindNotExecutable, Subject: name, Pos: c.Name.Pos,
			Message: name + " is in the dictionary to read about, but the sandbox has no network " +
				"and never executes it",
		})
	case !p.allowed[name]:
		vs = append(vs, Violation{
			Kind: KindUnknownCommand, Subject: name, Pos: c.Name.Pos,
			Message: name + " is not one of the commands this sandbox can run; only commands in " +
				"the dictionary are on the allowlist",
		})
	}

	if name == "chmod" {
		vs = append(vs, checkChmod(c)...)
	}

	for i, w := range c.Args {
		if w.Value == "/" {
			// A lone "/" is either the root directory or a field separator,
			// and the two are told apart by what precedes it: a delimiter
			// option, or tr, whose operands are always character sets.
			prev := ""
			if i > 0 {
				prev = c.Args[i-1].Value
			}
			if name == "tr" || delimiterFlags[prev] {
				continue
			}
			vs = append(vs, Violation{
				Kind: KindForbiddenPath, Subject: "/", Pos: w.Pos,
				Message: "/ is the root of the real filesystem; exercises work inside the fixture directory",
			})
			continue
		}
		vs = append(vs, checkPath(w)...)
	}
	for _, r := range c.Redirects {
		if r.Op == ">&" || r.Op == "<&" {
			// The target is a descriptor number, not a path.
			continue
		}
		vs = append(vs, checkPath(r.Target)...)
	}
	return vs
}

// checkChmod refuses modes that set the setuid, setgid or sticky bit. Nothing
// inside the sandbox could use them, but a rejection that explains itself
// teaches more than a mode that silently does nothing.
func checkChmod(c *shellparse.Command) []Violation {
	var vs []Violation
	for _, a := range c.Args {
		if setuidRe.MatchString(a.Value) || octalSetuidRe.MatchString(a.Value) {
			vs = append(vs, Violation{
				Kind: KindDangerous, Subject: a.Value, Pos: a.Pos,
				Message: "chmod " + a.Raw + " would set the setuid, setgid or sticky bit, which the sandbox refuses",
			})
		}
	}
	return vs
}

// checkPath refuses arguments that reach outside the fixture directory.
//
// It works on the value after quote removal, and on the part after the first
// "=" as well, so --file=/etc/passwd is caught alongside /etc/passwd. Escape
// detection cleans the path rather than searching for ".." anywhere in the
// text: ../../etc/passwd resolves above the root and is refused, while
// sed 's/../X/' resolves to "X" and is not a path at all.
func checkPath(w shellparse.Word) []Violation {
	var vs []Violation
	if strings.HasPrefix(w.Raw, "~") {
		vs = append(vs, Violation{
			Kind: KindForbiddenPath, Subject: w.Raw, Pos: w.Pos,
			Message: "~ expands to a home directory outside the fixture, so it is not allowed",
		})
	}
	for _, cand := range pathCandidates(w.Value) {
		switch {
		case isAbsolutePath(cand):
			vs = append(vs, Violation{
				Kind: KindForbiddenPath, Subject: cand, Pos: w.Pos,
				Message: cand + " is an absolute path; exercises may only touch files in the fixture directory",
			})
		case escapesRoot(cand):
			vs = append(vs, Violation{
				Kind: KindForbiddenPath, Subject: cand, Pos: w.Pos,
				Message: cand + " climbs out of the fixture directory with ..",
			})
		}
	}
	return vs
}

// pathCandidates returns the parts of a word that could name a file: the word
// itself, and whatever follows the first "=" in an option like --file=NAME.
func pathCandidates(v string) []string {
	if v == "" {
		return nil
	}
	out := []string{v}
	if _, after, ok := strings.Cut(v, "="); ok && after != "" {
		out = append(out, after)
	}
	return out
}

// isAbsolutePath reports whether v names a file by absolute path. The device
// paths above are excluded, and so is a lone "/", which checkCommand handles
// separately because deciding it needs the surrounding argument.
func isAbsolutePath(v string) bool {
	return len(v) > 1 && v[0] == '/' && !devicePaths[v]
}

// escapesRoot reports whether v resolves above the directory it starts in.
func escapesRoot(v string) bool {
	c := path.Clean(v)
	return c == ".." || strings.HasPrefix(c, "../")
}

// CheckConstraints applies an exercise's must_use and forbid lists. These are
// pedagogy, not safety: they force a technique. They are reported separately
// so the practice screen can explain the rule instead of showing a diff.
func (p *Policy) CheckConstraints(s *shellparse.Script, mustUse, forbid []string) []Violation {
	used := map[string]bool{}
	for _, c := range s.Commands() {
		used[c.Name.Value] = true
	}
	var vs []Violation
	for _, want := range mustUse {
		if !used[want] {
			vs = append(vs, Violation{
				Kind: KindConstraint, Subject: want,
				Message: "this exercise asks you to solve it with " + want,
			})
		}
	}
	for _, banned := range forbid {
		if used[banned] {
			vs = append(vs, Violation{
				Kind: KindConstraint, Subject: banned,
				Message: "this exercise asks you to solve it without " + banned,
			})
		}
	}
	return vs
}
