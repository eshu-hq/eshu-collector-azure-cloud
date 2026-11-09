# Operate

## How it gets run

The host runs one claim per process invocation. It starts the
binary with `--sdk-stdio`, writes one `collector-sdk/v1alpha1`
request to stdin, and reads one result from stdout. The host owns
admission, identity, storage, and recovery. This binary never
commits facts itself.

```bash
go run ./cmd/collector --sdk-stdio < request.json > result.json
```

The container image repeats the fixture proof with no arguments:

Installing it first? See [Seed and install](seed-and-install.md).

## What to watch

Each run writes one health line to stderr with the effective
limits, whether the run was fixture-only, the fact and warning
counts, the last success time and digest, or a bounded failure
class. The health line never carries Azure IDs, names, tags, query
text, URLs, or credential names, so it's safe to ship to your log
pipeline. The manifest declares the metric prefix `eshu_dp_azure_`;
treat the stderr health line as the operator signal proven by the
code here.

## Dependencies

| Dependency | Version or contract | Where it's pinned |
|---|---|---|
| `github.com/eshu-hq/eshu-sdk/collector` | `v0.2.0-rc.3` | `go.mod` |
| `github.com/eshu-hq/eshu-sdk/factschema` | `v0.1.0-rc.12` | `go.mod` |
| Host protocol | `collector-sdk/v1alpha1`, `process` adapter | [manifest.yaml](../../manifest.yaml) |
| Eshu core compatibility | `>=0.0.5 <0.2.0` | [manifest.yaml](../../manifest.yaml) |
| Producer identity | `dev.eshu.collector.azure-cloud`, version `0.1.0` | [manifest.yaml](../../manifest.yaml) |

`github.com/eshu-hq/eshu-sdk/wirecontract v0.1.0-rc.1` was recorded
in the previous README as not imported; it's not in `go.mod`.
