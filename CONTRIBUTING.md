# Contributing

## Local gates

`make ci` runs the same lint, test, and race commands CI runs: `go vet`,
`golangci-lint run` plus the gofumpt format check, `go mod tidy -diff`,
the 500-line `.go` file cap, `go build`, the full test suite, and the
race suite. Run it before every push. It needs Go 1.26.10 or newer;
`go.mod` is the version of record and CI installs exactly that.

Shell scripts pass `shellcheck` (see the `shell` lane).

## CI lanes

Job names are frozen because branch protection requires `ci-complete`:

| Lane | Runs when |
| --- | --- |
| `lint` | Go sources, module or lint-config changes, workflow changes, unclassified files |
| `test` | Above, plus fixtures and config the tests read |
| `race` | Go, dependency, workflow, or unclassified changes (never fixture-only), plus a coverage artifact and floor check |
| `shell` | Shell script changes |
| `image` | Vacuous proof until a Dockerfile lands (see #3) |
| `govulncheck` | Dependency changes, pushes to `main`, weekly schedule |
| `gosec` | Go or dependency changes, pushes to `main`, weekly schedule; findings fail red on schedule, advisory on PRs |
| `ci-complete` | Always; fails on any needed lane failure, passes on legitimate skips |

Lane bodies run inside the pinned `eshu-hq/.github` reusable; the caller
holds only `changes`, `lanes`, and `ci-complete`. Docs-only PRs run no
lane and stay mergeable through `ci-complete`.
A manual `workflow_dispatch` run and the weekly schedule run every lane as
a record. Neither lane uploads SARIF to the Security tab nor files an
issue: findings ride the run page and the uploaded SARIF artifact, and
per-scan filing waits on the upstream hook (eshu-hq/.github#10).

## Coverage floor

The `race` job fails below `COVERAGE_FLOOR` in `ci.yml`. The tree holds no
coverable statements yet, so the floor starts at 0 and may only rise toward
the 90% target as the collector port lands (see #2). Never lower it,
exclude packages silently, or delete assertions to move the number.
