# Yanki

**A single static binary that turns a folder of Markdown notes into Anki
flashcards.** Pure Markdown syntax, no Obsidian, no Node, no plugin.

Yanki reads Markdown files, infers the right Anki note type from the structure
of the document, and syncs the result to the Anki desktop application through
the language-agnostic [AnkiConnect](https://ankiweb.net/shared/info/2055492159)
protocol.

> **Status:** this is a redesign of the original `yanki-obsidian` plugin as a
> stand-alone Go CLI. The core Markdown → note conversion and sync are
> implemented and tested. See [Roadmap](#roadmap) for what is still to come.

## Why Go

- A single statically linked binary, no runtime to install.
- Trivial cross-compilation for Linux, macOS, and Windows.
- Straightforward, dependency-light HTTP and file system code.

## Prerequisites

- The [Anki desktop application](https://apps.ankiweb.net).
- The [AnkiConnect](https://ankiweb.net/shared/info/2055492159) add-on. Install
  it from Anki's _Tools → Add-ons → Get Add-ons…_ menu with the code
  `2055492159`, then restart Anki. AnkiConnect must be running while you sync.

## Install

```sh
go install github.com/ihaaae/yanki/cmd/yanki@latest
```

Or build from a checkout:

```sh
go build -o yanki ./cmd/yanki
```

## Quick start

1. Put your flashcard notes in a folder of Markdown files. A note like this:

   ```md
   This is the front of the card

   ---

   This is the back of the card
   ```

2. With Anki running, sync the folder:

   ```sh
   yanki sync ~/Notes/Flashcards
   ```

3. Yanki creates the decks and note types it needs in Anki and adds your cards.
   On later syncs it updates notes in place, preserving your review progress.

## Note types

Yanki supports the four note types that ship with Anki, and infers which one to
create from the structure of the Markdown.

### Basic

Any file containing a `---` thematic break becomes a front/back card.

```md
This is the front of the card

---

This is the back of the card
```

### Basic (and reversed card with extra)

Doubling the `---` makes the note reversible, producing two cards. An optional
third section becomes extra content shown on the back of both.

```md
Sometimes the answer is the question

---

---

Sometimes the question is the answer

---

This appears on the back of both generated cards
```

### Basic (type in the answer)

If the last statement in the file is `_emphasized like this_`, it becomes the
text the learner must type.

```md
Jazz isn't dead

_It just smells funny_
```

### Cloze

Text wrapped in `~~strikethrough~~` becomes a cloze deletion. Add a `---` for
back-of-card content, and end a cloze with `_emphasis_` to add a hint.

```md
All will be ~~revealed~~.

---

Additional revelations on the back of the card.
```

Clozes are numbered in order. Prefix a cloze with a number to control its index
explicitly, which lets several deletions reveal together:

```md
~~1 All~~ will be ~~1 revealed~~.
```

## Decks

A note's deck comes from its location. The folder hierarchy relative to the
longest common ancestor of the synced files becomes the `::`-delimited deck
hierarchy:

```text
~/Flashcards/Anki/Animals/Biped/Plato.md   → Anki::Animals::Biped
~/Flashcards/Anki/Animals/Quadruped/Horse.md → Anki::Animals::Quadruped
```

A `deckName` key in a note's frontmatter overrides the inferred deck.

### Priority decks

With a `[priority]` table in `yanki.toml`, Yanki files each note into one flat
deck per priority tier instead of a deck per folder, and records the folder as a
hierarchical tag:

```toml
[priority]
decks = ["A::P1", "A::P2", "A::P3"] # highest priority first
default = 2                         # tier for notes no rule covers
tag_prefix = "topic"

[priority.folders]                  # longest matching folder wins
"Math" = 1
"Eng" = 1
"Bayes" = 3
```

```text
Math/4-Stat/z-score.md → deck A::P1, tag topic::Math::4-Stat
```

A `priority: N` key in a note's frontmatter overrides the folder rules, and
`deckName` still overrides both. Spaces in folder names become `_` in tags.

To keep tags simpler than the directory tree, map folders to complete tags.
The longest matching folder rule applies to that folder and its descendants,
replacing the generated tag (including `tag_prefix`). Unmatched folders keep
their generated tags; frontmatter tags are still included.

```toml
[priority.folder_tags]
"Git" = "git"
"Math" = "Math"
"Math/0-Terms" = "Math::Terms"
"Math/2-Calculus" = "Math::Calculus"
```

## Math

`$inline$` and `$$display$$` math become Anki's MathJax `\(inline\)` and
`\[display\]`. Following Pandoc, an opening `$` must not be followed by a
space and a closing `$` must not be preceded by a space or followed by a digit,
so `$1 and $2` stays literal. Dollars in code are never math.

## Frontmatter

Yanki reads a small amount of YAML frontmatter and writes one key back:

```yaml
---
noteId: 1234567890 # managed by Yanki; do not edit by hand
tags:
  - parent/child # `/` becomes Anki's `::` tag hierarchy
deckName: Custom Deck # optional
priority: 1 # optional, see Priority decks
---
```

The `noteId` is how Yanki recognizes a note across syncs. Deleting a Markdown
file removes its note from Anki; deleting the `noteId` (or the frontmatter)
makes the next sync create a fresh note.

## Commands

| Command             | Description                                              |
| ------------------- | -------------------------------------------------------- |
| `yanki [directory]` | Sync Markdown notes to Anki. This is the default command. |
| `yanki sync`        | Same as above.                                            |
| `yanki config`      | Print the settings and note count a sync would use.       |
| `yanki list`        | List the notes Yanki manages in Anki.                     |
| `yanki clean`       | Delete every note Yanki manages for the namespace.        |

Useful flags:

| Flag                        | Description                                                        |
| --------------------------- | ------------------------------------------------------------------ |
| `--folder <path>`           | Sync only this folder (repeatable). Defaults to the whole directory. |
| `--namespace <name>`        | The namespace used to recognize Yanki's notes in Anki.             |
| `--dry-run`, `-d`           | Show what would change without modifying Anki.                     |
| `--anki-connect <url>`      | AnkiConnect URL. Defaults to `http://127.0.0.1:8765`.              |
| `--anki-key <key>`          | AnkiConnect API key, if your configuration requires one.           |
| `--anki-web`                | Push to AnkiWeb after syncing locally.                             |
| `--sync-media <mode>`       | Media sync mode: `off`, `local`, `remote`, or `all`. Not implemented yet. |
| `--strict-line-breaks`      | Treat single newlines in Markdown as line breaks.                  |
| `--ignore-folder-notes`     | Skip notes that share their parent folder's name.                  |
| `--json`                    | Print the result as JSON.                                          |
| `--verbose`, `-v`           | Print per-note details.                                            |

## Configuration

Yanki looks for a `yanki.toml` in the synced directory, or accepts one with
`--config`. Command-line flags override the file, and the file overrides the
built-in defaults.

```toml
[anki_connect]
host = "http://127.0.0.1"
port = 8765
key = ""

folders = ["Anki"]
ignore_folder_notes = true
namespace = "My Notes"
strict_line_breaks = true

[sync]
# Media sync is not implemented yet; this setting is reserved for it.
media_mode = "off"
push_to_anki_web = false
```

### Namespaces

A namespace is the label Yanki writes into a hidden `YankiNamespace` field on
every note it creates, and the only thing it will ever update or delete in Anki.
Notes that Yanki did not create are never touched.

If you do not set a namespace, Yanki derives one from the directory name
(`Yanki - <directory>`), so different folders do not collide. Set it explicitly
if you want a stable name across machines.

## How syncing works

- Each Markdown file is one Anki note. The folder structure supplies the deck.
- Yanki matches a local file to its Anki note through the `noteId` in the
  frontmatter. If a note has no ID, Yanki falls back to matching by content.
- When the content changes, the existing note is updated, so scheduling is kept.
- When a file disappears, its Anki note is deleted. Decks left empty by the
  deletion are pruned.
- Only notes in the active namespace are considered.

## Roadmap

Implemented and covered by tests:

- Markdown → note type inference (Basic, reversed, type-in, cloze)
- AnkiConnect client, note creation, updates, deletion, and deck pruning
- Deck inference from the folder hierarchy
- Frontmatter `noteId`, tags, and `deckName`
- TOML configuration and the `sync`, `config`, `list`, and `clean` commands

Not yet implemented (tracked for follow-up work):

- Media asset sync (images, audio, video, and remote URLs)
- Automatic note file renaming based on card content
- Model style management (`getStyle` / `setStyle` equivalents)
- Rich Markdown rendering: syntax highlighting, GitHub alerts, `==highlights==`,
  furigana, and Obsidian-style wiki links
- Filtered-deck handling when reading notes back from Anki

## Development

```sh
go build ./...     # build everything
go test ./...      # run the test suite
go vet ./...       # static checks
gofmt -l ./cmd ./internal  # formatting check
```

The test suite includes an in-memory fake AnkiConnect server, so the sync tests
run without Anki installed.

## License

MIT. See [LICENSE](./LICENSE).
