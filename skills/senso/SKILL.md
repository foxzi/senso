---
name: senso
description: >-
  Search local files and codebases with the senso CLI: fast lexical (and
  optional semantic) search over an SQLite index, with full chunk retrieval
  via refs. Use this skill whenever you need to find where something is
  implemented, mentioned or documented in a repository or directory —
  "where is X handled", "find the code that does Y", "what does the spec say
  about Z" — instead of walking the tree with grep or reading files blindly.
  Also the only way to read text from .docx, .pptx, .xlsx, .epub, .odp and
  other extracted formats that cannot be read as plain files.
---

# senso: local search for agents

senso indexes a directory into `.senso/index.db` and answers queries with
ranked chunks. It exists so you do not have to read whole repositories:
search narrows the field to a few chunks, `show` returns their full text,
and only then — if ever — you open the file itself.

## The core loop

Always follow this order; each step is cheaper than the next.

```sh
# 1. Make sure the index is fresh. Exit code 3 = out of date.
senso check --quiet || senso index --quiet .

# 2. Search. json-v2 gives a ref for every result plus warnings.
senso search --format json-v2 --k 5 --deduplicate --max-per-file 2 \
  "how is the result rank computed" > hits.json

# 3. Read promising chunks in full, with neighbors for context.
jq -r '.results[].ref' hits.json | while read -r ref; do
  senso show --before 1 --after 1 "$ref"
done

# 4. Only if chunks were not enough — open the file itself.
jq -r '.results[0].path' hits.json   # line_start/line_end say where to look
```

Why this order:

- `search` output is truncated to `--snippet` (the `truncated` field in
  json-v2 says so); `show` prints the chunk in full — never re-read a file
  from disk just to get text that `show` already has;
- for `source_type: "extracted"` results (.docx, .pptx, .xlsx, .epub, .odp)
  `show` is the **only** way to get the text — you cannot read those files
  as plain text;
- `--deduplicate --max-per-file 2` keeps one file from flooding the list.

## Choosing the search mode

- Default (lexical) wins for identifiers, file names and exact terms.
  Identifiers are normalized: `senso search "replace file"` finds
  `ReplaceFile`, `replace_file` and `replace-file`.
- A question in your own words works better with `--hybrid` (requires an
  index built with `senso index --embed`; if vectors are missing you get
  the `no_vectors` error — fall back to lexical, do not try to build
  embeddings unless the user asked).
- Know the area already? Filters are cheaper than a bigger `--k`:
  `--path 'internal/**'`, `--ext go,md`, `--exclude 'vendor/**'`,
  `--root <indexed-root>`.
- Need every occurrence, not a ranking:
  `senso search --format paths "term" | xargs grep -n "term"`.

## Reading results (json-v2)

Top-level: `schema`, `mode`, `query`, `filters`, `results`, `warnings`.
Each result: `ref` (feed it to `senso show`), `path`, `chunk`,
`line_start`/`line_end`, `rank`, `score`, `text`, `truncated`,
`source_type` (`text` | `extracted`).

Scores are not comparable across modes; use `rank`.

Warnings to act on:

| Code | Meaning | What to do |
|---|---|---|
| `file_modified` | file changed after indexing | chunk text may be stale: read the file from disk or re-index |
| `file_missing` | file deleted from disk | ignore the result; `senso index --prune` cleans it up |

## Contracts

Streams: **stdout carries only the machine result** (search output, `show`
text, `status`/`check` JSON); progress, warnings and summaries go to
stderr. `... > out.json` always parses.

Exit codes:

| Code | Meaning |
|---|---|
| 0 | success |
| 1 | runtime failure (no index, DB error, Ollama unreachable) |
| 2 | invalid arguments |
| 3 | `check` only: index is out of date (an answer, not a failure) |
| 130 | interrupted |

json-v2 error codes (in the `error` field on stdout): `usage`, `no_index`,
`no_vectors`, `embed_failed`, `internal`. The code is the contract; the
message text is translated and may change.

## Command reference

- `senso index [flags] <path>` — build/update the index. Useful flags:
  `--quiet`, `--report-json` (machine report to stdout), `--strict`
  (non-zero exit if any file failed), `--ext`, `--exclude`,
  `--hidden` / `--include-hidden <globs>` (hidden paths; secrets like
  `.env` stay excluded unless explicitly included),
  `--noisy` / `--include-noisy <globs>` (lock files, minified bundles).
- `senso search [flags] <query>` — see above. `--k` (default 10),
  `--snippet <runes>` (default 500), `--format text|json|json-v2|paths`.
- `senso show [--before N] [--after N] [--json] <path>#<chunk>` — full
  chunk text by ref; stale files produce a stderr warning and
  `stale`/`stale_reason` in JSON, exit code stays 0.
- `senso check [--json] [path]` — read-only freshness check; `--json` lists
  the diverging files (`changed`/`missing`/`unindexed`/`excluded`;
  `skipped` = not indexable content, does not make the index stale).
- `senso status [--json]` — index statistics, roots, chunking parameters.
- `senso rm <path>` — remove a file or subtree from the index.

If any command fails with `no_index` / "index not found", run
`senso index .` from the project root first (fully local, no Ollama
needed), then retry.
