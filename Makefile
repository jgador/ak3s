GO ?= go
VERSION ?= dev
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
LDFLAGS = -s -w -X github.com/jgador/ak3s/internal/ak3s.Version=$(VERSION) -X github.com/jgador/ak3s/internal/ak3s.Commit=$(COMMIT)

.PHONY: build test verify release
build:
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags '$(LDFLAGS)' -o bin/ak3s ./cmd/ak3s

test:
	$(GO) test -race -cover ./...

verify: test
	$(GO) vet ./...
	test -z "$$($(GO) fmt ./...)"
	bash -n install.sh

release:
	mkdir -p dist
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -trimpath -ldflags '$(LDFLAGS)' -o dist/ak3s_linux_amd64 ./cmd/ak3s
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(GO) build -trimpath -ldflags '$(LDFLAGS)' -o dist/ak3s_linux_arm64 ./cmd/ak3s
	cp install.sh dist/install.sh
	cd dist && sha256sum ak3s_linux_amd64 ak3s_linux_arm64 install.sh > checksums.txt
