// Command bt is the bash-teacher terminal UI: a dictionary of Unix commands,
// pipeline exercises, and flashcards, in one offline binary.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	embedded "bash-teacher/content"
	lib "bash-teacher/internal/content"
	"bash-teacher/internal/runner"
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
  bt content lint       validate the content library
  bt content expected   re-run every reference solution and check the expected
                        outputs; --write regenerates them

flags:
  --theme MODE          catppuccin flavour: latte, frappe, macchiato, mocha
                        (also: dark, light, none, auto — default auto)
  --no-exec             never run a subprocess; exercises fall back to
                        matching the reference solution
  --version             print the version and exit
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

	mode, err := theme.ParseMode(*themeFlag)
	if err != nil {
		return err
	}
	library, err := lib.Load(embedded.FS)
	if err != nil {
		return err
	}
	th := theme.Resolve(mode)
	run := runner.New(library, runner.WithNoExec(*noExec))

	switch cmd {
	case "":
		return launch(library, th, run, tui.ScreenHome)
	case "practice":
		opts, err := practiceOptions(library, rest)
		if err != nil {
			return err
		}
		return launch(library, th, run, tui.ScreenPractice, opts...)
	case "review":
		return launch(library, th, run, tui.ScreenFlashcards)
	case "dict":
		return dictCmd(library, th, rest)
	case "stats":
		return statsCmd(library)
	case "doctor":
		return doctorCmd(library, th, run)
	case "help", "-h", "--help":
		fmt.Print(usage)
		return nil
	default:
		return fmt.Errorf("unknown command %q; run `bt help`", cmd)
	}
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
			fmt.Printf("  %-12s %s\n", f.Flag, f.Gloss)
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

func statsCmd(library *lib.Library) error {
	fmt.Printf("commands   %d\n", len(library.Commands))
	fmt.Printf("exercises  %d in %d tracks\n", len(library.Exercises), len(library.Tracks))
	fmt.Printf("cards      %d\n", len(library.Cards))
	for _, t := range library.Tracks {
		fmt.Printf("  %-18s %d\n", t.Name, len(t.Exercises))
	}
	return nil
}

func doctorCmd(library *lib.Library, th *theme.Theme, run *runner.Runner) error {
	fmt.Printf("version        %s\n", version)
	fmt.Printf("theme          %s\n", th.Mode)
	fmt.Printf("palette        catppuccin\n")
	fmt.Printf("content        %d commands, %d exercises, %d cards (embedded)\n",
		len(library.Commands), len(library.Exercises), len(library.Cards))
	fmt.Printf("data dir       %s\n", dataDir())

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

// dataDir reports where the progress store will live once it exists, following
// the XDG base directory spec.
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
