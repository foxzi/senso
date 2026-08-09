# senso: architecture overview

## Package structure

- **`internal/text`** — normalizes file content to UTF-8 NFC: tells text
  files from binary ones by a heuristic over the first 8192 bytes
  (`SniffLen`, same as git), and if a file is written in a single-byte
  Cyrillic encoding, detects it and transcodes it. Only UTF-8 ever ends up
  in the index; the original encoding is never stored.
- **`internal/chunk`** — splits normalized document text into fragments of
  roughly `--chunk-size` runes with `--overlap` runes of overlap. All
  sizes are measured in runes, not bytes, so that Cyrillic and CJK text
  produce chunks of comparable semantic size.
- **`internal/walk`** — recursively walks a directory tree, filtering by
  extensions, glob exclusions and `.gitignore`. It only yields candidate
  metadata (path, size, mtime); deciding whether content is text or binary
  is the job of the `text` package, not `walk`.
- **`internal/dbpath`** — resolves and creates the database file path by
  priority: `--db` flag, `SENSO_DB` environment variable, walking up the
  directory tree for `.senso`; when creating a new database, writes a
  `.gitignore` inside `.senso`.
- **`internal/stem`** — stems document text and search queries with the
  Snowball algorithm (`github.com/kljensen/snowball`, pure Go, no cgo). The
  language is chosen per token by the presence of Cyrillic characters.
  Functions: `Tokens` (tokenizing), `Fold` (folds ё/е and lowercases),
  `Text` (per-token stemming that preserves the number and order of
  tokens — this is what keeps phrase search working on top of stems), and
  `Query` (builds the FTS5 `MATCH` expression: parses quotes as a phrase,
  does not stem asterisk-prefixed prefixes, wraps every token in quotes to
  guard against FTS5 keywords like `OR`/`NEAR`).
- **`internal/store`** — stores and searches the index in SQLite with the
  sqlite-vec extension: the schema, incremental updates of files and
  chunks, lexical search via FTS5/bm25, and vector search via
  vec0/cosine distance.
- **`internal/embed`** — an HTTP client to a local Ollama server for
  getting text embeddings, with retries (up to 3 attempts per request).
- **`internal/vecext`** — wires the sqlite-vec extension into the cgo
  driver `mattn/go-sqlite3` within a single process/connection.
- **`internal/cli`** — implementation of the subcommands (`index`,
  `search`, `status`, `rm`): flag parsing, incremental re-index decisions,
  human-readable and JSON output formatting.
- **`internal/i18n`** — picks the language of human-readable output
  (`Detect` reads `SENSO_LANG`/`LC_ALL`/`LC_MESSAGES`/`LANG`, `Set`
  applies it at startup, `T`/`Tf` pick and format a string). There is no
  catalog of translation keys: (en, ru) string pairs live right next to
  the code that uses them. The JSON format (`--json`) does not depend on
  the output language — this is intentional, machine-readable values are
  always the same.

There is no separate `cmd` layer — `main.go` at the module root is a thin
entry point: it parses the subcommand name, calls the matching `cli.Run*`
function, and turns the returned error into a process exit code (0 —
success or help, 1 — runtime error, 2 — argument parsing error or unknown
command).

## Database schema

```sql
CREATE TABLE meta(key TEXT PRIMARY KEY, value TEXT);

CREATE TABLE files(
  id INTEGER PRIMARY KEY,
  path TEXT NOT NULL UNIQUE,
  mtime INTEGER NOT NULL,
  size INTEGER NOT NULL,
  hash TEXT NOT NULL);

CREATE TABLE chunks(
  id INTEGER PRIMARY KEY,
  file_id INTEGER NOT NULL REFERENCES files(id) ON DELETE CASCADE,
  seq INTEGER NOT NULL,
  text TEXT NOT NULL,
  UNIQUE(file_id, seq));
CREATE INDEX idx_chunks_file ON chunks(file_id);

CREATE VIRTUAL TABLE fts_chunks USING fts5(
    body,
    chunk_id UNINDEXED,
    tokenize="unicode61 remove_diacritics 2",
    prefix='2 3'
);

-- created lazily, only if senso index --embed was ever run:
CREATE VIRTUAL TABLE vec_chunks USING vec0(
    chunk_id INTEGER PRIMARY KEY,
    embedding float[dim] distance_metric=cosine
);
```

- `meta` stores the schema version (currently 2), the embedding model and
  its dimensionality, the index root, and the last indexing time. An empty
  `model`/`dim=0` is a valid state for a purely lexical index. Databases
  created by an older schema version are incompatible: senso refuses to
  work with them and asks to remove `.senso` and re-index from scratch.
- `files` has one row per indexed file with its last known `mtime`,
  `size`, and content hash.
- `chunks` holds the text fragments produced by the `chunk` package in
  their original (non-stemmed) form — the raw text in `chunks.text` is
  used for snippets in search results; rows are deleted in cascade when a
  file is removed (`ON DELETE CASCADE`).
- `fts_chunks` is the virtual FTS5 table used for lexical search. The
  `body` column holds the stemmed text (package `internal/stem`, see
  above), not the raw text — that is what `MATCH` actually searches. The
  `unicode61 remove_diacritics 2` tokenizer gives case-insensitivity but
  does NOT fold the letters ё/е — `internal/stem` folds them by hand
  before text reaches the index and before a query reaches `MATCH`.
  `prefix='2 3'` enables efficient prefix search starting from 2-3
  characters (queries like `word*`, which are not stemmed). Ranking is
  done with the built-in `bm25()` function.
- `vec_chunks` is the sqlite-vec (`vec0`) virtual table for vector search
  with the `cosine` metric. It is not created when the schema is first
  initialized, but lazily (`EnsureVectors`/
  `CREATE VIRTUAL TABLE IF NOT EXISTS`) — on the first `--embed` indexing
  run, because the vector dimensionality is only known once the first
  embedding has actually been produced by the chosen model.

## Why CGO and a vendored sqlite-vec

The `mattn/go-sqlite3` driver is a cgo wrapper around its own SQLite
amalgamation (`sqlite3-binding.h`), not the system `libsqlite3-dev`. The
official `sqlite-vec` Go bindings (`asg017/sqlite-vec-go-bindings/cgo`)
are built with `-DSQLITE_CORE` and expect a `sqlite3.h` header, which
simply does not exist alongside `mattn/go-sqlite3`.

To work around this, the `sqlite-vec` sources (`sqlite-vec.c`/`.h`) are
copied into `internal/vecext`, and the `mattn/go-sqlite3` amalgamation is
placed next to them under the name `sqlite3.h`, so cgo can find it locally
via `-I${SRCDIR}`. This lets `sqlite-vec` be built as part of the same
process and the same SQLite connection as the main driver, with no header
conflicts and no need to install system SQLite packages. The trade-off is
that building senso requires CGO (`CGO_ENABLED=1`) and a C compiler.

## Incrementality

For every file in `files`, senso stores `mtime`, `size`, and `hash`
(FNV-1a, 64-bit, over the file content — a fast, non-cryptographic hash
that is enough to detect content changes).

The decision of what to do with a file on a repeated `senso index` run is
made by a pure function, `decideFile` (package `internal/cli`, no side
effects):

1. **fast path** — if the on-disk `mtime` and `size` match what is stored
   in the database, the file is considered unchanged and skipped
   (`actionSkip`), without reading or hashing its content;
2. if `mtime`/`size` differ, the file content is hashed and the hash is
   compared to the stored one:
   - hash matches — the content actually did not change (for example, the
     file was just `touch`ed), chunks are not recomputed, only
     `mtime`/`size` are updated (`actionTouch`);
   - hash differs — the content really changed, chunks are recomputed and
     re-indexed (`actionReindex`).

Vector backfill is handled separately (`applyBackfill`): if
`senso index --embed` is run and the existing database does not have a
`vec_chunks` table yet (the index used to be lexical-only), the
`mtime`/`size` fast path is disabled, and `actionSkip` is forced into
`actionReindex` for every file — so every chunk goes through the embedding
model and gets a vector, even if the file content has not changed since
the last purely lexical indexing run.
