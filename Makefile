.PHONY: build run clean test lint install-service uninstall-service

APP_NAME := kimchi
BUILD_DIR := bin
MAIN := .
SYSTEMD_DIR := $(HOME)/.config/systemd/user
SERVICE_FILE := $(APP_NAME).service
SERVICE_DEST := $(SYSTEMD_DIR)/$(SERVICE_FILE)

# Build flags
LDFLAGS := -s -w -X main.version=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_FLAGS := -ldflags "$(LDFLAGS)"

# Default target
all: build

# Build binary
build:
	@echo "Building $(APP_NAME)..."
	@mkdir -p $(BUILD_DIR)
	go build $(BUILD_FLAGS) -o $(BUILD_DIR)/$(APP_NAME) $(MAIN)
	@echo "Built: $(BUILD_DIR)/$(APP_NAME)"

# Run locally
run: build
	@echo "Running $(APP_NAME) on :18998..."
	SERVER_ADDR=:18998 ./$(BUILD_DIR)/$(APP_NAME)

# Run without building
run-dev:
	@echo "Running $(APP_NAME) on :18998..."
	SERVER_ADDR=:18998 go run $(MAIN)

# Clean build artifacts
clean:
	@echo "Cleaning..."
	rm -rf $(BUILD_DIR)
	@echo "Done"

# Run tests
test:
	go test ./... -count=1 -v

# Run tests (short)
test-short:
	go test ./... -count=1

# Run go vet
vet:
	go vet ./...

# Lint
lint: vet
	@which golangci-lint > /dev/null 2>&1 || echo "golangci-lint not installed"
	@golangci-lint run ./... 2>/dev/null || true

# Install to GOPATH
install: build
	@echo "Installing $(APP_NAME)..."
	go install $(BUILD_FLAGS) $(MAIN)

# Dev mode with auto-reload (requires air)
dev:
	@which air > /dev/null 2>&1 || go install github.com/air-verse/air@latest
	air

# Show help
help:
	@echo "Targets:"
	@echo "  build             - Build binary to $(BUILD_DIR)/"
	@echo "  run               - Build and run on :18998"
	@echo "  run-dev           - Run without building (go run)"
	@echo "  clean             - Remove build artifacts"
	@echo "  test              - Run all tests with verbose output"
	@echo "  test-short        - Run all tests (short mode)"
	@echo "  vet               - Run go vet"
	@echo "  lint              - Run linter"
	@echo "  install           - Install to GOPATH/bin"
	@echo "  dev               - Run with auto-reload (requires air)"
	@echo "  install-service   - Install systemd user service"
	@echo "  uninstall-service - Uninstall systemd user service"
	@echo "  help              - Show this help"

# Install systemd service (only if changed)
install-service: build
	@echo "Installing systemd service..."
	@mkdir -p $(SYSTEMD_DIR)
	@if [ -f $(SERVICE_DEST) ] && diff -q $(SERVICE_FILE) $(SERVICE_DEST) > /dev/null 2>&1; then \
		echo "Service file unchanged, skipping copy"; \
	else \
		cp $(SERVICE_FILE) $(SERVICE_DEST); \
		echo "Copied $(SERVICE_FILE) -> $(SERVICE_DEST)"; \
	fi
	@systemctl --user daemon-reload
	@systemctl --user enable $(SERVICE_FILE)
	@systemctl --user restart $(SERVICE_FILE)
	@echo "Service installed and started"
	@systemctl --user status $(SERVICE_FILE) --no-pager

# Uninstall systemd service
uninstall-service:
	@echo "Uninstalling systemd service..."
	@systemctl --user stop $(SERVICE_FILE) 2>/dev/null || true
	@systemctl --user disable $(SERVICE_FILE) 2>/dev/null || true
	@rm -f $(SERVICE_DEST)
	@systemctl --user daemon-reload
	@echo "Service uninstalled"
