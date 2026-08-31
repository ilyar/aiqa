#!/usr/bin/env bash

set -Eeuo pipefail

bash /arch/project/aiqa/script/model-provider.sh up /arch/model 8080
export OPENAI_BASE_URL="http://localhost:8080/v1"
export OPENAI_API_KEY="$(cat /arch/model/.model-provider/secrets.env | grep PROVIDER_API_KEY | cut -d= -f2)"
aiqa --verbose --concurrency 16 test=compat --timeout=60 --format=markdown,json --name=compat-report
bash /arch/project/aiqa/script/model-provider.sh down
