SHELL := /bin/bash

VERSION ?= 1.0.0-alpha.0
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || printf unknown)
DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)

.PHONY: all build test race vet fmt-check profiles-check schemas-check profile-upstream-check check install clean dist

all: check build

build:
	mkdir -p bin
	CGO_ENABLED=0 go build -trimpath -ldflags '$(LDFLAGS)' -o bin/aiqa ./cmd/aiqa

test:
	go test ./...

race:
	go test -race ./...

vet:
	go vet ./...

fmt-check:
	@test -z "$$(gofmt -l $$(find . -name '*.go' -type f))" || \
	  { echo 'Go files need gofmt:'; gofmt -l $$(find . -name '*.go' -type f); exit 1; }

profiles-check:
	@test -f profile/openai-compat-2026-08.json
	@test -f profile/openai-compat-2026-09.json

schemas-check:
	python3 -m json.tool schema/report.schema.json >/dev/null
	python3 -m json.tool profile/openai-compat-2026-08.json >/dev/null
	python3 -m json.tool profile/openai-compat-2026-09.json >/dev/null

profile-upstream-check:
	go run ./script/check-profile-upstream.go

check: fmt-check profiles-check schemas-check vet test

install: build
	install -Dm755 bin/aiqa "$${HOME}/.local/bin/aiqa"

dist: check
	rm -rf dist
	mkdir -p dist
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags '$(LDFLAGS)' -o dist/aiqa-linux-amd64 ./cmd/aiqa
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags '$(LDFLAGS)' -o dist/aiqa-linux-arm64 ./cmd/aiqa
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags '$(LDFLAGS)' -o dist/aiqa-darwin-arm64 ./cmd/aiqa
	(cd dist && sha256sum aiqa-* > SHA256SUMS)

clean:
	rm -rf bin dist report.md report.json
