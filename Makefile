.PHONY: build install test clean run dev

APP_NAME := just-talk-plus
CMD_DIR := ./cmd/just-talk-plus
BUILD_DIR := ./build

# Build for current platform
build:
	go build -o $(BUILD_DIR)/$(APP_NAME) $(CMD_DIR)

# Install to ~/.local/bin
install: build
	$(BUILD_DIR)/$(APP_NAME) --install

# Run (current platform) from a freshly built binary. Pass temporary diagnostics
# explicitly, for example: make run ARGS=--diagnostics
ARGS ?=
run: build
	$(BUILD_DIR)/$(APP_NAME) $(ARGS)

# Fast development loop. Requires https://github.com/air-verse/air.
# Pass application flags with: make dev ARGS="--no-tui --verbose"
dev:
	@command -v air >/dev/null 2>&1 || (echo "air is required: go install github.com/air-verse/air@latest"; exit 1)
	air -c .air.toml $(if $(ARGS),-build.args_bin "$(ARGS)",)

# Test
test:
	go test ./... -v

# Clean
clean:
	rm -rf $(BUILD_DIR)

# Install dependencies
deps:
	go mod tidy
	go mod download
