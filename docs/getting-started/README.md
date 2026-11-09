# Getting started

Hi! In about a minute you can watch this collector turn
Azure resource data (or checked-in fixture files) into facts for Eshu. It only collects and
returns evidence; the Eshu host validates the result and stores it.
Nothing is stored (or changed) here.

## Run it once

```bash
go run ./cmd/collector \
  --redaction-key-file /path/to/key \
  --generation-id generation-fixture-1
```

That runs the fixture proof: no Azure account, no live call. By default the collector reads checked-in fixture files; live Azure reads need the explicit `live` opt-in (see [Configuration](../reference/configuration.md)).

## What just happened?

The host runs one *claim* (one bounded request to collect one scope)
per process invocation: one `collector-sdk/v1alpha1` request on
stdin, one result full of typed *facts* on stdout. Eshu can then
answer which Azure resources Eshu has seen and how they connect. The picture and the step-by-step flow live
in [Diagrams](../diagrams/README.md) and
[Architecture](../architecture/README.md).

## Where next?

- Every knob: [Configuration](../reference/configuration.md)
- What comes out: [Facts](../reference/facts.md)
- Day-2 work: [Guides](../guides/README.md)
- New words and boundaries: [Concepts](../concepts/README.md)
