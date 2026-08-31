#!/usr/bin/env bash

set -Eeuo pipefail

export PROVIDER_API_KEY="$(cat /arch/model/.model-provider/secrets.env | grep PROVIDER_API_KEY | cut -d= -f2)"
export OPENAI_BASE_URL="http://localhost:8080/v1"
export OPENAI_API_KEY="$PROVIDER_API_KEY"
export ANTHROPIC_BASE_URL="http://127.0.0.1:8080/anthropic"
export ANTHROPIC_AUTH_TOKEN="$PROVIDER_API_KEY"
aiqa --verbose --concurrency 16 test=compat --timeout=60 --format=markdown,json --name=compat-report
