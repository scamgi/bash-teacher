package shellparse

import (
	"regexp"
	"strings"
)

// assignmentRe matches the VAR=value prefix form. The parser recognises it so
// that the policy layer can reject it with a message about assignments rather
// than reporting "VAR=value" as an unknown command.
var assignmentRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)

// Parse turns one line of learner input into a Script.
//
// Every error it returns is a *Error carrying the offset of the offending
// character, so the caller can draw a caret under it. Constructs that are
// valid shell but out of scope for a sandboxed exercise — subshells, command
// groups, background jobs — are rejected here with an explanation, not
// silently mis-parsed.
func Parse(src string) (*Script, error) {
	toks, err := lex(src)
	if err != nil {
		return nil, err
	}
	p := &parser{src: src, toks: toks}
	return p.script()
}

type parser struct {
	src  string
	toks []token
	i    int
}

func (p *parser) peek() token         { return p.toks[p.i] }
func (p *parser) next() token         { t := p.toks[p.i]; p.i++; return t }
func (p *parser) at(k tokenKind) bool { return p.toks[p.i].kind == k }

func (p *parser) errf(t token, format string, args ...any) *Error {
	return errf(t.pos, p.src, format, args...)
}

func (p *parser) script() (*Script, error) {
	if p.at(tEOF) {
		return nil, p.errf(p.peek(), "nothing to run")
	}
	s := &Script{}
	for {
		pl, err := p.pipeline()
		if err != nil {
			return nil, err
		}
		s.Pipelines = append(s.Pipelines, pl)

		t := p.peek()
		switch t.kind {
		case tEOF:
			return s, nil
		case tSemi, tAndAnd, tOrOr:
			p.next()
			// A trailing separator is fine; an empty one in the middle is not.
			if p.at(tEOF) {
				if t.kind == tSemi {
					return s, nil
				}
				return nil, p.errf(t, "%s must be followed by another command", t.val)
			}
			s.Ops = append(s.Ops, opFor(t.kind))
		case tAmp:
			return nil, p.errf(t, "background jobs (&) are not allowed in exercises")
		default:
			return nil, p.errf(t, "unexpected %s", t.kind)
		}
	}
}

func opFor(k tokenKind) Op {
	switch k {
	case tAndAnd:
		return OpAnd
	case tOrOr:
		return OpOr
	default:
		return OpSeq
	}
}

func (p *parser) pipeline() (*Pipeline, error) {
	pl := &Pipeline{}
	for {
		c, err := p.command()
		if err != nil {
			return nil, err
		}
		pl.Commands = append(pl.Commands, c)
		if !p.at(tPipe) {
			return pl, nil
		}
		pipe := p.next()
		if p.at(tEOF) || p.at(tPipe) {
			return nil, p.errf(pipe, "expected a command after |")
		}
	}
}

func (p *parser) command() (*Command, error) {
	c := &Command{Pos: p.peek().pos}
	for {
		t := p.peek()
		switch t.kind {
		case tWord:
			p.next()
			w := wordOf(t)
			switch {
			case c.Name.Raw == "" && assignmentRe.MatchString(t.raw) && !t.quoted:
				c.Assignments = append(c.Assignments, w)
			case c.Name.Raw == "":
				c.Name = w
			default:
				c.Args = append(c.Args, w)
			}
		case tRedir:
			p.next()
			target := p.peek()
			if target.kind != tWord {
				return nil, p.errf(target, "expected a filename after %s", t.val)
			}
			p.next()
			c.Redirects = append(c.Redirects, Redirect{Fd: t.fd, Op: t.val, Target: wordOf(target), Pos: t.pos})
		case tPipe, tSemi, tAndAnd, tOrOr, tEOF:
			if c.Name.Raw == "" {
				if len(c.Assignments) > 0 {
					// "FOO=bar" alone: still a command as far as the policy
					// layer is concerned, so let it through to be rejected
					// with an assignment-specific message.
					return c, nil
				}
				return nil, p.errf(t, "expected a command before %s", t.kind)
			}
			return c, nil
		case tAmp:
			return nil, p.errf(t, "background jobs (&) are not allowed in exercises")
		case tLParen, tRParen:
			return nil, p.errf(t, "subshells and function definitions are not supported in exercises")
		case tLBrace, tRBrace:
			return nil, p.errf(t, "command groups { ... } are not supported in exercises")
		default:
			return nil, p.errf(t, "unexpected %s", t.kind)
		}
	}
}

func wordOf(t token) Word {
	return Word{Value: t.val, Raw: t.raw, Quoted: t.quoted, Pos: t.pos}
}

// IsAssignment reports whether w has the shape of a VAR=value prefix, and
// returns the variable name.
func IsAssignment(w Word) (string, bool) {
	if !assignmentRe.MatchString(w.Raw) {
		return "", false
	}
	name, _, _ := strings.Cut(w.Raw, "=")
	return name, true
}
