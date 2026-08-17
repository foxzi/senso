# senso

`senso` is a command-line tool for indexing local text files and searching
across them. It works fully offline, with no AI services required for its
core functionality. Full-text search runs entirely on SQLite FTS5. An
optional semantic search mode is available through a local Ollama server,
if you want vector search on top of the lexical index.

senso is designed to be used both interactively and as a tool for coding
agents: it has a stable JSON output mode, absolute paths in results, and
predictable exit codes.

## Key features

- Full-text search on SQLite FTS5 (bm25 ranking), also indexing file paths
  and compound identifiers (`ReplaceFile`, `replace_file`, `replace-file`
  are all found by the same query).
- Works with Russian and English out of the box: case-insensitive matching,
  Unicode-aware tokenization, prefix queries (`поиск*`), and Snowball
  stemming per token (`файл` matches `файлам`/`файлов`, `search` matches
  `searching`).
- Phrase search in double quotes (`senso search '"поиск файлов"'`) — words
  must be adjacent, but any word form is matched.
- Result filters for `search`: `--path`, `--ext`, `--exclude`, `--root`,
  usable in lexical, semantic and hybrid modes alike.
- `--deduplicate` and `--max-per-file` for `search` trim overlapping and
  overrepresented chunks from the same file out of the results.
- `show <path>#<chunk>` prints the full text saved in the index for a chunk
  referenced from `search` output, including surrounding chunks with
  `--before`/`--after` — the only way to read the extracted text of binary
  formats like `.docx`, `.epub` or `.pptx`.
- Structure-aware chunking (`--chunker auto`, on by default): chunk
  boundaries follow Markdown headings, Go/Python/JS declarations and
  top-level YAML/JSON keys instead of cutting mid-expression.
- Incremental indexing: unchanged files are skipped by mtime/size, changed
  content is detected by a content hash.
- Indexes `.docx`, `.odt`, `.ods`, `.odp`, `.xlsx`, `.pptx`, `.rtf`, `.fb2`,
  `.ipynb` and `.epub` documents out of the box — text is extracted with the standard library, no
  external converters required.
- Respects `.gitignore` by default (can be disabled).
- Index is stored in a hidden `.senso` directory, found automatically by
  walking up the directory tree (like `.git`).
- JSON output for `search` and `status`, convenient for scripting and agents.
- Output is English by default; it switches to Russian based on the
  `SENSO_LANG`/`LC_ALL`/`LC_MESSAGES`/`LANG` locale, or `SENSO_LANG`
  explicitly (JSON output is unaffected).

## Install

Prebuilt Linux packages and archives for `amd64` and `arm64` are attached to
every [release](https://github.com/foxzi/senso/releases):

```sh
sudo dpkg -i senso_<version>_<arch>.deb        # Debian, Ubuntu
sudo rpm -i senso-<version>-1.<arch>.rpm       # Fedora, RHEL, openSUSE
tar -xzf senso_<version>_linux_<arch>.tar.gz   # any distribution
```

Checksums for all files are published in `SHA256SUMS`. To build from source
instead, see the requirements below.

## Requirements

- Go (see `go.mod` for the exact version).
- CGO enabled (the SQLite driver and the vendored sqlite-vec extension are
  both C code).
- The build tag `sqlite_fts5` is **required** for every `go build`, `go test`
  and `go vet` invocation — without it FTS5 is not compiled into
  `mattn/go-sqlite3` and senso fails with "no such module: fts5".

Use the provided `Makefile` instead of raw `go` commands so the tag is never
forgotten:

```sh
make build   # builds ./bin/senso
make test    # go test with the required tag
make vet     # go vet with the required tag
make install # installs the binary
```

## Quick start

```sh
make build
./bin/senso index .              # build/update the index for the current directory
./bin/senso search "query text"  # search the index
./bin/senso status                # show index statistics
```

The first `index` run creates `./.senso/index.db` (unless `--db` or
`SENSO_DB` says otherwise) and writes a `.gitignore` inside `.senso` so the
index is never committed by accident.

## Commands

| Command  | Purpose                                              |
|----------|-------------------------------------------------------|
| `index`  | build or update the index for a directory              |
| `search` | search the index, `--format text/json/json-v2/paths`   |
| `show`   | print the full saved text of a chunk by its `search` reference |
| `status` | show index statistics (files, chunks, mode, size)       |
| `check`  | check whether the index is up to date (exit code 3 if not) |
| `rm`     | remove a file or a subtree from the index (disk untouched) |
| `version`| print the binary version                               |
| `help`   | print top-level usage                                   |

Run `senso <command> --help` for the full list of flags with their defaults.

```sh
senso show --json '/docs/specification.docx#4'  # read the stored chunk text by search reference
senso check --quiet || senso index .            # reindex only when the index is out of date
```

See detailed usage and architecture docs:

- Russian: [`docs/ru/usage.md`](docs/ru/usage.md), [`docs/ru/architecture.md`](docs/ru/architecture.md), [`docs/ru/release.md`](docs/ru/release.md)
- English: [`docs/en/usage.md`](docs/en/usage.md), [`docs/en/architecture.md`](docs/en/architecture.md), [`docs/en/release.md`](docs/en/release.md)

## Optional semantic search

By default senso is purely lexical and never talks to any external service.
If you also want semantic (embedding-based) search:

1. Run a local [Ollama](https://ollama.com) server with an embedding model
   (default: `bge-m3`).
2. Build the index with embeddings: `senso index --embed .`
3. Search semantically: `senso search --semantic "query text"`, or combine
   both rankings with `senso search --hybrid "query text"`

Without `--embed`/`--semantic`/`--hybrid`, Ollama is never contacted.
