// Command bt is the bash-teacher terminal UI: a dictionary of Unix commands,
// pipeline exercises, and flashcards, in one offline binary.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"

	embedded "bash-teacher/content"
	lib "bash-teacher/internal/content"
	"bash-teacher/internal/theme"
	"bash-teacher/internal/tui"
)

// version is stamped at build time with -ldflags "-X main.version=...".
var version = "dev"

const usage = `bash-teacher — learn Unix commands and how to compose them.

usage:
  bt                    launch the TUI
  bt practice           launch straight into the exercise browser
  bt review             launch straight into the flashcards
  bt dict [COMMAND]     print a dictionary entry (or list every entry)
  bt stats              print a library summary
  bt doctor             report environment, data paths, and content health
  bt content lint       validate the content library

flags:
  --theme MODE          dark, light, none, or auto (default auto)
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
	themeFlag := fl.String("theme", "auto", "colour scheme: dark, light, none, auto")
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
		if len(rest) == 0 || rest[0] != "lint" {
			return fmt.Errorf("unknown content subcommand; try `bt content lint`")
		}
		return lintCmd()
	}

	library, err := lib.Load(embedded.FS)
	if err != nil {
		return err
	}
	th := theme.Resolve(theme.Mode(*themeFlag))

	switch cmd {
	case "":
		return launch(library, th, tui.ScreenHome)
	case "practice":
		return launch(library, th, tui.ScreenPractice)
	case "review":
		return launch(library, th, tui.ScreenFlashcards)
	case "dict":
		return dictCmd(library, th, rest)
	case "stats":
		return statsCmd(library)
	case "doctor":
		return doctorCmd(library, th)
	case "help", "-h", "--help":
		fmt.Print(usage)
		return nil
	default:
		return fmt.Errorf("unknown command %q; run `bt help`", cmd)
	}
}

func launch(library *lib.Library, th theme.Theme, start tui.Screen) error {
	p := tea.NewProgram(tui.New(library, th, version, start))
	_, err := p.Run()
	return err
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

func dictCmd(library *lib.Library, th theme.Theme, args []string) error {
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

func doctorCmd(library *lib.Library, th theme.Theme) error {
	fmt.Printf("version        %s\n", version)
	fmt.Printf("theme          %s\n", th.Mode)
	fmt.Printf("content        %d commands, %d exercises, %d cards (embedded)\n",
		len(library.Commands), len(library.Exercises), len(library.Cards))
	fmt.Printf("allowlist      %s\n", strings.Join(library.Allowlist(), " "))
	fmt.Printf("data dir       %s\n", dataDir())
	fmt.Printf("sandbox        not built yet (M3)\n")
	return nil
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
