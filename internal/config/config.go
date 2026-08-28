// Package config reads bash-teacher's optional settings file: the TOML
// document at $XDG_CONFIG_HOME/bash-teacher/config.toml that SPEC §8
// describes.
//
// Two rules shape the package. The file is optional — a learner who never
// writes one runs on Defaults, and a missing file is not an error — but a file
// that does exist is taken at its word: an unknown key or an out-of-range
// value is refused with a message rather than quietly dropped, because a
// setting that does nothing and says nothing is worse than no setting at all.
// And, like the content linter, Load reports every problem it finds at once,
// so that fixing a file is one round trip rather than five.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"bash-teacher/internal/fuzzy"
	"bash-teacher/internal/srs"
	"bash-teacher/internal/theme"
)

// Config is the whole settings file. Its zero value is not meaningful: start
// from Defaults, which is also what Load decodes over, so an absent key keeps
// its default without every field having to be a pointer to tell "unset" from
// "set to zero".
type Config struct {
	UI     UI     `toml:"ui"`
	Review Review `toml:"review"`
}

// UI is the [ui] table: how the app looks.
type UI struct {
	// Theme is a --theme value: a Catppuccin flavour, "auto", or "none".
	// The flag wins over the file, so that one odd terminal can be handled
	// with an argument instead of a permanent edit.
	Theme string `toml:"theme"`
}

// Review is the [review] table: the scheduler knobs SPEC §5 calls
// configurable, plus the answer timer, which is a preference about the screen
// rather than about the model.
//
// RelearnStep is deliberately absent. It is the one scheduling number that is
// part of the model's shape rather than of a learner's taste — SPEC §5 states
// it as ten minutes — and exposing it would let a file put a failed card back
// a fortnight away, which is not a preference but a broken scheduler.
type Review struct {
	NewCardsPerDay   int      `toml:"new_cards_per_day"`
	MaxReviewsPerDay int      `toml:"max_reviews_per_day"`
	SessionSize      int      `toml:"session_size"`
	DesiredRetention float64  `toml:"desired_retention"`
	Timer            bool     `toml:"timer"`
	SoftTarget       Duration `toml:"soft_target"`
}

// Duration is a time.Duration written the way Go spells it — "8s", "1m30s" —
// since TOML has no duration type and a bare number would leave the unit to be
// guessed.
type Duration time.Duration

// UnmarshalText parses a duration string. The error is returned as-is: the
// TOML decoder wraps it with the line the value came from, which is more
// useful than anything this could add.
func (d *Duration) UnmarshalText(text []byte) error {
	v, err := time.ParseDuration(string(text))
	if err != nil {
		return err
	}
	*d = Duration(v)
	return nil
}

func (d Duration) String() string { return time.Duration(d).String() }

// Defaults is the configuration of a machine with no settings file. The review
// half is read out of srs.Defaults rather than restated, so the file's
// defaults and the scheduler's can never drift apart.
func Defaults() Config {
	p := srs.Defaults()
	return Config{
		UI: UI{Theme: string(theme.Auto)},
		Review: Review{
			NewCardsPerDay:   p.NewPerDay,
			MaxReviewsPerDay: p.MaxReviewsPerDay,
			SessionSize:      p.SessionSize,
			DesiredRetention: p.DesiredRetention,
			Timer:            true,
			SoftTarget:       Duration(p.SoftTarget),
		},
	}
}

// Params returns the scheduler parameters this configuration asks for. It
// starts from srs.Defaults and overwrites only the knobs the file exposes, so
// a number the model grows before the file does keeps its default instead of
// arriving as a zero.
func (c Config) Params() srs.Params {
	p := srs.Defaults()
	p.NewPerDay = c.Review.NewCardsPerDay
	p.MaxReviewsPerDay = c.Review.MaxReviewsPerDay
	p.SessionSize = c.Review.SessionSize
	p.DesiredRetention = c.Review.DesiredRetention
	p.SoftTarget = time.Duration(c.Review.SoftTarget)
	return p
}

// Mode returns the theme the file asks for. Load has already validated the
// name, so a Config that came from it cannot fail here; one built by hand with
// nonsense in it falls back to auto rather than panicking a caller that has no
// error to return.
func (c Config) Mode() theme.Mode {
	m, err := theme.ParseMode(c.UI.Theme)
	if err != nil {
		return theme.Auto
	}
	return m
}

// Path is where the settings file lives: $XDG_CONFIG_HOME/bash-teacher/config.toml,
// falling back to ~/.config, and to a relative directory on a machine with no
// home at all — the same shape as the data directory that holds progress.db.
func Path() string {
	if d := os.Getenv("XDG_CONFIG_HOME"); d != "" {
		return filepath.Join(d, "bash-teacher", "config.toml")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".bash-teacher", "config.toml")
	}
	return filepath.Join(home, ".config", "bash-teacher", "config.toml")
}

// Load reads the settings file at path, and reports whether there was one to
// read. A missing file is the ordinary case — it leaves Defaults untouched and
// returns no error.
//
// A file that is present but wrong returns *Error listing everything wrong
// with it, along with Defaults, so that a caller which decides to carry on has
// something workable to carry on with. `bt doctor` is that caller: the command
// you reach for when something is broken has to start when something is
// broken.
func Load(path string) (Config, bool, error) {
	cfg := Defaults()
	md, err := toml.DecodeFile(path, &cfg)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return Defaults(), false, nil
	case err != nil:
		return Defaults(), true, &Error{Path: path, Problems: []Problem{{Msg: err.Error()}}}
	}
	if problems := validate(cfg, md); len(problems) > 0 {
		return Defaults(), true, &Error{Path: path, Problems: problems}
	}
	return cfg, true, nil
}

// The bounds every configurable number is held to. They are wide enough that
// no reasonable preference hits them and narrow enough that no setting can put
// the app somewhere it cannot work.
const (
	maxNewCardsPerDay = 500
	maxReviewsPerDay  = 2000
	maxSessionSize    = 500
	// The retention band is bounded at both ends by the interval formula in
	// internal/srs: at 1.0 its (R^(1/decay) - 1) term is zero and every
	// interval collapses to today, and below about 0.70 the intervals grow
	// faster than a learner can actually keep up with, which reads as the
	// scheduler being broken rather than as an aggressive setting.
	minRetention  = 0.70
	maxRetention  = 0.98
	minSoftTarget = time.Second
	maxSoftTarget = 5 * time.Minute
)

// validate collects every problem in a decoded file. Nothing here stops at the
// first: an author editing a config should get the whole list.
func validate(c Config, md toml.MetaData) []Problem {
	var ps []Problem
	for _, k := range md.Undecoded() {
		ps = append(ps, Problem{Key: k.String(), Msg: unknownKeyMsg(k.String())})
	}
	if _, err := theme.ParseMode(c.UI.Theme); err != nil {
		ps = append(ps, Problem{Key: "ui.theme", Msg: err.Error()})
	}
	if n := c.Review.NewCardsPerDay; n < 0 || n > maxNewCardsPerDay {
		ps = append(ps, Problem{Key: "review.new_cards_per_day",
			Msg: fmt.Sprintf("%d is outside 0–%d (0 introduces nothing new and reviews what is due)", n, maxNewCardsPerDay)})
	}
	if n := c.Review.MaxReviewsPerDay; n < 1 || n > maxReviewsPerDay {
		ps = append(ps, Problem{Key: "review.max_reviews_per_day",
			Msg: fmt.Sprintf("%d is outside 1–%d", n, maxReviewsPerDay)})
	}
	if n := c.Review.SessionSize; n < 1 || n > maxSessionSize {
		ps = append(ps, Problem{Key: "review.session_size",
			Msg: fmt.Sprintf("%d is outside 1–%d", n, maxSessionSize)})
	}
	if r := c.Review.DesiredRetention; r < minRetention || r > maxRetention {
		ps = append(ps, Problem{Key: "review.desired_retention",
			Msg: fmt.Sprintf("%.2f is outside %.2f–%.2f", r, minRetention, maxRetention)})
	}
	if d := time.Duration(c.Review.SoftTarget); d < minSoftTarget || d > maxSoftTarget {
		ps = append(ps, Problem{Key: "review.soft_target",
			Msg: fmt.Sprintf("%s is outside %s–%s", d, minSoftTarget, maxSoftTarget)})
	}
	return ps
}

// unknownKeyMsg explains a key that decoded into nothing, and suggests the
// setting it was probably meant to be. The suggestion is matched on the leaf
// name, so that a key in the wrong table is still recognised for what it is.
func unknownKeyMsg(key string) string {
	all := settings()
	leaf := key[strings.LastIndex(key, ".")+1:]
	best, which := 0, -1
	for i, s := range all {
		m, ok := fuzzy.Score(leaf, s[strings.LastIndex(s, ".")+1:])
		if ok && (which < 0 || m.Score > best) {
			best, which = m.Score, i
		}
	}
	if which < 0 {
		return "unknown setting; this file holds " + strings.Join(all, ", ")
	}
	return fmt.Sprintf("unknown setting; did you mean %s?", all[which])
}

// settings lists every key the file may hold, in dotted form. It is derived
// from the struct rather than written out, so the list a learner is shown
// cannot drift from the list that actually decodes.
func settings() []string {
	var out []string
	t := reflect.TypeOf(Config{})
	for i := range t.NumField() {
		table := t.Field(i)
		for j := range table.Type.NumField() {
			out = append(out, table.Tag.Get("toml")+"."+table.Type.Field(j).Tag.Get("toml"))
		}
	}
	return out
}

// Problem is one thing wrong with a settings file.
type Problem struct {
	// Key is the dotted setting at fault, or empty when the file could not
	// be parsed at all and there is no key to name.
	Key string
	Msg string
}

// Error is everything wrong with a settings file, reported together.
type Error struct {
	Path     string
	Problems []Problem
}

func (e *Error) Error() string {
	var b strings.Builder
	noun := "problems"
	if len(e.Problems) == 1 {
		noun = "problem"
	}
	fmt.Fprintf(&b, "%s: %d %s", e.Path, len(e.Problems), noun)
	for _, p := range e.Problems {
		b.WriteString("\n  ")
		if p.Key != "" {
			b.WriteString(p.Key + ": ")
		}
		b.WriteString(p.Msg)
	}
	return b.String()
}
