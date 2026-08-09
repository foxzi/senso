# Building and releases

## What runs automatically

The repository has two GitHub Actions workflows.

**`.github/workflows/ci.yml`** runs on every push to `main` and on every pull
request. It checks formatting (`gofmt -l`), then runs `make vet`, `make test`
and `make build` on both architectures — `amd64` and `arm64`.

**`.github/workflows/release.yml`** runs when a tag matching `v*` is pushed.
For each architecture it runs the tests, builds the binary, verifies that
`senso version` reports the version from the tag, and uploads the artifacts.
The `publish` job then collects everything, computes `SHA256SUMS` and creates
a GitHub Release with generated release notes.

## Build matrix

| Platform     | Runner             | Artifacts                                                       |
|--------------|--------------------|-----------------------------------------------------------------|
| linux/amd64  | `ubuntu-22.04`     | `.tar.gz`, `senso_<v>_amd64.deb`, `senso-<v>-1.x86_64.rpm`      |
| linux/arm64  | `ubuntu-22.04-arm` | `.tar.gz`, `senso_<v>_arm64.deb`, `senso-<v>-1.aarch64.rpm`     |

Builds run on native runners rather than through cross-compilation: senso
requires CGO (the `mattn/go-sqlite3` driver and the vendored `internal/vecext`
extension are C code), so every architecture needs its own compiler.

`ubuntu-22.04` is chosen deliberately: it ships glibc 2.35, so the binary runs
on any distribution no older than that. Building on a newer image would raise
the minimum requirement for no benefit.

## Cutting a release

```sh
git tag -a v0.2.0 -m "Release description"
git push origin v0.2.0
```

Nothing else is needed. The version embedded in the binary comes from the tag:
the `Makefile` feeds `git describe` into `-ldflags`, so `senso version` prints
exactly what the tag said.

A dedicated workflow step compares the `senso version` output against the tag
name and fails the build on a mismatch — a guard against publishing a release
with the wrong version.

## Building packages locally

```sh
go install github.com/goreleaser/nfpm/v2/cmd/nfpm@latest
make package
```

Packages appear in `dist/` for the current architecture. Their layout is
described in `packaging/nfpm.yaml`: the binary goes to `/usr/bin/senso`, the
documentation to `/usr/share/doc/senso/`.

The package version is derived from `VERSION` in the `Makefile`: the leading
`v` is stripped and hyphens are replaced with `~`, because rpm does not allow
a hyphen in the Version field. A build off-tag yields something like
`0.1.0~3~gabc1234`.
