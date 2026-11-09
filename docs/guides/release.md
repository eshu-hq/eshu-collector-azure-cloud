# Release

```bash
make ci
```

`make ci` runs `make lint`, `make test`, and `make race` (see
[Makefile](../../Makefile)): vet, lint and format checks, module
drift, the 500-line file cap, the full test suite, and the race
suite. See [CONTRIBUTING.md](../../CONTRIBUTING.md) for the coverage
floor, which may only rise.

Releases: `version` in [manifest.yaml](../../manifest.yaml) and the
git tag must match (`v0.1.0` for `0.1.0`). Pushing a `v*` tag on
the protected `main` head runs the release workflow, which builds,
signs, and publishes the image with SBOM and provenance
attestations. Any other tag fails before anything builds or
pushes. Artifacts stay digest-pinned (`@sha256:...`, never
`:latest` in the manifest).

Before tagging, run the local gate from
[Contributing](../contributing/README.md) and re-read
[manifest.yaml](../../manifest.yaml): the manifest version, the
git tag, and the pinned image digest must agree.
