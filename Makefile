GO ?= go
GOLANGCI_LINT ?= golangci-lint

.PHONY: check fmt lint test test-race

check: test test-race lint

fmt:
	$(GOLANGCI_LINT) fmt ./...

lint:
	$(GOLANGCI_LINT) run ./...

test:
	$(GO) test ./...

test-race:
	$(GO) test -race ./...
