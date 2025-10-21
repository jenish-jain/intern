# AI Intern Agent Makefile

# Variables
BINARY_NAME=agent
BUILD_DIR=build
CMD_DIR=cmd/agent
GO_FILES=$(shell find . -name "*.go" -type f)
VERSION?=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS=-ldflags "-X main.version=$(VERSION)"

# Default target
.PHONY: all
all: clean build

# Help target
.PHONY: help
help: ## Show this help message
	@echo "AI Intern Agent - Available targets:"
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  %-15s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

# Build targets
.PHONY: build
build: ## Build the binary
	@echo "Building $(BINARY_NAME)..."
	@mkdir -p $(BUILD_DIR)
	go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) ./$(CMD_DIR)

.PHONY: build-race
build-race: ## Build with race detector
	@echo "Building $(BINARY_NAME) with race detector..."
	@mkdir -p $(BUILD_DIR)
	go build -race $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) ./$(CMD_DIR)

.PHONY: install
install: ## Install the binary to $GOPATH/bin
	go install $(LDFLAGS) ./$(CMD_DIR)

# Development targets
.PHONY: run
run: build ## Build and run the agent
	export $(cat .env | xargs)
	./$(BUILD_DIR)/$(BINARY_NAME)

.PHONY: run-init
run-init: build ## Build and run with --init flag to create sample config
	./$(BUILD_DIR)/$(BINARY_NAME) --init

.PHONY: dev
dev: ## Run with go run for development
	go run ./$(CMD_DIR)

# Testing targets
.PHONY: test
test: ## Run all tests
	go test ./...

.PHONY: test-v
test-v: ## Run all tests with verbose output
	go test -v ./...

.PHONY: test-race
test-race: ## Run tests with race detector
	go test -race ./...

.PHONY: test-cover
test-cover: ## Run tests with coverage
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

.PHONY: test-integration
test-integration: ## Run integration tests (if any)
	go test -tags=integration ./...

.PHONY: benchmark
benchmark: ## Run benchmarks
	go test -bench=. -benchmem ./...

# Code quality targets
.PHONY: vet
vet: ## Run go vet
	go vet ./...

.PHONY: fmt
fmt: ## Format code with go fmt
	go fmt ./...

.PHONY: lint
lint: ## Run golangci-lint (requires golangci-lint to be installed)
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run; \
	else \
		echo "golangci-lint not found. Install with: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest"; \
	fi

.PHONY: check
check: vet test ## Run vet and tests
	@echo "All checks passed!"

.PHONY: quality
quality: fmt vet lint test ## Run all quality checks
	@echo "Quality checks completed!"

# Dependency management
.PHONY: deps
deps: ## Download dependencies
	go mod download

.PHONY: deps-update
deps-update: ## Update dependencies
	go get -u ./...
	go mod tidy

.PHONY: deps-vendor
deps-vendor: ## Vendor dependencies
	go mod vendor

.PHONY: tidy
tidy: ## Tidy go.mod
	go mod tidy

# Mock generation
.PHONY: mocks
mocks: ## Generate mocks
	@echo "Generating mocks..."
	@if command -v mockgen >/dev/null 2>&1; then \
		go generate ./...; \
	else \
		echo "mockgen not found. Install with: go install go.uber.org/mock/mockgen@latest"; \
	fi

# Clean targets
.PHONY: clean
clean: ## Clean build artifacts
	@echo "Cleaning..."
	rm -rf $(BUILD_DIR)
	rm -f coverage.out coverage.html

.PHONY: clean-all
clean-all: clean ## Clean all generated files
	rm -rf vendor/
	go clean -cache
	go clean -testcache
	go clean -modcache

# Docker targets (for future use)
.PHONY: docker-build
docker-build: ## Build Docker image
	@if [ -f Dockerfile ]; then \
		docker build -t ai-intern-agent:$(VERSION) .; \
	else \
		echo "Dockerfile not found"; \
	fi

.PHONY: docker-run
docker-run: ## Run Docker container
	docker run --rm -it ai-intern-agent:$(VERSION)

# Configuration targets
.PHONY: config-sample
config-sample: build ## Generate sample configuration files
	./$(BUILD_DIR)/$(BINARY_NAME) --init

.PHONY: config-validate
config-validate: build ## Validate configuration
	@echo "Validating configuration..."
	@if [ -f configs/config.yaml ]; then \
		echo "Configuration file found"; \
	else \
		echo "No configuration file found. Run 'make config-sample' first"; \
	fi

# Development setup
.PHONY: setup
setup: ## Setup development environment
	@echo "Setting up development environment..."
	go mod download
	@echo "Installing development tools..."
	@if ! command -v mockgen >/dev/null 2>&1; then \
		echo "Installing mockgen..."; \
		go install go.uber.org/mock/mockgen@latest; \
	fi
	@if ! command -v golangci-lint >/dev/null 2>&1; then \
		echo "Installing golangci-lint..."; \
		go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest; \
	fi
	@echo "Development environment setup complete!"

# Release targets
.PHONY: release-build
release-build: ## Build release binaries for multiple platforms
	@echo "Building release binaries..."
	@mkdir -p $(BUILD_DIR)/release
	# Linux
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o $(BUILD_DIR)/release/$(BINARY_NAME)-linux-amd64 ./$(CMD_DIR)
	GOOS=linux GOARCH=arm64 go build $(LDFLAGS) -o $(BUILD_DIR)/release/$(BINARY_NAME)-linux-arm64 ./$(CMD_DIR)
	# macOS
	GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o $(BUILD_DIR)/release/$(BINARY_NAME)-darwin-amd64 ./$(CMD_DIR)
	GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o $(BUILD_DIR)/release/$(BINARY_NAME)-darwin-arm64 ./$(CMD_DIR)
	# Windows
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o $(BUILD_DIR)/release/$(BINARY_NAME)-windows-amd64.exe ./$(CMD_DIR)
	@echo "Release binaries built in $(BUILD_DIR)/release/"

.PHONY: release-package
release-package: release-build ## Package release binaries
	@echo "Packaging release binaries..."
	@cd $(BUILD_DIR)/release && \
	for binary in $(BINARY_NAME)-*; do \
		if [[ $$binary == *.exe ]]; then \
			zip "$${binary%.exe}.zip" "$$binary"; \
		else \
			tar -czf "$$binary.tar.gz" "$$binary"; \
		fi; \
	done
	@echo "Release packages created in $(BUILD_DIR)/release/"

# CI/CD targets
.PHONY: ci
ci: deps vet test ## Run CI pipeline
	@echo "CI pipeline completed successfully!"

.PHONY: pre-commit
pre-commit: fmt vet test ## Run pre-commit checks
	@echo "Pre-commit checks passed!"

# Utility targets
.PHONY: version
version: ## Show version information
	@echo "Version: $(VERSION)"
	@echo "Go version: $(shell go version)"
	@echo "Git commit: $(shell git rev-parse --short HEAD 2>/dev/null || echo 'unknown')"

.PHONY: env
env: ## Show environment information
	@echo "GOPATH: $(GOPATH)"
	@echo "GOROOT: $(GOROOT)"
	@echo "GOOS: $(GOOS)"
	@echo "GOARCH: $(GOARCH)"
	@go env

# Documentation targets
.PHONY: docs
docs: ## Generate documentation
	@echo "Generating documentation..."
	@if command -v godoc >/dev/null 2>&1; then \
		echo "Documentation server available at: http://localhost:6060/pkg/intern/"; \
		echo "Run: godoc -http=:6060"; \
	else \
		echo "godoc not found. Install with: go install golang.org/x/tools/cmd/godoc@latest"; \
	fi

# Watch targets (requires entr or similar)
.PHONY: watch
watch: ## Watch for changes and rebuild
	@if command -v find >/dev/null 2>&1 && command -v entr >/dev/null 2>&1; then \
		find . -name "*.go" | entr -r make build; \
	else \
		echo "Watch requires 'entr'. Install with your package manager."; \
		echo "Example: brew install entr (macOS) or apt-get install entr (Ubuntu)"; \
	fi

.PHONY: watch-test
watch-test: ## Watch for changes and run tests
	@if command -v find >/dev/null 2>&1 && command -v entr >/dev/null 2>&1; then \
		find . -name "*.go" | entr -r make test; \
	else \
		echo "Watch requires 'entr'. Install with your package manager."; \
	fi
