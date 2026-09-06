SHELL := /bin/bash

VERSION ?= 1.0.0-alpha.6
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || printf unknown)
DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)
GOVULNCHECK_VERSION := v1.7.0
CYCLONEDX_GOMOD_VERSION := v1.12.0
EXAMPLE_SOURCES := ./script/example.go ./script/example_good.go ./script/example_bad.go

.PHONY: all build test race vet fmt-check profiles-check schemas-check dependency-check vuln-check sbom-check security-check profile-upstream-check check install example clean dist http-client.private.env.json

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
	python3 -m json.tool schema/report-v3.schema.json >/dev/null
	python3 -m json.tool schema/profile-v3.schema.json >/dev/null
	python3 -m json.tool schema/ext-v1.schema.json >/dev/null
	python3 -m json.tool profile/openai-compat-2026-09.json >/dev/null
	python3 -m json.tool profile/anthropic-compat-2026-09.json >/dev/null
	python3 -m json.tool profile/google-genai-compat-2026-09.json >/dev/null
	python3 -m json.tool profile/google-genai-generate-content-compat-2026-09.json >/dev/null
	python3 -c 'import json; from jsonschema import Draft202012Validator, FormatChecker; schema=json.load(open("schema/profile-v3.schema.json")); Draft202012Validator.check_schema(schema); validator=Draft202012Validator(schema, format_checker=FormatChecker()); [validator.validate(json.load(open(path))) for path in ("profile/openai-compat-2026-09.json", "profile/anthropic-compat-2026-09.json", "profile/google-genai-compat-2026-09.json", "profile/google-genai-generate-content-compat-2026-09.json")]'
	python3 -c 'import json; from jsonschema import Draft202012Validator; Draft202012Validator.check_schema(json.load(open("schema/report-v3.schema.json")))'
	python3 -c 'import json; from jsonschema import Draft202012Validator; Draft202012Validator.check_schema(json.load(open("schema/ext-v1.schema.json")))'
	python3 -c 'import glob,json; from jsonschema import Draft202012Validator, FormatChecker; schema=json.load(open("schema/report-v3.schema.json")); validator=Draft202012Validator(schema, format_checker=FormatChecker()); paths=glob.glob("example/*.json"); assert paths, "no example report found"; [validator.validate(json.load(open(path))) for path in paths]'
	python3 -m json.tool test/wiremock/mappings/openai-models.json >/dev/null
	python3 -m json.tool test/wiremock/mappings/anthropic-models.json >/dev/null
	python3 -m json.tool test/wiremock/mappings/google-genai-models.json >/dev/null
	python3 -m json.tool test/wiremock/mappings/unavailable-api.json >/dev/null

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
	python3 -c 'import json,sys; components=json.load(open(sys.argv[1]))["components"]; versions={item["name"]:item["version"] for item in components}; assert versions["github.com/openai/openai-go/v3"] == "v3.54.0"; assert versions["github.com/anthropics/anthropic-sdk-go"] == "v1.68.0"; assert versions["google.golang.org/genai"] == "v1.71.0"' "$$bom"

security-check: dependency-check vuln-check sbom-check

profile-upstream-check:
	go run ./script/check-profile-upstream.go
	go run ./script/check-profile-upstream.go --profile profile/anthropic-compat-2026-09.json
	go run ./script/check-profile-upstream.go --profile profile/google-genai-compat-2026-09.json
	go run ./script/check-profile-upstream.go --profile profile/google-genai-generate-content-compat-2026-09.json

check: fmt-check profiles-check schemas-check dependency-check vet test

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
