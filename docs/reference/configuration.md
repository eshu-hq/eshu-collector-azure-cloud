# Configuration

Everything the collector needs arrives in the host request.
[config.example.json](../../config.example.json) shows the source shape.
In host mode the request carries `protocol_version`, the host claim,
and `config.source` with the fields below.

| Name | Required | Default | What it does |
|---|---|---|---|
| `collector_instance_id` | Yes | Flag default `azure-instance-1` in local mode | Instance that owns target policy |
| `targets` | Yes, at least one | None | Bounded Azure scope shards to read |
| `targets[].tenant_id` | Yes | None | Azure tenant for the shard |
| `targets[].scope_kind` | Yes | None | `subscription`, `management_group`, or `tenant` |
| `targets[].provider_scope_id` | Yes | None | Subscription, management group, or tenant identifier |
| `targets[].resource_type_family` | No | Empty means whole scope | Provider namespace filter, such as `microsoft.compute` |
| `targets[].location_bucket` | No | Empty means all locations | Azure location filter, such as `eastus` |
| `targets[].credential_ref` | For live reads | None | Name of the read-only credential; never a secret value |
| `targets[].source_uri` | No | `https://management.azure.com/` | Credential-free source address recorded on facts |
| `targets[].source_lane` | No | `resource_graph` | Read lane. `resource_changes` is fixture-only; `arm_fallback` is not served from fixture pages by the stdio runner |
| `targets[].fencing_token` | No | `1` | Ordering token so a stale run can't replace newer facts |
| `fixture_pages` | No | `testdata/resources_page1.json,testdata/resources_page2.json` via `--pages` | Fixture files to read instead of Azure |
| `live` | No | `false` | Explicit opt-in to live Azure reads |
| `redaction_key_file` (`--redaction-key-file`) | No | Empty | Path to read-only key material; enables tag, identity, DNS, and image facts |
| `limits.max_records_per_claim` | No | `10000` | Maximum facts in one claim |
| `limits.max_payload_bytes` | No | `1048576` | Maximum bytes in one fact payload |
| `limits.claim_timeout_seconds` | No | `300` | Maximum wall-clock seconds for one claim |

That `source_lane` note comes straight from the stdio runner:
`singleFixtureLane` in `cmd/collector/stdio.go` rejects any lane
other than `resource_graph` or `resource_changes` for fixture
pages, so an `arm_fallback` target never gets served from fixtures.

Local flags mirror the limits: `--max-records`,
`--max-payload-bytes`, and `--claim-timeout-seconds`.
Zero selects the default; negative values fail.

Try the fixture proof locally, no Azure account needed:

```bash
go run ./cmd/collector \
  --redaction-key-file /path/to/key \
  --generation-id generation-fixture-1
```

## Credentials and least privilege

Config names a credential with `credential_ref`; it never holds a
secret value. For live reads, the collector resolves that name from
the ambient workload identity (the identity of the running
workload) and fails closed otherwise. Credential names and values
are never read from fixture config into facts and never logged.

This repo doesn't name a specific Azure role or policy, so grant
the named credential only the read access the claimed scope needs
and stop there. Don't copy a role recommendation from another repo.

## Claim budget

One claim is bounded by the limits above (defaults: 10,000 records,
1,048,576 payload bytes per fact, 300 seconds). A run that passes a
bound fails instead of returning late or oversized evidence. If
you're seeing failures on a big scope, shard the scope with
`resource_type_family` or `location_bucket` before you raise limits.

---

Looking for what comes out the other side? See [Facts](facts.md).
Stuck on a value? [Troubleshoot](../guides/troubleshoot.md) covers
the failures people hit most.
