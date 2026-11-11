.DEFAULT_GOAL := help
.PHONY: help build test-ui run test test-race test-e2e vet fmt fmt-check verify clean

help: ## Show available targets
	@awk 'BEGIN {FS = ":.*## "} /^[a-zA-Z_-]+:.*## / {printf "  %-16s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

build: ## Build the standalone binary
	go build -trimpath -ldflags='-s -w' -o bin/easycr ./cmd/easycr

test-ui: ## Run browser regression tests (run npm ci first)
	npm run test:ui

run: build ## Run locally with ./data
	./bin/easycr

test: ## Run unit and integration tests (Docker E2E excluded)
	go test ./... -count=1

test-race: ## Run tests with the race detector
	go test -race ./... -count=1

test-e2e: ## Run opt-in tests against a real Docker daemon
	EASYCR_E2E=1 go test ./tests/e2e -v -count=1

vet: ## Run Go static analysis
	go vet ./...

fmt: ## Format Go source
	go fmt ./...

fmt-check: ## Check formatting without changing files
	@test -z "$$(gofmt -l cmd internal tests)" || { gofmt -l cmd internal tests; exit 1; }

verify: fmt-check vet test-race build ## Check formatting, vet, race tests and build
	go mod verify

clean: ## Remove only the default build output; never remove data
	go clean
	rm -f bin/easycr
