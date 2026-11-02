# PORT_PLAN.md — Azure collector satellite port (issue #2)

The satellite observes Azure provider truth from fixtures and emits versioned
facts through the published SDK stdio contract. This plan maps the in-tree
sources to their satellite targets. Nothing here grants producer authority,
deploys, or removes in-tree code.

Source of truth for paths and identities: `eshu-hq/eshu` at the cutover base
(`drive/8853-azure-port` read-only worktree). Contract sources:
`github.com/eshu-hq/eshu-sdk/collector` (pinned `v0.2.0-rc.3`),
`github.com/eshu-hq/eshu-sdk/factschema` (pinned `v0.1.0-rc.12`).

## 1. Entry point: `go/cmd/collector-azure-cloud/` → `cmd/collector/`

| In-tree file | Satellite target | Notes |
|---|---|---|
| `main.go` (flag/mode parsing, telemetry bootstrap, hosted service run) | `cmd/collector/main.go` | Host protocol only (`--sdk-stdio`) plus local fixture proof flags; no Postgres, no hosted service, no OTel — core owns the runtime |
| `config.go` (env targets, fixture pages) | `cmd/collector/stdio.go` + `local.go` | Targets/fixtures arrive in the stdio request or flags; credentials by name only |
| `claimed_config.go` (instance selection) | claim authorization in `source.go` `targetForClaim` | Host owns claims; the satellite authorizes the claim scope against its targets and rejects the rest before any provider call |
| `service.go` (collector/claimed service wiring) | `collectWithPages`/`collectWithProvider` in `cmd/collector/stdio.go` | No committer: the SDK `Result` is the commit boundary the host consumes |
| `azure_live_client.go` (ambient credential + live factory) | `cmd/collector/live.go` (ambient `DefaultAzureCredential` by NAME, claim-first `collectLivePlan`) + `LiveProviderFactory` in `live.go` | Shipped: explicit opt-in plus an authorized claim reach the injected SDK adapter; the zero value still fails closed; tenant proof deferred to eshu-hq/eshu#9825 |
| `status_committer.go` (claim-status committer) | `Monitor` in `health.go` + `Result.Statuses` | Bounded claim outcomes ride the result and the stderr health line; durable status is host-owned |

## 2. Fact engine: `go/internal/collector/cloud/azure/` → root package

Ported file-for-file (the repo name already says which collector this is):

- `types.go` (enums, `Boundary`, observations, targets, config) → `types.go`
- `armid.go` → `armid.go` (verbatim; stable-identity derivation in `stableid.go`, proven byte-identical to the in-tree oracle)
- `resourcegraph.go`, `resourcechanges.go`, `payload_numbers.go` → same names (verbatim)
- `redaction.go` → `redaction.go` (`redact.Key` → SDK `marker.Key`; markers proven byte-identical to the SDK vector)
- `envelope.go` → `envelope.go` (emits `sdk.Fact`, not the core envelope; `FactID` stays core-owned)
- `relationship.go`, `identity_observation.go`, `dns_record.go`, `image_reference.go`, `resource_change.go` → same names (emits `sdk.Fact`)
- `source_lane_emission.go`, `relationship_identity_emission.go` → same names (verbatim logic)
- `collector.go` → `collector.go` (emits `sdk.Fact`; metrics via the local bounded `Metrics`)
- `metrics.go` → `metrics.go` (same vocabulary; in-memory counting instead of OTel — no OTel dependency crosses the boundary)
- `runtime/provider.go` (`PageProviderFactory`, fixture providers) → `provider.go`
- `runtime/source.go` + `runtime/claimed_source.go` + `runtime/config.go` → `source.go` + `types.go` (config)
- `runtime/live_provider.go` → `live.go` (factory, retry bounds, error kinds); `runtime/live_provider_sdk.go` → `live_client.go` (SDK adapter, 64KiB row cap, error classification, retry-after); `runtime/live_provider_arm_sdk.go` + `runtime/live_provider_armfallback.go` → `livearm.go` (allowlist, byte bound)

## 3. Port rules (kept, with satellite adaptations)

- Import boundary: ported code imports only the published SDK modules, never
  `go/internal` or core stores (`boundary_test.go` enforces this).
- Telemetry labels stay bounded enums; metric names keep the `eshu_dp_azure_`
  prefix (`manifest.yaml` pins it).
- Stable keys derive from normalized identity + lane (+ tenant where the
  in-tree derivation does), so idempotent re-emission converges byte-for-byte
  with the in-tree producer for the same inputs.
- Tags: resource facts preserve raw tags as reducer drift parity (readback
  strip is core-owned); tag observations fingerprint every value; extension
  secret keys are dropped, never masked.
- `manifest.yaml` declares the eight azure kinds; `grant.example.json`
  carries one grant per kind per schema version; `config.example.json`
  documents the fixture target shape.
- Fixture mode is the default; live transport requires the explicit `--live`
  / `"live": true` opt-in plus a host claim the satellite authorizes before
  any adapter is built, resolves the credential by NAME from the ambient
  workload identity, and fails closed otherwise. Emission shapes never change
  between seams: live-collected rows must converge byte-for-byte with fixture
  rows (`TestLiveCollectionMatchesFixtureParity`).
- No reducer admission, no new fact families, no API/MCP readback, no Helm —
  all core-owned (see `NOT_PORTED.md`).
