GOLANGCI_LINT_VERSION ?= v1.64.8
GOLANGCI_LINT_BIN ?= $(CURDIR)/bin/golangci-lint

.PHONY: fmt lint test test-race check

fmt:
	gofmt -w .

$(GOLANGCI_LINT_BIN):
	@mkdir -p $(dir $(GOLANGCI_LINT_BIN))
	@GOBIN=$(dir $(GOLANGCI_LINT_BIN)) GOTOOLCHAIN=go1.25.0 go install github.com/golangci/golangci-lint/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)

lint: $(GOLANGCI_LINT_BIN)
	$(GOLANGCI_LINT_BIN) run ./...

test:
	go test ./...

test-race:
	go test -race ./...

check: fmt lint test
