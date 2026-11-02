# eshu-collector-azure-cloud

Eshu Azure cloud collector satellite. Pre-release: holds no producer authority
until its reviewed cutover. Refs eshu-hq/eshu#8853, part of #6707; satellite
port tracked as #2, image lane as #3 (closed: the Dockerfile builds the actual
collector, no push).

The satellite observes Azure provider truth from fixtures and emits versioned
source facts through the published Eshu SDK stdio contract
(`collector-sdk/v1alpha1`). Core keeps admission, identity, projection,
persistence, and recovery. See [PORT_PLAN.md](PORT_PLAN.md) for the in-tree
source map and [NOT_PORTED.md](NOT_PORTED.md) for what stays in core.

## Contracts

- `github.com/eshu-hq/eshu-sdk/collector` `v0.2.0-rc.3` (immutable, proxy-verified)
- `github.com/eshu-hq/eshu-sdk/factschema` `v0.1.0-rc.12` (immutable, proxy-verified;
  pins the fixture pack too)
- `github.com/eshu-hq/eshu-sdk/wirecontract` `v0.1.0-rc.1` (latest pin recorded;
  not imported — it publishes API/MCP wire shapes this collector never touches)

Every repo imports the contracts; no repo imports another repo. All eight
emitted kinds (`azure_cloud_resource`, `azure_cloud_relationship`,
`azure_tag_observation`, `azure_identity_observation`, `azure_dns_record`,
`azure_image_reference`, `azure_resource_change`,
`azure_collection_warning`) have typed structs in `factschema/azure/v1`; a
kind with no typed struct would be flagged, never invented inline.

## Run

Fixture proof (default; issues no live Azure call):

```bash
go run ./cmd/collector \
  --redaction-key-file /path/to/key \
  --generation-id generation-fixture-1
```

Host protocol (the host owns claims and commits; one claim per invocation):

```bash
go run ./cmd/collector --sdk-stdio < request.json > result.json
```

The request carries `protocol_version`, the host `claim`, and `config.source`
with `collector_instance_id`, `targets`, `fixture_pages`, and an optional
`redaction_key_file`. Live transport ships but stays unproven (no tenant
exists yet): it requires the explicit `"live": true` opt-in plus a host claim
the satellite authorizes before any adapter is built, resolves the credential
by NAME from the ambient workload identity, and fails closed otherwise. The
zero-value factory still returns `ErrLiveProviderGated`. Credential values
are never read from config and never logged; live-shaped rows redact
identically to fixtures. Tenant-backed proof stays deferred to
eshu-hq/eshu#9825.

Image lane (builds the actual collector; `docker run` with no args repeats
the fixture proof):

```bash
docker build --platform linux/amd64 -t azure-cloud-collector:local .
docker run --rm --platform linux/amd64 azure-cloud-collector:local
```

## Verify

```bash
make ci
```

`make ci` runs the same lint, test, and race commands CI runs: `go vet`,
`golangci-lint run` plus the gofumpt format check, `go mod tidy -diff`, the
500-line `.go` file cap, `go build`, the full test suite, and the race suite.
The race lane additionally enforces the coverage floor (currently 76; see
`CONTRIBUTING.md` — it may only rise).

## Releases

Every artifact in `manifest.yaml` is digest-pinned (`@sha256:…`, never
`:latest`). The current digest is a local-build digest (see the comment in
`manifest.yaml`): after the first image push, re-pin it with the registry
digest from the release job summary. Base images in `Dockerfile` are pinned
the same way; re-pin them when bumping either base. `version` in
`manifest.yaml` and the git tag must match (`v0.1.0` for `0.1.0`); pushing
a `v*` tag runs `release.yml`, which builds, signs, and publishes the image
plus SBOM and provenance attestations. The workflow publishes only when the
tag is exactly `v` plus the manifest version and points at the protected
`main` head; anything else fails before anything builds or pushes.

What pushes publish: every push to `main` publishes `:main`, `:latest`
(both moving), and `:sha-<long-sha>`; every push of a version tag publishes
that tag. Nothing runs on a pull request and nothing here deploys. Copy the
`seed.image` reference
(`ghcr.io/eshu-hq/eshu-collector-azure-cloud@sha256:…`) from the release job
summary into the activation. No tenant exists yet, so a release ships the
image but proves nothing live; tenant-backed proof stays deferred to
eshu-hq/eshu#9825.
