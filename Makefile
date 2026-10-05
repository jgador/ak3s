# Default settings can be overridden, for example: make build VERSION=v1.2.3
# ?= sets a value only when it has not already been provided.
GO ?= go
VERSION ?= dev

# := evaluates this command once when Make reads the file.
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)

# Strip symbol/debug tables (-s -w) and embed version/commit metadata (-X).
LDFLAGS = -s -w -X github.com/jgador/ak3s/internal/ak3s.Version=$(VERSION) -X github.com/jgador/ak3s/internal/ak3s.Commit=$(COMMIT)

# These names are tasks, so run them even if a file has the same name.
.PHONY: build test verify release

# Build bin/ak3s for the current OS and architecture without C bindings.
build:

# -trimpath removes local build paths. Recipe commands must start with a tab.
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags '$(LDFLAGS)' -o bin/ak3s ./cmd/ak3s

# Run all Go package tests with race detection and coverage reporting.
test:
	$(GO) test -race -cover ./...

# Run test first, then check code, formatting, and installer shell syntax.
verify: test
	$(GO) vet ./...

# go fmt fixes formatting; this task fails if it had to change any files.
	test -z "$$($(GO) fmt ./...)"
	bash -n install.sh

# Cross-compile static Linux binaries for amd64 and arm64 into dist/.
release:
	mkdir -p dist
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -trimpath -ldflags '$(LDFLAGS)' -o dist/ak3s_linux_amd64 ./cmd/ak3s
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(GO) build -trimpath -ldflags '$(LDFLAGS)' -o dist/ak3s_linux_arm64 ./cmd/ak3s

# Include the installer and SHA-256 checksums for all three release files.
	cp install.sh dist/install.sh
	cd dist && sha256sum ak3s_linux_amd64 ak3s_linux_arm64 install.sh > checksums.txt
