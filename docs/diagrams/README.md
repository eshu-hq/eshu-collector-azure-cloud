# Diagrams

Every diagram for this collector lives on this page, each with a
caption and a plain-text version so nothing depends on rendering.
Other pages link here instead of copying a diagram. (The repo
[README](../../README.md) keeps the first one as its overview.)

## 1. Where it sits

*Caption: source data flows into this collector, then to the Eshu
host process, then to Eshu core. The collector itself stores
nothing.*

```mermaid
flowchart LR
    A[Azure, or fixture files] --> C[This collector]
    C --> H[Eshu host process]
    H --> E[Eshu core]
```

Text version: Azure (or fixture files) -> this collector -> Eshu host
process -> Eshu core.

## 2. One claim, start to finish

*Caption: one process invocation serves exactly one claim. The host
writes the request, the collector reads the claimed scope, facts
come back on stdout, and the host validates and commits.*

```mermaid
sequenceDiagram
    participant H as Eshu host process
    participant C as This collector
    participant A as Azure (or fixture files)
    participant E as Eshu core
    H->>C: one claim on stdin (--sdk-stdio)
    C->>A: read the claimed Azure scope
    A-->>C: resource pages
    C-->>H: one result with facts on stdout
    H->>E: validate and commit the facts
```

Text version: the host writes one `collector-sdk/v1alpha1` request to
stdin, the collector reads the claimed scope and builds facts, then
writes one result to stdout. The host validates and commits.
One process invocation serves exactly one claim.

## 3. When something goes wrong

*Caption: the failure paths. Bounded, fail-closed outcomes are
preferred over late, oversized, or silently incomplete evidence;
partial reads say so instead of pretending.*

```mermaid
flowchart TD
    S[Claim arrives] --> L{Within claim limits?}
    L -- no --> F[Fail instead of returning late or oversized evidence]
    L -- yes --> R[Read the scope]
    R --> P{Whole scope readable?}
    P -- no --> W[Emit what was read plus an azure_collection_warning fact]
    P -- yes --> O[Return facts on stdout]
    W --> O
```

Text version: a run that passes a limit fails rather than returning late
or oversized evidence. A scope that can't be read fully still
returns what was read, marked with an `azure_collection_warning`
fact. Live reads fail closed too: the zero-value live factory
refuses to run, and credential resolution fails closed when the
ambient workload identity isn't there.
