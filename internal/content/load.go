package content

import (
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Source is a content tree: the directory layout described in SPEC.md, with
// commands/, exercises/, cards/, fixtures/ and expected/ at its root.
type Source fs.FS

// Load parses every YAML file in src and validates the result. A Library is
// returned only when validation passes; otherwise the error is a *LintError
// carrying every problem found, so authors see all of them at once.
func Load(src Source) (*Library, error) {
	lib := &Library{
		src:          src,
		byCommandID:  map[string]*Command{},
		byExerciseID: map[string]*Exercise{},
		byCardID:     map[string]*Card{},
	}
	var errs LintError

	// Commands: one entry per file.
	if err := eachYAML(src, "commands", func(name string, data []byte) {
		var c Command
		if err := yaml.Unmarshal(data, &c); err != nil {
			errs.addf(name, "parse: %v", err)
			return
		}
		lib.Commands = append(lib.Commands, &c)
	}); err != nil {
		return nil, err
	}

	// Exercises: one entry per file.
	if err := eachYAML(src, "exercises", func(name string, data []byte) {
		var e Exercise
		if err := yaml.Unmarshal(data, &e); err != nil {
			errs.addf(name, "parse: %v", err)
			return
		}
		lib.Exercises = append(lib.Exercises, &e)
	}); err != nil {
		return nil, err
	}

	// Cards: a list per file, since cards are small and grouped by topic.
	if err := eachYAML(src, "cards", func(name string, data []byte) {
		var batch []*Card
		if err := yaml.Unmarshal(data, &batch); err != nil {
			errs.addf(name, "parse: %v", err)
			return
		}
		lib.Cards = append(lib.Cards, batch...)
	}); err != nil {
		return nil, err
	}

	if len(errs.Problems) > 0 {
		return nil, &errs
	}

	lib.index()
	lib.buildTracks()

	if problems := Lint(lib, src); len(problems) > 0 {
		return nil, &LintError{Problems: problems}
	}
	return lib, nil
}

// eachYAML calls fn for every .yaml file directly under dir, in sorted order so
// that load order — and therefore display order within a category — is stable.
func eachYAML(src Source, dir string, fn func(name string, data []byte)) error {
	entries, err := fs.ReadDir(src, dir)
	if err != nil {
		return fmt.Errorf("read %s/: %w", dir, err)
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)
	for _, n := range names {
		p := path.Join(dir, n)
		data, err := fs.ReadFile(src, p)
		if err != nil {
			return fmt.Errorf("read %s: %w", p, err)
		}
		fn(p, data)
	}
	return nil
}

func (l *Library) index() {
	for _, c := range l.Commands {
		l.byCommandID[c.ID] = c
	}
	for _, e := range l.Exercises {
		l.byExerciseID[e.ID] = e
	}
	for _, c := range l.Cards {
		l.byCardID[c.ID] = c
	}
}

// buildTracks groups exercises by track and orders each track by level, then by
// id, so a learner walks a track in increasing difficulty.
func (l *Library) buildTracks() {
	byName := map[string]*Track{}
	var order []string
	for _, e := range l.Exercises {
		t, ok := byName[e.Track]
		if !ok {
			t = &Track{Name: e.Track}
			byName[e.Track] = t
			order = append(order, e.Track)
		}
		t.Exercises = append(t.Exercises, e)
	}
	sort.Strings(order)
	for _, name := range order {
		t := byName[name]
		sort.SliceStable(t.Exercises, func(i, j int) bool {
			if t.Exercises[i].Level != t.Exercises[j].Level {
				return t.Exercises[i].Level < t.Exercises[j].Level
			}
			return t.Exercises[i].ID < t.Exercises[j].ID
		})
		l.Tracks = append(l.Tracks, t)
	}
}
