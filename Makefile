VERSION ?= $(shell git describe --tags --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null)
DATE    ?= $(shell date -u +%Y-%m-%d)
PKG     := github.com/BrainerVirus/youtrack-cli/internal/build
LDFLAGS := -s -w -X $(PKG).Version=$(VERSION) -X $(PKG).Commit=$(COMMIT) -X $(PKG).Date=$(DATE)

.PHONY: build test lint fmt vet clean

build:
	go build -trimpath -ldflags "$(LDFLAGS)" -o bin/ytrack ./cmd/ytrack

test:
	go test -race ./...

vet:
	go vet ./...

lint: vet
	golangci-lint run ./...

fmt:
	golangci-lint fmt ./...

clean:
	rm -rf bin
