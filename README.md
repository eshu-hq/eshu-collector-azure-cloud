# eshu-collector-azure-cloud

**Status: pre-release.** Eshu core still owns this data in
production. This repo has no authority to publish facts on its own
yet.

In a nutshell: this collector reads Azure resource data and hands
Eshu evidence about what exists in an Azure scope, how those
resources relate, and what changed. Eshu can then answer which
Azure resources it has seen and how they connect. The collector
only collects and returns evidence; it doesn't store anything
itself.

```mermaid
flowchart LR
    A[Azure, or fixture files] --> C[This collector]
    C --> H[Eshu host process]
    H --> E[Eshu core]
```

In text: Azure (or fixture files) -> this collector -> Eshu host
process -> Eshu core.

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

## How Eshu runs it

The host runs one claim per process invocation.
It starts the binary with `--sdk-stdio`, writes one
`collector-sdk/v1alpha1` request to stdin, and reads one result
from stdout. The host owns admission and storage; this binary
never commits facts itself.

## Documentation

Start at the [docs index](docs/README.md). Shortcuts people use most:

| Page | What you'll find there |
|---|---|
| [Getting started](docs/getting-started/README.md) | Run it once, see facts come back |
| [Configuration](docs/reference/configuration.md) | Every config field, credentials, and the claim budget |
| [Facts](docs/reference/facts.md) | The fact kinds it emits, mirroring the manifest |
| [Diagrams](docs/diagrams/README.md) | Every diagram, with captions and text versions |

Older notes: [PORT_PLAN.md](PORT_PLAN.md) covers where the in-core
source came from, and [NOT_PORTED.md](NOT_PORTED.md) lists what
stays in Eshu core.

## Related repos

- [eshu](https://github.com/eshu-hq/eshu), Eshu core
- [eshu-sdk](https://github.com/eshu-hq/eshu-sdk), collector and fact-schema contracts
- [eshu-deploy](https://github.com/eshu-hq/eshu-deploy), deployment charts and activations

## Development

Run `make ci` to check a change the way CI does.

## Contributing, security, and license

See [CONTRIBUTING.md](CONTRIBUTING.md) before opening a change.
There's no `SECURITY.md` and no `LICENSE` file in this repo.
Source files carry `SPDX-License-Identifier: MIT` headers.
