.PHONY: help test coverage coverhtml lint build tidy fmt vet tools localstack-up localstack-down itest

# Enforce static builds/tests by default
export CGO_ENABLED=0

# Configurable variables
BIN_DIR ?= bin
BIN_NAME ?= secret-injector
CMD_DIR ?= ./cmd/secret-injector
PKG_ALL ?= ./...
GOLANGCI_LINT ?= golangci-lint

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
	@echo "  localstack-up   Start LocalStack (SSM) via docker compose"
	@echo "  localstack-down Stop LocalStack"
	@echo "  itest       Run integration tests (requires LocalStack)"

test:
	go test $(PKG_ALL)

coverage:
	go test -covermode=atomic -coverprofile=coverage.out $(PKG_ALL)
	@echo "Wrote coverage.out"

coverhtml: coverage
	go tool cover -html=coverage.out -o coverage.html
	@echo "Open coverage.html in your browser"

lint:
	$(GOLANGCI_LINT) run --timeout=5m

fmt:
	go fmt $(PKG_ALL)

vet:
	go vet $(PKG_ALL)

tidy:
	go mod tidy

build: fmt vet
	@mkdir -p $(BIN_DIR)
	go build -o $(BIN_DIR)/$(BIN_NAME) $(CMD_DIR)
	@echo "Built $(BIN_DIR)/$(BIN_NAME)"

localstack-up:
	docker compose up -d

localstack-down:
	docker compose down

itest:
	LOCALSTACK=1 go test -tags=integration ./...
