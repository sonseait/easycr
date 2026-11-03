GO ?= go
BINARY ?= bin/easycr
IMAGE ?= easycr:local
PORT ?= 8080

.DEFAULT_GOAL := help
.PHONY: help build test-ui run test test-race test-e2e vet fmt fmt-check verify docker-build docker-push docker-release clean

help: ## Show available targets
	@awk 'BEGIN {FS = ":.*## "} /^[a-zA-Z_-]+:.*## / {printf "  %-16s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

build: ## Build the standalone binary
	$(GO) build -trimpath -ldflags='-s -w' -o $(BINARY) ./cmd/easycr

test-ui: ## Run browser regression tests (run npm ci first)
	npm run test:ui

run: build ## Run locally with ./data
	PORT=$(PORT) ./$(BINARY)

test: ## Run unit and integration tests (Docker E2E excluded)
	$(GO) test ./... -count=1

test-race: ## Run tests with the race detector
	$(GO) test -race ./... -count=1

test-e2e: ## Run opt-in tests against a real Docker daemon
	EASYCR_E2E=1 $(GO) test ./tests/e2e -v -count=1

vet: ## Run Go static analysis
	$(GO) vet ./...

fmt: ## Format Go source
	$(GO) fmt ./...

fmt-check: ## Check formatting without changing files
	@test -z "$$(gofmt -l cmd internal tests)" || { gofmt -l cmd internal tests; exit 1; }

verify: fmt-check vet test-race build ## Check formatting, vet, race tests and build
	$(GO) mod verify

docker-build: ## Build the distroless container (override IMAGE)
	docker build -t $(IMAGE) .

docker-push: ## Push an existing image (set IMAGE to registry/repository:tag)
	docker push $(IMAGE)

docker-release: docker-build ## Build then push the image (override IMAGE)
	$(MAKE) docker-push IMAGE="$(IMAGE)"

clean: ## Remove only the default build output; never remove data
	$(GO) clean
	rm -f bin/easycr
