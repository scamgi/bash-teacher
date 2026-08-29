# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

A Go TUI that teaches Unix commands and pipeline composition, through three surfaces
sharing one content library: a Dictionary, sandbox-executed Pipeline Exercises, and
Flashcards. **`SPEC.md` is the design source of truth** — read it before adding a
feature, and update it when a decision changes. It defines six milestones (M1–M6);
M1 (skeleton), M2 (dictionary), M3 (runner), M4 (practice) and M5 (flashcards) are
complete, and M6 is under way: the progress store is built, so a session now picks up
where the last one stopped, the settings file is built, so the learner sets the theme,
the caps and the timer, `bt export`/`bt import` are built, so progress moves between
machines, and Stats is finished, so the review log is drawn as a retention curve, an
activity history and a per-command mastery grid. The rest of M6 — keybinding remapping
and packaging — is still open.

## Commands

```sh
make build      # -> ./bt, with version stamped from git describe
make run        # build and launch the TUI
make check      # vet + golangci-lint + tests + content lint. Run before finishing.
make lint-go    # golangci-lint (2.x, config uses the v2 schema)
make lint       # content lint only: go run ./cmd/bt content lint
make expected   # regenerate every exercise's expected output from its solution
make test       # go test ./...
```

Single test: `go test ./internal/tui -run TestNumberKeysNavigate -v`

The adversarial corpus is the M3 exit criterion and the thing to run after touching the
runner: `go test ./internal/runner -run Adversarial -v`. `bt doctor` reports which sandbox
backend this machine gets.

The M4 exit criterion is `go test ./internal/runner -run Reference -v`, which runs all
120 reference solutions in the sandbox and diffs them against the committed expected
outputs. After editing an exercise or a fixture, run `make expected` and commit what it
writes — never hand-edit a file under `content/expected/`.

The M5 exit criteria are `go test ./internal/srs -v` — the interval ladder is pinned to
exact numbers, so any change to the model breaks it loudly and on purpose — and
`go test ./internal/answer -v`, whose last case walks all 250 cards and fails if any
card's own answer would not be accepted from a learner.

Stats is covered by `go test ./internal/srs -run 'History|Streak|Totals' -v` for the
aggregations and `go test ./internal/tui -run Stats -v` for the panes, the latter over a
scripted three weeks seeded by `seedHistory` in `internal/tui/stats_test.go`.

The store's own guard is `go test ./internal/tui -run Restart -v`: it answers a card and
solves an exercise in one process and asserts both are there in the next.
Export/import is covered by `go test ./internal/store -run 'Export|Import|Archive' -v`.

Dump rendered frames to eyeball layout without a terminal:
`BT_DUMP=1 go test ./internal/tui -run DumpFrames -v`

## Non-obvious constraints

**Charm v2 modules live at `charm.land/`, not `github.com/charmbracelet/`.** The imports
are `charm.land/bubbletea/v2`, `charm.land/lipgloss/v2`, `charm.land/bubbles/v2`.
`go get github.com/charmbracelet/bubbletea@latest` silently resolves to v1 — never use it.

**Bubble Tea v2 differs from the v1 API in ways that matter here:** `View()` returns a
`tea.View` struct, not a string; the alternate screen is set per-frame with
`v.AltScreen = true` (see `altView` in `internal/tui/app.go`), not with a program option;
key input arrives as `tea.KeyPressMsg`, whose `.Text` field holds printable characters.

**The module path is the bare `bash-teacher`** because the repo has no remote. Imports are
`bash-teacher/internal/...`. Rename with `go mod edit -module` if it is ever pushed.

**`theme.Theme` is always passed by pointer.** It holds sixteen Lip Gloss styles (~10 KB);
`gocritic`'s `hugeParam` enforces this, with the threshold raised to 1500 bytes so ordinary
value types are not flagged.

## Architecture

### Two content packages, deliberately

`go:embed` cannot reach outside its own package directory, so the content tree at the repo
root has a package of its own:

- **`content/`** (package `library`) — nothing but the `//go:embed` declaration exposing `FS`.
- **`internal/fuzzy`** — subsequence matching and ranking for the search box. Filtering
  the dictionary switches it from category-grouped to a flat ranked list, and the cursor
  always follows the top match while a filter is active (a command can match through its
  summary alone, so preserving the old selection would strand the cursor).
- **`internal/content`** — the schema, the loader, and the linter. `Load(fs.FS)` parses,
  indexes, builds tracks, then lints; it returns a `*LintError` listing **every** problem at
  once rather than failing on the first, so authors fix a batch per run.

Tests substitute a `fstest.MapFS` for the embedded tree, so loader and lint behaviour are
testable without touching real content.

### Portable expected outputs

`match: squeezed` exists because GNU and BSD coreutils pad numeric columns differently:
`uniq -c` writes `     31 x` on one and `  31 x` on the other, and `wc -l` and `nl` differ
the same way. Any exercise whose output ends in a count uses `squeezed`, which compares
lines with leading and trailing whitespace stripped and internal runs collapsed —
otherwise the committed expected file is pinned to whichever coreutils generated it.

Two more portability rules for new exercises: keep to commands present on both macOS and
Linux (`tac` and `timeout` are not, and `xz` is not under the sandbox's `PATH` of
`/usr/bin:/bin:/usr/sbin:/sbin`), and never let a `sort -rn` cut through a tie — either
pick data with distinct counts or name the tie-break explicitly, as
`sort -k1,1rn -k2,2` does.

### TUI screen contract (`internal/tui`)

One root `App` model routes between five screens, each an independent sub-model
implementing the unexported `screen` interface:

- **Sub-models never navigate or quit themselves.** They return a `tea.Cmd` from `Navigate(to)`
  and let `App.Update` decide. Global keys (`0`–`4`, `esc`, `q`, `?`) are handled in
  `App.handleGlobalKey` before the message reaches the screen.
- **`Capturing() bool` is the text-input gate.** When a screen is consuming raw keystrokes
  (the dictionary's `/` filter), the root model skips every global binding except `ctrl+c`,
  so typing `2` or `q` into a search box does not navigate or quit. New screens with input
  must implement this or they will lose keystrokes.
- **The footer is a status bar with two zones** (`internal/tui/statusbar.go`): the
  screen's key legend on the left, the session's counters on the right. They are sized
  independently and the counters give way first — the legend is truncated only once
  every chip has been dropped, because a key the learner cannot find is worse than a
  count Home and Stats also report. A `flashMsg` takes the legend's place, not the
  whole bar.
- **The header is a tab bar** (`internal/tui/topbar.go`): the five screens in the order
  their number keys select them, with the rule underneath drawn heavy under the current
  one. The mark is a glyph rather than a colour so it survives `--theme none`, and the
  tabs are navigation — the version gives way first on a narrow terminal, then the brand,
  and the tabs never do.
- **`Body(a, width, height)` renders only the interior**, excluding header and footer.
  `fitBlock` pads and truncates it to exactly that box, so the chrome never drifts. A pane
  that can overflow must `clip` itself.

### Cross-screen navigation

Screens never hold references to one another. The dictionary's `p` and `f` shortcuts
return commands carrying `openExerciseMsg` / `showCardsMsg`, which `App.Update` routes
by asserting the target screen against a small interface (`exerciseOpener`,
`cardFilterer`). The target returns false when it cannot honour the request — no cards
for that command — and the root model then leaves the screen alone. `flashMsg` puts a
transient line in the footer; it is cleared by the next keystroke, so no timers.

### Dictionary detail pane (`internal/tui/dictionary.go`)

The right pane is a `viewport` whose content is rebuilt only when the command, width,
focused item, or focus changes — the focus caret is drawn *into* the content rather than
overlaid, so moving the cursor invalidates the cache. `dictItem` records the line each
actionable row landed on, which is what `EnsureVisible` and the caret both index by.
`TestEveryCommandEntryRenders` walks all 80 entries at two widths; it is the M2 exit
criterion and it has already caught a panic on flags too wide for their column.

### Practice workspace (`internal/tui/practice.go`)

The screen has two modes. `modeBrowse` is the exercise library; `modeWork` is the
workspace, and there `Capturing()` is true — the pipeline editor owns every printable
key, so every action is a chord (`^R` run, `^H` hint, `^S` solution, `^N`/`^P` move,
`^B` fixture preview, `^G` dictionary lookup, `esc` back to the list). `internal/tui/editor.go`
is a small readline-shaped editor rather than bubbles' textinput, because completion,
word motions and the lookup all need to know which word the cursor is in. Beware that
`Keys.Left`/`Keys.Right` carry vim's `h`/`l`: the editor uses `Keys.CharLeft`/`CharRight`
so those letters stay typable.

Runs are async: `^R` returns a `tea.Cmd` and the result comes back as a `runResultMsg`,
which `App.Update` routes to Practice by name rather than to the current screen, so a
result that lands while the learner is reading a dictionary entry is not lost. The
workspace divides its height from the bottom up — the editor and `minOutputLines` of
output are reserved before the fixture preview gets what is left.

Track locking is computed (`internal/tui/progress.go`, 80% per SPEC §2.2), displayed, and
flashed on entry, but **not enforced** — `open` runs before the check, so a locked
exercise opens anyway. The reason it was advisory (progress lived in memory, so a hard
gate would have re-locked the library on every launch) expired with the store; whether it
becomes a real gate is an open decision, not an oversight to quietly fix.

### The Stats screen (`internal/tui/stats.go`, `internal/tui/mastery.go`)

Four panes cycled with `tab` / `shift+tab` — Review, History, Mastery, Library — because
SPEC §2.4 asks for five reports and the smallest supported terminal is 24 rows. The
screen computes nothing durable: it reads the scheduler and the practice summaries the
root model already holds, which is why a pane can be added without touching persistence.

- **The aggregations live in `internal/srs`, not in the screen.** `History`, `LongestStreak`,
  `ActiveDays` and `Totals` bucket the review log by local day; the package stays
  content-free, so they are unit-testable over synthetic ids. `History` returns a
  fixed-width series with the empty days present, so a chart can index it by column.
- **Practice credit counts as turning up, never as recall.** `Day.Answered` includes it and
  `Day.Reviewed` does not, which is what keeps a solved pipeline from lifting the
  retention curve. `Day.Retention` returns false on a day with no reviews and the chart
  draws that column as a gap — a day not studied is not a day failed.
- **Mastery is scored from cards, not exercises.** A command's band is the mean of its
  cards' scores, floored, with the bands at 7 and 21 days of stability — readable only
  because stability *is* the interval in days at the default retention. Exercises reach
  the grid through the credit `App.CreditPractice` already gives every card they teach,
  so there is one path in and not two differently weighted ones.
- The grid's cursor column is a **wish, not a position**: `focusedColumn` clamps it per
  row, so scanning down through a four-command category does not drag the cursor left.
  The focused cell is bracketed as well as highlighted, so it survives `--theme none`,
  and the bands are a shade ramp for the same reason.
- Panel widths are shared (`panelWidth`, `masteryBoxWidth`) so the frame does not breathe
  as `tab` walks the panes. `TestStatsPanesFitTheSmallestTerminal` asserts every pane
  closes its box inside an 80×24 frame — `fitBlock` truncates silently, so a pane that
  outgrew the terminal would otherwise just lose its last rows.

### The progress store (`internal/store`)

SQLite through `modernc.org/sqlite`, which is SQLite translated into Go rather than a
binding, so `bt` stays cgo-free and a single binary. The database is
`$XDG_DATA_HOME/bash-teacher/progress.db`; `--no-store` runs without one, which is what
the TUI tests do.

The store computes nothing. `Grade` and `Credit` return the state the scheduler decided
on and the app writes that, pairing it with the log entry read back from
`Scheduler.LastReview` rather than rebuilt from the arguments — so the log on disk cannot
describe something other than the log in memory. `reviews` and `attempts` are append-only;
`cards` and `exercises` are upserted summaries that could in principle be rebuilt from
them.

Migrations are a numbered, forward-only slice, each applied in its own transaction, with
the version in SQLite's `user_version`. **Never edit a released migration** — add another.
A file whose `user_version` exceeds this build's is refused with `ErrNewerSchema`.

Wiring: `tui.WithStore` is an option, not a parameter, so a test that does not care about
persistence does not have to open a database. It hydrates the scheduler
(`srs.Scheduler.Restore`) and the practice screen (`RestoreProgress`). Writes go through
`App.persistCard` / `App.SaveExercise` / `App.LogAttempt`, which no-op on a nil store, and
the first failure is kept in `App.storeErr`: `App.Persisting()` then reports false for the
rest of the session and Home and Stats say plainly that nothing is being saved, rather
than showing a streak that would be a lie. An unreadable database costs the learner their
history, never their app.

`internal/tui/progress.go` holds `store.Exercise` values directly rather than a parallel
struct, so a save cannot drop a field the screen was relying on.

### Export and import (`internal/store/transfer.go`)

`bt export` dumps the four progress tables to stdout as JSON and `bt import` restores
them; SPEC §7.3 is the contract. Three decisions carry the design:

- **The archive mirrors the SQL schema**, column name for column name and unit for unit —
  Unix seconds with 0 for "never", durations in milliseconds. That is what makes the round
  trip lossless by construction rather than by care, and it versions the file by the
  schema: an older archive restores with the columns it predates left at zero, exactly as
  the migration that added them would have, and a newer one is refused with
  `ErrNewerArchive`. Adding a migration therefore extends the format for free — but a
  column renamed in a migration renames a JSON key, so **never rename an archive field
  without a migration behind it**. `meta` is deliberately not carried: it describes an
  installation, not a learner.
- **Import replaces; it never merges.** `Restore` clears all four tables and writes the
  archive inside one transaction, so a file that fails halfway leaves the old history
  intact. A database that already holds progress is refused with `ErrNotEmpty` unless
  `force`, and the error names the counts that would have been lost; `cmd/bt` is what adds
  the "`bt import --force` replaces it" advice, so the store stays a library.
- **`ReadArchive` checks the envelope permissively, then the rows strictly.** The two
  passes exist so a file that is not an export at all is named as such rather than
  reported through whichever key decoded badly; the strict pass then refuses unknown
  fields, since the only writer of this format is `bt export`. Validation collects every
  problem at once into an `*ArchiveError`, the same contract the content linter and the
  settings file have, and it covers what would poison the scheduler — an invalid rating, an
  unknown source, a non-finite stability — not merely what SQLite would reject.

Ids the current content library has no entry for are a warning printed by `cmd/bt`, never
a refusal: the store is content-free by design, and a renamed card is not a reason to
throw away the history of every card that still exists.

### The settings file (`internal/config`)

Optional TOML at `$XDG_CONFIG_HOME/bash-teacher/config.toml`, described in SPEC §8.1. The
package is a leaf: `main.go` reads it and translates it into `srs.Params` and TUI options,
so `internal/tui` never learns what a TOML file is and a test can ask for one scheduler
parameter without writing a file to say so.

- **A missing file is not an error; a broken one is.** `Load` decodes over `Defaults()`, so
  an absent key keeps its default without every field being a pointer. Unknown keys
  (`MetaData.Undecoded`) and out-of-range values are collected and returned together as
  `*config.Error`, the same all-at-once contract the content linter has.
- **`Defaults()` reads its review half out of `srs.Defaults()`**, and `Config.Params()`
  starts from `srs.Defaults()` and overwrites only the exposed knobs — so a parameter the
  model grows before the file does keeps its default instead of arriving as a zero.
  `TestDefaultsAreTheSchedulerDefaults` pins that.
- **`bt doctor` is exempt from the refusal.** Every other command exits with the problem
  list; doctor starts on the defaults and prints it, because the command you reach for
  when something is wrong has to run when something is wrong.
- `--theme` wins over the file only when it was actually passed (`flagGiven`): its own
  default of `auto` is not an override.
- `srs.Scheduler.SetParams` exists so `tui.WithParams` is order-independent against
  `tui.WithStore`. Parameters are a preference, not state: nothing already restored is
  re-derived from them, and a card keeps the due date its own review earned it.

### The scheduler (`internal/srs`)

FSRS-lite: the same two-variable model as FSRS — stability and difficulty over the power
forgetting curve — with a handful of documented constants instead of nineteen fitted
weights, because there is no corpus to fit weights to without telemetry.

The two curve constants (`decay = -0.5`, `factor = 19/81`) are chosen together so that
**at the default 0.90 retention the interval equals the stability**. The test
`TestIntervalEqualsStabilityAtDefaultRetention` pins that identity; it is what makes
every other number in the package readable, so never change one constant without the
other.

The package knows nothing about content — cards are addressed by id — so it can be
simulated over synthetic ids. `Grade` and `Preview` both go through the pure `next`,
which is why the interval a rating key advertises can never disagree with the interval
it delivers. `Credit` is the half-strength review SPEC §5 grants for solving an exercise;
it never pulls a due date forward.

`srs_test.go` holds two simulations, and they assert different things: a punctual learner
must converge on the target retention (that is the model's own claim), while a once-a-day
learner lands about ten points lower because every card is answered a few hours to a day
late. Do not "fix" the second by widening the first.

### Answer normalization (`internal/answer`)

Typed flashcard answers are compared after normalization, not as strings. The grader is
built from the `Library` because the interesting questions are about commands, not about
shell syntax: whether `-f3` is a cluster or a flag carrying `3` is a fact about `cut`,
and the dictionary's flag table already answers it. Adding a `long:` to a dictionary flag
is therefore what makes `--ignore-case` grade equal to `-i`.

Value-carrying flags are compared by name with their values in order, which keeps
`cut -f3 -d,` equal to `cut -d, -f3` while leaving `sort -k1 -k2` distinct from
`sort -k2 -k1`. Verdicts are `Correct`, `Wrong`, and `Unsure`; `Unsure` sends the card to
self-grading and is returned when either side fails to parse or when the two differ only
inside quoted text, where a regex has many right spellings. A false negative on a regex
is worse than a question — do not turn `Unsure` into `Wrong` to tidy up a table.

### Review sessions (`internal/tui/flashcards.go`)

Three phases: `phaseIdle` between sittings, `phaseAsk` with the card face down, and
`phaseGrade` with the rating keys. `Capturing()` is true only in `phaseAsk` on a typed
card, so the answer editor owns every printable key there and nothing else on the screen
does.

Ratings are `a`/`h`/`g`/`e` rather than `1`–`4` because the digits are the global screen
switches; `j`/`k` nudge the preselected rating and `enter` commits it. Each key is
labelled with `Scheduler.Preview`, so the cost of "hard" is visible before it is paid.

The scheduler and the grader live on the root `App`, not on this screen, because Home
reports the day's load, Stats draws the forecast, and Practice credits the cards an
exercise teaches. `App.Now()` is the clock everything schedules through, so a test can
place a session on a known day.

### Theme (`internal/theme`)

Catppuccin, all four flavours. `Palette` names colours by **role** (`Accent`, `Pass`, `Fail`,
`Warn`, `Dim`, `Faint`) rather than hue, so swapping flavours cannot change what a colour
means — do not reach for a hue directly. `ParseMode` rejects unknown names with an error
instead of falling back. Colour degrades to plain text on `--theme none`, a non-terminal
stdout, or a non-empty `NO_COLOR`. The theme paints no page background on purpose.

Colour is never the only signal: pass/fail carry `✓`/`✗` glyphs too.

### Content authoring (`content/`)

`commands/` and `exercises/` hold one mapping per file; `cards/` holds a list per file;
`fixtures/<name>/` are flat directories of plain files; `expected/` holds each exercise's
expected stdout.

- **Quote any YAML scalar containing a colon followed by a space** — `cmd: "cut -d: -f1
  /etc/passwd"` — or the parser reads it as a nested mapping. This has bitten every batch
  of new content so far.
- **Expected outputs are generated by actually running the reference solution** against the
  fixture, not written by hand: `make expected`. The reference-solution test re-runs them
  and asserts they still match.
- Fixtures are flat directories of plain files. They must not contain dot files: the
  `go:embed` directive has no `all:` prefix, so a dot file would exist on disk and be
  missing from the binary. The `sourcetree` fixture holds real `.go` files, which makes
  it a Go package as far as the toolchain is concerned; it is excluded in `.golangci.yml`.
- `bt content lint` enforces unique kebab-case ids, resolvable cross-references between
  commands, exercises and cards, an example plus caption on every command, hints on every
  exercise, and fixtures that exist, are flat, and stay under 256 KB. Every card must name at
  least one command so per-command mastery is computable.
- **A card's `back` has to survive `shellparse`.** `recall` and `flag` answers are graded
  by parsing them, so a back containing a subshell, a backtick, a bare `&`, or an
  unbalanced quote grades as `Unsure` and the learner can never simply be right.
  `TestEveryTypedCardAcceptsItsOwnAnswer` is the guard. `identify` cards are exempt: they
  are prose, and never parsed.
- A **`flag` card's `back` is an argument list with no command in front of it**, and it is
  expanded against `commands[0]` — so that entry must be the command whose flags they are.
- Where a card's answer has a genuinely different right spelling, list it under `accepts`
  rather than loosening the grader. The obsolescent `head -5` for `head -n 5` is the
  usual case: the grader reads `-5` as an operand on purpose, since only `head` and
  `tail` spell counts that way.
- Commands in the `network` category are documented but **never executable**; `Library.Allowlist()`
  excludes them, and a test asserts it. The M3 sandbox depends on this.

### The runner (`internal/shellparse`, `internal/runner`)

Learner input is executed through five independent layers, and SPEC.md §6 is the
contract. Each layer is meant to hold on its own; do not weaken one on the grounds that
another covers it.

- **`internal/shellparse`** is a parser, not a shell. It handles words, quoting, pipes,
  redirections and `;`/`&&`/`||`, and returns a positioned `*shellparse.Error` for
  everything else — subshells, command groups, `&`, `$(...)`, backticks, `<(...)`. A
  parse failure is a safety feature: the constructs it cannot represent are the ones the
  sandbox should not run. `Error.Caret()` renders the input with a caret under the fault.
- The **script operand** of `sed`, `awk` and `grep` is exempt from the path rules: it is
  a program, not a filename, and programs start with a slash all the time
  (`sed -n '/start/,/end/p'`). The exemption covers that one operand and lapses when
  `-e`/`-f`/`--regexp` supplies the script, since then every operand is a file.
- **`runner.Policy`** is the static allowlist, **derived from the dictionary** —
  `CanExecute()` plus the four builtins in `runner.Builtins`. `TestAllowlistIsDerivedFromTheDictionary`
  fails if anything is added by hand, and `TestDangerousListDoesNotContradictTheDictionary`
  fails if the hard-refusal list and the dictionary disagree. Path rules are subtle on
  purpose: escape is decided by `path.Clean`, so `sed 's/../X/'` passes and
  `cat ../../etc/passwd` does not; `/dev/null` and friends are exempt from the
  absolute-path rule, and a bare `/` is a separator only after a delimiter flag or for
  `tr`. `Check` returns **every** violation, like the content linter.
- **Fixtures** are copied into `os.MkdirTemp` per run and removed by a deferred `Close`,
  so exercises may mutate them freely. The path is resolved through symlinks because the
  macOS profile matches real paths.
- **`runner.Sandbox`** has three implementations picked once by `DetectSandbox`. Two
  hard-won details: the seatbelt profile must allow reading `(literal "/")` or dyld
  aborts every process before `main` with no diagnostic, and `ulimit -u` is applied only
  under `bwrap`, since `RLIMIT_NPROC` counts per user id and would otherwise stop the
  sandbox forking at all.
- **Limits** are a `ulimit` prelude prepended to the script, which is why `ulimit` is
  itself refused. The learner's text is handed to `sh -c` verbatim rather than
  reassembled from the parse tree, so that what runs is exactly what was checked.

`runner.Compare` implements the four match modes; `Diff.String()` renders the two-column
block with the column caret.

### Testing style

The TUI is verified almost entirely through headless `View()` assertions. `internal/tui/app_test.go`
provides `newTestApp` (real library, colour off, fixed size), `press` (feeds a `tea.KeyPressMsg`),
and `view`. `TestFrameFillsTerminal` asserts every screen renders exactly the terminal's height and
never exceeds its width — keep new screens passing it.

## Security note

`internal/runner` executes learner-typed shell text. `gosec` is enabled in the linter for this
reason. Do not weaken any layer, and do not add a command to the allowlist by hand — it derives
from the dictionary.

`internal/runner/adversarial_test.go` holds the fixed corpus from SPEC.md §10. Every case is
tagged with how it is expected to be stopped: `refusedStatically` cases are asserted against the
**bare** backend, so they prove the static layers hold with no OS confinement at all;
`boundedByLimits` cases must start but never finish; `blockedByOS` cases are skipped where no
backend confines. Adding a case is cheap. Removing one needs a reason written down.
