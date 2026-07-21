.PHONY: help check-env setup test build check fix

help: ## Show development commands
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "%-16s %s\n", $$1, $$2}'

check-env: ## Check required local tools
	@command -v go >/dev/null 2>&1 || { echo "go not found in PATH"; exit 1; }

setup: check-env ## Download Go module dependencies
	go mod download

test: check-env ## Run the fast Go test suite
	go test ./...

build: check-env ## Compile all packages without leaving a repository binary
	go build ./...

check: check-env ## Run the complete local handoff gate
	test -z "$$(gofmt -l .)"
	go mod tidy -diff
	go vet ./...
	go test -race ./...
	go build ./...

fix: check-env ## Format Go source files
	gofmt -w .

.DEFAULT_GOAL := help
