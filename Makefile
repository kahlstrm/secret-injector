.PHONY: help test coverage coverhtml lint build tidy fmt vet tools itest itest-v release-dry-run verify-release-artifacts

# Enforce static builds/tests by default
export CGO_ENABLED=0

# Configurable variables
BIN_DIR ?= bin
BIN_NAME ?= secret-injector
CMD_DIR ?= ./cmd/secret-injector
PKG_ALL ?= ./...
GOLANGCI_LINT ?= golangci-lint
GORELEASER ?= goreleaser
VERSION ?= dev
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS ?= -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)

help:
	@echo "Targets:"
	@echo "  test        Run unit tests"
	@echo "  coverage    Run tests with coverage profile (coverage.out)"
	@echo "  coverhtml   Generate HTML report (coverage.html) from coverage.out"
	@echo "  lint        Run golangci-lint (v2)"
	@echo "  build       Build CLI to $(BIN_DIR)/$(BIN_NAME)"
	@echo "  fmt         go fmt all packages"
	@echo "  vet         go vet all packages"
	@echo "  tidy        go mod tidy"
	@echo "  itest       Run integration tests (uses testcontainers)"
	@echo "  itest-v     Run integration tests with verbose output"
	@echo "  release-dry-run  Build and verify a GoReleaser snapshot without publishing"
	@echo "  verify-release-artifacts  Verify artifacts from an existing GoReleaser build"

test:
	go test $(PKG_ALL)

coverage:
	go test -covermode=atomic -coverprofile=coverage.out $(PKG_ALL)
	@echo "Wrote coverage.out"

coverhtml: coverage
	go tool cover -html=coverage.out -o coverage.html
	@echo "Open coverage.html in your browser"

lint:
	$(GOLANGCI_LINT) run --build-tags=integration --timeout=5m

fmt:
	go fmt $(PKG_ALL)

vet:
	go vet $(PKG_ALL)

tidy:
	go mod tidy

build: fmt vet
	@mkdir -p $(BIN_DIR)
	go build -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(BIN_NAME) $(CMD_DIR)
	@echo "Built $(BIN_DIR)/$(BIN_NAME)"

itest:
	go test -count=1 -tags=integration ./...

itest-v:
	go test -count=1 -tags=integration -v ./...

release-dry-run:
	$(GORELEASER) release --snapshot --clean --skip=publish
	$(MAKE) verify-release-artifacts

verify-release-artifacts:
	./scripts/verify-release-artifacts.sh
