// Package content defines the schema for the bash-teacher learning library and
// the loader and linter that turn the embedded YAML into validated Go values.
package content

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
	ID             string   `yaml:"id"`
	Name           string   `yaml:"name"`
	Category       Category `yaml:"category"`
	Summary        string   `yaml:"summary"`
	Purpose        string   `yaml:"purpose"`
	Synopsis       string   `yaml:"synopsis"`
	Flags          []Flag   `yaml:"flags"`
	Examples       []Example `yaml:"examples"`
	PlaysWellWith  []string `yaml:"plays_well_with"`
	Gotchas        []string `yaml:"gotchas"`
	SeeAlso        []string `yaml:"see_also"`
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

// Library is the whole validated content set.
type Library struct {
	Commands  []*Command
	Exercises []*Exercise
	Cards     []*Card
	Tracks    []*Track

	byCommandID  map[string]*Command
	byExerciseID map[string]*Exercise
	byCardID     map[string]*Card
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
