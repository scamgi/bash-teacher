// Command bt is the bash-teacher terminal UI: a dictionary of Unix commands,
// pipeline exercises, and flashcards, in one offline binary.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	embedded "bash-teacher/content"
	"bash-teacher/internal/config"
	lib "bash-teacher/internal/content"
	"bash-teacher/internal/runner"
	"bash-teacher/internal/srs"
	"bash-teacher/internal/store"
	"bash-teacher/internal/theme"
	"bash-teacher/internal/tui"
)

// version is stamped at build time with -ldflags "-X main.version=...".
var version = "dev"

const usage = `bash-teacher — learn Unix commands and how to compose them.

usage:
  bt                    launch the TUI
  bt practice [TRACK]   launch straight into the exercise browser
  bt review             launch straight into the flashcards
  bt dict [COMMAND]     print a dictionary entry (or list every entry)
  bt stats              print a library summary
  bt doctor             report environment, data paths, and content health
  bt export             write the progress database to stdout as JSON
  bt import [FILE]      replace the progress database with a JSON export,
                        reading stdin if no file is named; --force is required
                        when there is already progress to replace
  bt content lint       validate the content library
  bt content expected   re-run every reference solution and check the expected
                        outputs; --write regenerates them

flags:
  --theme MODE          catppuccin flavour: latte, frappe, macchiato, mocha
                        (also: dark, light, none, auto — default auto); it
                        overrides the theme in the settings file
  --no-exec             never run a subprocess; exercises fall back to
                        matching the reference solution
  --no-store            do not read or write the progress database; the
                        session is remembered only while it runs
  --version             print the version and exit

settings are optional TOML at $XDG_CONFIG_HOME/bash-teacher/config.toml;
bt doctor prints its path and whether it was read.
`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "bt: "+err.Error())
		os.Exit(1)
	}
}

func run(args []string) error {
	fl := flag.NewFlagSet("bt", flag.ContinueOnError)
	fl.SetOutput(os.Stderr)
	fl.Usage = func() { fmt.Fprint(os.Stderr, usage) }
	themeFlag := fl.String("theme", "auto", "colour scheme: "+strings.Join(theme.Modes(), ", "))
	noExec := fl.Bool("no-exec", false, "never execute learner input")
	noStore := fl.Bool("no-store", false, "do not read or write the progress database")
	showVersion := fl.Bool("version", false, "print the version and exit")
	if err := fl.Parse(args); err != nil {
		return err
	}
	if *showVersion {
		fmt.Println("bash-teacher " + version)
		return nil
	}

	rest := fl.Args()
	cmd := ""
	if len(rest) > 0 {
		cmd = rest[0]
		rest = rest[1:]
	}

	// `content lint` must report problems rather than fail to start, so it
	// loads the library itself instead of going through mustLoad.
	if cmd == "content" {
		return contentCmd(rest)
	}

	// Help is answered before anything is read, so that a broken settings
	// file cannot take away the page that explains the settings file.
	switch cmd {
	case "help", "-h", "--help":
		fmt.Print(usage)
		return nil
	}

	// The settings file is optional; a file that exists but is wrong stops
	// every command except doctor, which is the command you reach for when
	// something is wrong and so has to start when something is wrong.
	cfg, cfgFound, cfgErr := config.Load(config.Path())
	if cfgErr != nil && cmd != "doctor" {
		return cfgErr
	}

	// The flag wins over the file, but only when it was actually given: its
	// default is "auto", which is indistinguishable from a learner asking
	// for auto unless the flag set is asked which flags were visited.
	mode := cfg.Mode()
	if flagGiven(fl, "theme") {
		m, err := theme.ParseMode(*themeFlag)
		if err != nil {
			return err
		}
		mode = m
	}
	library, err := lib.Load(embedded.FS)
	if err != nil {
		return err
	}
	th := theme.Resolve(mode)
	run := runner.New(library, runner.WithNoExec(*noExec))

	// The database is opened only for the commands that read or write
	// progress, so `bt dict` stays a print-and-exit with no file to create.
	var db *store.Store
	switch cmd {
	case "", "practice", "review", "stats", "doctor", "export", "import":
		if !*noStore {
			db, err = store.Open(progressPath())
			if err != nil {
				return err
			}
			defer func() { _ = db.Close() }()
		}
	}

	switch cmd {
	case "":
		return launch(library, th, run, tui.ScreenHome, sessionOptions(cfg, db)...)
	case "practice":
		opts, err := practiceOptions(library, rest)
		if err != nil {
			return err
		}
		return launch(library, th, run, tui.ScreenPractice, append(opts, sessionOptions(cfg, db)...)...)
	case "review":
		return launch(library, th, run, tui.ScreenFlashcards, sessionOptions(cfg, db)...)
	case "dict":
		return dictCmd(library, th, rest)
	case "stats":
		return statsCmd(library, db)
	case "doctor":
		return doctorCmd(library, th, run, db, configReport(cfgFound, cfgErr))
	case "export":
		return exportCmd(db, rest)
	case "import":
		return importCmd(library, db, rest)
	default:
		return fmt.Errorf("unknown command %q; run `bt help`", cmd)
	}
}

// sessionOptions is everything the TUI is told before it starts: the settings
// file's preferences and the progress database, if there is one.
//
// The configuration is passed as values rather than as a *config.Config so the
// TUI never learns what a TOML file is; translating between the file and the
// model is this command's job.
func sessionOptions(cfg config.Config, db *store.Store) []tui.Option {
	opts := []tui.Option{
		tui.WithParams(cfg.Params()),
		tui.WithTimer(cfg.Review.Timer),
	}
	if db != nil {
		opts = append(opts, tui.WithStore(db))
	}
	return opts
}

// flagGiven reports whether a flag was actually passed, as opposed to sitting
// at its default. It is what lets a flag override the settings file without
// its own default overriding it too.
func flagGiven(fl *flag.FlagSet, name string) bool {
	given := false
	fl.Visit(func(f *flag.Flag) {
		if f.Name == name {
			given = true
		}
	})
	return given
}

func launch(library *lib.Library, th *theme.Theme, run *runner.Runner, start tui.Screen, opts ...tui.Option) error {
	p := tea.NewProgram(tui.New(library, th, run, version, start, opts...))
	_, err := p.Run()
	return err
}

// practiceOptions turns `bt practice [track]` into a model option, rejecting a
// track name that does not exist rather than opening the library at the top
// and leaving the learner to wonder why.
func practiceOptions(library *lib.Library, args []string) ([]tui.Option, error) {
	if len(args) == 0 {
		return nil, nil
	}
	name := args[0]
	if _, ok := library.Track(name); !ok {
		names := make([]string, 0, len(library.Tracks))
		for _, t := range library.Tracks {
			names = append(names, t.Name)
		}
		return nil, fmt.Errorf("no track called %q; the tracks are %s", name, strings.Join(names, ", "))
	}
	return []tui.Option{tui.WithTrack(name)}, nil
}

// contentCmd routes the authoring subcommands. They take their own flags, so
// that `bt content expected --write` reads the way it is documented rather
// than requiring the flag before the subcommand.
func contentCmd(args []string) error {
	sub := ""
	if len(args) > 0 {
		sub, args = args[0], args[1:]
	}
	fl := flag.NewFlagSet("bt content "+sub, flag.ContinueOnError)
	fl.SetOutput(os.Stderr)
	write := fl.Bool("write", false, "rewrite the expected output files instead of checking them")
	dir := fl.String("dir", "content", "the content tree to work on")
	if err := fl.Parse(args); err != nil {
		return err
	}
	switch sub {
	case "lint":
		return lintCmd()
	case "expected":
		return expectedCmd(*dir, *write)
	default:
		return errors.New("unknown content subcommand; try `bt content lint` or `bt content expected`")
	}
}

func lintCmd() error {
	library, err := lib.Load(embedded.FS)
	if err != nil {
		var le *lib.LintError
		if errors.As(err, &le) {
			for _, p := range le.Problems {
				fmt.Printf("%s: %s\n", p.Where, p.Message)
			}
			return fmt.Errorf("%d problem(s)", len(le.Problems))
		}
		return err
	}
	fmt.Printf("ok: %d commands, %d exercises, %d cards, %d tracks\n",
		len(library.Commands), len(library.Exercises), len(library.Cards), len(library.Tracks))
	return nil
}

// expectedCmd re-runs every exercise's reference solution and compares what it
// printed with the committed expected output, rewriting the files with --write.
//
// It works on the content tree on disk rather than the embedded copy, since
// the point is to write files, and it loads without linting because a new
// exercise has no expected file for the linter to find yet.
func expectedCmd(dir string, write bool) error {
	library, err := lib.LoadUnlinted(os.DirFS(dir))
	if err != nil {
		return err
	}
	run := runner.New(library)
	if run.NoExec() {
		return errors.New("cannot generate expected output with execution disabled")
	}

	stale := 0
	for _, ex := range library.Exercises {
		res, err := run.Run(context.Background(), runner.Job{
			Input: ex.ReferenceSolution, Fixture: ex.Fixture,
			MustUse: ex.MustUse, Forbid: ex.Forbid,
		})
		if err != nil {
			return fmt.Errorf("%s: %w", ex.ID, err)
		}
		if !res.Ran() {
			return fmt.Errorf("%s: the reference solution was refused:\n%s", ex.ID, res.Refusal())
		}
		if res.ExitCode != 0 || res.Stderr != "" {
			fmt.Printf("warn  %-24s exit %d %s\n", ex.ID, res.ExitCode, strings.TrimSpace(res.Stderr))
		}
		if ex.ExpectedStdoutFile == "" {
			return fmt.Errorf("%s: no expected_stdout_file", ex.ID)
		}

		path := filepath.Join(dir, filepath.FromSlash(ex.ExpectedStdoutFile))
		old, readErr := os.ReadFile(path) //#nosec G304 -- the path comes from the content tree being generated
		switch {
		case readErr == nil && string(old) == res.Stdout:
			continue
		case !write:
			stale++
			what := "differs from"
			if readErr != nil {
				what = "is missing"
			}
			fmt.Printf("stale %-24s %s %s\n", ex.ID, ex.ExpectedStdoutFile, what+" what the reference solution prints")
			continue
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte(res.Stdout), 0o600); err != nil {
			return err
		}
		fmt.Printf("wrote %-24s %s\n", ex.ID, ex.ExpectedStdoutFile)
	}
	if stale > 0 {
		return fmt.Errorf("%d expected output(s) out of date; re-run with --write", stale)
	}
	fmt.Printf("ok: %d expected output(s) match their reference solution\n", len(library.Exercises))
	return nil
}

func dictCmd(library *lib.Library, th *theme.Theme, args []string) error {
	if len(args) == 0 {
		for _, cat := range lib.Categories {
			cmds := library.CommandsByCategory(cat)
			if len(cmds) == 0 {
				continue
			}
			fmt.Println(th.Title.Render(lib.CategoryTitles[cat]))
			for _, c := range cmds {
				fmt.Printf("  %-10s %s\n", c.Name, th.Dim.Render(c.Summary))
			}
		}
		return nil
	}
	name := args[0]
	c, ok := library.Command(name)
	if !ok {
		return fmt.Errorf("no dictionary entry for %q", name)
	}
	fmt.Println(th.Title.Render(c.Name) + " — " + c.Summary)
	fmt.Println()
	fmt.Println(strings.TrimSpace(c.Purpose))
	fmt.Println()
	fmt.Println(th.Faint.Render("SYNOPSIS"))
	fmt.Println("  " + c.Synopsis)
	if len(c.Flags) > 0 {
		fmt.Println()
		fmt.Println(th.Faint.Render("FLAGS"))
		for _, f := range c.Flags {
			gloss := f.Gloss
			if f.Long != "" {
				gloss += "  (long form: " + f.Long + ")"
			}
			fmt.Printf("  %-12s %s\n", f.Flag, gloss)
		}
	}
	fmt.Println()
	fmt.Println(th.Faint.Render("EXAMPLES"))
	for _, ex := range c.Examples {
		fmt.Println("  " + th.Code.Render(ex.Cmd))
		fmt.Println("    " + th.Dim.Render(ex.Caption))
	}
	if len(c.Gotchas) > 0 {
		fmt.Println()
		fmt.Println(th.Faint.Render("GOTCHAS"))
		for _, g := range c.Gotchas {
			fmt.Println("  · " + g)
		}
	}
	if len(c.PlaysWellWith) > 0 {
		fmt.Println()
		fmt.Println(th.Faint.Render("PLAYS WELL WITH ") + strings.Join(c.PlaysWellWith, ", "))
	}
	return nil
}

// statsCmd prints the library's shape, and what the progress database holds
// against it. Without a store it is the library alone: a summary that invented
// a streak would be worse than one that admits it has nothing to report.
func statsCmd(library *lib.Library, db *store.Store) error {
	fmt.Printf("commands   %d\n", len(library.Commands))
	fmt.Printf("exercises  %d in %d tracks\n", len(library.Exercises), len(library.Tracks))
	fmt.Printf("cards      %d\n", len(library.Cards))

	passed := map[string]bool{}
	if db != nil {
		rows, err := db.LoadExercises()
		if err != nil {
			return err
		}
		for _, e := range rows {
			if e.Passed() {
				passed[e.ID] = true
			}
		}
	}
	for _, t := range library.Tracks {
		if db == nil {
			fmt.Printf("  %-18s %d\n", t.Name, len(t.Exercises))
			continue
		}
		n := 0
		for _, e := range t.Exercises {
			if passed[e.ID] {
				n++
			}
		}
		fmt.Printf("  %-18s %d of %d passed\n", t.Name, n, len(t.Exercises))
	}
	if db == nil {
		return nil
	}
	return reviewStats(library, db)
}

// reviewStats replays the stored schedule through a scheduler of its own. The
// package is content-free and addresses cards by id, so the numbers the TUI
// shows can be computed here without a TUI.
func reviewStats(library *lib.Library, db *store.Store) error {
	states, err := db.LoadCards()
	if err != nil {
		return err
	}
	log, err := db.LoadReviews()
	if err != nil {
		return err
	}
	sched := srs.New(srs.Defaults())
	sched.Restore(states, log)

	ids := make([]string, 0, len(library.Cards))
	for _, c := range library.Cards {
		ids = append(ids, c.ID)
	}
	now := time.Now()
	fmt.Printf("due now    %d\n", sched.DueCount(ids, now))
	fmt.Printf("introduced %d of %d\n", len(ids)-sched.UnseenCount(ids), len(ids))
	if recalled, answered := sched.Accuracy(); answered > 0 {
		fmt.Printf("recalled   %d%% of %d answers\n", 100*recalled/answered, answered)
	}
	fmt.Printf("streak     %d days\n", sched.Streak(now))
	fmt.Printf("progress   %s\n", db.Path())
	return nil
}

// exportCmd dumps the progress database to stdout as JSON.
//
// There is no file operand on purpose: `bt export > backup.json` already says
// where the dump goes, and a command that opened the file itself would owe the
// learner an answer about overwriting one that was already there.
func exportCmd(db *store.Store, args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("bt export takes no arguments; redirect it: bt export > %s", args[0])
	}
	if db == nil {
		return errors.New("nothing to export: --no-store was given")
	}
	archive, err := db.Snapshot("bash-teacher " + version)
	if err != nil {
		return err
	}
	return archive.Write(os.Stdout)
}

// importCmd replaces the progress database with a JSON export.
//
// The archive is read and checked in full before the database is touched, and
// the replacement itself is one transaction, so the two ways this can go wrong
// — a file that is not an export, and a write that fails halfway — both leave
// the learner's history where it was.
func importCmd(library *lib.Library, db *store.Store, args []string) error {
	fl := flag.NewFlagSet("bt import", flag.ContinueOnError)
	fl.SetOutput(os.Stderr)
	force := fl.Bool("force", false, "replace progress that is already stored")
	if err := fl.Parse(args); err != nil {
		return err
	}
	rest := fl.Args()
	if len(rest) > 1 {
		return errors.New("bt import reads one file, or stdin")
	}
	if db == nil {
		return errors.New("cannot import: --no-store was given")
	}

	src := io.Reader(os.Stdin)
	if len(rest) == 1 && rest[0] != "-" {
		f, err := os.Open(rest[0])
		if err != nil {
			return err
		}
		defer func() { _ = f.Close() }()
		src = f
	}
	archive, err := store.ReadArchive(src)
	if err != nil {
		return err
	}

	written, err := db.Restore(archive, *force)
	if err != nil {
		if errors.Is(err, store.ErrNotEmpty) {
			return fmt.Errorf("%w; `bt import --force` replaces it", err)
		}
		return err
	}
	fmt.Printf("imported %s into %s\n", written, db.Path())
	if generator := archive.Generator; generator != "" {
		fmt.Printf("exported by %s on %s\n", generator, archive.Exported.Format(time.RFC3339))
	}
	for _, line := range strayReport(library, archive) {
		fmt.Println("warn  " + line)
	}
	return nil
}

// strayReport names the imported ids this build's content library has no entry
// for, which is what an archive from a different content version looks like.
// It is a warning and not a refusal: the ids are harmless — nothing ever shows
// a card that does not exist — and refusing the whole file over them would
// throw away the history of everything that does still exist.
func strayReport(library *lib.Library, a *store.Archive) []string {
	var out []string
	stray := 0
	var names []string
	for _, c := range a.Cards {
		if _, ok := library.Card(c.ID); !ok {
			stray++
			if len(names) < 3 {
				names = append(names, c.ID)
			}
		}
	}
	if stray > 0 {
		out = append(out, fmt.Sprintf("%d imported card(s) are not in this content library: %s",
			stray, strings.Join(names, ", ")))
	}

	stray, names = 0, nil
	for _, e := range a.Exercises {
		if _, ok := library.Exercise(e.ID); !ok {
			stray++
			if len(names) < 3 {
				names = append(names, e.ID)
			}
		}
	}
	if stray > 0 {
		out = append(out, fmt.Sprintf("%d imported exercise(s) are not in this content library: %s",
			stray, strings.Join(names, ", ")))
	}
	return out
}

func doctorCmd(library *lib.Library, th *theme.Theme, run *runner.Runner, db *store.Store, cfgReport string) error {
	fmt.Printf("version        %s\n", version)
	fmt.Printf("theme          %s\n", th.Mode)
	fmt.Printf("palette        catppuccin\n")
	fmt.Printf("content        %d commands, %d exercises, %d cards (embedded)\n",
		len(library.Commands), len(library.Exercises), len(library.Cards))
	fmt.Printf("config         %s\n", cfgReport)
	fmt.Printf("data dir       %s\n", dataDir())
	fmt.Printf("progress       %s\n", progressReport(db))

	sb := run.Sandbox()
	fmt.Printf("sandbox        %s — %s\n", sb.Name(), sb.Describe())
	for _, s := range runner.AvailableSandboxes() {
		mark := "✗"
		if s.Available {
			mark = "✓"
		}
		fmt.Printf("               %s %-13s %s\n", mark, s.Name, s.Note)
	}
	if run.NoExec() {
		fmt.Printf("execution      disabled by --no-exec\n")
	} else if !sb.Confines() {
		fmt.Printf("execution      WITHOUT an OS sandbox: exercises run with your own permissions\n")
	}
	names := run.Policy().Names()
	fmt.Printf("allowlist      %d commands: %s\n", len(names), strings.Join(names, " "))
	fmt.Printf("shell          %s\n", shellReport())
	return nil
}

// shellReport names the coreutils flavour on this machine, which is the
// answer to the GNU-versus-BSD question the dictionary annotates.
func shellReport() string {
	out, err := exec.Command("sort", "--version").Output()
	if err != nil {
		return "unknown (sort --version failed)"
	}
	first, _, _ := strings.Cut(string(out), "\n")
	if strings.Contains(first, "GNU") {
		return "GNU coreutils (" + strings.TrimSpace(first) + ")"
	}
	return "BSD coreutils"
}

// configReport describes the settings file for `bt doctor`: where it is,
// whether it was read, and what was wrong with it if it was not. A rejected
// file is reported in full, since doctor is running only because the same
// error stopped every other command.
func configReport(found bool, err error) string {
	path := config.Path()
	var cfgErr *config.Error
	switch {
	case errors.As(err, &cfgErr):
		out := path + " — rejected, running on defaults"
		for _, p := range cfgErr.Problems {
			line := p.Msg
			if p.Key != "" {
				line = p.Key + ": " + p.Msg
			}
			out += "\n               " + line
		}
		return out
	case err != nil:
		return path + " — unreadable: " + err.Error()
	case !found:
		return path + " — not present, running on defaults"
	}
	return path + " — loaded"
}

// progressPath is the progress database, which lives in the data directory.
func progressPath() string { return filepath.Join(dataDir(), "progress.db") }

// progressReport describes the progress database for `bt doctor`: where it is,
// what schema it is on, and how much it holds.
func progressReport(db *store.Store) string {
	if db == nil {
		return "disabled by --no-store"
	}
	cards, err := db.LoadCards()
	if err != nil {
		return db.Path() + " — unreadable: " + err.Error()
	}
	reviews, err := db.LoadReviews()
	if err != nil {
		return db.Path() + " — unreadable: " + err.Error()
	}
	exercises, err := db.LoadExercises()
	if err != nil {
		return db.Path() + " — unreadable: " + err.Error()
	}
	passed := 0
	for _, e := range exercises {
		if e.Passed() {
			passed++
		}
	}
	return fmt.Sprintf("%s (schema %d) — %d cards, %d reviews, %d exercises passed",
		db.Path(), store.SchemaVersion, len(cards), len(reviews), passed)
}

// dataDir reports where the progress store lives, following the XDG base
// directory spec.
func dataDir() string {
	if d := os.Getenv("XDG_DATA_HOME"); d != "" {
		return d + "/bash-teacher"
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "./.bash-teacher"
	}
	return home + "/.local/share/bash-teacher"
}
