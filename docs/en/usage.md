# senso: user guide

## Installation and build

You need Go (see `go.mod` for the exact version) and CGO enabled — the
SQLite driver (`mattn/go-sqlite3`) and the vendored `sqlite-vec` extension
(`internal/vecext`) are written in C and built through cgo.

Every `go build`/`go test`/`go vet` invocation **must** use the build tag
`sqlite_fts5`. Without it, the FTS5 full-text engine is not compiled into
`mattn/go-sqlite3`, and senso fails with `no such module: fts5` as soon as
it touches the index.

Use the `Makefile` so the tag is never forgotten:

```sh
make build   # go build -tags sqlite_fts5 -> ./bin/senso
make test    # go test -tags sqlite_fts5 ./...
make vet     # go vet -tags sqlite_fts5 ./...
make install # install the binary
```

Manual build:

```sh
go build -tags sqlite_fts5 -o bin/senso .
```

## Where the database lives

The database file path is resolved with this priority (see
`internal/dbpath`):

1. explicit `--db <file>` flag, if passed to any command;
2. the `SENSO_DB` environment variable, if `--db` is not set;
3. a `.senso` directory found by walking up from the current directory
   (the same way git finds `.git`) — the file
   `<found directory>/.senso/index.db` is used;
4. if `.senso` is not found anywhere (only relevant for `index`, which is
   the only command allowed to create the database), `./.senso/index.db`
   is created in the **current working directory**, not next to the
   indexed path, and a `.gitignore` with `*` is written inside `.senso`
   so the index is never committed by accident.

The distinction in point 4 matters: running `senso index ./project` from
your home directory puts the database at `~/.senso/index.db`, not
`./project/.senso`. This keeps the database visible to later `search` and
`status` calls, which only walk up the tree looking for `.senso` and never
look into subdirectories. To avoid any doubt about where the database
ended up, `index` prints a `database: <path>` line to stderr
(`база: <путь>` in the Russian locale).

`search`, `status` and `rm` never create the database — if it is not
found, they fail with exit code 1. This also applies to an explicit
`--db /path/with/a/typo.db`: senso checks that the file exists before
opening it. Without that check, the SQLite driver would silently create an
empty database, and instead of a clear "index not found" error you would
get a search that simply finds nothing.

## Output language

The language of human-readable output is determined from environment
variables in this order of priority: `SENSO_LANG`, `LC_ALL`,
`LC_MESSAGES`, `LANG`. The first non-empty variable decides the outcome:
if its value starts with `ru` (case-insensitive), output is Russian,
otherwise English; the values `C` and `POSIX` are treated as English. If
none of the variables is set, output is English (the default language).

`SENSO_LANG` is an explicit override and takes precedence over the system
locale in both directions: `SENSO_LANG=en` gives English output on a
Russian machine, `SENSO_LANG=ru` gives Russian output on an English one.

Localized: the general help (`senso help`), flag descriptions for every
subcommand, error messages, and the labels in the human-readable output
of `status` and `index`.

The JSON format (`--json` for `search` and `status`) does **not** depend
on the locale: keys and machine-readable values are identical in any
locale — for example, the `mode` field always stays `lexical` or
`lexical+semantic` and is never translated. This is intentional, so
scripts do not break when the locale changes.

Examples:

```sh
SENSO_LANG=en senso status   # English output regardless of the OS locale
SENSO_LANG=ru senso status   # Russian output regardless of the OS locale
```

## Commands

General form: `senso <command> [flags] [arguments]`. Running `senso` with
no arguments and `senso help` print short usage to stdout and exit with
code 0. An unknown command prints an error to stderr and exits with code 2.

### `senso index [flags] [path]`

Builds or updates the index for the directory tree at `path` (default `.`).

Flags:

| Flag | Default | Purpose |
|---|---|---|
| `--db <file>` | `""` | path to the database file |
| `--embed` | `false` | build vector embeddings via Ollama; without it indexing is fully local and lexical, Ollama is never contacted |
| `--model <name>` | `bge-m3` | Ollama embedding model (only relevant with `--embed`) |
| `--ext <list>` | `""` | comma-separated list of file extensions (empty = any) |
| `--exclude <list>` | `""` | comma-separated list of glob exclusion patterns |
| `--no-gitignore` | `false` | do not honor `.gitignore` |
| `--chunk-size <n>` | `1200` | chunk size in runes |
| `--overlap <n>` | `150` | chunk overlap in runes |
| `--query-prefix <s>` | `""` | prefix for search queries (only relevant with `--embed`) |
| `--doc-prefix <s>` | `""` | prefix for documents during indexing (only relevant with `--embed`) |
| `--max-file-size <mb>` | `10` | maximum file size in MB |
| `--concurrency <n>` | `4` | number of parallel embedding workers (only relevant with `--embed`) |
| `--prune` | `true` | remove files from the index that no longer exist on disk |
| `--ollama <url>` | `$OLLAMA_HOST` or `http://localhost:11434` | Ollama server address (only relevant with `--embed`) |
| `--quiet` | `false` | suppress progress output |

Constraints: `--chunk-size` > 0, `--overlap` >= 0 and less than
`--chunk-size`, `--concurrency` > 0, `--max-file-size` > 0 — otherwise the
command fails with an argument parsing error (exit code 2).

Example:

```sh
senso index .                       # purely lexical indexing of the current directory
senso index --ext go,md ./project   # only .go and .md files
senso index --embed .               # + vector embeddings via Ollama
```

Indexing always skips the `.senso` service directory, `.git`,
`node_modules`, `vendor` and any hidden directory, as well as "noisy"
files (`*.lock`, `*-lock.json`, `*.min.js`, `*.min.css`, `*.map`, `*.svg`)
— these exclusions cannot be turned off with flags.

### `senso search [flags] query...`

Searches the index and prints the most relevant chunks.

**Important:** flags must be given before the query text. Argument parsing
stops at the first word that does not start with `-`, so
`senso search "text" --json` will not behave as expected — `--json` ends
up as part of the query text instead of being recognized as a flag. Use
`senso search --json "text"` instead.

Flags:

| Flag | Default | Purpose |
|---|---|---|
| `--db <file>` | `""` | path to the database file |
| `--k <n>` | `10` | number of chunks to return |
| `--json` | `false` | print results as JSON |
| `--paths-only` | `false` | print only unique file paths |
| `--snippet <n>` | `500` | length of the text snippet in results, in runes |
| `--semantic` | `false` | search by vectors instead of lexical search (requires an index built with `senso index --embed` and a reachable Ollama) |
| `--ollama <url>` | `$OLLAMA_HOST` or `http://localhost:11434` | Ollama server address (only relevant with `--semantic`) |
| `--query-prefix <s>` | `""` | prefix for the search query (only relevant with `--semantic`) |

Examples:

```sh
senso search "search and files"
senso search --k 5 "specific term"
senso search --json "text" | jq '.[].path'
senso search --paths-only "text" | xargs -I{} wc -l {}
senso search --semantic "a query about the meaning, not the words"
```

#### Output formats

Human-readable (default): for each result a line
`path#chunk_number  score`, followed by an indented snippet of text up to
`--snippet` runes long.

The snippet is not simply cut from the start of the chunk: it is centered
on the first word that matches the query. The match is found by word
stem (the same stemming used by search itself), so a query for `payment`
highlights a window around "paying", and a prefix term like `pay*` matches
around any word with that prefix. The match is placed roughly a third of
the window's width from the left edge, so both the preceding context and
the continuation stay visible. Truncated edges of the snippet are marked
with an ellipsis.

When no match can be found — for example the matching word ended up in
a neighboring chunk, or the result came from a semantic search where an
exact occurrence may not exist at all — the start of the chunk is
printed, as before.

`--json`: an array of objects shaped like:

```json
[
  {
    "path": "/abs/path/to/file.txt",
    "chunk": 0,
    "score": 0.478,
    "text": "matched fragment text"
  }
]
```

Paths are always absolute. The `text` field is built the same way as the
snippet in the human-readable output: the same `--snippet` length and the
same centering on the match. If there are no results, an empty array `[]`
is printed (valid JSON, not an empty string).

`--paths-only`: a list of unique absolute file paths, one per line, with
no duplicates and no scores.

**Score:** higher is more relevant in both modes. In lexical mode this is
inverted bm25 (`score = -bm25`, since SQLite returns bm25 as a negative
number that gets smaller — more negative — for better matches). In
semantic mode this is cosine similarity (`score = 1 - cosine_distance`).
Scores from the two modes are not comparable to each other.

### `senso status [flags]`

Prints statistics about the current index: database path, root, mode,
number of files and chunks, database size, last indexing time, and a
breakdown by root path.

A single database can contain several independent trees: each `index` run
adds its path to the list of roots (`meta.roots`), and a root nested
inside an already known one does not become a separate entry — it is
absorbed by the ancestor. The `roots` breakdown shows the number of files
per root; a file is attributed to the longest matching root. Paths that do
not fall under any known root (usually leftovers from older databases) are
collected under `(other)` (`(прочее)` in the Russian locale).

Flags:

| Flag | Default | Purpose |
|---|---|---|
| `--db <file>` | `""` | path to the database file |
| `--json` | `false` | print statistics as JSON |

`--json` prints an object:

```json
{
  "db": "/abs/path/.senso/index.db",
  "root": "/abs/path",
  "mode": "lexical",
  "model": "",
  "dim": 0,
  "files": 3,
  "chunks": 3,
  "fts_rows": 3,
  "vectors": 0,
  "size_bytes": 53248,
  "indexed_at": "2026-08-09T12:20:53+03:00",
  "roots": {"/abs/path": 2, "/abs/path/sub": 1}
}
```

The `mode` field takes one of two machine-readable values: `lexical`
(lexical index only) or `lexical+semantic` (the database also has
vectors) — this JSON value is never translated. The human-readable output
shows the same information as a localized `mode: lexical only` /
`mode: lexical and semantic` line (English by default, Russian if the
output language is Russian — see "Output language" above).

### `senso rm <path>`

Removes a file or an entire subtree from the index by the given path.
Files on disk are not touched — only the index is cleaned up.

Flags:

| Flag | Default | Purpose |
|---|---|---|
| `--db <file>` | `""` | path to the database file |

Exactly one positional argument — a path (file or directory). Example:

```sh
senso rm ./old-directory
senso rm ./notes.txt
```

Along with the chunks, the command also cleans up the list of indexed
roots (`meta.roots`, see `senso status`): if the removed path covered
an entire registered root, that root is dropped from the list —
otherwise it would linger in `status` with zero files. Roots nested
inside the removed path are dropped as well; an ancestor root is kept,
since it may still contain files that were not touched by the removal.

### `senso version` and `senso help`

`senso version` (as well as `senso --version`) prints the build version
on a single line:

```
senso v0.1.1 (a1b2c3d, 2026-08-09T12:20:53Z)
```

The version, commit and date come from ldflags that the `Makefile` sets
from `git describe`. If the binary was built without them, the values
are recovered from Go's embedded build info (`debug.ReadBuildInfo`):
`go install module@version` yields a tag, while a plain working-tree
build yields `vcs.revision` and `vcs.time`. The commit and date are
printed in parentheses only when known; when nothing is known at all,
the output is `senso dev`.

`senso help` (as well as `senso -h`, `senso --help`, and running with no
arguments) prints the list of commands. Help for a specific command —
`senso <command> --help` — is assembled from the same flag descriptions
used by argument parsing, so it can never drift from actual behavior.

## Using senso in pipelines and with agents

```sh
# List paths of found files via jq
senso search --json "authorization error" | jq -r '.[].path'

# Check whether there are any results at all
[ "$(senso search --json "term" | jq 'length')" -gt 0 ] && echo found

# Run grep over the found files
senso search --paths-only "TODO" | xargs grep -n "TODO"

# Index stats as JSON for monitoring
senso status --json | jq '.files, .chunks'
```

`--json` and `--paths-only` do not mix results with progress logs:
`index` progress goes to stderr, while `search`/`status --json` results go
to stdout, which makes it easy to redirect them separately.

## Multilingual support

FTS5 is configured with the `unicode61 remove_diacritics 2` tokenizer and
prefix support (`prefix='2 3'`). Out of the box this gives:

- Russian and English text working in the same database with no
  configuration;
- case-insensitive search (`SEARCH`, `Search` and `search` all match the
  same content);
- prefix queries: `search*` matches "search", "searching", etc.;
- folding of the Russian letters ё/е (a query for `елка` finds `Ёлка`) —
  this is done by hand in code before writing to the index and before the
  query, the tokenizer itself does not fold these letters.

**Stemming.** The index and the search query are stemmed with the Snowball
algorithm (package `internal/stem`, dependency
`github.com/kljensen/snowball`, pure Go, no cgo). The language is chosen
per token by the presence of Cyrillic characters, so mixed Russian/English
text is handled without detecting the language of the whole document. For
example, a query for `file` finds text containing `files` or `filing`, and
a query for `search` finds `searching`.

**Query syntax:**

- plain words separated by spaces are matched with an implicit AND (all
  words are required);
- a phrase in double quotes matches words strictly adjacent to each other,
  but in any word form the stemmer allows. In the shell, the query's
  double quotes must be wrapped in single quotes, otherwise the shell eats
  them: `senso search '"search files"'` — such a query finds text like
  "Searching files is handled by the indexer", but does not find "Local
  search over project files" if another word sits between "search" and
  "files" — that is a real adjacency check, not just a word-overlap check;
- a prefix with a trailing asterisk (`sear*`) is not stemmed, since the
  asterisk already marks a deliberately incomplete word;
- punctuation in the query is safe and simply discarded, e.g.
  `search (files)` behaves the same as `search files`.

**Honest limitations:**

- Snowball only handles inflection (declension, conjugation), not
  derivation: `index` and `indexer` get different stems and do not match
  each other — that is a property of the algorithm, not a bug.
- there is no literal "form to form" search like `grep` — both plain words
  and phrases are matched against stems; there is no flag for exact word
  form matching today.

## Optional semantic mode

By default senso never talks to any external service. Semantic (vector)
search is a fully optional layer on top of the lexical index, useful when
you need to find matches by meaning rather than exact words.

Requirements:

- a running local [Ollama](https://ollama.com) server;
- an embedding model loaded in Ollama (default: `bge-m3`).

How to enable:

```sh
senso index --embed .                 # build the index with vectors
senso search --semantic "query"       # search by vectors
```

If an index already exists and was built without `--embed` (lexical
only), running `senso index --embed` again backfills the vectors: for
databases where the vector table does not exist yet, senso forces a
re-index of every file through the embedding model (even if mtime/size
have not changed), so that every chunk ends up with a vector.

If Ollama is unreachable at the time of `index --embed` or
`search --semantic`, the command fails (exit code 1) — senso does not
silently fall back to lexical-only mode, to avoid creating a partially
indexed database or unexpectedly returning a different kind of result.

## Limitations

- Snowball only covers inflection, not derivation: `index` and `indexer`
  do not match each other since they get different stems.
- No literal "form to form" search (like `grep`) — all search, including
  phrases, is matched against stems; there is no flag for exact word form
  matching.
- No syntax-aware chunking of code: files of any type are split into
  chunks by rune count (`--chunk-size`/`--overlap`), with no understanding
  of code structure (functions, classes, etc.).
- Databases created by previous versions of senso are incompatible with
  the current schema (the schema version is now 2). Any command
  (`index`, `search`, `status`, `rm`) refuses to work with such a
  database, exiting with code 1 and a message like "database was created
  by an incompatible senso version (schema 1, need 2), remove the .senso
  directory and re-index" — fixed by removing the `.senso` directory and
  re-indexing; there is no on-the-fly schema migration.
