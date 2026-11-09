# Architecture

Short version: the Eshu host hands this collector one bounded scope,
the collector reads it and hands typed facts back, and the host
validates and commits them to Eshu core. This collector stores
nothing itself. All diagrams live in
[Diagrams](../diagrams/README.md); this page is the words.

## Where it sits

By default the collector reads checked-in fixture files and makes no
live Azure call. Live reads need an explicit opt-in; see
[Configuration](../reference/configuration.md).

Overview diagram: [Where it sits](../diagrams/README.md#1-where-it-sits).

## One claim, start to finish

the host writes one `collector-sdk/v1alpha1` request to
stdin, the collector reads the claimed scope and builds facts, then
writes one result to stdout. The host validates and commits.
One process invocation serves exactly one claim.

Sequence diagram:
[One claim, start to finish](../diagrams/README.md#2-one-claim-start-to-finish).

## When something goes wrong

a run that passes a limit fails rather than returning late
or oversized evidence. A scope that can't be read fully still
returns what was read, marked with an `azure_collection_warning`
fact. Live reads fail closed too: the zero-value live factory
refuses to run, and credential resolution fails closed when the
ambient workload identity isn't there.

Failure-flow diagram:
[When something goes wrong](../diagrams/README.md#3-when-something-goes-wrong).

## Trust boundary

- It never writes to the Eshu graph, Postgres, or any queue.
  It imports no core storage handle; the host validates and commits.
- It never commits facts or decides final truth.
  Its manifest declares reducer phase `source_evidence_only:no_graph_truth`.
- It makes no live Azure call by default, and no test does either.
- It never reads a credential value from config or logs one.
