package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bash-teacher/internal/srs"
	"bash-teacher/internal/theme"
)

// write puts a settings file in a temporary directory and returns its path.
func write(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// loadErr loads a file that is expected to be refused and returns the problems.
func loadErr(t *testing.T, body string) *Error {
	t.Helper()
	cfg, found, err := Load(write(t, body))
	if err == nil {
		t.Fatalf("expected the file to be refused, got %+v", cfg)
	}
	if !found {
		t.Error("a file that was read and refused should still report as found")
	}
	if cfg != Defaults() {
		t.Error("a refused file should hand back the defaults, so a caller that carries on has something to carry on with")
	}
	var cfgErr *Error
	if !errors.As(err, &cfgErr) {
		t.Fatalf("expected *config.Error, got %T", err)
	}
	return cfgErr
}

func TestMissingFileIsNotAnError(t *testing.T) {
	cfg, found, err := Load(filepath.Join(t.TempDir(), "nothing.toml"))
	if err != nil {
		t.Fatalf("a learner who has never written a settings file is the ordinary case: %v", err)
	}
	if found {
		t.Error("found should be false when there is no file")
	}
	if cfg != Defaults() {
		t.Errorf("got %+v, want the defaults", cfg)
	}
}

func TestDefaultsAreTheSchedulerDefaults(t *testing.T) {
	if got, want := Defaults().Params(), srs.Defaults(); got != want {
		t.Errorf("the file's defaults have drifted from the scheduler's:\n got %+v\nwant %+v", got, want)
	}
	if !Defaults().Review.Timer {
		t.Error("the timer is on unless a file turns it off")
	}
	if Defaults().Mode() != theme.Auto {
		t.Errorf("got %q, want auto", Defaults().Mode())
	}
}

func TestAFullFileIsApplied(t *testing.T) {
	cfg, found, err := Load(write(t, `
[ui]
theme = "latte"

[review]
new_cards_per_day = 5
max_reviews_per_day = 60
session_size = 10
desired_retention = 0.85
timer = false
soft_target = "12s"
`))
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Error("found should be true for a file that exists")
	}
	if cfg.Mode() != theme.Mode("latte") {
		t.Errorf("theme: got %q, want latte", cfg.Mode())
	}
	if cfg.Review.Timer {
		t.Error("timer: got on, want off")
	}
	p := cfg.Params()
	want := srs.Params{
		DesiredRetention: 0.85,
		NewPerDay:        5,
		MaxReviewsPerDay: 60,
		SessionSize:      10,
		RelearnStep:      srs.Defaults().RelearnStep,
		SoftTarget:       12 * time.Second,
	}
	if p != want {
		t.Errorf("params:\n got %+v\nwant %+v", p, want)
	}
}

func TestAbsentKeysKeepTheirDefaults(t *testing.T) {
	cfg, _, err := Load(write(t, "[review]\nsession_size = 7\n"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Review.SessionSize != 7 {
		t.Errorf("session_size: got %d, want 7", cfg.Review.SessionSize)
	}
	// Everything the file did not mention must be untouched, and in
	// particular must not have decoded as a zero.
	def := Defaults()
	def.Review.SessionSize = 7
	if cfg != def {
		t.Errorf("a one-key file changed more than one key:\n got %+v\nwant %+v", cfg, def)
	}
}

func TestZeroNewCardsIsAllowed(t *testing.T) {
	// Nothing new today, but keep reviewing what is due, is a real request
	// and the one place a zero means something.
	if _, _, err := Load(write(t, "[review]\nnew_cards_per_day = 0\n")); err != nil {
		t.Fatalf("new_cards_per_day = 0 should be accepted: %v", err)
	}
}

func TestUnknownKeyIsRefusedWithASuggestion(t *testing.T) {
	e := loadErr(t, "[review]\nnew_per_day = 15\n")
	if len(e.Problems) != 1 {
		t.Fatalf("got %d problems, want 1: %v", len(e.Problems), e)
	}
	if e.Problems[0].Key != "review.new_per_day" {
		t.Errorf("key: got %q", e.Problems[0].Key)
	}
	if !strings.Contains(e.Problems[0].Msg, "review.new_cards_per_day") {
		t.Errorf("a near miss should be named: %q", e.Problems[0].Msg)
	}
}

func TestAKeyInTheWrongTableIsStillRecognised(t *testing.T) {
	e := loadErr(t, "[ui]\nsession_size = 10\n")
	if !strings.Contains(e.Problems[0].Msg, "review.session_size") {
		t.Errorf("got %q, want the right table suggested", e.Problems[0].Msg)
	}
}

func TestAnUnrecognisableKeyListsTheSettings(t *testing.T) {
	e := loadErr(t, "[ui]\nzzzzz = 1\n")
	if !strings.Contains(e.Problems[0].Msg, "ui.theme") {
		t.Errorf("got %q, want the available settings listed", e.Problems[0].Msg)
	}
}

func TestEveryProblemIsReportedAtOnce(t *testing.T) {
	// Four bad values and an unknown key: an author editing a file should
	// get the whole list, the way the content linter reports content.
	e := loadErr(t, `
[ui]
theme = "solarized"

[review]
max_reviews_per_day = 0
session_size = 9000
desired_retention = 1.0
soft_target = "3h"
`)
	if len(e.Problems) != 5 {
		t.Fatalf("got %d problems, want 5:\n%v", len(e.Problems), e)
	}
	for _, want := range []string{
		"ui.theme", "review.max_reviews_per_day", "review.session_size",
		"review.desired_retention", "review.soft_target",
	} {
		if !strings.Contains(e.Error(), want) {
			t.Errorf("%q is missing from:\n%v", want, e)
		}
	}
}

func TestRetentionOfOneIsRefused(t *testing.T) {
	// At 1.0 the interval formula collapses to today and every card would be
	// due forever, so the band is closed at the top as well as the bottom.
	for _, r := range []string{"1.0", "0.5"} {
		e := loadErr(t, "[review]\ndesired_retention = "+r+"\n")
		if len(e.Problems) != 1 || e.Problems[0].Key != "review.desired_retention" {
			t.Errorf("retention %s: got %v", r, e.Problems)
		}
	}
}

func TestMalformedFilesAreRefused(t *testing.T) {
	for name, body := range map[string]string{
		"not toml":             "[review\nsession_size = 3\n",
		"wrong type":           "[review]\nsession_size = \"ten\"\n",
		"bad duration":         "[review]\nsoft_target = \"eight seconds\"\n",
		"bad theme":            "[ui]\ntheme = \"gruvbox\"\n",
		"duration as a number": "[review]\nsoft_target = 8\n",
	} {
		t.Run(name, func(t *testing.T) {
			e := loadErr(t, body)
			if len(e.Problems) == 0 {
				t.Fatal("an error with no problems in it explains nothing")
			}
			if !strings.Contains(e.Error(), "config.toml") {
				t.Errorf("the message should name the file:\n%v", e)
			}
		})
	}
}

func TestSettingsListCoversTheStruct(t *testing.T) {
	// Every field must carry a toml tag, or it would be silently
	// unreachable from a file and missing from the suggestion list.
	for _, s := range settings() {
		table, key, ok := strings.Cut(s, ".")
		if !ok || table == "" || key == "" {
			t.Errorf("untagged field produced %q", s)
		}
	}
	if len(settings()) != 7 {
		t.Errorf("got %d settings, want 7: %v", len(settings()), settings())
	}
}

func TestPathFollowsXDG(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	if got, want := Path(), filepath.Join(dir, "bash-teacher", "config.toml"); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	t.Setenv("XDG_CONFIG_HOME", "")
	if !strings.HasSuffix(Path(), filepath.Join(".config", "bash-teacher", "config.toml")) {
		t.Errorf("got %q, want a ~/.config path", Path())
	}
}
