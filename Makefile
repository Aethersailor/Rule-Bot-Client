GO ?= go
VERSION ?= dev
COMMIT ?= $(shell git rev-parse --short=12 HEAD 2>/dev/null || echo unknown)
BUILD_DATE ?= $(shell git show -s --format=%cI HEAD 2>/dev/null || echo unknown)
LDFLAGS = -s -w -buildid= \
	-X github.com/Aethersailor/Rule-Bot-Client/internal/client.BuildVersion=$(VERSION) \
	-X github.com/Aethersailor/Rule-Bot-Client/internal/client.BuildCommit=$(COMMIT) \
	-X github.com/Aethersailor/Rule-Bot-Client/internal/client.BuildDate=$(BUILD_DATE)

.PHONY: build test race vet fmt-check clean

build:
	mkdir -p bin
	CGO_ENABLED=0 $(GO) build -buildvcs=false -trimpath -ldflags="$(LDFLAGS)" -o bin/rule-bot-client ./cmd/rule-bot-client

test:
	$(GO) test ./...

race:
	$(GO) test -race ./...

vet:
	$(GO) vet ./...

fmt-check:
	test -z "$$(gofmt -l cmd internal)"

clean:
	rm -rf bin dist
