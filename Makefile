VERSION ?= $(shell git describe --tags --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null)
DATE    ?= $(shell date -u +%Y-%m-%d)
PKG     := github.com/BrainerVirus/youtrack-cli/internal/build
LDFLAGS := -s -w -X $(PKG).Version=$(VERSION) -X $(PKG).Commit=$(COMMIT) -X $(PKG).Date=$(DATE)

.PHONY: build test it lint fmt vet clean release snapshot completions

build:
	go build -trimpath -ldflags "$(LDFLAGS)" -o bin/ytrack ./cmd/ytrack

test:
	go test -race ./...

# Opt-in real-instance suite against your personal YouTrack Cloud instance,
# using the session from `ytrack auth login`: make it YTRACK_IT_PROJECT=<short name>
# It creates issues prefixed "ytrack-it <run id>" and deletes them. See docs/integration-tests.md.
it: build
	YTRACK_IT=1 YTRACK_IT_BIN="$(CURDIR)/bin/ytrack" YTRACK_IT_PROJECT="$(YTRACK_IT_PROJECT)" \
		go test -count=1 -v -run '^TestIntegration$$' ./test/integration/

vet:
	go vet ./...

lint: vet
	golangci-lint run ./...

fmt:
	golangci-lint fmt ./...

clean:
	rm -rf bin dist completions

# Cut a release: make release VERSION=v0.1.0
# Tags the current origin/main and pushes the tag; .github/workflows/release.yml does the rest.
release:
	@test "$(origin VERSION)" = "command line" \
		|| { echo "usage: make release VERSION=vX.Y.Z (VERSION must be given on the command line)" >&2; exit 1; }
	@echo "$(VERSION)" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?$$' \
		|| { echo "usage: make release VERSION=vX.Y.Z (got '$(VERSION)')" >&2; exit 1; }
	@! echo "$(VERSION)" | grep -Eq -- '-[0-9]+-g[0-9a-f]+' \
		|| { echo "'$(VERSION)' looks like git describe output, not a release version" >&2; exit 1; }
	@test "$$(git branch --show-current)" = main || { echo "release from main" >&2; exit 1; }
	@test -z "$$(git status --porcelain)" || { echo "working tree is not clean" >&2; exit 1; }
	git fetch origin main --tags
	@test "$$(git rev-parse HEAD)" = "$$(git rev-parse origin/main)" \
		|| { echo "main is not at origin/main; pull first" >&2; exit 1; }
	go test -race ./...
	git tag -a $(VERSION) -m "ytrack $(VERSION)"
	git push origin refs/tags/$(VERSION)

# Shell completions packed into the release archives and the Homebrew cask.
completions:
	rm -rf completions
	mkdir completions
	go run ./cmd/ytrack completion bash > completions/ytrack.bash
	go run ./cmd/ytrack completion zsh > completions/_ytrack
	go run ./cmd/ytrack completion fish > completions/ytrack.fish

# Local release dry run into dist/ (needs goreleaser v2).
snapshot: completions
	goreleaser release --snapshot --clean
