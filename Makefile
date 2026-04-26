# Makefile — dhs (Device Hub Systems)
#
# All targets work on Linux, macOS, and Windows (GNU Make + Go 1.22+).
# On Windows install make via: winget install --id GnuWin32.Make -e
#
# Quick reference:
#   make              — build the dhs binary
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
CMD_DHS      := ./cmd/dhs

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

.PHONY: build
build:
	@mkdir -p $(BIN_DIR)
	$(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS_FULL)" -o $(BIN_DIR)/dhs$(EXE) $(CMD_DHS)

# ---------------------------------------------------------------- Cross-compile

.PHONY: build-all build-linux-amd64 build-linux-arm64 \
        build-darwin-amd64 build-darwin-arm64 build-windows-amd64

build-all: build-linux-amd64 build-linux-arm64 \
           build-darwin-amd64 build-darwin-arm64 \
           build-windows-amd64

define _xbuild
	@mkdir -p $(DIST_DIR)/dhs_$(1)_$(2)
	GOOS=$(1) GOARCH=$(2) $(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS_FULL)" \
		-o $(DIST_DIR)/dhs_$(1)_$(2)/dhs$(3) $(CMD_DHS)
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
	@cd $(DIST_DIR) && for d in dhs_linux_* dhs_darwin_*; do \
		tar -czf $$d.tar.gz $$d; \
	done
	@cd $(DIST_DIR) && for d in dhs_windows_*; do \
		zip -qr $$d.zip $$d; \
	done
	@ls -lh $(DIST_DIR)/*.tar.gz $(DIST_DIR)/*.zip 2>/dev/null || true

# ---------------------------------------------------------------- Test

.PHONY: test test-race test-cover test-integration \
        test-integration-acp1 test-integration-acp2 \
        test-conformance-nmos

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
	$(GO) test -tags integration ./internal/acp1/smoke/... ./internal/acp1/integration/...

test-integration-acp2:
	$(GO) test -tags integration ./internal/acp2/integration/...

# AMWA NMOS conformance — runs the AMWA NMOS Testing tool against dhs
# in an isolated docker-compose bridge per
# internal/amwa/docs/conformance.md. Phase 1 step #1 ships only the
# harness skeleton (smoke-test the docker-compose bring-up + image
# pull); later phases add per-suite directories (01-discovery,
# 02-is09, 03-is04-node, ...).
test-conformance-nmos: SUITE_DIR ?= tests/integration/nmos/_template
test-conformance-nmos:
	bash scripts/nmos-run-suite.sh $(SUITE_DIR)

# ---------------------------------------------------------------- Fixtures

.PHONY: fixtures-emberplus fixtures-acp1 fixtures-acp2 fixtures

# Re-extract all per-type fixtures from bin/*.pcapng sources.
# Requires Wireshark (tshark + editcap) on PATH. See
# internal/<proto>/testdata/protocol_types/README.md for the fixture maps.
fixtures: fixtures-emberplus fixtures-acp1 fixtures-acp2

fixtures-emberplus:
	@scripts/fixturize.sh bin/emberplus_glow_stream_subscribe_lua.pcapng   internal/emberplus/testdata/protocol_types/root_node              1
	@scripts/fixturize.sh bin/emberplus_glow_mtx_labels_param_lua.pcapng   internal/emberplus/testdata/protocol_types/qualified_node         582
	@scripts/fixturize.sh bin/emberplus_glow_glow_lua.pcapng               internal/emberplus/testdata/protocol_types/parameter              19
	@scripts/fixturize.sh bin/emberplus_glow_mtx_labels_param_lua.pcapng   internal/emberplus/testdata/protocol_types/qualified_parameter    19
	@scripts/fixturize.sh bin/emberplus_glow_mtx_lua.pcapng                internal/emberplus/testdata/protocol_types/matrix                 41
	@scripts/fixturize.sh bin/emberplus_glow_mtx_lua.pcapng                internal/emberplus/testdata/protocol_types/qualified_matrix       43
	@scripts/fixturize.sh bin/emberplus_glow_mtx_lua.pcapng                internal/emberplus/testdata/protocol_types/matrix_connection      41
	@scripts/fixturize.sh bin/emberplus_glow_lua.pcapng                    internal/emberplus/testdata/protocol_types/label                  127
	@scripts/fixturize.sh bin/emberplus_glow_lua.pcapng                    internal/emberplus/testdata/protocol_types/stream_collection      9
	@scripts/fixturize.sh bin/emberplus_glow_lua.pcapng                    internal/emberplus/testdata/protocol_types/command_get_directory  125
	@scripts/fixturize.sh bin/emberplus_glow_stream_subscribe_lua.pcapng   internal/emberplus/testdata/protocol_types/command_subscribe      52
	@scripts/fixturize.sh bin/emberplus_glow_stream_subscribe_lua.pcapng   internal/emberplus/testdata/protocol_types/command_unsubscribe    105
	@scripts/fixturize.sh bin/emberplus_glow_functions_lua.pcapng          internal/emberplus/testdata/protocol_types/function_invoke        346
	@scripts/fixturize.sh bin/emberplus_glow_functions_lua.pcapng          internal/emberplus/testdata/protocol_types/invocation_result      348

# Re-extract all ACP1 per-type fixtures from a Synapse-emulator walk.
fixtures-acp1:
	@scripts/fixturize.sh bin/acp1_walk_slot0_slot1.pcapng  internal/acp1/testdata/protocol_types/root          6
	@scripts/fixturize.sh bin/acp1_walk_slot0_slot1.pcapng  internal/acp1/testdata/protocol_types/integer       64
	@scripts/fixturize.sh bin/acp1_walk_slot0_slot1.pcapng  internal/acp1/testdata/protocol_types/ip_address    26
	@scripts/fixturize.sh bin/acp1_walk_slot0_slot1.pcapng  internal/acp1/testdata/protocol_types/float         68
	@scripts/fixturize.sh bin/acp1_walk_slot0_slot1.pcapng  internal/acp1/testdata/protocol_types/enumerated    24
	@scripts/fixturize.sh bin/acp1_walk_slot0_slot1.pcapng  internal/acp1/testdata/protocol_types/string        8
	@scripts/fixturize.sh bin/acp1_walk_slot0_slot1.pcapng  internal/acp1/testdata/protocol_types/frame_status  2
	@scripts/fixturize.sh bin/acp1_walk_slot0_slot1.pcapng  internal/acp1/testdata/protocol_types/alarm         114
	@scripts/fixturize.sh bin/acp1_walk_slot0_slot1.pcapng  internal/acp1/testdata/protocol_types/long          400
	@scripts/fixturize.sh bin/acp1_walk_slot0_slot1.pcapng  internal/acp1/testdata/protocol_types/byte          38
	@scripts/fixturize.sh bin/acp1_walk_slot0_slot1.pcapng  internal/acp1/testdata/protocol_types/request       1
	@scripts/fixturize.sh bin/acp1_walk_slot0_slot1.pcapng  internal/acp1/testdata/protocol_types/reply         2
	@scripts/fixturize.sh bin/acp1_walk_slot0_slot1.pcapng  internal/acp1/testdata/protocol_types/error         1274

# Re-extract all ACP2 per-type fixtures from a self-driven loopback capture.
# Run scripts/capture-acp2-fixtures.sh first to regenerate bin/acp2_fixtures.pcapng,
# then this target slims each fixture to its frame-list. Frame numbers are
# deterministic for a given fixture_tree.json + capture script pair; if they
# drift after editing either file, re-read the capture with tshark -V and
# update this list.
fixtures-acp2:
	@scripts/fixturize.sh bin/acp2_fixtures.pcapng  internal/acp2/testdata/protocol_types/node                  34
	@scripts/fixturize.sh bin/acp2_fixtures.pcapng  internal/acp2/testdata/protocol_types/string                42
	@scripts/fixturize.sh bin/acp2_fixtures.pcapng  internal/acp2/testdata/protocol_types/number                46
	@scripts/fixturize.sh bin/acp2_fixtures.pcapng  internal/acp2/testdata/protocol_types/enum                  50
	@scripts/fixturize.sh bin/acp2_fixtures.pcapng  internal/acp2/testdata/protocol_types/ipv4                  54
	@scripts/fixturize.sh bin/acp2_fixtures.pcapng  internal/acp2/testdata/protocol_types/preset                58
	@scripts/fixturize.sh bin/acp2_fixtures.pcapng  internal/acp2/testdata/protocol_types/get_version           28 30
	@scripts/fixturize.sh bin/acp2_fixtures.pcapng  internal/acp2/testdata/protocol_types/get_object            44 46
	@scripts/fixturize.sh bin/acp2_fixtures.pcapng  internal/acp2/testdata/protocol_types/get_property          229 231
	@scripts/fixturize.sh bin/acp2_fixtures.pcapng  internal/acp2/testdata/protocol_types/set_property          296 298
	@scripts/fixturize.sh bin/acp2_fixtures.pcapng  internal/acp2/testdata/protocol_types/announce              300
	@scripts/fixturize.sh bin/acp2_fixtures.pcapng  internal/acp2/testdata/protocol_types/error_protocol        741 743
	@scripts/fixturize.sh bin/acp2_fixtures.pcapng  internal/acp2/testdata/protocol_types/error_invalid_obj_id  721 723
	@scripts/fixturize.sh bin/acp2_fixtures.pcapng  internal/acp2/testdata/protocol_types/error_invalid_idx     611 613
	@scripts/fixturize.sh bin/acp2_fixtures.pcapng  internal/acp2/testdata/protocol_types/error_invalid_pid     678 680
	@scripts/fixturize.sh bin/acp2_fixtures.pcapng  internal/acp2/testdata/protocol_types/error_no_access       477 479
	@scripts/fixturize.sh bin/acp2_fixtures.pcapng  internal/acp2/testdata/protocol_types/error_invalid_value   544 546

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

.PHONY: run

run: build
	./$(BIN_DIR)/dhs$(EXE) $(ARGS)

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
	@echo "dhs Makefile targets:"
	@echo "  build                   build the dhs binary to bin/"
	@echo "  build-all               cross-compile for linux/darwin/windows (amd64+arm64)"
	@echo "  package                 tar.gz / zip archives in dist/"
	@echo "  test                    unit tests"
	@echo "  test-race               unit tests with -race"
	@echo "  test-cover              unit tests with coverage"
	@echo "  test-integration        ACP1 + ACP2 integration tests (needs *_TEST_HOST)"
	@echo "  lint / vet / fmt-check  static analysis"
	@echo "  fmt / tidy              auto-format + go mod tidy"
	@echo "  run                     build and run locally (ARGS=...)"
	@echo "  setup                   enable pre-commit hook (go vet + lint)"
	@echo "  clean                   remove bin/ and dist/"
