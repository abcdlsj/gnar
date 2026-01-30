# Gnar Makefile v2
# Build automation for gnar v2

# Variables
BINARY_NAME := gnar
SERVER_BINARY := gnar-server
VERSION := 2.0.0
BUILD_TIME := $(shell date -u '+%Y-%m-%d_%H:%M:%S')
GIT_COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")

# Build flags
LDFLAGS := -ldflags "-X main.Version=$(VERSION) -X main.BuildTime=$(BUILD_TIME) -X main.GitCommit=$(GIT_COMMIT)"
GOPROXY := https://goproxy.cn,direct

# Go commands
GOCMD := go
GOBUILD := $(GOCMD) build
GOCLEAN := $(GOCMD) clean
GOTEST := $(GOCMD) test
GOMOD := $(GOCMD) mod

# Directories
CMD_DIR := ./cmd
DIST_DIR := ./dist

# Default target
.DEFAULT_GOAL := help

# Build both binaries
.PHONY: build
build: build-client build-server

# Build client binary
.PHONY: build-client
build-client:
	@echo "Building $(BINARY_NAME) client..."
	@mkdir -p $(DIST_DIR)
	@GOPROXY=$(GOPROXY) $(GOBUILD) $(LDFLAGS) -o $(DIST_DIR)/$(BINARY_NAME) $(CMD_DIR)/$(BINARY_NAME)
	@echo "✓ Built: $(DIST_DIR)/$(BINARY_NAME)"

# Build server binary
.PHONY: build-server
build-server:
	@echo "Building $(SERVER_BINARY)..."
	@mkdir -p $(DIST_DIR)
	@GOPROXY=$(GOPROXY) $(GOBUILD) $(LDFLAGS) -o $(DIST_DIR)/$(SERVER_BINARY) $(CMD_DIR)/$(SERVER_BINARY)
	@echo "✓ Built: $(DIST_DIR)/$(SERVER_BINARY)"

# Build for multiple platforms
.PHONY: build-all
build-all: build-linux build-darwin build-windows

# Build for Linux
.PHONY: build-linux
build-linux:
	@echo "Building for Linux..."
	@mkdir -p $(DIST_DIR)/linux
	@GOPROXY=$(GOPROXY) GOOS=linux GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o $(DIST_DIR)/linux/$(BINARY_NAME) $(CMD_DIR)/$(BINARY_NAME)
	@GOPROXY=$(GOPROXY) GOOS=linux GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o $(DIST_DIR)/linux/$(SERVER_BINARY) $(CMD_DIR)/$(SERVER_BINARY)
	@echo "✓ Linux binaries: $(DIST_DIR)/linux/"

# Build for macOS
.PHONY: build-darwin
build-darwin:
	@echo "Building for macOS..."
	@mkdir -p $(DIST_DIR)/darwin
	@GOPROXY=$(GOPROXY) GOOS=darwin GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o $(DIST_DIR)/darwin/$(BINARY_NAME) $(CMD_DIR)/$(BINARY_NAME)
	@GOPROXY=$(GOPROXY) GOOS=darwin GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o $(DIST_DIR)/darwin/$(SERVER_BINARY) $(CMD_DIR)/$(SERVER_BINARY)
	@echo "✓ macOS binaries: $(DIST_DIR)/darwin/"

# Build for Windows
.PHONY: build-windows
build-windows:
	@echo "Building for Windows..."
	@mkdir -p $(DIST_DIR)/windows
	@GOPROXY=$(GOPROXY) GOOS=windows GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o $(DIST_DIR)/windows/$(BINARY_NAME).exe $(CMD_DIR)/$(BINARY_NAME)
	@GOPROXY=$(GOPROXY) GOOS=windows GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o $(DIST_DIR)/windows/$(SERVER_BINARY).exe $(CMD_DIR)/$(SERVER_BINARY)
	@echo "✓ Windows binaries: $(DIST_DIR)/windows/"

# Run tests
.PHONY: test
test:
	@echo "Running tests..."
	@GOPROXY=$(GOPROXY) $(GOTEST) -v ./...

# Run tests with coverage
.PHONY: test-coverage
test-coverage:
	@echo "Running tests with coverage..."
	@GOPROXY=$(GOPROXY) $(GOTEST) -v -coverprofile=coverage.out ./...
	@$(GOCMD) tool cover -html=coverage.out -o coverage.html
	@echo "✓ Coverage report: coverage.html"

# Download dependencies
.PHONY: deps
deps:
	@echo "Downloading dependencies..."
	@GOPROXY=$(GOPROXY) $(GOMOD) download
	@echo "✓ Dependencies downloaded"

# Tidy dependencies
.PHONY: tidy
tidy:
	@echo "Tidying dependencies..."
	@GOPROXY=$(GOPROXY) $(GOMOD) tidy
	@echo "✓ Dependencies tidied"

# Clean build artifacts
.PHONY: clean
clean:
	@echo "Cleaning..."
	@rm -rf $(DIST_DIR)
	@rm -f coverage.out coverage.html
	@rm -f ./gnar
	@$(GOCLEAN)
	@echo "✓ Cleaned"

# Install binaries to GOPATH/bin
.PHONY: install
install: build
	@echo "Installing binaries..."
	@cp $(DIST_DIR)/$(BINARY_NAME) $(GOPATH)/bin/ 2>/dev/null || cp $(DIST_DIR)/$(BINARY_NAME) ~/go/bin/ 2>/dev/null || echo "Please manually copy binaries to your PATH"
	@echo "✓ Installed $(BINARY_NAME)"

# Uninstall binaries
.PHONY: uninstall
uninstall:
	@echo "Uninstalling..."
	@rm -f $(GOPATH)/bin/$(BINARY_NAME) 2>/dev/null || rm -f ~/go/bin/$(BINARY_NAME) 2>/dev/null || true
	@echo "✓ Uninstalled"

# Format code
.PHONY: fmt
fmt:
	@echo "Formatting code..."
	@gofmt -s -w .
	@echo "✓ Code formatted"

# Run linter
.PHONY: lint
lint:
	@echo "Running linter..."
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run ./...; \
	else \
		echo "golangci-lint not installed. Install from https://golangci-lint.run/"; \
	fi

# Development mode - build and run client
.PHONY: dev-client
dev-client: build-client
	@echo "Running client..."
	@$(DIST_DIR)/$(BINARY_NAME)

# Development mode - build and run server
.PHONY: dev-server
dev-server: build-server
	@echo "Running server..."
	@$(DIST_DIR)/$(SERVER_BINARY) -domain=localhost

# Show help
.PHONY: help
help:
	@echo "Gnar v$(VERSION) - Build Automation"
	@echo ""
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@echo "  build            Build both binaries (default)"
	@echo "  build-client     Build client binary only"
	@echo "  build-server     Build server binary only"
	@echo "  build-all        Build for all platforms"
	@echo "  test             Run all tests"
	@echo "  test-coverage    Run tests with coverage report"
	@echo "  deps             Download dependencies"
	@echo "  tidy             Tidy go.mod and go.sum"
	@echo "  clean            Remove build artifacts"
	@echo "  install          Install binaries"
	@echo "  uninstall        Remove installed binaries"
	@echo "  fmt              Format Go code"
	@echo "  lint             Run golangci-lint"
	@echo "  dev-client       Build and run client"
	@echo "  dev-server       Build and run server"
	@echo "  help             Show this help message"
	@echo ""
	@echo "Output directory: $(DIST_DIR)"

# CI/CD targets
.PHONY: ci
ci: tidy fmt test build-all

# Release target
.PHONY: release
release: clean ci
	@echo "Creating release artifacts..."
	@mkdir -p $(DIST_DIR)/release
	@tar -czf $(DIST_DIR)/release/$(BINARY_NAME)-$(VERSION)-linux-amd64.tar.gz -C $(DIST_DIR)/linux $(BINARY_NAME) $(SERVER_BINARY)
	@tar -czf $(DIST_DIR)/release/$(BINARY_NAME)-$(VERSION)-darwin-amd64.tar.gz -C $(DIST_DIR)/darwin $(BINARY_NAME) $(SERVER_BINARY)
	@zip -j $(DIST_DIR)/release/$(BINARY_NAME)-$(VERSION)-windows-amd64.zip $(DIST_DIR)/windows/*.exe
	@echo "✓ Release artifacts created in $(DIST_DIR)/release/"
