#!/usr/bin/env bash

set -Eeuo pipefail

bash /arch/project/aiqa/script/model-provider.sh up /arch/model 8080
bash /arch/project/aiqa/script/test-report.sh
bash /arch/project/aiqa/script/model-provider.sh down
