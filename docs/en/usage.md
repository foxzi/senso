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
   is created relative to the indexed path, and a `.gitignore` with `*`
   is written inside `.senso` so the index is never committed by accident.

`search`, `status` and `rm` never create the database — if it is not
found, they fail with exit code 1.

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

Paths are always absolute. If there are no results, an empty array `[]`
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
vectors). The human-readable output shows the same information as
`mode: только лексический` (lexical only) or
`mode: лексический и семантический` (lexical and semantic) — that line
stays in Russian, matching the rest of the human-readable output.

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

### `senso version` and `senso help`

`senso version` prints the build version. `senso help` (as well as
`senso -h`, `senso --help`, and running with no arguments) prints the list
of commands.

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
