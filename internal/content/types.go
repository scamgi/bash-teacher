// Package content defines the schema for the bash-teacher learning library and
// the loader and linter that turn the embedded YAML into validated Go values.
package content

import (
	"fmt"
	"io/fs"
	"path"
	"strings"
)

// Category groups commands in the dictionary. The order of the slice below is
// the order categories are presented in the UI.
type Category string

const (
	CatNavigation  Category = "navigation"
	CatInspection  Category = "inspection"
	CatText        Category = "text"
	CatSearch      Category = "search"
	CatStreams     Category = "streams"
	CatProcess     Category = "process"
	CatArchives    Category = "archives"
	CatPermissions Category = "permissions"
	CatNetwork     Category = "network"
)

// Categories is the display order of dictionary categories.
var Categories = []Category{
	CatNavigation, CatInspection, CatText, CatSearch, CatStreams,
	CatProcess, CatArchives, CatPermissions, CatNetwork,
}

// CategoryTitles maps a category to its human-readable heading.
var CategoryTitles = map[Category]string{
	CatNavigation:  "Files & Navigation",
	CatInspection:  "Inspection",
	CatText:        "Text Processing",
	CatSearch:      "Search & Find",
	CatStreams:     "Streams & Redirection",
	CatProcess:     "Process Control",
	CatArchives:    "Archives",
	CatPermissions: "Permissions",
	CatNetwork:     "Networking",
}

// Flag is one option of a command, with the one-line gloss shown in the
// dictionary's flag table.
type Flag struct {
	Flag  string `yaml:"flag"`
	Gloss string `yaml:"gloss"`
	// Long is the long-form spelling, when one exists. Answer normalization
	// treats it as equivalent to Flag.
	Long string `yaml:"long,omitempty"`
}

// Example is a worked invocation plus the reason a learner would reach for it.
type Example struct {
	Cmd     string `yaml:"cmd"`
	Caption string `yaml:"caption"`
}

// Command is one dictionary entry.
type Command struct {
	ID            string    `yaml:"id"`
	Name          string    `yaml:"name"`
	Category      Category  `yaml:"category"`
	Summary       string    `yaml:"summary"`
	Purpose       string    `yaml:"purpose"`
	Synopsis      string    `yaml:"synopsis"`
	Flags         []Flag    `yaml:"flags"`
	Examples      []Example `yaml:"examples"`
	PlaysWellWith []string  `yaml:"plays_well_with"`
	Gotchas       []string  `yaml:"gotchas"`
	SeeAlso       []string  `yaml:"see_also"`
	// Executable reports whether the runner may ever execute this command.
	// Network commands are documented but never run. Defaults to true.
	Executable *bool `yaml:"executable,omitempty"`
}

// CanExecute reports whether the sandbox allowlist should include this command.
func (c Command) CanExecute() bool {
	if c.Executable == nil {
		return c.Category != CatNetwork
	}
	return *c.Executable
}

// MatchMode selects how an exercise's actual stdout is compared to expected.
type MatchMode string

const (
	MatchExact     MatchMode = "exact"
	MatchTrimmed   MatchMode = "trimmed"
	MatchSqueezed  MatchMode = "squeezed"
	MatchUnordered MatchMode = "unordered"
	MatchRegex     MatchMode = "regex"
)

// Exercise is one pipeline-building task.
type Exercise struct {
	ID                 string    `yaml:"id"`
	Track              string    `yaml:"track"`
	Level              int       `yaml:"level"`
	Title              string    `yaml:"title"`
	Prompt             string    `yaml:"prompt"`
	Fixture            string    `yaml:"fixture"`
	ExpectedStdoutFile string    `yaml:"expected_stdout_file"`
	Match              MatchMode `yaml:"match"`
	Teaches            []string  `yaml:"teaches"`
	MustUse            []string  `yaml:"must_use"`
	Forbid             []string  `yaml:"forbid"`
	ReferenceSolution  string    `yaml:"reference_solution"`
	SolutionNotes      string    `yaml:"solution_notes"`
	Hints              []string  `yaml:"hints"`
}

// TrackOrder is the sequence of exercise tracks a learner walks, and the order
// they are presented in. A track is unlocked by the one before it, so the
// order is content, not presentation: it is declared here rather than derived
// from the exercises, which know only their own track's name.
var TrackOrder = []string{
	"files-navigation",
	"inspection",
	"text-processing",
	"search-find",
	"streams",
	"real-world",
}

// TrackTitles maps a track id to its heading.
var TrackTitles = map[string]string{
	"files-navigation": "Files & Navigation",
	"inspection":       "Inspection",
	"text-processing":  "Text Processing",
	"search-find":      "Search & Find",
	"streams":          "Streams & Redirection",
	"real-world":       "Real-World Pipelines",
}

// TrackTitle returns a track's heading, falling back to its id so that content
// under review is still readable before it is added to TrackOrder.
func TrackTitle(id string) string {
	if t, ok := TrackTitles[id]; ok {
		return t
	}
	return id
}

// CardType is the kind of recall drill a flashcard performs.
type CardType string

const (
	// CardRecall shows a task and expects the learner to type the command.
	CardRecall CardType = "recall"
	// CardIdentify shows a command and expects a plain-English reading,
	// graded by the learner.
	CardIdentify CardType = "identify"
	// CardFlag asks for the flags alone.
	CardFlag CardType = "flag"
)

// Card is one flashcard.
type Card struct {
	ID       string   `yaml:"id"`
	Type     CardType `yaml:"type"`
	Front    string   `yaml:"front"`
	Back     string   `yaml:"back"`
	Accepts  []string `yaml:"accepts"`
	Commands []string `yaml:"commands"`
}

// SelfGraded reports whether the card is graded by the learner rather than by
// string comparison.
func (c Card) SelfGraded() bool { return c.Type == CardIdentify }

// Track is a named, ordered sequence of exercises, derived from the exercises
// themselves rather than declared separately.
type Track struct {
	Name      string
	Exercises []*Exercise
}

// Title returns the track's display heading.
func (t *Track) Title() string { return TrackTitle(t.Name) }

// Library is the whole validated content set.
type Library struct {
	Commands  []*Command
	Exercises []*Exercise
	Cards     []*Card
	Tracks    []*Track

	byCommandID  map[string]*Command
	byExerciseID map[string]*Exercise
	byCardID     map[string]*Card

	// src is the tree the library was loaded from. It is kept so that the
	// runner can read fixtures and expected outputs without being handed the
	// same filesystem a second time and risking a mismatch with the content
	// that was actually validated.
	src Source
}

// Files returns the content tree the library was loaded from.
func (l *Library) Files() fs.FS { return l.src }

// ExpectedOutput reads the expected stdout for an exercise.
func (l *Library) ExpectedOutput(e *Exercise) (string, error) {
	if e.ExpectedStdoutFile == "" {
		return "", fmt.Errorf("exercise %s has no expected_stdout_file", e.ID)
	}
	data, err := fs.ReadFile(l.src, e.ExpectedStdoutFile)
	if err != nil {
		return "", fmt.Errorf("exercise %s: %w", e.ID, err)
	}
	return string(data), nil
}

// FixtureFile describes one file of a fixture: what the practice screen shows
// a learner before they have run anything.
type FixtureFile struct {
	Name  string
	Size  int64
	Lines int
	// Preview holds the first few lines, for the fixture preview pane. It is
	// capped so that opening the preview is cheap however large the file is.
	Preview []string
	// Truncated reports whether Preview stops short of the whole file.
	Truncated bool
}

// FixturePreviewLines is how much of a fixture file the preview pane holds.
const FixturePreviewLines = 40

// Fixture describes the files an exercise's fixture directory contains.
func (l *Library) Fixture(name string) ([]FixtureFile, error) {
	dir := path.Join("fixtures", name)
	entries, err := fs.ReadDir(l.src, dir)
	if err != nil {
		return nil, fmt.Errorf("fixture %q: %w", name, err)
	}
	out := make([]FixtureFile, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		data, err := fs.ReadFile(l.src, path.Join(dir, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("fixture %q: %w", name, err)
		}
		f := FixtureFile{Name: e.Name(), Size: int64(len(data))}
		text := strings.TrimSuffix(string(data), "\n")
		if text != "" {
			f.Preview = strings.Split(text, "\n")
			f.Lines = len(f.Preview)
		}
		if len(f.Preview) > FixturePreviewLines {
			f.Preview, f.Truncated = f.Preview[:FixturePreviewLines], true
		}
		out = append(out, f)
	}
	return out, nil
}

// Command returns the dictionary entry with the given id, if any.
func (l *Library) Command(id string) (*Command, bool) { c, ok := l.byCommandID[id]; return c, ok }

// Exercise returns the exercise with the given id, if any.
func (l *Library) Exercise(id string) (*Exercise, bool) { e, ok := l.byExerciseID[id]; return e, ok }

// Card returns the flashcard with the given id, if any.
func (l *Library) Card(id string) (*Card, bool) { c, ok := l.byCardID[id]; return c, ok }

// CommandsByCategory returns the commands in a category, in load order.
func (l *Library) CommandsByCategory(cat Category) []*Command {
	var out []*Command
	for _, c := range l.Commands {
		if c.Category == cat {
			out = append(out, c)
		}
	}
	return out
}

// ExercisesUsing returns the exercises whose Teaches list names the command.
func (l *Library) ExercisesUsing(commandID string) []*Exercise {
	var out []*Exercise
	for _, e := range l.Exercises {
		for _, t := range e.Teaches {
			if t == commandID {
				out = append(out, e)
				break
			}
		}
	}
	return out
}

// CardsFor returns the flashcards that drill the given command.
func (l *Library) CardsFor(commandID string) []*Card {
	var out []*Card
	for _, c := range l.Cards {
		for _, id := range c.Commands {
			if id == commandID {
				out = append(out, c)
				break
			}
		}
	}
	return out
}

// Track returns the track with the given id, if any.
func (l *Library) Track(name string) (*Track, bool) {
	for _, t := range l.Tracks {
		if t.Name == name {
			return t, true
		}
	}
	return nil, false
}

// Allowlist returns the set of command names the sandbox runner may execute.
func (l *Library) Allowlist() []string {
	var out []string
	for _, c := range l.Commands {
		if c.CanExecute() {
			out = append(out, c.Name)
		}
	}
	return out
}
