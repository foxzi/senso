# senso: architecture overview

## Package structure

- **`internal/text`** — normalizes file content to UTF-8 NFC: tells text
  files from binary ones by a heuristic over the first 8192 bytes
  (`SniffLen`, same as git), and if a file is written in a single-byte
  Cyrillic encoding, detects it and transcodes it. Only UTF-8 ever ends up
  in the index; the original encoding is never stored.
- **`internal/extract`** — pulls plain text out of the `.docx`, `.odt`,
  `.ods`, `.odp`, `.xlsx`, `.pptx`, `.rtf`, `.fb2`, `.ipynb` and `.epub`
  document formats using nothing but the standard library. DOCX, ODT, ODS
  and ODP are ZIP archives, so exactly one entry is read from each (`word/document.xml`
  and `content.xml` respectively) and parsed with streaming
  `encoding/xml`; in ODS and ODT table cells within a row are joined with
  a tab, and rows with a newline; ODP reuses the same parser, because a
  slide is described by ordinary paragraphs inside text frames, and slides
  are separated from each other by a blank line. XLSX is a ZIP archive too, but cell text
  lives in a shared string table (`xl/sharedStrings.xml`) that sheets
  reference by index, so that table is read first; sheets are visited by
  the number in their file name, and completely empty rows are skipped.
  PPTX keeps every slide in a separate entry (`ppt/slides/slideN.xml`),
  visited by the number in the file name; only the text of shapes reaches
  the index, while speaker notes and slide layouts are skipped, since
  layouts repeat the same boilerplate on every slide. RTF is handled by a small hand-written control-word parser
  that understands groups, `\'hh` escapes with the code page from
  `\ansicpgN`, and `\uN` with `\ucN` replacement characters. FB2 is plain
  XML whose declared encoding (often windows-1251) is transcoded to
  UTF-8 first; metadata (`description`) and base64 images are excluded
  from the index. IPYNB is JSON; only the code and text of cells (the
  `source` field) is read, and execution outputs (`outputs`) are
  discarded. EPUB is a ZIP archive whose chapter order is defined
  indirectly: `META-INF/container.xml` points at the package document
  (OPF), whose `manifest` maps identifiers to files and whose `spine`
  lists them in reading order; chapters are read in exactly that order,
  the table of contents is skipped, and chapter markup is parsed by a
  non-strict decoder because it is frequently not well-formed XML.
  The `.doc` format (Word 97-2003) is deliberately
  unsupported: it would require an OLE2 container and piece table
  reader, which is out of proportion with the rest of the package.
- **`internal/chunk`** — splits normalized document text into fragments of
  roughly `--chunk-size` runes with `--overlap` runes of overlap. All
  sizes are measured in runes, not bytes, so that Cyrillic and CJK text
  produce chunks of comparable semantic size.
- **`internal/walk`** — recursively walks a directory tree, filtering by
  extensions, glob exclusions and `.gitignore`. It only yields candidate
  metadata (path, size, mtime); deciding whether content is text or binary
  is the job of the `text` package, not `walk`. All path exclusion rules
  (the hard-excluded `.git` and `.senso`, dependency directories, hidden
  paths, secret files, noisy files) live in a single `exclude.go` file;
  callers do not duplicate them. The default noisy file list
  (`DefaultNoisyPatterns`) is configurable: `Options.NoisyPatterns`
  replaces it entirely, while `Options.Noisy` and `Options.IncludeNoisy`
  selectively include noisy files on top of the exclusions.
- **`internal/dbpath`** — resolves and creates the database file path by
  priority: `--db` flag, `SENSO_DB` environment variable, walking up the
  directory tree for `.senso`; when creating a new database, writes a
  `.gitignore` inside `.senso`.
- **`internal/stem`** — stems document text and search queries with the
  Snowball algorithm (`github.com/kljensen/snowball`, pure Go, no cgo). The
  language is chosen per token by the presence of Cyrillic characters.
  Functions: `Tokens` (tokenizing), `Fold` (folds ё/е and lowercases),
  `Text` (per-token stemming that preserves the number and order of
  tokens — this is what keeps phrase search working on top of stems),
  `Query` (builds the FTS5 `MATCH` expression: parses quotes as a phrase,
  does not stem asterisk-prefixed prefixes, wraps every token in quotes to
  guard against FTS5 keywords like `OR`/`NEAR`), `Path` (prepares a file
  path for search: tokenizes it like plain text and additionally splits
  compound names into words) and `Idents` (file `idents.go`: pulls compound
  identifiers out of text — `ReplaceFile`, `replace_file`, `replace-file` —
  and splits each into stems of its individual words plus a stem of the
  joined form; abbreviations are split on the case transition
  (`HTTPServer` → `http`, `server`), and digits stay attached to the
  preceding word (`utf8` is one word); single-word tokens are skipped since
  they already live in `body`).
- **`internal/store`** — stores and searches the index in SQLite with the
  sqlite-vec extension: the schema, incremental updates of files and
  chunks, lexical search via FTS5/bm25, and vector search via
  vec0/cosine distance. `Chunks(path, fromSeq, toSeq)` returns a file's
  chunks by sequence range without going through search, and
  `ChunkSeqRange(path)` returns the file's minimum and maximum chunk
  number — the `show` subcommand is built on top of these two methods.
- **`internal/embed`** — an HTTP client to a local Ollama server for
  getting text embeddings, with retries (up to 3 attempts per request).
- **`internal/vecext`** — wires the sqlite-vec extension into the cgo
  driver `mattn/go-sqlite3` within a single process/connection.
- **`internal/cli`** — implementation of the subcommands (`index`,
  `search`, `status`, `rm`, `show`): flag parsing, incremental re-index
  decisions, filtering `search` results by
  `--path`/`--ext`/`--exclude`/`--root`, post-processing the result list
  (`--deduplicate`, `--max-per-file`), human-readable and JSON output
  formatting.
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
  line_start INTEGER NOT NULL,
  line_end INTEGER NOT NULL,
  UNIQUE(file_id, seq));
CREATE INDEX idx_chunks_file ON chunks(file_id);

CREATE VIRTUAL TABLE fts_chunks USING fts5(
    body,
    path,
    ids,
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

- `meta` stores the schema version (currently 4), the embedding model and
  its dimensionality, the index root, and the last indexing time. An empty
  `model`/`dim=0` is a valid state for a purely lexical index. Databases
  created by an older schema version are incompatible: senso refuses to
  work with them and asks to remove `.senso` and re-index from scratch.
- `files` has one row per indexed file with its last known `mtime`,
  `size`, and content hash.
- `chunks` holds the text fragments produced by the `chunk` package in
  their original (non-stemmed) form — the raw text in `chunks.text` is
  used for snippets in search results; `line_start` and `line_end` are the
  first and last line of the fragment in the source file, computed from
  byte offsets during splitting and printed in search output; rows are
  deleted in cascade when a file is removed (`ON DELETE CASCADE`).
- `fts_chunks` is the virtual FTS5 table used for lexical search, with
  three indexed columns. `body` holds the stemmed text of the chunk
  (package `internal/stem`, see above) — that is what `MATCH` actually
  searches. `path` holds the stemmed file path (`stem.Path`) — the same
  string in every chunk of the file, which lets a file be found by a word
  from its path (`senso search "migrations"`). `ids` holds the stems of
  the words that compound identifiers in the chunk text are split into
  (`stem.Idents`), so `ReplaceFile`, `replace_file` and `replace file` are
  all found by the same query. The columns are kept separate so that
  phrase search (`senso search '"search files"'`) matches within a single
  column and is not thrown off by insertions from the path or identifiers.
  The `unicode61 remove_diacritics 2` tokenizer gives case-insensitivity
  but does NOT fold the letters ё/е — `internal/stem` folds them by hand
  before text reaches the index and before a query reaches `MATCH`.
  `prefix='2 3'` enables efficient prefix search starting from 2-3
  characters (queries like `word*`, which are not stemmed). Ranking is
  done with `bm25(fts_chunks, 1.0, 0.4, 0.8, 0.0)`, with column weights in
  the order body, path, ids, chunk_id: the chunk text carries the highest
  weight, identifiers are weighted a bit lower (their words already
  partly overlap with `body`), and the path is weighted lowest, since it
  is the same for every chunk of a file and by itself says nothing about
  which chunk is more relevant. Known limitation: since the path is
  duplicated into every chunk of a file, a word from the path of a very
  large file becomes common in the corpus and barely affects ranking (its
  IDF collapses); this does not hurt combined queries, since those are
  ranked by the term from the text.
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

Tree walk, file read and document parsing errors no longer make a file
silently disappear from the result: each such error is collected into
the indexing report (the `failed` field, `--strict` and `--report-json`
flags on `index`) instead of being dropped, and does not stop processing
of the remaining files. A SIGINT/SIGTERM interruption is handled
separately from regular file errors: indexing exits with code 130, the
report's `interrupted` field becomes `true`, `--prune` and the last indexing
timestamp update are skipped, and files already processed before the signal
stay fully in the index.
