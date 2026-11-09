# Contributing

Thanks for helping out! This page gets you from clone to a green
pull request.

The repo-root [CONTRIBUTING.md](../../CONTRIBUTING.md) is authoritative for CI lanes, the coverage floor, and the rules that do not bend. This page is the friendly summary; if the two ever disagree, the root file wins.

## Setup

You need Go at the version in `go.mod` (the version of record; CI
installs exactly that). It needs Go 1.26.10 or newer (`go.mod` is the version of record). Lint runs through `go run` in the Makefile, so there's no separate golangci-lint install step here.

## Check your change

```bash
make ci
```

That's the whole local contract: lint, test, and race, the same
commands CI runs (see [Makefile](../../Makefile)). Run it before
every push. Docs-only changes still deserve a link check: click
through the pages you touched, starting at the
[docs index](../README.md).

## Opening a PR

1. Branch from `main`; keep the change to one topic.
2. Run `make ci` and make sure it's green locally first.
3. If you touched `manifest.yaml` facts, update
   [Facts](../reference/facts.md) in the same change; the table there
   mirrors the manifest.
4. Push and open the PR. Only `ci-complete` is required to merge,
   and it passes when every needed lane passes.

Releasing after merge? See the [Release guide](../guides/release.md).
