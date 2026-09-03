#!/usr/bin/env bash

set -Eeuo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
readonly SCRIPT_DIR
PROJECT_ROOT="$(cd -- "$SCRIPT_DIR/.." && pwd)"
readonly PROJECT_ROOT
readonly EXAMPLE_DIR="$PROJECT_ROOT/example"
readonly PROVIDER_STORAGE="${AIQA_EXAMPLE_PROVIDER_STORAGE:-/arch/model}"
readonly PROVIDER_LISTEN="${AIQA_EXAMPLE_PROVIDER_LISTEN:-0.0.0.0:8080}"
readonly WIREMOCK_IMAGE="${AIQA_EXAMPLE_WIREMOCK_IMAGE:-wiremock/wiremock:3.13.2}"

warn() {
  printf 'WARNING: %s\n' "$*" >&2
}

die() {
  printf 'ERROR: %s\n' "$*" >&2
  exit 1
}

if ! command -v model-provider >/dev/null 2>&1; then
  warn 'model-provider was not found in PATH; example reports were not generated'
  exit 0
fi

if ! command -v aiqa >/dev/null 2>&1; then
  die 'aiqa was not found in PATH'
fi

provider_status=''
if provider_status="$(model-provider --json status 2>/dev/null)" &&
  grep -Eq '"running"[[:space:]]*:[[:space:]]*true' <<<"$provider_status"; then
  printf 'model-provider is running\n'
else
  printf 'model-provider is not running; starting it\n'
  model-provider up --force --storage "$PROVIDER_STORAGE" --listen "$PROVIDER_LISTEN"
fi

printf 'Generating successful example reports\n'
(
  cd "$EXAMPLE_DIR"
  provider_env="$(model-provider env)"
  eval "$provider_env"
  aiqa --verbose --concurrency 16 --timeout=60 \
    test=compat,ext-suite \
    --ext-suite=official-sdk \
    --format=markdown,json \
    --name=good-report
)

if ! command -v docker >/dev/null 2>&1; then
  die 'Docker is required to generate the WireMock failure report'
fi
if ! command -v curl >/dev/null 2>&1; then
  die 'curl is required to wait for WireMock readiness'
fi

readonly WIREMOCK_CONTAINER="aiqa-example-wiremock-$$"
wiremock_started=false
cleanup() {
  if [[ "$wiremock_started" == true ]]; then
    docker stop "$WIREMOCK_CONTAINER" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT INT TERM

printf 'Starting WireMock %s\n' "$WIREMOCK_IMAGE"
docker run --rm --detach \
  --name "$WIREMOCK_CONTAINER" \
  --publish '127.0.0.1::8080/tcp' \
  --volume "$PROJECT_ROOT/test/wiremock:/home/wiremock:ro" \
  "$WIREMOCK_IMAGE" >/dev/null
wiremock_started=true

wiremock_address="$(docker port "$WIREMOCK_CONTAINER" 8080/tcp | head -n 1)"
[[ -n "$wiremock_address" ]] || die 'Docker did not publish the WireMock HTTP port'
readonly WIREMOCK_URL="http://$wiremock_address"

wiremock_ready=false
for _ in {1..50}; do
  if curl --fail --silent --show-error "$WIREMOCK_URL/__admin/health" >/dev/null 2>&1; then
    wiremock_ready=true
    break
  fi
  sleep 0.2
done
[[ "$wiremock_ready" == true ]] || die 'WireMock did not become ready'

printf 'Generating intentionally failing example reports\n'
bad_exit=0
(
  cd "$EXAMPLE_DIR"
  OPENAI_BASE_URL="$WIREMOCK_URL/openai/v1" \
    OPENAI_API_KEY='wiremock-example-key' \
    ANTHROPIC_BASE_URL="$WIREMOCK_URL/anthropic" \
    ANTHROPIC_API_KEY='wiremock-example-key' \
    GOOGLE_GENAI_BASE_URL="$WIREMOCK_URL/google" \
    GOOGLE_GENAI_API_KEY='wiremock-example-key' \
    aiqa --verbose --concurrency 16 --timeout=60 \
      test=compat,ext-suite \
      --target=openai,anthropic,google-genai \
      --google-genai-model=wiremock-broken-model \
      --ext-suite=official-sdk \
      --format=markdown,json \
      --name=bad-report
) || bad_exit=$?

[[ -s "$EXAMPLE_DIR/bad-report.json" && -s "$EXAMPLE_DIR/bad-report.md" ]] ||
  die "AIQA did not write both bad-report artifacts (exit $bad_exit)"
grep -Eq '"status"[[:space:]]*:[[:space:]]*"error"|"error"[[:space:]]*:[[:space:]]*[1-9]' \
  "$EXAMPLE_DIR/bad-report.json" ||
  die 'WireMock report did not contain an error result'

printf 'Generated example/good-report.{md,json} and example/bad-report.{md,json}\n'
