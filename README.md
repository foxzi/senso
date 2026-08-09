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

- Full-text search on SQLite FTS5 (bm25 ranking).
- Works with Russian and English out of the box: case-insensitive matching,
  Unicode-aware tokenization, prefix queries (`поиск*`), and Snowball
  stemming per token (`файл` matches `файлам`/`файлов`, `search` matches
  `searching`).
- Phrase search in double quotes (`senso search '"поиск файлов"'`) — words
  must be adjacent, but any word form is matched.
- Incremental indexing: unchanged files are skipped by mtime/size, changed
  content is detected by a content hash.
- Respects `.gitignore` by default (can be disabled).
- Index is stored in a hidden `.senso` directory, found automatically by
  walking up the directory tree (like `.git`).
- JSON output for `search` and `status`, convenient for scripting and agents.

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
| `search` | search the index, text/JSON/paths-only output          |
| `status` | show index statistics (files, chunks, mode, size)       |
| `rm`     | remove a file or a subtree from the index (disk untouched) |
| `version`| print the binary version                               |
| `help`   | print top-level usage                                   |

Run `senso <command> --help` for the full list of flags with their defaults.

See detailed usage and architecture docs:

- Russian: [`docs/ru/usage.md`](docs/ru/usage.md), [`docs/ru/architecture.md`](docs/ru/architecture.md)
- English: [`docs/en/usage.md`](docs/en/usage.md), [`docs/en/architecture.md`](docs/en/architecture.md)

## Optional semantic search

By default senso is purely lexical and never talks to any external service.
If you also want semantic (embedding-based) search:

1. Run a local [Ollama](https://ollama.com) server with an embedding model
   (default: `bge-m3`).
2. Build the index with embeddings: `senso index --embed .`
3. Search semantically: `senso search --semantic "query text"`

Without `--embed`/`--semantic`, Ollama is never contacted.
