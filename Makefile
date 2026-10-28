# Local CI parity: `make ci` runs the lint, test, and race lane commands.
GO ?= go
GOLANGCI_LINT_VERSION ?= v2.12.2

.PHONY: ci lint test race

ci: lint test race

lint:
	$(GO) vet ./...
	$(GO) run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION) run ./...
	$(GO) run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION) fmt --diff ./... > /tmp/azure-cloud-fmt.diff || true
	if [ -s /tmp/azure-cloud-fmt.diff ]; then cat /tmp/azure-cloud-fmt.diff; exit 1; fi
	$(GO) mod tidy -diff
	fail=0; \
	for f in $$(git ls-files '*.go'); do \
		lines=$$(wc -l < "$$f"); \
		if [ "$$lines" -gt 500 ]; then echo "$$f over the 500-line cap ($$lines lines)"; fail=1; fi; \
	done; \
	exit $$fail

test:
	$(GO) build ./...
	$(GO) test ./... -count=1

race:
	$(GO) test ./... -race -count=1
