// Package theme holds the colour palette and the shared Lip Gloss styles.
//
// The palettes are Catppuccin: Latte for light terminals and Frappé,
// Macchiato, or Mocha for dark ones. Colour is always semantic — green for
// pass, red for fail, yellow for a used hint — and never the only signal:
// pass and fail carry a glyph too, so the UI still reads correctly with
// NO_COLOR set or on a monochrome terminal.
//
// The theme deliberately does not paint a page background. Catppuccin's Base
// is available in the palette for components that need a surface, but the
// terminal's own background shows through everywhere else, so bash-teacher sits
// inside whatever colour scheme the user already runs.
package theme

import (
	"fmt"
	"image/color"
	"os"
	"sort"
	"strings"

	"charm.land/lipgloss/v2"
	catppuccin "github.com/catppuccin/go"
)

// Mode names a palette. The four Catppuccin flavours are selectable by name;
// Light and Dark are aliases for Latte and Mocha; Auto asks the terminal; None
// disables colour entirely.
type Mode string

// The selectable modes.
const (
	Auto      Mode = "auto"
	None      Mode = "none"
	Latte     Mode = "latte"
	Frappe    Mode = "frappe"
	Macchiato Mode = "macchiato"
	Mocha     Mode = "mocha"
	Light     Mode = "light"
	Dark      Mode = "dark"
)

// flavors maps a resolved mode to its Catppuccin flavour.
var flavors = map[Mode]catppuccin.Flavor{
	Latte:     catppuccin.Latte,
	Frappe:    catppuccin.Frappe,
	Macchiato: catppuccin.Macchiato,
	Mocha:     catppuccin.Mocha,
}

// aliases are the modes that resolve to another mode before a flavour is
// chosen. Light and Dark exist so that `--theme dark` keeps working and reads
// naturally to someone who has never heard of Catppuccin.
var aliases = map[Mode]Mode{Light: Latte, Dark: Mocha}

// Modes lists every accepted --theme value, for help text and error messages.
func Modes() []string {
	out := []string{string(Auto), string(None)}
	for m := range flavors {
		out = append(out, string(m))
	}
	for m := range aliases {
		out = append(out, string(m))
	}
	sort.Strings(out)
	return out
}

// ParseMode validates a --theme value. An unknown name is an error rather than
// a silent fallback, so a typo does not quietly give you the wrong palette.
func ParseMode(s string) (Mode, error) {
	m := Mode(strings.ToLower(strings.TrimSpace(s)))
	if m == "" {
		return Auto, nil
	}
	if m == Auto || m == None {
		return m, nil
	}
	if _, ok := aliases[m]; ok {
		return m, nil
	}
	if _, ok := flavors[m]; ok {
		return m, nil
	}
	return "", fmt.Errorf("unknown theme %q; choose one of %s", s, strings.Join(Modes(), ", "))
}

// Palette is the small set of colours every style is built from, named by role
// rather than by hue so that swapping flavours cannot change the meaning of a
// colour.
type Palette struct {
	// Base is the flavour's page background. Nothing paints it by default; it
	// is here for components that need a surface of their own.
	Base   color.Color
	Fg     color.Color
	Dim    color.Color
	Faint  color.Color
	Accent color.Color
	// Accent2 is the secondary accent, used for code and command text.
	Accent2 color.Color
	Pass    color.Color
	Fail    color.Color
	Warn    color.Color
	Border  color.Color
	SelBg   color.Color
	SelFg   color.Color
}

// paletteFor maps Catppuccin's colour names onto bash-teacher's roles. The
// mapping follows the palette's own guidance: Text for body copy, Subtext0 and
// Overlay0 for the two levels of de-emphasis, Surface1 for borders and
// selection, and the standard Green/Red/Yellow for state.
func paletteFor(f catppuccin.Flavor) Palette {
	return Palette{
		Base:    f.Base(),
		Fg:      f.Text(),
		Dim:     f.Subtext0(),
		Faint:   f.Overlay0(),
		Accent:  f.Blue(),
		Accent2: f.Mauve(),
		Pass:    f.Green(),
		Fail:    f.Red(),
		Warn:    f.Yellow(),
		Border:  f.Surface1(),
		SelBg:   f.Surface1(),
		SelFg:   f.Text(),
	}
}

// noPalette leaves every role uncoloured, for NO_COLOR and for piped output.
var noPalette = Palette{
	Base: lipgloss.NoColor{}, Fg: lipgloss.NoColor{}, Dim: lipgloss.NoColor{},
	Faint: lipgloss.NoColor{}, Accent: lipgloss.NoColor{}, Accent2: lipgloss.NoColor{},
	Pass: lipgloss.NoColor{}, Fail: lipgloss.NoColor{}, Warn: lipgloss.NoColor{},
	Border: lipgloss.NoColor{}, SelBg: lipgloss.NoColor{}, SelFg: lipgloss.NoColor{},
}

// Theme bundles the resolved palette with the styles built from it. It is
// always passed by pointer: a Lip Gloss Style is a large value, and a Theme
// holds sixteen of them, so copying one per render call would be wasteful.
type Theme struct {
	// Mode is the mode after aliases and detection have been resolved, so it
	// is always a flavour name or None.
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
// COLORFGBG. An explicit flavour always wins; detection only decides between
// Latte and Mocha when the mode is Auto.
func Resolve(requested Mode) *Theme {
	mode := requested
	if mode == "" {
		mode = Auto
	}
	if alias, ok := aliases[mode]; ok {
		mode = alias
	}
	// The NO_COLOR convention is "present and not an empty string", so an
	// empty value must not disable colour. It overrides an explicitly
	// requested flavour: someone who sets it has a reason to.
	if os.Getenv("NO_COLOR") != "" {
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
	if mode == None {
		return build(None, noPalette)
	}
	f, ok := flavors[mode]
	if !ok {
		f, mode = catppuccin.Mocha, Mocha
	}
	return build(mode, paletteFor(f))
}

// detect chooses between the light and dark ends of the palette. It reads
// COLORFGBG first — it is cheap, explicit, and set by several terminals —
// before falling back to Lip Gloss's background query.
func detect() Mode {
	if v := os.Getenv("COLORFGBG"); v != "" {
		// The format is "fg;bg" (sometimes "fg;_;bg"); a low background
		// number means a dark background.
		parts := strings.Split(v, ";")
		bg := parts[len(parts)-1]
		switch bg {
		case "0", "1", "2", "3", "4", "5", "6", "8":
			return Mocha
		case "7", "15":
			return Latte
		}
	}
	if lipgloss.HasDarkBackground(os.Stdin, os.Stdout) {
		return Mocha
	}
	return Latte
}

// stdoutIsTerminal reports whether stdout is a character device.
func stdoutIsTerminal() bool {
	info, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

func build(mode Mode, p Palette) *Theme {
	t := &Theme{Mode: mode, Palette: p}
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
	t.Badge = lipgloss.NewStyle().Foreground(p.Base).Background(p.Accent).Padding(0, 1)
	return t
}

// IsDark reports whether the resolved palette is one of the dark flavours.
// Components that ship their own light and dark defaults, such as the help
// bubble, need this to pick a matching set.
func (t *Theme) IsDark() bool { return t.Mode != Latte }
