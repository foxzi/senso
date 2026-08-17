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
| `--hidden` | `false` | index hidden files and directories |
| `--include-hidden <list>` | `""` | comma-separated list of glob patterns to selectively include hidden paths and secret files |
| `--noisy` | `false` | index machine-generated files too: lock files, minified bundles, source maps, SVG |
| `--include-noisy <list>` | `""` | comma-separated list of glob patterns to selectively include noisy files |
| `--noisy-patterns <list>` | `""` | comma-separated list of glob patterns that replace the built-in list of noisy files |
| `--chunk-size <n>` | `1200` | chunk size in runes |
| `--overlap <n>` | `150` | chunk overlap in runes |
| `--query-prefix <s>` | `""` | prefix for search queries (only relevant with `--embed`) |
| `--doc-prefix <s>` | `""` | prefix for documents during indexing (only relevant with `--embed`) |
| `--max-file-size <mb>` | `10` | maximum file size in MB |
| `--concurrency <n>` | `4` | number of parallel embedding workers (only relevant with `--embed`) |
| `--prune` | `true` | remove files from the index that no longer exist on disk |
| `--ollama <url>` | `$OLLAMA_HOST` or `http://localhost:11434` | Ollama server address (only relevant with `--embed`) |
| `--quiet` | `false` | do not print progress and summary |
| `--strict` | `false` | exit with code 1 if at least one file could not be read or parsed (the report is still printed either way) |
| `--report-json` | `false` | print a machine-readable indexing report as a single line of JSON to stdout |

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
`node_modules`, `vendor` — this exclusion cannot be turned off with any
flag. In addition, by default it skips "noisy", machine-generated files
(`*.lock`, `*-lock.json`, `*.min.js`, `*.min.css`, `*.map`, `*.svg`) —
these can be enabled with the flags described below.

Hidden files and directories (name starting with a dot), as well as
secret files (`.env`, `.env.*`, `*.env`, `*.pem`, `*.key`, `*.p12`,
`*.pfx`, `*.jks`, `*.keystore`, `*.ppk`, `id_rsa`, `id_dsa`,
`id_ecdsa`, `id_ed25519`, `.netrc`, `.pgpass`, `.npmrc`, `.htpasswd`,
`.git-credentials`, `credentials`, `credentials.json`,
`secrets.json`, `secrets.yaml`, `secrets.yml`), are excluded from
the index by default. This is a behavior change: previously only
hidden directories were excluded by default, while hidden files
(for example `.env`, `.editorconfig`, `.gitignore`) were indexed.

The `--hidden` flag enables indexing of hidden files and
directories, but does not open `.git` or `.senso` and does not
include secret files. The `--include-hidden <list>` flag
selectively includes the given comma-separated glob patterns of
hidden paths and secret files (also without opening `.git` or
`.senso`) and works independently of `--hidden`. A pattern is
matched both against the path relative to the indexing root and
against the file name: for example, `.github/**` includes the
whole subtree, while `.env` includes files with that name at any
depth.

```sh
senso index --include-hidden '.github/**,.agents/**' .
senso index --hidden .
senso index --hidden --include-hidden '.env' .
```

Warning: `--hidden` can pull sensitive data from hidden directories
into the index (the secrets list only protects by file name), so
for project configuration directories the targeted
`--include-hidden` is preferable.

The `--noisy` flag enables indexing of all noisy files at once. The
`--include-noisy <list>` flag selectively includes the given
comma-separated glob patterns of noisy files and works independently
of `--noisy`; a pattern is matched both against the path relative to
the indexing root and against the file name, same as for
`--include-hidden`. The `--noisy-patterns <list>` flag does not add
to but entirely replaces the built-in list of noisy patterns; an
empty value is equivalent to the default list.

`--exclude` takes precedence over including noise: the command
`senso index --noisy --exclude '**/*.svg'` indexes lock files but
not SVG. Noise and hidden paths are orthogonal: `--noisy` does not
open hidden directories and does not include secrets, and `--hidden`
does not include noisy files inside hidden directories — indexing a
noisy file inside a hidden directory requires both permissions at
once (`--hidden`/`--include-hidden` and `--noisy`/`--include-noisy`).

```sh
senso index --noisy .
senso index --include-noisy 'poetry.lock,icons/**' .
senso index --noisy-patterns '*.pb.go' .
```

Besides plain text files, senso indexes `.docx`, `.odt`, `.ods`, `.odp`,
`.xlsx`, `.pptx`, `.rtf`, `.fb2`, `.ipynb` and `.epub` documents: plain text is extracted
from them by the standard library alone, so no external converters need to
be installed. Markup, images and metadata are discarded — only the text of
paragraphs, lists and tables reaches the index (for `.ipynb`, the code and
text of cells, without execution outputs; for `.epub`, the chapters in
reading order, without the table of contents; for `.pptx` and `.odp`, the
text of slides in order, without speaker notes), which means line numbers in
search results refer to the extracted text, not to the original file. In
`.ods` and `.xlsx` sheets, cells of one row are separated by tabs, so a
query matches neighbouring values of a row next to each other; the same
holds for tables placed on a slide. The
legacy `.doc` format (Word 97-2003) is not supported; convert such files
to `.docx` or `.rtf`.

#### Indexing report: `--strict`, `--report-json`

An error on one file (unreadable, fails to parse as a document, walk
error) does not stop indexing of the rest, but it is not silently
dropped either — it goes into the report. Progress and the
human-readable summary always go to stderr, so `--report-json` can be
safely combined with `--quiet` and its stdout parsed by a script.

The human-readable summary (stderr) splits files into new, updated,
unchanged and deleted, and also prints a `skipped: N (code: N, ...)`
line with skip reasons and a `failed to process: N` line with a list of
paths (at most 10 lines, the rest shown as `... and N more`).

`--report-json` prints a single line of JSON to stdout with these
fields:

| Field | Type | Meaning |
|---|---|---|
| `scanned` | number | total files considered |
| `indexed` | number | new files added to the index |
| `updated` | number | changed files re-indexed |
| `unchanged` | number | files whose content did not change (including those where only mtime and size were updated) |
| `deleted` | number | files removed from the index (`--prune`) |
| `chunks` | number | total chunks written |
| `skipped` | number | total files skipped (see codes below) |
| `skipped_by_code` | object | skip reason -> number of files; absent if nothing was skipped |
| `excluded` | number | total paths dropped by the selection rules during the tree walk |
| `excluded_by_reason` | object | exclusion reason -> number of paths; absent if nothing was excluded |
| `failed` | array | objects `{path, code, message}`; always present, empty as `[]` |
| `interrupted` | bool | indexing was interrupted by a SIGINT/SIGTERM signal |
| `duration_ms` | number | indexing time in milliseconds |
| `database` | string | path to the database file |
| `vectors` | bool | the index contains vector embeddings (`--embed`) |

Skip reason codes (`skipped_by_code`): `empty` (empty file, or no text
could be extracted from the document), `too_large` (bigger than
`--max-file-size`), `binary` (content not recognized as text),
`vanished` (the file disappeared between scanning and processing),
`no_schema` (rare: the first file under `--embed` produced no chunk at
all).

Exclusion reason codes (`excluded_by_reason`): `hard_excluded` (the
`.git` or `.senso` service directory), `vendor` (`node_modules`,
`vendor`), `hidden` (a hidden path without `--hidden`), `secret` (the
file looks like a credential store), `gitignore` (a `.gitignore` rule),
`exclude_glob` (an `--exclude` pattern), `noisy` (a machine-generated
file), `ext` (the extension is not in `--ext`), `symlink` (a symbolic
link), `empty` (zero size), `too_large` (bigger than
`--max-file-size`). An excluded directory counts as a single entry: its
contents are never walked, so the files inside it are not counted. These
counters answer the "why is my file not in the index" question: when
`indexed` is zero, the breakdown immediately shows which selection rule
fired.

Error codes (`failed[].code`): `walk_failed` (tree walk error),
`read_failed` (file read error), `extract_failed` (document parsing
error, for example a corrupt `.docx`).

```sh
$ senso index --report-json --quiet .
{"scanned":3,"indexed":1,"updated":0,"unchanged":0,"deleted":0,"chunks":1,"skipped":1,"skipped_by_code":{"binary":1},"excluded":12,"excluded_by_reason":{"gitignore":10,"hidden":1,"secret":1},"failed":[{"path":"/tmp/w/broken.docx","code":"extract_failed","message":"zip: not a valid zip file"}],"interrupted":false,"duration_ms":2,"database":"/tmp/w/.senso/index.db","vectors":false}
```

Exit codes for `index`: `0` — success; `1` — an indexing error, or (with
`--strict`) at least one entry in `failed`; `2` — argument parsing
error; `130` — indexing interrupted by a SIGINT/SIGTERM signal. On
interruption, `--prune` does not run and the last indexing time is not
updated (the index is knowingly incomplete), while files processed
before the signal remain fully in the index: there are no half-processed
entries.

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
| `--path <glob,...>` | `""` | keep only results whose path matches at least one of the comma-separated glob patterns |
| `--ext <list>` | `""` | keep only results with one of the comma-separated extensions (with or without leading dot) |
| `--exclude <glob,...>` | `""` | drop results whose path matches at least one of the comma-separated glob patterns (takes priority over `--path`) |
| `--root <dir>` | `""` | keep only results inside the given indexed root (see `senso status`) |
| `--deduplicate` | `false` | suppress neighboring results from the same file with overlapping line ranges |
| `--max-per-file <n>` | `0` | keep at most `n` results per file, `0` means no limit |
| `--semantic` | `false` | search by vectors instead of lexical search (requires an index built with `senso index --embed` and a reachable Ollama) |
| `--hybrid` | `false` | combine lexical and semantic results (same requirements as `--semantic`; cannot be used together with it) |
| `--ollama <url>` | `$OLLAMA_HOST` or `http://localhost:11434` | Ollama server address (relevant with `--semantic` and `--hybrid`) |
| `--query-prefix <s>` | `""` | prefix for the search query (relevant with `--semantic` and `--hybrid`) |

Examples:

```sh
senso search "search and files"
senso search --k 5 "specific term"
senso search --json "text" | jq '.[].path'
senso search --paths-only "text" | xargs -I{} wc -l {}
senso search --semantic "a query about the meaning, not the words"
senso search --hybrid "an exact term and its meaning at once"
senso search --json --path 'internal/**' --ext go "replace file transaction"
```

#### Result filters: `--path`, `--ext`, `--exclude`, `--root`

These four flags filter results that have already been found, and behave
the same way in lexical, semantic (`--semantic`) and hybrid (`--hybrid`)
search modes.

`--path` and `--exclude` take a comma-separated list of glob patterns
(`internal/**`, `*.go`, `docs/**/*.md`); a pattern is matched both against
the result path relative to each indexed root and against the absolute
path. `--exclude` takes priority over `--path`: a result matching an
exclusion pattern is dropped even if it also matches `--path`. `--root
<dir>` restricts results to one specific indexed root — if the given path
is not among the known roots (`senso status`), the command fails with a
usage error (exit code 2).

Because filtering reduces the number of results, senso requests an
expanded pool of candidates from the database whenever a filter is active
— this ensures that, as long as enough matching chunks exist, `-k`
results remain after filtering.

#### `--deduplicate` and `--max-per-file`

Unlike `--path`/`--ext`/`--exclude`/`--root`, these flags do not filter by
path content — they remove redundancy within the results already found
for a single file:

- `--deduplicate` suppresses neighboring results from the same file whose
  line ranges overlap. This is common for long documents, where adjacent
  chunks overlap because of the `--overlap` value used at `index` time and
  end up in the results with nearly the same text. Each group of
  overlapping chunks is reduced to the one with the highest score.
- `--max-per-file <n>` limits the results to `n` per file (after
  deduplication, if enabled) — useful when one large file fills the
  whole results page, crowding out other matching files.

Both flags are applied as post-processing on the already ranked results,
before the `-k` cutoff, and work in lexical, semantic and hybrid modes.
Like the regular filters, they increase the candidate pool requested from
the database so that, after dropping duplicates and excess per-file
chunks, `-k` results remain.

#### Output formats

Human-readable (default): for each result a line
`path#chunk_number  first_line-last_line  score`, followed by an indented
snippet of text up to `--snippet` runes long. Line numbers refer to the
source file, so you can jump straight to the place, for example
`vim +40 path`. Databases built by older versions of senso have no line
information and the range is omitted — re-index to get it.

The snippet keeps its line breaks: the indent is applied to every line, so
code indentation and Markdown structure stay readable. Blank lines at the
edges of the snippet and trailing whitespace are stripped.

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
    "line_start": 40,
    "line_end": 58,
    "score": 0.478,
    "text": "matched fragment text"
  }
]
```

Paths are always absolute. `line_start` and `line_end` are the first and
last line of the chunk in the source file (`0` when the database was built
by an older version of senso). The `text` field is built the same way as the
snippet in the human-readable output: the same `--snippet` length and the
same centering on the match. If there are no results, an empty array `[]`
is printed (valid JSON, not an empty string).

`--paths-only`: a list of unique absolute file paths, one per line, with
no duplicates and no scores.

**Score:** higher is more relevant in both modes. In lexical mode this is
inverted bm25 (`score = -bm25`, since SQLite returns bm25 as a negative
number that gets smaller — more negative — for better matches). In
semantic mode this is cosine similarity (`score = 1 - cosine_distance`).
In hybrid mode the score is a Reciprocal Rank Fusion weight: each list
contributes `1 / (60 + position)` to a document, and the contributions are
summed, so a document present in both lists outranks one that leads only a
single list. Scores from different modes are not comparable to each other.

### `senso show [flags] <path>#<chunk>`

Prints the full text of a chunk saved in the index, by reference from
`search` output. `search` prints each result header as
`path#chunk_number  first_line-last_line  score` — the reference that
`show` accepts is the part up to the second run of spaces:
`path#chunk_number`.

The path in the reference can be relative — `show` resolves it to an
absolute path the same way paths are stored in the index.

Flags:

| Flag | Default | Purpose |
|---|---|---|
| `--db <file>` | `""` | path to the database file |
| `--before <n>` | `0` | number of preceding chunks of the same file to include |
| `--after <n>` | `0` | number of following chunks of the same file to include |
| `--json` | `false` | print the chunk as JSON |

Examples:

```sh
senso show --json '/docs/specification.docx#4'
senso show --json --before 1 --after 2 '/docs/specification.docx#4'
senso show ./README.md#2
```

Neighboring chunks added via `--before`/`--after` are not merged into one
text: each is returned as a separate item, exactly as it is stored in the
index. Neighboring chunks in the index overlap (see `internal/chunk`), so
adjacent items show a shared piece of text at the seam — that is not an
output bug, it is an honest reflection of what is actually stored in the
database. Unlike the snippet printed by `search --snippet`, the text
printed by `show` is never truncated.

If the given file has no chunk with the requested number, the command
exits with code 1, and the error message states the available range of
chunk numbers for that file.

If the file on disk has changed since the last indexing, or is gone
entirely, `show` does not block output and does not trigger a re-index:
it prints the text saved in the index, prints a warning to stderr, and
exits with code 0. In `--json` the same fact is carried by the `stale`
field (`true`/`false`) and, when the file is stale, by a `stale_reason`
field set to `"modified"` (mtime or size changed) or `"missing"` (the
file is no longer on disk); when `stale` is `false`, `stale_reason` is
absent from the object.

This is especially useful for formats indexed through `internal/extract`
(`.docx`, `.odt`, `.ods`, `.odp`, `.xlsx`, `.pptx`, `.rtf`, `.fb2`,
`.ipynb`, `.epub`): the source file for these formats is binary, and
`show` is the only way for an agent to see the text already extracted
from it without re-parsing the format itself.

`--json`: an object shaped like:

```json
{
  "ref": "/docs/specification.docx#4",
  "path": "/docs/specification.docx",
  "chunk": 4,
  "stale": false,
  "chunks": [
    {
      "ref": "/docs/specification.docx#4",
      "chunk": 4,
      "line_start": 120,
      "line_end": 168,
      "text": "full chunk text"
    }
  ]
}
```

Top-level fields: `ref` and `path` are the canonical (absolute) path and
the requested chunk number, rebuilt from scratch, so `ref` can be passed
back into `show` unchanged even if the original reference used a relative
path; `chunk` is the requested number; `stale`/`stale_reason` describe the
file's freshness (see above); `chunks` is the list of all returned
chunks, including the neighbors from `--before`/`--after`, each with its
own `ref`, `chunk`, `line_start`, `line_end` and the full `text`.

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

### `senso check [flags] [path]`

Answers a single question: does the tree need reindexing? The command is
read-only — the database is opened in read-only mode, so a check
physically cannot modify the index.

`check` applies exactly the same file selection rules as `index`
(`--ext`, `--exclude`, `--hidden`, `--noisy`, `.gitignore`,
`--max-file-size` and the rest), compares the resulting list with the
contents of the index and splits the differences into categories:

- `changed` — the file is in the index but changed on disk;
- `missing` — the file is in the index but is gone from disk;
- `unindexed` — the file passes the filters but is not in the index yet;
- `excluded` — the file is both in the index and on disk, but the current
  selection rules no longer accept it (for example `--ext` changed);
- `issues` — indexing parameter mismatches that a plain `senso index`
  will not fix.

If the selection flags differ from the ones used for indexing, some files
will honestly land in `unindexed` or `excluded` — that is not an error but
the answer to "what would happen if I indexed with these flags".

Flags:

| Flag | Default | Purpose |
|---|---|---|
| `--db <file>` | `""` | path to the database file |
| `--hash` | `false` | compare contents by hash instead of mtime and size |
| `--json` | `false` | print the result as JSON |
| `--quiet` | `false` | do not print the human-readable summary |

The remaining file selection flags are the same as for `index` (see the
`senso index` flag table above).

By default a file counts as changed when its modification time or size
differs: that is fast and reads no contents. With `--hash`, a metadata
difference triggers a content comparison, so a file rewritten with the
same text (`touch`, `git checkout`, reinstalled dependencies) is not
reported as changed. The decision is made by the same function that
indexing uses, so `check` cannot disagree with `index` about a file.

Exit codes: `0` — the index is up to date; `3` — the index is out of date;
`1` — the check itself failed (for example a corrupted database); `2` —
argument parsing error. The dedicated code `3` exists so that an agent can
tell "time to reindex" from "the check broke".

```sh
$ senso check
index is out of date: 1 changed, 0 missing, 2 unindexed, 0 newly excluded
last indexed: 2026-08-17T21:21:44+03:00
database: .senso/index.db
```

`--json` prints a single JSON line with these fields:

| Field | Type | Meaning |
|---|---|---|
| `fresh` | bool | the index fully matches disk and parameters |
| `mode` | string | how contents were compared: `mtime` or `hash` |
| `scanned` | number | files on disk that passed the selection rules |
| `unchanged` | number | files matching the index |
| `changed` | number | indexed files that changed on disk |
| `missing` | number | files in the index that are gone from disk |
| `unindexed` | number | files on disk that are not indexed yet |
| `excluded` | number | indexed files that no longer pass the filters |
| `excluded_by_reason` | object | exclusion reason -> number of files; the same codes as in `index --report-json` |
| `issues` | array | objects `{code, message}`; always present, empty as `[]` |
| `failed` | array | objects `{path, code, message}` for files whose state could not be checked |
| `indexed_at` | string | time of the last indexing run |
| `model` | string | embedding model of the index (empty for lexical) |
| `vectors` | bool | the index contains vectors |
| `database` | string | path to the database file (empty when no database was found) |

Codes in `issues`: `no_index` (no index database found — every discovered
file counts as `unindexed`), `model_mismatch` (the index was built with a
different embedding model, checked only with `--embed`), `vectors_missing`
(`--embed` was requested but the index has no vectors).

`excluded_by_reason` answers right away why a file left the selection:
`gitignore: 1`, for example, means an indexed file was caught by a new
`.gitignore` rule rather than deleted.

Files in `failed` (possible only with `--hash`; the codes match `index`:
`read_failed`, `walk_failed`) do not make the index stale on their own:
nothing is simply known about such a file.

```sh
$ senso check --json --quiet
{"fresh":false,"mode":"mtime","scanned":3,"unchanged":2,"changed":1,"missing":0,"unindexed":0,"excluded":0,"issues":[],"failed":[],"indexed_at":"2026-08-17T21:21:44+03:00","model":"","vectors":false,"database":"/tmp/w/.senso/index.db"}
```

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

# Reindex only when the index is out of date (exit code 3 means stale)
senso check --quiet || senso index .

# Inspect what exactly diverged, for the agent to decide
senso check --json --quiet | jq '{changed, missing, unindexed, excluded}'
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

**Searching by path and identifiers.** Besides the chunk text, the index
also stores the file path and any compound identifiers found in the text
(camelCase, snake_case, kebab-case). This lets you:

- find a file by a word from its path: `senso search "migrations"` finds
  chunks of files whose path contains `migrations`, even if that word is
  not in the text itself;
- find a compound identifier regardless of its spelling: `ReplaceFile`,
  `replace_file` and `replace file` are all found by the same query
  `senso search "replace file"`, because every one of these spellings is
  split into the same word stems;
- combine "where" and "what" in one query: `senso search "store ReplaceFile"`
  finds the chunk where both a path containing `store` and the
  `ReplaceFile` identifier are present.

Path and identifiers carry less ranking weight than the chunk text itself
(see `bm25Weights` in the architecture doc), so adding such terms to a
query does not pull results away from actual text matches.

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
senso search --hybrid "query"         # merge both modes with RRF
```

If an index already exists and was built without `--embed` (lexical
only), running `senso index --embed` again backfills the vectors: for
databases where the vector table does not exist yet, senso forces a
re-index of every file through the embedding model (even if mtime/size
have not changed), so that every chunk ends up with a vector.

If Ollama is unreachable at the time of `index --embed`,
`search --semantic` or `search --hybrid`, the command fails (exit code 1) — senso does not
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
  the current schema (the schema version is now 4). Any command
  (`index`, `search`, `status`, `rm`) refuses to work with such a
  database, exiting with code 1 and a message like "database was created
  by an incompatible senso version (schema 3, need 4), remove the .senso
  directory and re-index" — fixed by removing the `.senso` directory and
  re-indexing; there is no on-the-fly schema migration.
