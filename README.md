# bash-teacher

A terminal UI that teaches Unix commands and, above all, how to compose them into
pipelines. Three learning surfaces — a **Dictionary**, **Pipeline Exercises**, and
**Flashcards** — share one content library.

See [SPEC.md](SPEC.md) for the full design.

## Status

**M1 (skeleton), M2 (dictionary) and M3 (runner) are done.**

- Bubble Tea v2 shell with screen routing, Catppuccin theming across all four
  flavours, a global key map, a help overlay, and a minimum-size fallback.
- A validated YAML content library, embedded in the binary, with `bt content lint`
  as the CI gate: **80 commands** across nine categories, plus exercises and cards.
- A two-pane dictionary: fuzzy-ranked search, a scrollable entry with flags,
  worked examples, and gotchas, and actionable cross-references — jump to a
  related command, copy an example, open an exercise, or drill the cards.
- An exercise browser, a deck walk over the flashcards, and a coverage view.
- A sandboxed runner: a shell parser that refuses what it cannot reason about, a
  static allowlist derived from the dictionary, throwaway fixture copies, OS-level
  confinement (`bubblewrap` on Linux, `sandbox-exec` on macOS, and an honest
  unconfined fallback that says so), resource limits, and output diffing in four
  match modes. `bt doctor` reports which backend you get; `--no-exec` turns
  execution off entirely.

Still to come: the pipeline editor and grading (M4), FSRS-lite scheduling and the
progress store (M5), stats and packaging (M6).

### Dictionary keys

| Key | Does |
| --- | --- |
| `/` | fuzzy search; typing narrows and re-ranks, `esc` clears |
| `→` | focus the entry; `↑`/`↓` then walk its examples, related commands, and exercises |
| `Enter` | copy the focused example, jump to the focused command, or open the focused exercise |
| `y` | copy the focused example to the clipboard |
| `p` | open an exercise that teaches this command |
| `f` | narrow the flashcard deck to this command |
| `backspace` | walk back out of a jump, or restore the whole deck |

## Build and run

```sh
make build     # -> ./bt
make run
make lint-go   # golangci-lint, configured in .golangci.yml
make check     # vet, golangci-lint, tests, content lint
```

`make lint-go` needs [golangci-lint](https://golangci-lint.run) 2.x; the config
uses the v2 schema.

## CLI

```
bt                  launch the TUI
bt practice         launch straight into the exercise browser
bt review           launch straight into the flashcards
bt dict [COMMAND]   print a dictionary entry, or list every entry
bt stats            print a library summary
bt doctor           report environment, data paths, content health
bt content lint     validate the content library
```

`--theme` picks a [Catppuccin](https://catppuccin.com) flavour: `latte`,
`frappe`, `macchiato`, or `mocha`. `light` and `dark` are aliases for Latte and
Mocha, `auto` (the default) detects the terminal background, and `none` disables
colour. Colour is also dropped automatically when stdout is not a terminal or
`NO_COLOR` is set. The theme does not paint a page background, so bash-teacher
sits inside whatever colour scheme your terminal already runs.

## Authoring content

Content lives under `content/` as YAML and is embedded at build time.

| Directory | Holds | Shape |
| --- | --- | --- |
| `commands/` | one dictionary entry per file | a mapping |
| `exercises/` | one exercise per file | a mapping |
| `cards/` | flashcards grouped by topic | a list |
| `fixtures/<name>/` | flat directories of plain files an exercise runs against | — |
| `expected/` | the expected stdout of each exercise | — |

`bt content lint` enforces the schema: unique kebab-case ids, resolvable
cross-references between cards, exercises and commands, an example and a caption on
every command, hints on every exercise, and fixtures that exist, are flat, and stay
under 256 KB. Every card must name at least one command so that per-command mastery
is computable and the dictionary can link to it.

Quote any YAML scalar containing a colon followed by a space — `cmd: "cut -d: -f1
/etc/passwd"` — or the parser reads it as a nested mapping.
