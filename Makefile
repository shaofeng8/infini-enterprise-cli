APP_NAME    := infini-cli
MODULE      := github.com/chaozwn/infini-enterprise-cli
VERSION     := $(shell git describe --tags --dirty 2>/dev/null || echo "0.1.0")
COMMIT      := $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
BUILD_DATE  := $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')
BUILD_DIR   := build

LDFLAGS := -s -w \
	-X '$(MODULE)/cmd.Version=$(VERSION)' \
	-X '$(MODULE)/cmd.Commit=$(COMMIT)' \
	-X '$(MODULE)/cmd.BuildDate=$(BUILD_DATE)'

PLATFORMS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64

.PHONY: all build run install test lint clean cross release help

all: clean build

build:
	@echo "==> Building $(APP_NAME) $(VERSION) ..."
	@mkdir -p $(BUILD_DIR)
	go build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(APP_NAME) .

run: build
	@$(BUILD_DIR)/$(APP_NAME)

install:
	@echo "==> Installing $(APP_NAME) ..."
	go install -ldflags "$(LDFLAGS)" .

test:
	@echo "==> Running tests ..."
	go test ./...

lint:
	@echo "==> Vetting ..."
	go vet ./...

clean:
	@echo "==> Cleaning ..."
	@rm -rf $(BUILD_DIR)

cross: clean
	@echo "==> Cross-compiling $(APP_NAME) $(VERSION) ..."
	@mkdir -p $(BUILD_DIR)
	@$(foreach platform,$(PLATFORMS),\
		$(eval OS   := $(word 1,$(subst /, ,$(platform)))) \
		$(eval ARCH := $(word 2,$(subst /, ,$(platform)))) \
		$(eval EXT  := $(if $(filter windows,$(OS)),.exe,)) \
		echo "  -> $(OS)/$(ARCH)" && \
		mkdir -p $(BUILD_DIR)/$(OS)-$(ARCH) && \
		GOOS=$(OS) GOARCH=$(ARCH) go build -ldflags "$(LDFLAGS)" \
			-o $(BUILD_DIR)/$(OS)-$(ARCH)/$(APP_NAME)$(EXT) . ; \
	)

# Builds every platform and writes latest.json alongside them, so $(BUILD_DIR)
# can be served as an update channel as-is. Runs anywhere the Go toolchain
# does, including Windows without make: go run ./scripts/release --version X
release:
	@go run ./scripts/release --version $(VERSION) --notes "$(NOTES)"

help:
	@echo "Usage:"
	@echo "  make build    - Build for the current platform"
	@echo "  make run      - Build and run"
	@echo "  make install  - Install into GOPATH/bin"
	@echo "  make test     - Run tests"
	@echo "  make lint     - Run go vet"
	@echo "  make cross    - Cross-compile for linux/darwin/windows on amd64/arm64"
	@echo "  make release  - Cross-compile and write an update channel (latest.json)"
	@echo "  make clean    - Remove build artifacts"
