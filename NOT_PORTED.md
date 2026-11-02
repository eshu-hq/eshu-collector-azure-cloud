# NOT_PORTED.md — what stays in core

This satellite owns provider observation and versioned fact emission only.
Everything below stays in Eshu core and is deliberately absent here; its
absence is the bounded-contract port, not a gap.

- Admission, identity resolution, projection, persistence, and recovery. The
  satellite emits `sdk.Fact` source evidence; durable fact identity (`FactID`),
  graph edges, and generations are committed and owned by the host.
- Reducer admission for the eight azure kinds (resource identity, tag
  evidence, changes, relationships, DNS, image references, warnings) and the
  API/MCP readback surfaces, including the readback strip of raw tag values.
- Live Azure proof against a real tenant. The satellite ships the live
  Resource Graph/ARM transport (explicitly injected `LiveProviderFactory`,
  the SDK client adapter, throttled/permission-hidden/token-expired/skip-token
  error classification, quota and retry bounds, and the ARM fallback
  allowlist) behind the explicit `--live` / `"live": true` opt-in plus a host
  claim the satellite authorizes before any adapter is built. The zero value
  still fails closed. Exercising the transport against a real tenant stays
  deferred to eshu-hq/eshu#9825; no live proof is claimed here and none is
  possible without a tenant.
- Claim scheduling, workflow control, fencing, and lease handoff
  (`collector.ClaimedService`, the workflow control store). The host drives
  one claim per stdio invocation; the satellite authorizes the scope and
  collects.
- Helm values and chart wiring, scan-status stores, and shared-registry
  telemetry. The satellite's operator signal is the stderr `collector health`
  line and the result statuses, both bounded-enum only.
- No `S3`, object-store, or Terraform-state code is ported here: this
  satellite observes Azure only. Cross-domain precedent (file layout, stdio
  shape, manifest/grant/test patterns) is adapted, never copied with
  Azure-irrelevant behavior.
