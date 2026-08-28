package theme_test

import (
	"strings"
	"testing"

	"bash-teacher/internal/theme"
)

func TestParseModeAcceptsFlavoursAndAliases(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"", "auto"},
		{"auto", "auto"},
		{"none", "none"},
		{"latte", "latte"},
		{"Frappe", "frappe"},
		{" macchiato ", "macchiato"},
		{"mocha", "mocha"},
		{"dark", "dark"},
		{"light", "light"},
	} {
		got, err := theme.ParseMode(tc.in)
		if err != nil {
			t.Errorf("ParseMode(%q): %v", tc.in, err)
			continue
		}
		if string(got) != tc.want {
			t.Errorf("ParseMode(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestParseModeRejectsUnknown guards against a typo silently giving the wrong
// palette instead of an error.
func TestParseModeRejectsUnknown(t *testing.T) {
	_, err := theme.ParseMode("dracula")
	if err == nil {
		t.Fatal("expected an error for an unknown theme")
	}
	if !strings.Contains(err.Error(), "mocha") {
		t.Errorf("the error should list the valid choices, got: %v", err)
	}
}

// TestAliasesResolveToFlavours checks that Resolve always lands on a concrete
// flavour, never on an alias, so IsDark and the palette are well defined.
func TestAliasesResolveToFlavours(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	for _, tc := range []struct {
		in     theme.Mode
		want   theme.Mode
		isDark bool
	}{
		{theme.Light, theme.Latte, false},
		{theme.Dark, theme.Mocha, true},
		{theme.Frappe, theme.Frappe, true},
		{theme.Macchiato, theme.Macchiato, true},
	} {
		got := theme.Resolve(tc.in)
		if got.Mode != tc.want {
			t.Errorf("Resolve(%q).Mode = %q, want %q", tc.in, got.Mode, tc.want)
		}
		if got.IsDark() != tc.isDark {
			t.Errorf("Resolve(%q).IsDark() = %v, want %v", tc.in, got.IsDark(), tc.isDark)
		}
	}
}

// TestFlavoursDifferInColour is a sanity check that the Catppuccin palettes are
// actually wired up rather than every role resolving to the same value.
func TestFlavoursDifferInColour(t *testing.T) {
	latte := theme.Resolve(theme.Latte)
	mocha := theme.Resolve(theme.Mocha)
	if latte.Palette.Fg == mocha.Palette.Fg {
		t.Error("Latte and Mocha should not share a foreground")
	}
	if mocha.Palette.Accent == mocha.Palette.Fail {
		t.Error("accent and fail must stay visually distinct")
	}
}

// TestNoColorWins checks the accessibility escape hatch: NO_COLOR overrides an
// explicitly requested flavour.
func TestNoColorWins(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	got := theme.Resolve(theme.Mocha)
	if got.Mode != theme.None {
		t.Errorf("NO_COLOR should force None, got %q", got.Mode)
	}
	if plain := got.Title.Render("x"); plain != "x" {
		t.Errorf("None mode should emit no escapes, got %q", plain)
	}
}
