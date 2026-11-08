# Concepts

Short version: the Eshu host hands this collector one bounded Azure
scope, the collector reads that scope (or fixture files, by default),
turns what it finds into small typed facts, and hands them back.
It stores nothing itself. The host validates the result and commits
the facts to Eshu core.

A few words we'll use a lot. A *fact* is one small, typed piece of
evidence. A *claim* is one bounded request to collect one scope.
The *host* is the Eshu process that starts this collector, sends it
a claim, and stores the result.

## When to use it

Use it when Eshu needs evidence about Azure resources in a
subscription, management group, or tenant-level scope, including
their tags, identities, DNS records, image references, and
reported changes.

## When not to use it

- Don't use it as a live Azure browser or reporting tool.
  It returns facts to the Eshu host; it has no user interface.
- Don't expect proven live Azure reads yet.
  Live transport ships behind an explicit opt-in, no tenant-backed
  proof exists yet, and the zero-value live factory fails closed.
- Don't use it for non-Azure clouds or for Azure data-plane
  contents (object data, secrets, or record payloads).

## What you get

The full fact table lives in [Facts](../reference/facts.md), which
mirrors `spec.emittedFacts` in [manifest.yaml](../../manifest.yaml).

Tag, identity, DNS, and image facts need redaction key material.
Without a redaction key file, those kinds aren't emitted. Sensitive
values get fingerprinted or removed before emission, never passed
through raw.

## Boundaries: what this never does

- It never writes to the Eshu graph, Postgres, or any queue.
  It imports no core storage handle; the host validates and commits.
- It never commits facts or decides final truth.
  Its manifest declares reducer phase `source_evidence_only:no_graph_truth`.
- It makes no live Azure call by default, and no test does either.
- It never reads a credential value from config or logs one.

For the flow behind these boundaries, see
[Architecture](../architecture/README.md) and
[Diagrams](../diagrams/README.md).
