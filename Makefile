# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOTEST=$(GOCMD) test
GOGET=$(GOCMD) get
GOMOD=$(GOCMD) mod
GOLINT=golangci-lint

# Default target
.DEFAULT_GOAL := help

# Colors for output
GREEN=\033[0;32m
YELLOW=\033[1;33m
RED=\033[0;31m
NC=\033[0m # No Color

## Help
help: ## Show this help message
	@echo 'Usage: make [target]'
	@echo ''
	@echo 'Targets:'
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

## Test commands
test: ## Run all tests
	@echo "$(GREEN)Running tests...$(NC)"
	$(GOTEST) -v ./...

test-coverage: ## Run tests with coverage
	@echo "$(GREEN)Running tests with coverage...$(NC)"
	$(GOTEST) -v -race -coverprofile=coverage.out ./...
	$(GOCMD) tool cover -html=coverage.out -o coverage.html
	@echo "$(GREEN)Coverage report generated: coverage.html$(NC)"

test-race: ## Run tests with race detector
	@echo "$(GREEN)Running tests with race detector...$(NC)"
	$(GOTEST) -v -race ./...

test-bench: ## Run benchmarks
	@echo "$(GREEN)Running benchmarks...$(NC)"
	$(GOTEST) -bench=. -benchmem ./...

## Lint commands
lint: ## Run linter
	@echo "$(GREEN)Running linter...$(NC)"
	@if ! command -v $(GOLINT) > /dev/null 2>&1; then \
		echo "$(RED)golangci-lint not found. Installing...$(NC)"; \
		curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $$(go env GOPATH)/bin v1.54.2; \
	fi
	$(GOLINT) run

lint-fix: ## Run linter and fix issues automatically
	@echo "$(GREEN)Running linter with auto-fix...$(NC)"
	@if ! command -v $(GOLINT) > /dev/null 2>&1; then \
		echo "$(RED)golangci-lint not found. Installing...$(NC)"; \
		curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $$(go env GOPATH)/bin v1.54.2; \
	fi
	$(GOLINT) run --fix

## Combined commands
verify: test lint ## Run tests and linter
	@echo "$(GREEN)✓ All checks passed!$(NC)"

## Dependency management
deps: ## Download dependencies
	@echo "$(GREEN)Downloading dependencies...$(NC)"
	$(GOMOD) download

deps-update: ## Update dependencies
	@echo "$(GREEN)Updating dependencies...$(NC)"
	$(GOMOD) tidy
	$(GOGET) -u ./...

deps-verify: ## Verify dependencies
	@echo "$(GREEN)Verifying dependencies...$(NC)"
	$(GOMOD) verify

## Build commands
build: ## Build the project
	@echo "$(GREEN)Building...$(NC)"
	$(GOBUILD) -v ./...

clean: ## Clean build artifacts
	@echo "$(GREEN)Cleaning...$(NC)"
	$(GOCLEAN)
	rm -f coverage.out coverage.html

## CI/CD targets
ci: deps verify ## Run full CI pipeline
	@echo "$(GREEN)✓ CI pipeline completed successfully!$(NC)"

## Install tools
install-tools: ## Install development tools
	@echo "$(GREEN)Installing development tools...$(NC)"
	@if ! command -v $(GOLINT) > /dev/null 2>&1; then \
		echo "Installing golangci-lint..."; \
		curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $$(go env GOPATH)/bin v1.54.2; \
	fi
	@echo "$(GREEN)✓ Tools installed!$(NC)"

.PHONY: help test test-coverage test-race test-bench lint lint-fix verify deps deps-update deps-verify build clean ci install-tools