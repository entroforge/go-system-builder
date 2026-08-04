GO ?= go
HARNESS_BIN ?= .claude/bin/loop-harness

.PHONY: build build-all manual fmt-check vet test test-race verify doctor validate ci-verify release-graph release-graph-validate release release-checksum release-list clean-release

# Local dev build (host platform only). Used by `verify`, `ci-verify`, and
# other targets that need a runnable binary on this host.
build:
	mkdir -p $(dir $(HARNESS_BIN))
	$(GO) build -o $(HARNESS_BIN) ./cmd/loop-harness

# Regenerate the checked-in, agent-facing Manual from the executable Loop
# Definition and the compiled guard/action documentation registries.
manual:
	$(GO) run ./cmd/loop-harness manual --root . --target loop-harness.md

# Cross-compile the Harness for every release platform. CGO is disabled so
# each binary is statically linked against the Go runtime and has no host
# libc dependency; -trimpath strips the build host's working directory for
# reproducible builds; -s -w strips symbol/debug tables to keep the
# tarball small. Output names match INSTALL.md's uname-based dispatch.
BUILD_DIR ?= dist/bin

build-all:
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 GOOS=darwin  GOARCH=arm64 $(GO) build -trimpath -ldflags="-s -w" -o $(BUILD_DIR)/loop-harness-darwin-arm64      ./cmd/loop-harness
	CGO_ENABLED=0 GOOS=linux   GOARCH=amd64 $(GO) build -trimpath -ldflags="-s -w" -o $(BUILD_DIR)/loop-harness-linux-amd64       ./cmd/loop-harness
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 $(GO) build -trimpath -ldflags="-s -w" -o $(BUILD_DIR)/loop-harness-windows-amd64.exe ./cmd/loop-harness

fmt-check:
	@test -z "$$(gofmt -l cmd internal tests)" || { gofmt -l cmd internal tests; exit 1; }

vet:
	$(GO) vet ./...

test:
	$(GO) test ./...

test-race:
	$(GO) test -race ./...

doctor:
	$(GO) run ./cmd/loop-harness doctor --root .

validate:
	$(GO) run ./cmd/loop-harness validate --all --root .

release-graph-validate: build
	@set -e; \
	version=release-graph-check; \
	dist=$$(mktemp -d); \
	tarball=$$dist/vibe-coding-loop-template-$$version.tar.gz; \
	bash packaging/build-release.sh $$version $$tarball >/dev/null; \
	stage=$$(mktemp -d); \
	tar -xzf $$tarball -C $$stage; \
	cp $(HARNESS_BIN) $$stage/vibe-coding-loop-template-$$version/.claude/bin/loop-harness; \
	$(HARNESS_BIN) release-graph validate --root $$stage/vibe-coding-loop-template-$$version; \
	rm -rf $$dist $$stage

# ci-verify runs the full CI matrix used by .github/workflows/verify.yml:
# format check, static analysis, unit tests (with race), the in-tree doctor
# against the source tree, the doctor against the staged release tree, the
# full validate --all, and the release-graph validator. Any failure
# surfaces as a non-zero exit.
ci-verify: fmt-check vet test test-race doctor doctor-staged validate release-graph-validate build

# doctor-staged builds the release tarball, extracts it to a tmpdir, and
# runs `loop-harness doctor` against the extracted tree to verify the
# shipped template itself passes every check (schemas, examples, runtime
# reachability, inline methodology fingerprints).
doctor-staged: build
	@set -e; \
	version=$$(git describe --tags --always --dirty 2>/dev/null || echo dev); \
	dist=$$(mktemp -d); \
	tarball=$$dist/vibe-coding-loop-template-$$version.tar.gz; \
	bash packaging/build-release.sh $$version $$tarball >/dev/null; \
	stage=$$(mktemp -d); \
	tar -xzf $$tarball -C $$stage; \
	stage_root=$$stage/vibe-coding-loop-template-$$version; \
	cp $(HARNESS_BIN) $$stage_root/.claude/bin/loop-harness; \
	$(HARNESS_BIN) release-graph validate --root $$stage_root; \
	$$stage_root/.claude/bin/loop-harness init --root $$stage_root; \
	$$stage_root/.claude/bin/loop-harness doctor --root $$stage_root; \
	$$stage_root/.claude/bin/loop-harness validate --all --root $$stage_root; \
	rm -rf $$dist $$stage

verify: fmt-check vet test test-race doctor validate doctor-staged

# --- Release packaging ------------------------------------------------------
#
# Builds a distributable tarball containing only template assets (no Go source,
# no REQ-002 instance runtime, no tests). The include list is an explicit
# whitelist in packaging/include.txt.

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
DIST ?= dist
TARBALL := $(DIST)/vibe-coding-loop-template-$(VERSION).tar.gz

# Strip comment and blank lines from the include list.
INCLUDE := $(shell grep -v '^[[:space:]]*\#' packaging/include.txt | grep -v '^[[:space:]]*$$')

# `release` cross-compiles all three platform binaries via `build-all` and
# then stages them into the tarball. We deliberately do NOT depend on
# `build` here: the tarball ships no host-platform binary, and build-all
# already produces the host's binary as a side effect of the matching
# cross-compile line.
release: build-all $(TARBALL)

$(TARBALL): $(INCLUDE) packaging/install.md packaging/build-release.sh
	@bash packaging/build-release.sh "$(VERSION)" "$(TARBALL)"

release-list:
	@echo "Tarball will include:"
	@echo "$(INCLUDE)" | sed 's/ /\n  /g'

release-checksum: release
	@cd $(DIST) && shasum -a 256 vibe-coding-loop-template-$(VERSION).tar.gz > sha256sums-$(VERSION).txt
	@echo "Wrote $(DIST)/sha256sums-$(VERSION).txt"

clean-release:
	@rm -rf $(DIST)
