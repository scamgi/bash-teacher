# bash-teacher

A terminal UI that teaches Unix commands and, above all, how to compose them into
pipelines. Three learning surfaces — a **Dictionary**, **Pipeline Exercises**, and
**Flashcards** — share one content library.

See [SPEC.md](SPEC.md) for the full design.

## Status

**M1 (skeleton) is done.** The app runs, navigates, and loads its content:

- Bubble Tea v2 shell with screen routing, Catppuccin theming across all four
  flavours, a global key map, a help overlay, and a minimum-size fallback.
- A validated YAML content library, embedded in the binary, with `bt content lint`
  as the CI gate.
- Dictionary browsing with fuzzy search and a detail pane; an exercise browser; a
  deck walk over the flashcards; a coverage view.

Still to come: the sandboxed runner (M3), the pipeline editor and grading (M4),
FSRS-lite scheduling and the progress store (M5).

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
