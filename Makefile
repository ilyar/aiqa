SHELL := /bin/bash

VERSION ?= 1.0.0-alpha.13
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || printf unknown)
DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)
GOVULNCHECK_VERSION := v1.7.0
CYCLONEDX_GOMOD_VERSION := v1.12.0
GREMLINS_VERSION := v0.6.0
EXAMPLE_SOURCES := ./script/example.go ./script/example_good.go ./script/example_bad.go

.PHONY: all build test race vet fmt-check profiles-check schemas-check dependency-check vuln-check sbom-check security-check profile-upstream-check check example-check diff-check version-check scheduler-check monitoring-check qa qa-gate qa-gate-full qa-gate-diff qa-update-gate qa-install-gremlins install example clean dist http-client.private.env.json

all: check build

build:
	@mkdir -p bin
	@CGO_ENABLED=0 go build -trimpath -ldflags '$(LDFLAGS)' -o bin/aiqa ./cmd/aiqa

test:
	go test ./...
	go test $(EXAMPLE_SOURCES) ./script/example_test.go

race:
	go test -race ./...
	go test -race $(EXAMPLE_SOURCES) ./script/example_test.go

vet:
	go vet ./...
	go vet $(EXAMPLE_SOURCES)

fmt-check:
	@test -z "$$(gofmt -l $$(find . -name '*.go' -type f))" || \
	  { echo 'Go files need gofmt:'; gofmt -l $$(find . -name '*.go' -type f); exit 1; }

profiles-check:
	@test -f profile/openai-compat-2026-08.json
	@test -f profile/openai-compat-2026-09.json
	@test -f profile/anthropic-compat-2026-09.json
	@test -f profile/google-genai-compat-2026-09.json
	@test -f profile/google-genai-generate-content-compat-2026-09.json

schemas-check:
	go run ./cmd/qa-check schemas

dependency-check:
	go mod verify
	@test "$$(go list -m -f '{{.Version}}' github.com/openai/openai-go/v3)" = "v3.54.0"
	@test "$$(go list -m -f '{{.Version}}' github.com/anthropics/anthropic-sdk-go)" = "v1.68.0"
	@test "$$(go list -m -f '{{.Version}}' google.golang.org/genai)" = "v1.71.0"

vuln-check:
	go run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) ./...

sbom-check:
	@bom="$$(mktemp "$${TMPDIR:-/tmp}/aiqa-bom.XXXXXX")"; \
	trap 'rm -f "$$bom"' EXIT; \
	go run github.com/CycloneDX/cyclonedx-gomod/cmd/cyclonedx-gomod@$(CYCLONEDX_GOMOD_VERSION) mod -licenses -json -output "$$bom"; \
	go run ./cmd/qa-check sbom "$$bom"

security-check: dependency-check vuln-check sbom-check

profile-upstream-check:
	go run ./script/check-profile-upstream.go
	go run ./script/check-profile-upstream.go --profile profile/anthropic-compat-2026-09.json
	go run ./script/check-profile-upstream.go --profile profile/google-genai-compat-2026-09.json
	go run ./script/check-profile-upstream.go --profile profile/google-genai-generate-content-compat-2026-09.json

check: fmt-check profiles-check schemas-check dependency-check vet test

# qa is the required local pre-commit gate. It combines deterministic local
# checks and does not regenerate time-dependent public sample reports.
qa: check example-check security-check race scheduler-check monitoring-check build version-check diff-check qa-gate

scheduler-check:
	go test -race ./internal/scheduler

monitoring-check:
	go test -race ./internal/execution ./internal/eta ./internal/terminal ./internal/logview

qa-gate:
	PATH="$(CURDIR)/bin:$$PATH" go run ./script/qa-gate.go check --scope=full

qa-gate-full: qa-gate

qa-gate-diff:
	@test -n "$(BASE)" || { echo 'BASE is required, for example BASE=origin/main'; exit 2; }
	PATH="$(CURDIR)/bin:$$PATH" go run ./script/qa-gate.go check --scope=diff --base="$(BASE)"

qa-update-gate:
	PATH="$(CURDIR)/bin:$$PATH" go run ./script/qa-gate.go update --scope=full

qa-install-gremlins:
	GOBIN="$(CURDIR)/bin" go install github.com/go-gremlins/gremlins/cmd/gremlins@$(GREMLINS_VERSION)

# Deterministic generator contract check. The real `make example` additionally
# requires a local model-provider and Docker; its reports include live timing,
# generated timestamps, and provider request IDs, so it remains explicit.
example-check:
	go test $(EXAMPLE_SOURCES) ./script/example_test.go

version-check:
	@./bin/aiqa --version

diff-check:
	@git diff --check

install: build
	@install -Dm755 bin/aiqa "$${HOME}/.local/bin/aiqa"
	@aiqa --version

example: build
	@PATH="$(CURDIR)/bin:$$PATH" go run $(EXAMPLE_SOURCES)

dist: check
	rm -rf dist
	mkdir -p dist
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags '$(LDFLAGS)' -o dist/aiqa-linux-amd64 ./cmd/aiqa
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags '$(LDFLAGS)' -o dist/aiqa-linux-arm64 ./cmd/aiqa
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags '$(LDFLAGS)' -o dist/aiqa-darwin-arm64 ./cmd/aiqa
	(cd dist && sha256sum aiqa-* > SHA256SUMS)

http-client.private.env.json: http-client.env.json
	go run ./script/http-client-private-env.go

clean:
	rm -rf bin dist report.md report.json
