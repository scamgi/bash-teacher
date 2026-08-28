package content

import (
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strings"
)

// maxFixtureBytes is the per-file ceiling for fixture files. Fixtures ship in
// the binary and are copied before every run, so they stay small.
const maxFixtureBytes = 256 * 1024

// Problem is one lint finding, attributed to the file or id it came from.
type Problem struct {
	Where   string // file path or content id
	Message string
}

func (p Problem) String() string { return p.Where + ": " + p.Message }

// LintError collects every problem found in a content set.
type LintError struct{ Problems []Problem }

func (e *LintError) addf(where, format string, args ...any) {
	e.Problems = append(e.Problems, Problem{Where: where, Message: fmt.Sprintf(format, args...)})
}

func (e *LintError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d content problem(s):", len(e.Problems))
	for _, p := range e.Problems {
		b.WriteString("\n  " + p.String())
	}
	return b.String()
}

var idRe = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// Lint validates a loaded library: schema completeness, unique ids, resolvable
// cross-references, and fixture existence and size. It is the same check the
// `bt content lint` command runs in CI.
func Lint(l *Library, src Source) []Problem {
	var e LintError

	validCat := map[Category]bool{}
	for _, c := range Categories {
		validCat[c] = true
	}

	seen := map[string]string{} // id -> where it was first defined

	for _, c := range l.Commands {
		where := "command " + c.ID
		checkID(&e, where, c.ID)
		if prev, dup := seen[c.ID]; dup {
			e.addf(where, "duplicate id, already defined by %s", prev)
		}
		seen[c.ID] = where
		requireNonEmpty(&e, where, map[string]string{
			"name":     c.Name,
			"summary":  c.Summary,
			"purpose":  c.Purpose,
			"synopsis": c.Synopsis,
		})
		if !validCat[c.Category] {
			e.addf(where, "unknown category %q", c.Category)
		}
		if len(c.Examples) < 1 {
			e.addf(where, "needs at least one example")
		}
		for i, ex := range c.Examples {
			if strings.TrimSpace(ex.Cmd) == "" {
				e.addf(where, "example %d has no cmd", i+1)
			}
			if strings.TrimSpace(ex.Caption) == "" {
				e.addf(where, "example %d (%s) has no caption", i+1, ex.Cmd)
			}
		}
		for i, f := range c.Flags {
			if strings.TrimSpace(f.Flag) == "" {
				e.addf(where, "flag %d has no flag string", i+1)
			}
			if strings.TrimSpace(f.Gloss) == "" {
				e.addf(where, "flag %q has no gloss", f.Flag)
			}
		}
	}

	// Cross-references between commands are checked after every command is
	// indexed, so forward references are fine.
	for _, c := range l.Commands {
		where := "command " + c.ID
		checkRefs(&e, where, l, "plays_well_with", c.PlaysWellWith)
		checkRefs(&e, where, l, "see_also", c.SeeAlso)
		if slicesContains(c.PlaysWellWith, c.ID) || slicesContains(c.SeeAlso, c.ID) {
			e.addf(where, "references itself")
		}
	}

	validMatch := map[MatchMode]bool{MatchExact: true, MatchTrimmed: true, MatchUnordered: true, MatchRegex: true}
	for _, x := range l.Exercises {
		where := "exercise " + x.ID
		checkID(&e, where, x.ID)
		if prev, dup := seen[x.ID]; dup {
			e.addf(where, "duplicate id, already defined by %s", prev)
		}
		seen[x.ID] = where
		requireNonEmpty(&e, where, map[string]string{
			"track":              x.Track,
			"title":              x.Title,
			"prompt":             x.Prompt,
			"fixture":            x.Fixture,
			"reference_solution": x.ReferenceSolution,
		})
		if x.Level < 1 || x.Level > 5 {
			e.addf(where, "level %d is outside 1..5", x.Level)
		}
		if !validMatch[x.Match] {
			e.addf(where, "unknown match mode %q", x.Match)
		}
		if len(x.Teaches) == 0 {
			e.addf(where, "teaches no commands")
		}
		checkRefs(&e, where, l, "teaches", x.Teaches)
		checkRefs(&e, where, l, "must_use", x.MustUse)
		checkRefs(&e, where, l, "forbid", x.Forbid)
		for _, id := range x.MustUse {
			if slicesContains(x.Forbid, id) {
				e.addf(where, "%q is both required and forbidden", id)
			}
		}
		for _, id := range x.Teaches {
			if c, ok := l.Command(id); ok && !c.CanExecute() {
				e.addf(where, "teaches %q, which the runner may never execute", id)
			}
		}
		if len(x.Hints) == 0 {
			e.addf(where, "has no hints")
		}
		checkFixture(&e, where, src, x.Fixture)
		if x.ExpectedStdoutFile == "" {
			e.addf(where, "no expected_stdout_file")
		} else if _, err := fs.Stat(src, x.ExpectedStdoutFile); err != nil {
			e.addf(where, "expected_stdout_file %q not found", x.ExpectedStdoutFile)
		}
	}

	validType := map[CardType]bool{CardRecall: true, CardIdentify: true, CardFlag: true}
	for _, c := range l.Cards {
		where := "card " + c.ID
		checkID(&e, where, c.ID)
		if prev, dup := seen[c.ID]; dup {
			e.addf(where, "duplicate id, already defined by %s", prev)
		}
		seen[c.ID] = where
		requireNonEmpty(&e, where, map[string]string{"front": c.Front, "back": c.Back})
		if !validType[c.Type] {
			e.addf(where, "unknown card type %q", c.Type)
		}
		// Every card must be reachable from the dictionary, so that mastery
		// per command can be computed and cards can be found from an entry.
		if len(c.Commands) == 0 {
			e.addf(where, "is not linked to any command")
		}
		checkRefs(&e, where, l, "commands", c.Commands)
	}

	sort.SliceStable(e.Problems, func(i, j int) bool { return e.Problems[i].Where < e.Problems[j].Where })
	return e.Problems
}

func checkID(e *LintError, where, id string) {
	if id == "" {
		e.addf(where, "missing id")
		return
	}
	if !idRe.MatchString(id) {
		e.addf(where, "id %q is not lower-case kebab-case", id)
	}
}

func requireNonEmpty(e *LintError, where string, fields map[string]string) {
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if strings.TrimSpace(fields[k]) == "" {
			e.addf(where, "missing %s", k)
		}
	}
}

func checkRefs(e *LintError, where string, l *Library, field string, ids []string) {
	for _, id := range ids {
		if _, ok := l.Command(id); !ok {
			e.addf(where, "%s references unknown command %q", field, id)
		}
	}
}

// checkFixture verifies the fixture directory exists and holds only plain files
// within the size ceiling. Anything else — a symlink, a nested directory, an
// oversized blob — is rejected here rather than surprising the runner later.
func checkFixture(e *LintError, where string, src Source, name string) {
	if name == "" {
		return
	}
	root := path.Join("fixtures", name)
	entries, err := fs.ReadDir(src, root)
	if err != nil {
		e.addf(where, "fixture %q not found under fixtures/", name)
		return
	}
	if len(entries) == 0 {
		e.addf(where, "fixture %q is empty", name)
	}
	for _, ent := range entries {
		if ent.IsDir() {
			e.addf(where, "fixture %q contains a subdirectory %q; fixtures are flat", name, ent.Name())
			continue
		}
		info, err := ent.Info()
		if err != nil {
			e.addf(where, "fixture %q: cannot stat %q: %v", name, ent.Name(), err)
			continue
		}
		if !info.Mode().IsRegular() {
			e.addf(where, "fixture %q: %q is not a regular file", name, ent.Name())
		}
		if info.Size() > maxFixtureBytes {
			e.addf(where, "fixture %q: %q is %d bytes, over the %d-byte limit", name, ent.Name(), info.Size(), maxFixtureBytes)
		}
	}
}

func slicesContains(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}
