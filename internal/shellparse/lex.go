package shellparse

import (
	"fmt"
	"strconv"
	"strings"
)

// Error is a parse failure, positioned at the byte offset that caused it.
type Error struct {
	Msg   string
	Pos   int
	Input string
}

func (e *Error) Error() string { return e.Msg }

// Caret renders the offending input with a caret under the failure, for the
// two-line message the practice screen shows above the editor.
func (e *Error) Caret() string {
	col := e.Pos
	if col < 0 {
		col = 0
	}
	if col > len(e.Input) {
		col = len(e.Input)
	}
	// Count display columns rather than bytes so multi-byte input lines up.
	width := len([]rune(e.Input[:col]))
	return e.Input + "\n" + strings.Repeat(" ", width) + "^"
}

func errf(pos int, input, format string, args ...any) *Error {
	return &Error{Msg: fmt.Sprintf(format, args...), Pos: pos, Input: input}
}

type tokenKind int

const (
	tWord tokenKind = iota
	tPipe
	tAndAnd
	tOrOr
	tSemi
	tAmp
	tRedir
	tLParen
	tRParen
	tLBrace
	tRBrace
	tEOF
)

func (k tokenKind) String() string {
	switch k {
	case tWord:
		return "word"
	case tPipe:
		return "|"
	case tAndAnd:
		return "&&"
	case tOrOr:
		return "||"
	case tSemi:
		return ";"
	case tAmp:
		return "&"
	case tRedir:
		return "redirection"
	case tLParen:
		return "("
	case tRParen:
		return ")"
	case tLBrace:
		return "{"
	case tRBrace:
		return "}"
	default:
		return "end of input"
	}
}

type token struct {
	kind   tokenKind
	val    string // unquoted value for words, operator text for redirections
	raw    string
	fd     int // redirections only
	quoted bool
	pos    int
}

// isMeta reports whether c ends an unquoted word.
func isMeta(c byte) bool {
	switch c {
	case ' ', '\t', '\n', '\r', '|', '&', ';', '<', '>', '(', ')':
		return true
	}
	return false
}

// lex splits src into tokens. It rejects the constructs that have no place in
// a sandboxed exercise — command substitution, process substitution, and
// unterminated quotes — at the point they appear, so the message can name the
// exact character rather than a downstream symptom.
func lex(src string) ([]token, error) {
	var toks []token
	i := 0
	for i < len(src) {
		c := src[i]
		switch {
		case c == ' ' || c == '\t' || c == '\r':
			i++
			continue
		case c == '\n':
			// The editor is single-line, but pasted input may wrap; a
			// newline separates pipelines exactly like ";".
			toks = append(toks, token{kind: tSemi, val: ";", raw: "\n", pos: i})
			i++
			continue
		case c == '#' && (len(toks) == 0 || toks[len(toks)-1].kind != tWord):
			// A comment runs to the end of the line.
			for i < len(src) && src[i] != '\n' {
				i++
			}
			continue
		case c == '|':
			if i+1 < len(src) && src[i+1] == '|' {
				toks = append(toks, token{kind: tOrOr, val: "||", raw: "||", pos: i})
				i += 2
				continue
			}
			toks = append(toks, token{kind: tPipe, val: "|", raw: "|", pos: i})
			i++
			continue
		case c == '&':
			if i+1 < len(src) && src[i+1] == '&' {
				toks = append(toks, token{kind: tAndAnd, val: "&&", raw: "&&", pos: i})
				i += 2
				continue
			}
			toks = append(toks, token{kind: tAmp, val: "&", raw: "&", pos: i})
			i++
			continue
		case c == ';':
			toks = append(toks, token{kind: tSemi, val: ";", raw: ";", pos: i})
			i++
			continue
		case c == '(':
			toks = append(toks, token{kind: tLParen, val: "(", raw: "(", pos: i})
			i++
			continue
		case c == ')':
			toks = append(toks, token{kind: tRParen, val: ")", raw: ")", pos: i})
			i++
			continue
		case c == '`':
			return nil, errf(i, src, "command substitution with backticks is not allowed in exercises")
		case c == '<' || c == '>':
			tok, next, err := lexRedirect(src, i, -1)
			if err != nil {
				return nil, err
			}
			toks = append(toks, tok)
			i = next
			continue
		}

		// A run of digits directly against "<" or ">" is a descriptor prefix
		// ("2>file"), not a word.
		if c >= '0' && c <= '9' {
			j := i
			for j < len(src) && src[j] >= '0' && src[j] <= '9' {
				j++
			}
			if j < len(src) && (src[j] == '<' || src[j] == '>') {
				fd, err := strconv.Atoi(src[i:j])
				if err != nil {
					return nil, errf(i, src, "file descriptor %q is not a number", src[i:j])
				}
				tok, next, lerr := lexRedirect(src, j, fd)
				if lerr != nil {
					return nil, lerr
				}
				tok.pos = i
				tok.raw = src[i:j] + tok.raw
				toks = append(toks, tok)
				i = next
				continue
			}
		}

		// Braces are only metacharacters when they stand alone; "{print $1}"
		// is an ordinary awk argument and must lex as a word.
		if (c == '{' || c == '}') && (i+1 >= len(src) || isMeta(src[i+1])) {
			kind := tLBrace
			if c == '}' {
				kind = tRBrace
			}
			toks = append(toks, token{kind: kind, val: string(c), raw: string(c), pos: i})
			i++
			continue
		}

		tok, next, err := lexWord(src, i)
		if err != nil {
			return nil, err
		}
		toks = append(toks, tok)
		i = next
	}
	toks = append(toks, token{kind: tEOF, pos: len(src)})
	return toks, nil
}

// lexRedirect reads the operator at src[i], which is "<" or ">", together with
// any second character that extends it.
func lexRedirect(src string, i, fd int) (token, int, error) {
	op := string(src[i])
	end := i + 1
	if end < len(src) {
		switch {
		case src[end] == '(':
			return token{}, 0, errf(i, src, "process substitution %s(...) is not allowed in exercises", op)
		case op == ">" && src[end] == '>':
			op, end = ">>", end+1
		case src[end] == '&':
			op, end = op+"&", end+1
		case op == ">" && src[end] == '|':
			// ">|" forces truncation; treat it as a plain ">".
			end++
		}
	}
	if fd < 0 {
		if strings.HasPrefix(op, "<") {
			fd = 0
		} else {
			fd = 1
		}
	}
	return token{kind: tRedir, val: op, raw: op, fd: fd, pos: i}, end, nil
}

// lexWord reads one word, applying quote removal as it goes and rejecting the
// expansions the runner will not execute.
func lexWord(src string, start int) (token, int, error) {
	var val strings.Builder
	quoted := false
	i := start
	for i < len(src) {
		c := src[i]
		if isMeta(c) {
			break
		}
		switch c {
		case '\\':
			if i+1 >= len(src) {
				return token{}, 0, errf(i, src, "trailing backslash: nothing to escape")
			}
			if src[i+1] == '\n' {
				i += 2 // line continuation
				continue
			}
			val.WriteByte(src[i+1])
			quoted = true
			i += 2
		case '\'':
			end := strings.IndexByte(src[i+1:], '\'')
			if end < 0 {
				return token{}, 0, errf(i, src, "unterminated single quote")
			}
			val.WriteString(src[i+1 : i+1+end])
			quoted = true
			i += end + 2
		case '"':
			var err error
			i, err = lexDoubleQuoted(src, i, &val)
			if err != nil {
				return token{}, 0, err
			}
			quoted = true
		case '`':
			return token{}, 0, errf(i, src, "command substitution with backticks is not allowed in exercises")
		case '$':
			if i+1 < len(src) && src[i+1] == '(' {
				return token{}, 0, errf(i, src, "command substitution $(...) is not allowed in exercises")
			}
			val.WriteByte(c)
			i++
		default:
			val.WriteByte(c)
			i++
		}
	}
	if i == start {
		return token{}, 0, errf(start, src, "unexpected %q", string(src[start]))
	}
	return token{kind: tWord, val: val.String(), raw: src[start:i], quoted: quoted, pos: start}, i, nil
}

// lexDoubleQuoted consumes a "..." run starting at src[i], appending its
// contents to val, and returns the index just past the closing quote.
func lexDoubleQuoted(src string, i int, val *strings.Builder) (int, error) {
	open := i
	i++ // opening quote
	for i < len(src) {
		switch src[i] {
		case '"':
			return i + 1, nil
		case '\\':
			if i+1 >= len(src) {
				return 0, errf(i, src, "trailing backslash inside double quotes")
			}
			// Inside double quotes a backslash is literal unless it escapes
			// one of the four characters that keep their meaning there.
			switch src[i+1] {
			case '$', '`', '"', '\\':
				val.WriteByte(src[i+1])
			default:
				val.WriteByte('\\')
				val.WriteByte(src[i+1])
			}
			i += 2
		case '`':
			return 0, errf(i, src, "command substitution with backticks is not allowed in exercises")
		case '$':
			if i+1 < len(src) && src[i+1] == '(' {
				return 0, errf(i, src, "command substitution $(...) is not allowed in exercises")
			}
			val.WriteByte('$')
			i++
		default:
			val.WriteByte(src[i])
			i++
		}
	}
	return 0, errf(open, src, "unterminated double quote")
}
