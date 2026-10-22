# Contributing to LUX TUI

Thanks for improving LUX TUI. Keep changes small, measured, and attached to a
real terminal flow.

## Before coding

1. Search existing issues and packages.
2. For a large API, compatibility promise, or dependency, open a design issue
   first.
3. Trace the full slice—input, state, layout, render, output, cleanup—before
   choosing the smallest root-cause change.

## Local checks

Use the Go version declared in `go.mod` or newer:

```sh
gofmt -w ./path/to/changed.go
go test ./...
go vet ./...
go test -run '^$' -bench . -benchmem ./...
```

Run benchmarks only for performance-sensitive changes. Record before/after
commands, environment, and raw output; do not infer a universal win from one
machine.

## Change rules

- Preserve the dependency direction in `docs/ARCHITECTURE.md`.
- Do not add an abstraction or dependency without a current slice that needs it.
- Add one focused test for nontrivial behavior.
- Include Unicode edge cases for text or cell changes.
- Bound input, queues, and memory at trust boundaries.
- Restore terminal modes on every lifecycle path.
- Update compatibility, notices, and status documentation when behavior changes.
- Avoid unrelated formatting or generated-file churn.

## Releasing

A release, including a release candidate, is cut from a `main` commit whose
CI run is green on every job: tests with `-race` on Linux, macOS and Windows
for both Go versions, lint, the nested `e2e`, `examples/07-ssh` and
`benchmarks` modules, and govulncheck. Before tagging, also run:

```sh
gofmt -l $(git ls-files '*.go')
for os in linux darwin windows freebsd; do GOOS=$os go build ./...; done
for m in . e2e examples/07-ssh benchmarks; do (cd $m && go mod tidy -diff); done
```

The nested modules are Unix-only (`benchmarks` is Linux-only); on a Windows
host, vet them with `GOOS=linux go vet ./...`. Re-run the lanes in
[`.verification/matrix/`](.verification/matrix/README.md) that the release
touches, and refresh their evidence.

Move `[Unreleased]` in CHANGELOG.md to a dated version section, add the
upgrade steps to [docs/MIGRATION.md](docs/MIGRATION.md), then create an
annotated tag (`git tag -a vX.Y.Z[-rc.N]`) and push the commit before the tag.

## Commits and pull requests

Use clear, imperative commits. A pull request should state:

- the user-visible problem and root cause;
- the smallest chosen fix;
- exact validation commands and results;
- compatibility or performance impact;
- remaining limitations.

By contributing, you agree that your contribution is licensed under either the
Apache License 2.0 or MIT License, at the recipient's option, unless you clearly
state otherwise before submission.

Please follow the [Code of Conduct](CODE_OF_CONDUCT.md) and report security
issues through the process in [SECURITY.md](SECURITY.md).
