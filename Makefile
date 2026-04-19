# Makefile — acp
#
# All targets work on Linux, macOS, and Windows (GNU Make + Go 1.22+).
# On Windows install make via: winget install --id GnuWin32.Make -e
#
# Quick reference:
#   make              — build both binaries
#   make test         — unit tests
#   make test-integration-acp1 ACP1_TEST_HOST=192.168.1.5
#   make lint         — golangci-lint
#   make build-all    — cross-compile for all OS/arch
#   make package      — archived release artefacts under dist/
#   make clean        — remove bin/ and dist/

# ---------------------------------------------------------------- Variables

GO           ?= go
GOFLAGS      ?=
LDFLAGS      ?= -s -w
BIN_DIR      ?= bin
DIST_DIR     ?= dist
PKG          := ./...
CMD_ACP      := ./cmd/acp
CMD_SRV      := ./cmd/acp-srv

# Version injected into binaries via -ldflags. Uses git tag if available,
# otherwise "dev".
VERSION      ?= $(shell git describe --tags --dirty --always 2>/dev/null || echo dev)
COMMIT       ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILD_DATE   ?= $(shell date -u '+%Y-%m-%dT%H:%M:%SZ' 2>/dev/null || echo unknown)
LDFLAGS_FULL := $(LDFLAGS) -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(BUILD_DATE)

# Host binary suffix (.exe on Windows).
EXE :=
ifeq ($(OS),Windows_NT)
  EXE := .exe
endif

# ---------------------------------------------------------------- Default

.PHONY: all
all: build

# ---------------------------------------------------------------- Build

.PHONY: build build-cli build-srv
build: build-cli build-srv

build-cli:
	@mkdir -p $(BIN_DIR)
	$(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS_FULL)" -o $(BIN_DIR)/acp$(EXE) $(CMD_ACP)

build-srv:
	@mkdir -p $(BIN_DIR)
	$(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS_FULL)" -o $(BIN_DIR)/acp-srv$(EXE) $(CMD_SRV)

# ---------------------------------------------------------------- Cross-compile

.PHONY: build-all build-linux-amd64 build-linux-arm64 \
        build-darwin-amd64 build-darwin-arm64 build-windows-amd64

build-all: build-linux-amd64 build-linux-arm64 \
           build-darwin-amd64 build-darwin-arm64 \
           build-windows-amd64

define _xbuild
	@mkdir -p $(DIST_DIR)/acp_$(1)_$(2)
	GOOS=$(1) GOARCH=$(2) $(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS_FULL)" \
		-o $(DIST_DIR)/acp_$(1)_$(2)/acp$(3)     $(CMD_ACP)
	GOOS=$(1) GOARCH=$(2) $(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS_FULL)" \
		-o $(DIST_DIR)/acp_$(1)_$(2)/acp-srv$(3) $(CMD_SRV)
endef

build-linux-amd64:
	$(call _xbuild,linux,amd64,)

build-linux-arm64:
	$(call _xbuild,linux,arm64,)

build-darwin-amd64:
	$(call _xbuild,darwin,amd64,)

build-darwin-arm64:
	$(call _xbuild,darwin,arm64,)

build-windows-amd64:
	$(call _xbuild,windows,amd64,.exe)

# ---------------------------------------------------------------- Package

.PHONY: package
package: build-all
	@cd $(DIST_DIR) && for d in acp_linux_* acp_darwin_*; do \
		tar -czf $$d.tar.gz $$d; \
	done
	@cd $(DIST_DIR) && for d in acp_windows_*; do \
		zip -qr $$d.zip $$d; \
	done
	@ls -lh $(DIST_DIR)/*.tar.gz $(DIST_DIR)/*.zip 2>/dev/null || true

# ---------------------------------------------------------------- Test

.PHONY: test test-race test-cover test-integration \
        test-integration-acp1 test-integration-acp2

test:
	$(GO) test $(PKG)

test-race:
	$(GO) test -race $(PKG)

test-cover:
	$(GO) test -cover $(PKG)

# Integration targets use build tag "integration". Env vars gate execution
# — tests skip themselves if the host variable is unset.
test-integration: test-integration-acp1 test-integration-acp2

test-integration-acp1:
	$(GO) test -tags integration ./tests/integration/acp1/...

test-integration-acp2:
	$(GO) test -tags integration ./tests/integration/acp2/...

# ---------------------------------------------------------------- Fixtures

.PHONY: fixtures-emberplus fixtures-acp1 fixtures

# Re-extract all per-type fixtures from bin/*.pcapng sources.
# Requires Wireshark (tshark + editcap) on PATH. See
# tests/fixtures/protocol_types/<proto>/README.md for the fixture maps.
fixtures: fixtures-emberplus fixtures-acp1

fixtures-emberplus:
	@scripts/fixturize.sh bin/emberplus_glow_stream_subscribe_lua.pcapng   tests/fixtures/protocol_types/emberplus/root_node              1
	@scripts/fixturize.sh bin/emberplus_glow_mtx_labels_param_lua.pcapng   tests/fixtures/protocol_types/emberplus/qualified_node         582
	@scripts/fixturize.sh bin/emberplus_glow_glow_lua.pcapng               tests/fixtures/protocol_types/emberplus/parameter              19
	@scripts/fixturize.sh bin/emberplus_glow_mtx_labels_param_lua.pcapng   tests/fixtures/protocol_types/emberplus/qualified_parameter    19
	@scripts/fixturize.sh bin/emberplus_glow_mtx_lua.pcapng                tests/fixtures/protocol_types/emberplus/matrix                 41
	@scripts/fixturize.sh bin/emberplus_glow_mtx_lua.pcapng                tests/fixtures/protocol_types/emberplus/qualified_matrix       43
	@scripts/fixturize.sh bin/emberplus_glow_mtx_lua.pcapng                tests/fixtures/protocol_types/emberplus/matrix_connection      41
	@scripts/fixturize.sh bin/emberplus_glow_lua.pcapng                    tests/fixtures/protocol_types/emberplus/label                  127
	@scripts/fixturize.sh bin/emberplus_glow_lua.pcapng                    tests/fixtures/protocol_types/emberplus/stream_collection      9
	@scripts/fixturize.sh bin/emberplus_glow_lua.pcapng                    tests/fixtures/protocol_types/emberplus/command_get_directory  125
	@scripts/fixturize.sh bin/emberplus_glow_stream_subscribe_lua.pcapng   tests/fixtures/protocol_types/emberplus/command_subscribe      52
	@scripts/fixturize.sh bin/emberplus_glow_stream_subscribe_lua.pcapng   tests/fixtures/protocol_types/emberplus/command_unsubscribe    105
	@scripts/fixturize.sh bin/emberplus_glow_functions_lua.pcapng          tests/fixtures/protocol_types/emberplus/function_invoke        346
	@scripts/fixturize.sh bin/emberplus_glow_functions_lua.pcapng          tests/fixtures/protocol_types/emberplus/invocation_result      348

# Re-extract all ACP1 per-type fixtures from a Synapse-emulator walk.
fixtures-acp1:
	@scripts/fixturize.sh bin/acp1_walk_slot0_slot1.pcapng  tests/fixtures/protocol_types/acp1/root          6
	@scripts/fixturize.sh bin/acp1_walk_slot0_slot1.pcapng  tests/fixtures/protocol_types/acp1/integer       64
	@scripts/fixturize.sh bin/acp1_walk_slot0_slot1.pcapng  tests/fixtures/protocol_types/acp1/ip_address    26
	@scripts/fixturize.sh bin/acp1_walk_slot0_slot1.pcapng  tests/fixtures/protocol_types/acp1/float         68
	@scripts/fixturize.sh bin/acp1_walk_slot0_slot1.pcapng  tests/fixtures/protocol_types/acp1/enumerated    24
	@scripts/fixturize.sh bin/acp1_walk_slot0_slot1.pcapng  tests/fixtures/protocol_types/acp1/string        8
	@scripts/fixturize.sh bin/acp1_walk_slot0_slot1.pcapng  tests/fixtures/protocol_types/acp1/frame_status  2
	@scripts/fixturize.sh bin/acp1_walk_slot0_slot1.pcapng  tests/fixtures/protocol_types/acp1/alarm         114
	@scripts/fixturize.sh bin/acp1_walk_slot0_slot1.pcapng  tests/fixtures/protocol_types/acp1/long          400
	@scripts/fixturize.sh bin/acp1_walk_slot0_slot1.pcapng  tests/fixtures/protocol_types/acp1/byte          38
	@scripts/fixturize.sh bin/acp1_walk_slot0_slot1.pcapng  tests/fixtures/protocol_types/acp1/request       1
	@scripts/fixturize.sh bin/acp1_walk_slot0_slot1.pcapng  tests/fixtures/protocol_types/acp1/reply         2
	@scripts/fixturize.sh bin/acp1_walk_slot0_slot1.pcapng  tests/fixtures/protocol_types/acp1/error         1274

# ---------------------------------------------------------------- Lint / vet / fmt

.PHONY: lint vet fmt fmt-check tidy

lint:
	golangci-lint run $(PKG)

vet:
	$(GO) vet $(PKG)

fmt:
	goimports -w .

fmt-check:
	@out=$$(goimports -l .); \
	if [ -n "$$out" ]; then \
		echo "files need formatting:"; echo "$$out"; exit 1; \
	fi

tidy:
	$(GO) mod tidy

# ---------------------------------------------------------------- Run

.PHONY: run run-srv

run: build-cli
	./$(BIN_DIR)/acp$(EXE) $(ARGS)

run-srv: build-srv
	./$(BIN_DIR)/acp-srv$(EXE) --addr :8080 --log-level debug

# ---------------------------------------------------------------- Setup

.PHONY: setup
setup:
	git config core.hooksPath .githooks
	@echo "pre-commit hook enabled (go vet + golangci-lint)"

# ---------------------------------------------------------------- Clean

.PHONY: clean
clean:
	rm -rf $(BIN_DIR) $(DIST_DIR)

# ---------------------------------------------------------------- Help

.PHONY: help
help:
	@echo "acp Makefile targets:"
	@echo "  build                   build both binaries to bin/"
	@echo "  build-cli / build-srv   build one binary"
	@echo "  build-all               cross-compile for linux/darwin/windows (amd64+arm64)"
	@echo "  package                 tar.gz / zip archives in dist/"
	@echo "  test                    unit tests"
	@echo "  test-race               unit tests with -race"
	@echo "  test-cover              unit tests with coverage"
	@echo "  test-integration        ACP1 + ACP2 integration tests (needs *_TEST_HOST)"
	@echo "  lint / vet / fmt-check  static analysis"
	@echo "  fmt / tidy              auto-format + go mod tidy"
	@echo "  run / run-srv           build and run locally"
	@echo "  setup                   enable pre-commit hook (go vet + lint)"
	@echo "  clean                   remove bin/ and dist/"
