# Default settings can be overridden, for example: make build VERSION=v1.2.3
# ?= sets a value only when it has not already been provided.
GO ?= go
VERSION ?= dev
DASHBOARD_IMAGE ?= ghcr.io/jgador/ak3s-dashboard:$(VERSION)
AK3S_CONFIG ?= /etc/ak3s/values.yaml

# := evaluates this command once when Make reads the file.
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)

# Remove symbol and debug tables (-s -w) and embed the version and commit (-X).
LDFLAGS = -s -w -X github.com/jgador/ak3s/internal/ak3s.Version=$(VERSION) -X github.com/jgador/ak3s/internal/ak3s.Commit=$(COMMIT) -X github.com/jgador/ak3s/internal/ak3s.DashboardImage=$(DASHBOARD_IMAGE)

# These names are tasks, so run them even if a file has the same name.
.PHONY: build test verify release secrets-setup secrets-scan secrets-staged secrets-history test-secrets
.PHONY: dashboard-image install-local test-install-local port-forward port-forward-status port-forward-stop test-port-forward test-release

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
	for script in scripts/*.sh scripts/lib/*.sh; do bash -n "$$script" || exit; done
	sh -n .githooks/pre-commit
	$(MAKE) test-release
	$(MAKE) test-install-local

# Check release branch and version guards without contacting GitHub.
test-release:
	bash scripts/check-release.test.sh

# Local secret scanning uses Bash, jq, and Gitleaks 8.30.1 or newer 8.x.
secrets-setup:
	bash scripts/install-git-hooks.sh

secrets-scan:
	bash scripts/check-secrets.sh

secrets-staged:
	bash scripts/check-secrets.sh staged

secrets-history:
	bash scripts/check-secrets.sh history

test-secrets:
	bash scripts/check-secrets.test.sh

# Build the production dashboard image from the repository root.
dashboard-image:
	docker build -f dashboard/Dockerfile -t $(DASHBOARD_IMAGE) .

# Build and install local source on a test server, including the dashboard image.
install-local: build
	bash scripts/install-local.sh '$(DASHBOARD_IMAGE)' '$(AK3S_CONFIG)'

test-install-local: build
	bash scripts/install-local.test.sh

# Start all four local UIs in the background; DASHBOARD=0 skips the dashboard.
port-forward:
ifeq ($(DASHBOARD),0)
	bash scripts/port-forward.sh start --no-dashboard
else
	bash scripts/port-forward.sh start
endif

port-forward-status:
	bash scripts/port-forward.sh status

port-forward-stop:
	bash scripts/port-forward.sh stop

test-port-forward:
	bash scripts/port-forward.test.sh

# Cross-compile static Linux binaries for amd64 and arm64 into dist/.
release:
	mkdir -p dist
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -trimpath -ldflags '$(LDFLAGS)' -o dist/ak3s_linux_amd64 ./cmd/ak3s
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(GO) build -trimpath -ldflags '$(LDFLAGS)' -o dist/ak3s_linux_arm64 ./cmd/ak3s

# Include the installer and SHA-256 checksums for all three release files.
	cp install.sh dist/install.sh
	cd dist && sha256sum ak3s_linux_amd64 ak3s_linux_arm64 install.sh > checksums.txt
