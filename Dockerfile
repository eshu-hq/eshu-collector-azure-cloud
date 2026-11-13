# Standalone Azure cloud collector satellite image: pure-Go binary plus
# manifest and parity fixtures, built reproducibly with -trimpath.
#
# Fixture mode is the default: without --sdk-stdio the binary collects the
# baked parity pages for one synthesized fixture claim and prints the SDK
# result. The host drives --sdk-stdio; live transport stays gated behind an
# explicit opt-in that fails closed with no injected adapter.
FROM golang:1.26.11-alpine@sha256:ae52b5bc6ed045a9ed26ab03dc19327a802a9cc9aa8b27ed57f5c6b60579b0bb AS build
RUN apk add --no-cache ca-certificates
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# EKS nodes are linux/amd64; pin GOARCH so builds on any host (e.g. arm64
# dev boxes) produce a runnable binary. An unpinned build shipped an arm64
# image that failed with "exec format error" on ops-qa (eshu-hq/eshu#8868).
RUN CGO_ENABLED=0 GOARCH=amd64 go build -trimpath -ldflags='-s -w' -o /out/collector ./cmd/collector

# Base digests resolved 2026-11-02 by clean pull; re-pin with the same flow
# when bumping either base.
FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab
COPY --from=build /out/collector /usr/local/bin/collector
COPY manifest.yaml /collector/manifest.yaml
COPY testdata/resources_page1.json /collector/testdata/resources_page1.json
COPY testdata/resources_page2.json /collector/testdata/resources_page2.json
WORKDIR /collector
USER 65532:65532
ENTRYPOINT ["/usr/local/bin/collector"]
CMD ["--pages", "testdata/resources_page1.json,testdata/resources_page2.json", "--generation-id", "generation-fixture-1"]
