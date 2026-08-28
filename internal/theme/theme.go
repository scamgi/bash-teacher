// Package theme holds the colour palette and the shared Lip Gloss styles.
//
// Colour is always semantic — green for pass, red for fail, yellow for a used
// hint — and never the only signal: pass and fail carry a glyph too, so the UI
// still reads correctly with NO_COLOR set or on a monochrome terminal.
package theme

import (
	"image/color"
	"os"
	"strings"

	"charm.land/lipgloss/v2"
)

// Mode is the resolved colour scheme.
type Mode string

const (
	Dark  Mode = "dark"
	Light Mode = "light"
	// Auto asks the terminal, falling back to Dark.
	Auto Mode = "auto"
	// None disables colour entirely.
	None Mode = "none"
)

// Palette is the small set of colours every style is built from.
type Palette struct {
	Fg, Dim, Faint  color.Color
	Accent, Accent2 color.Color
	Pass, Fail      color.Color
	Warn            color.Color
	Border          color.Color
	SelBg, SelFg    color.Color
}

var darkPalette = Palette{
	Fg:      lipgloss.Color("252"),
	Dim:     lipgloss.Color("245"),
	Faint:   lipgloss.Color("240"),
	Accent:  lipgloss.Color("81"),
	Accent2: lipgloss.Color("177"),
	Pass:    lipgloss.Color("78"),
	Fail:    lipgloss.Color("204"),
	Warn:    lipgloss.Color("221"),
	Border:  lipgloss.Color("238"),
	SelBg:   lipgloss.Color("24"),
	SelFg:   lipgloss.Color("231"),
}

var lightPalette = Palette{
	Fg:      lipgloss.Color("236"),
	Dim:     lipgloss.Color("242"),
	Faint:   lipgloss.Color("248"),
	Accent:  lipgloss.Color("31"),
	Accent2: lipgloss.Color("91"),
	Pass:    lipgloss.Color("28"),
	Fail:    lipgloss.Color("160"),
	Warn:    lipgloss.Color("136"),
	Border:  lipgloss.Color("250"),
	SelBg:   lipgloss.Color("153"),
	SelFg:   lipgloss.Color("232"),
}

var noPalette = Palette{
	Fg: lipgloss.NoColor{}, Dim: lipgloss.NoColor{}, Faint: lipgloss.NoColor{},
	Accent: lipgloss.NoColor{}, Accent2: lipgloss.NoColor{},
	Pass: lipgloss.NoColor{}, Fail: lipgloss.NoColor{}, Warn: lipgloss.NoColor{},
	Border: lipgloss.NoColor{}, SelBg: lipgloss.NoColor{}, SelFg: lipgloss.NoColor{},
}

// Theme bundles the palette with the styles built from it.
type Theme struct {
	Mode    Mode
	Palette Palette

	Title      lipgloss.Style
	Subtitle   lipgloss.Style
	Body       lipgloss.Style
	Dim        lipgloss.Style
	Faint      lipgloss.Style
	Accent     lipgloss.Style
	Code       lipgloss.Style
	Key        lipgloss.Style
	Pass       lipgloss.Style
	Fail       lipgloss.Style
	Warn       lipgloss.Style
	Selected   lipgloss.Style
	Panel      lipgloss.Style
	PanelTitle lipgloss.Style
	Footer     lipgloss.Style
	Badge      lipgloss.Style
}

// Resolve turns a requested mode into a concrete theme, honouring NO_COLOR and
// COLORFGBG. The requested mode wins over detection; detection wins over the
// dark default.
func Resolve(requested Mode) Theme {
	mode := requested
	if mode == "" {
		mode = Auto
	}
	if _, noColor := os.LookupEnv("NO_COLOR"); noColor && mode != Light && mode != Dark {
		mode = None
	}
	// When stdout is a pipe or a file rather than a terminal, colour would be
	// literal escape bytes in someone's grep. `bt dict foo | less` should be
	// readable, so auto resolves to no colour off a terminal.
	if mode == Auto && !stdoutIsTerminal() {
		mode = None
	}
	if mode == Auto {
		mode = detect()
	}
	switch mode {
	case None:
		return build(None, noPalette)
	case Light:
		return build(Light, lightPalette)
	default:
		return build(Dark, darkPalette)
	}
}

// detect reads COLORFGBG first — it is cheap, explicit, and set by several
// terminals — before falling back to Lip Gloss's background query.
func detect() Mode {
	if v := os.Getenv("COLORFGBG"); v != "" {
		// The format is "fg;bg" (sometimes "fg;_;bg"); a low background
		// number means a dark background.
		parts := strings.Split(v, ";")
		bg := parts[len(parts)-1]
		switch bg {
		case "0", "1", "2", "3", "4", "5", "6", "8":
			return Dark
		case "7", "15":
			return Light
		}
	}
	if lipgloss.HasDarkBackground(os.Stdin, os.Stdout) {
		return Dark
	}
	return Light
}

// stdoutIsTerminal reports whether stdout is a character device.
func stdoutIsTerminal() bool {
	info, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

func build(mode Mode, p Palette) Theme {
	t := Theme{Mode: mode, Palette: p}
	// In None mode even bold is dropped, so that piped output is plain bytes.
	bold := mode != None
	t.Body = lipgloss.NewStyle().Foreground(p.Fg)
	t.Title = lipgloss.NewStyle().Foreground(p.Accent).Bold(bold)
	t.Subtitle = lipgloss.NewStyle().Foreground(p.Accent2)
	t.Dim = lipgloss.NewStyle().Foreground(p.Dim)
	t.Faint = lipgloss.NewStyle().Foreground(p.Faint)
	t.Accent = lipgloss.NewStyle().Foreground(p.Accent)
	t.Code = lipgloss.NewStyle().Foreground(p.Accent2)
	t.Key = lipgloss.NewStyle().Foreground(p.Accent).Bold(bold)
	t.Pass = lipgloss.NewStyle().Foreground(p.Pass).Bold(bold)
	t.Fail = lipgloss.NewStyle().Foreground(p.Fail).Bold(bold)
	t.Warn = lipgloss.NewStyle().Foreground(p.Warn)
	t.Selected = lipgloss.NewStyle().Foreground(p.SelFg).Background(p.SelBg).Bold(bold)
	t.Panel = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(p.Border).Padding(0, 1)
	t.PanelTitle = lipgloss.NewStyle().Foreground(p.Accent).Bold(bold)
	t.Footer = lipgloss.NewStyle().Foreground(p.Dim)
	t.Badge = lipgloss.NewStyle().Foreground(p.SelFg).Background(p.Accent).Padding(0, 1)
	return t
}
