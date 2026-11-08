# Seed and install

Good news: there's no separate install step to learn here. This
collector has no `seed` subcommand; the container image entrypoint
is the collector itself (see [Dockerfile](../../Dockerfile)), and
the Eshu host starts it per claim. The image details:

```bash
docker build --platform linux/amd64 -t azure-cloud-collector:local .
docker run --rm --platform linux/amd64 azure-cloud-collector:local
```

[manifest.yaml](../../manifest.yaml) pins the Linux amd64 image by
digest:
`ghcr.io/eshu-hq/eshu-collector-azure-cloud@sha256:a5dcbc1d4e404ea68a67673c846276f63adb54689e3b6a832b32aeec38df7e29`.
The comment in that file says this is a local-build digest to re-pin
with the registry digest on the first image push.

There's no `seed` subcommand here. The image entrypoint is the
collector itself (see [Dockerfile](../../Dockerfile)), with the
fixture pages and `generation-fixture-1` as default arguments.

First run? [Getting started](../getting-started/README.md) has the
one-minute fixture proof.
