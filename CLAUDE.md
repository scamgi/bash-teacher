# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

A Go TUI that teaches Unix commands and pipeline composition, through three surfaces
sharing one content library: a Dictionary, sandbox-executed Pipeline Exercises, and
Flashcards. **`SPEC.md` is the design source of truth** — read it before adding a
feature, and update it when a decision changes. It defines six milestones (M1–M6);
M1 (skeleton) and M2 (dictionary) are complete, and the runner, editor, scheduler, and
store are not built yet. Screens carry visible in-app notes saying which milestone fills them in.

## Commands

```sh
make build      # -> ./bt, with version stamped from git describe
make run        # build and launch the TUI
make check      # vet + golangci-lint + tests + content lint. Run before finishing.
make lint-go    # golangci-lint (2.x, config uses the v2 schema)
make lint       # content lint only: go run ./cmd/bt content lint
make test       # go test ./...
```

Single test: `go test ./internal/tui -run TestNumberKeysNavigate -v`

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
  fixture, not written by hand. M4's CI check re-runs them and asserts they still match.
- `bt content lint` enforces unique kebab-case ids, resolvable cross-references between
  commands, exercises and cards, an example plus caption on every command, hints on every
  exercise, and fixtures that exist, are flat, and stay under 256 KB. Every card must name at
  least one command so per-command mastery is computable.
- Commands in the `network` category are documented but **never executable**; `Library.Allowlist()`
  excludes them, and a test asserts it. The M3 sandbox depends on this.

### Testing style

The TUI is verified almost entirely through headless `View()` assertions. `internal/tui/app_test.go`
provides `newTestApp` (real library, colour off, fixed size), `press` (feeds a `tea.KeyPressMsg`),
and `view`. `TestFrameFillsTerminal` asserts every screen renders exactly the terminal's height and
never exceeds its width — keep new screens passing it.

## Security note

M3 introduces `internal/runner`, which executes learner-typed shell text. SPEC.md §6 specifies the
required layers: AST parse, a static allowlist derived from the dictionary, fixture materialization
into a temp dir, OS-level confinement (`bubblewrap` on Linux, `sandbox-exec` on macOS), rlimits and a
3 s timeout, then unconditional teardown. `gosec` is enabled in the linter for this reason. Do not
weaken any layer, and do not add a command to the allowlist by hand — it derives from the dictionary.
