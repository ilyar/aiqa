#!/usr/bin/env bash

set -Eeuo pipefail

# Install:
#   cp model-provider.sh ~/.local/bin/model-provider
#   or
#   scp model-provider.sh <ssh-alias>:/usr/local/bin/model-provider
# Usage:
#   LAN:    model-provider /arch/model 8080
#   Public: model-provider /srv/model example.net
#
# Optional:
#   MODEL_CONTEXT=8192 bash model-provider.sh ...
#   BIFROST_ADMIN_PORT=8081 bash model-provider.sh ...
#   LLAMA_IMAGE=... BIFROST_IMAGE=... CADDY_IMAGE=... bash model-provider.sh ...

MODEL_REPO="prism-ml/Ternary-Bonsai-1.7B-gguf"
MODEL_FILE="Ternary-Bonsai-1.7B-Q2_0_g64.gguf"
MODEL_SHA256="6d0ecb3d9055969b5cde332b6fdb60e67ed3599e9f73e56b977731e1467e5c91"

# Internal llama.cpp id is intentionally short. Bifrost adds the provider prefix,
# so clients see: prism-ml/Ternary-Bonsai-1.7B
MODEL_ID="Ternary-Bonsai-1.7B"
PROVIDER_ID="prism-ml"
PUBLIC_MODEL_ID="$PROVIDER_ID/$MODEL_ID"
MODEL_CONTEXT="${MODEL_CONTEXT:-4096}"

# Pinned defaults. Override explicitly when upgrading and test before changing them here.
LLAMA_IMAGE="${LLAMA_IMAGE:-ghcr.io/ggml-org/llama.cpp:server-cuda-b10362}"
BIFROST_IMAGE="${BIFROST_IMAGE:-maximhq/bifrost:v1.6.6}"
CADDY_IMAGE="${CADDY_IMAGE:-caddy:2.11.3-alpine}"

NETWORK="model-provider"
LLAMA_CONTAINER="model-provider-llama"
BIFROST_CONTAINER="model-provider-bifrost"
CADDY_CONTAINER="model-provider-caddy"

log() { printf '\n==> %s\n' "$*"; }
die() { printf '\nERROR: %s\n' "$*" >&2; exit 1; }
need() { command -v "$1" >/dev/null 2>&1 || die "missing command: $1"; }

[[ $# -eq 2 ]] || die "usage: $0 STORAGE PORT|DOMAIN"
STORAGE="$1"
TARGET="$2"

if [[ "$TARGET" =~ ^[0-9]+$ ]]; then
  MODE="lan"
  PORT="$TARGET"
  (( PORT >= 1 && PORT <= 65535 )) || die "invalid port: $PORT"
else
  MODE="public"
  DOMAIN="$TARGET"
  [[ "$DOMAIN" =~ ^[A-Za-z0-9][A-Za-z0-9.-]*[A-Za-z0-9]$ ]] || die "invalid domain: $DOMAIN"
fi

for cmd in docker curl jq openssl sha256sum nvidia-smi; do need "$cmd"; done
docker info >/dev/null 2>&1 || die "Docker daemon is unavailable"
nvidia-smi >/dev/null 2>&1 || die "NVIDIA GPU/driver is unavailable"

mkdir -p "$STORAGE"
STORAGE="$(cd "$STORAGE" && pwd)"
STATE="$STORAGE/.model-provider"
SECRETS="$STATE/secrets.env"
BIFROST_CONFIG="$STATE/bifrost.json"
CADDYFILE="$STATE/Caddyfile"
mkdir -p "$STATE"
chmod 700 "$STATE"

# -----------------------------------------------------------------------------
# Model
# -----------------------------------------------------------------------------
MODEL_PATH="$STORAGE/$MODEL_FILE"
if [[ ! -f "$MODEL_PATH" ]]; then
  need hf
  log "Downloading model"
  hf download "$MODEL_REPO" "$MODEL_FILE" --local-dir "$STORAGE"
fi

actual_sha="$(sha256sum "$MODEL_PATH" | awk '{print $1}')"
[[ "$actual_sha" == "$MODEL_SHA256" ]] || \
  die "bad model checksum: expected $MODEL_SHA256, got $actual_sha"
log "Model checksum OK"

# -----------------------------------------------------------------------------
# Stable credentials: rerunning does not silently rotate API keys.
# Delete $SECRETS explicitly when you want rotation.
# -----------------------------------------------------------------------------
if [[ ! -f "$SECRETS" ]]; then
  umask 077
  cat > "$SECRETS" <<EOF_SECRETS
LLAMA_API_KEY=llama-$(openssl rand -hex 32)
PROVIDER_API_KEY=sk-bf-$(openssl rand -hex 32)
BIFROST_ENCRYPTION_KEY=$(openssl rand -hex 32)
BIFROST_ADMIN_USERNAME=admin
BIFROST_ADMIN_PASSWORD=$(openssl rand -hex 24)
EOF_SECRETS
fi
chmod 600 "$SECRETS"
set -a
# shellcheck disable=SC1090
source "$SECRETS"
set +a

# -----------------------------------------------------------------------------
# llama.cpp router preset
# -----------------------------------------------------------------------------
cat > "$STORAGE/models.ini" <<EOF_MODELS
version = 1

[*]
parallel = 1
batch-size = 512
ubatch-size = 128
flash-attn = auto
jinja = true
n-gpu-layers = all
cache-prompt = true

[$MODEL_ID]
model = /models/$MODEL_FILE
ctx-size = $MODEL_CONTEXT
tags = ternary,1.58bit,qwen3,gguf,q2_0,g64
load-on-startup = true
stop-timeout = 10
EOF_MODELS

# -----------------------------------------------------------------------------
# Bifrost: one declarative custom OpenAI-compatible upstream.
# A single virtual key works as both OpenAI Bearer and Anthropic x-api-key.
# config_store=false makes config.json the source of truth.
# -----------------------------------------------------------------------------
cat > "$BIFROST_CONFIG" <<EOF_BIFROST
{
  "\$schema": "https://www.getbifrost.ai/schema",
  "encryption_key": "env.BIFROST_ENCRYPTION_KEY",
  "client": {
    "enable_logging": false,
    "enforce_auth_on_inference": true,
    "allowed_origins": ["*"]
  },
  "governance": {
    "auth_config": {
      "is_enabled": true,
      "admin_username": "env.BIFROST_ADMIN_USERNAME",
      "admin_password": "env.BIFROST_ADMIN_PASSWORD",
      "disable_auth_on_inference": true
    },
    "virtual_keys": [
      {
        "id": "model-provider-default",
        "name": "model-provider-default",
        "value": "env.PROVIDER_API_KEY",
        "is_active": true,
        "provider_configs": [
          {
            "provider": "$PROVIDER_ID",
            "allowed_models": ["$MODEL_ID", "$PUBLIC_MODEL_ID"],
            "weight": 1.0
          }
        ]
      }
    ]
  },
  "providers": {
    "$PROVIDER_ID": {
      "keys": [
        {
          "id": "llama-upstream",
          "name": "llama-upstream",
          "value": "env.LLAMA_API_KEY",
          "models": ["$MODEL_ID"],
          "weight": 1.0
        }
      ],
      "network_config": {
        "base_url": "http://$LLAMA_CONTAINER:8080",
        "allow_private_network": true,
        "default_request_timeout_in_seconds": 300,
        "max_retries": 0
      },
      "custom_provider_config": {
        "base_provider_type": "openai",
        "allowed_requests": {
          "list_models": true,
          "text_completion": true,
          "text_completion_stream": true,
          "chat_completion": true,
          "chat_completion_stream": true,
          "responses": true,
          "responses_stream": true,
          "count_tokens": false
        }
      }
    }
  },
  "config_store": { "enabled": false }
}
EOF_BIFROST
jq -e . "$BIFROST_CONFIG" >/dev/null
chmod 644 "$BIFROST_CONFIG"

# -----------------------------------------------------------------------------
# Runtime
# -----------------------------------------------------------------------------
log "Pulling pinned images"
docker pull "$LLAMA_IMAGE" >/dev/null
docker pull "$BIFROST_IMAGE" >/dev/null
[[ "$MODE" == "public" ]] && docker pull "$CADDY_IMAGE" >/dev/null

# Record immutable digests actually used by this deployment.
{
  printf 'LLAMA_IMAGE=%s\n' "$(docker image inspect "$LLAMA_IMAGE" --format '{{index .RepoDigests 0}}')"
  printf 'BIFROST_IMAGE=%s\n' "$(docker image inspect "$BIFROST_IMAGE" --format '{{index .RepoDigests 0}}')"
  [[ "$MODE" == "public" ]] && \
    printf 'CADDY_IMAGE=%s\n' "$(docker image inspect "$CADDY_IMAGE" --format '{{index .RepoDigests 0}}')"
} > "$STATE/images.lock"

docker rm -f \
  "$CADDY_CONTAINER" "$BIFROST_CONTAINER" "$LLAMA_CONTAINER" \
  model-provider llama.cpp \
  >/dev/null 2>&1 || true

docker network inspect "$NETWORK" >/dev/null 2>&1 || docker network create "$NETWORK" >/dev/null
docker volume inspect model-provider-bifrost-data >/dev/null 2>&1 || \
  docker volume create model-provider-bifrost-data >/dev/null

log "Starting llama.cpp"
docker run -d \
  --name "$LLAMA_CONTAINER" \
  --restart unless-stopped \
  --network "$NETWORK" \
  --gpus all \
  -e "LLAMA_API_KEY=$LLAMA_API_KEY" \
  -v "$STORAGE:/models:ro" \
  "$LLAMA_IMAGE" \
  --models-preset /models/models.ini \
  --models-max 1 \
  --host 0.0.0.0 \
  --port 8080 \
  >/dev/null

if [[ "$MODE" == "lan" ]]; then
  BIFROST_HOST_PORT="$PORT"
  PUBLISH=( -p "0.0.0.0:$PORT:8080" )
else
  BIFROST_HOST_PORT="${BIFROST_ADMIN_PORT:-8081}"
  PUBLISH=( -p "127.0.0.1:$BIFROST_HOST_PORT:8080" )
fi

log "Starting Bifrost"
docker run -d \
  --name "$BIFROST_CONTAINER" \
  --restart unless-stopped \
  --network "$NETWORK" \
  "${PUBLISH[@]}" \
  --env-file "$SECRETS" \
  -e APP_HOST=0.0.0.0 \
  -e APP_PORT=8080 \
  -v model-provider-bifrost-data:/app/data \
  -v "$BIFROST_CONFIG:/app/data/config.json:ro" \
  "$BIFROST_IMAGE" \
  >/dev/null

LOCAL_GATEWAY="http://127.0.0.1:$BIFROST_HOST_PORT"

wait_url() {
  local url="$1"
  for _ in {1..60}; do
    curl -fsS --max-time 3 "$url" >/dev/null 2>&1 && return 0
    sleep 1
  done
  return 1
}

log "Health check"
wait_url "$LOCAL_GATEWAY/health" || {
  docker logs --tail 100 "$BIFROST_CONTAINER" >&2 || true
  die "Bifrost health check failed"
}

# -----------------------------------------------------------------------------
# HTTPS for public mode. Only provider APIs are Internet-facing; Bifrost
# management remains on 127.0.0.1:$BIFROST_HOST_PORT.
# -----------------------------------------------------------------------------
if [[ "$MODE" == "public" ]]; then
  cat > "$CADDYFILE" <<EOF_CADDY
$DOMAIN {
    encode zstd gzip

    @api path /openai /openai/* /anthropic /anthropic/* /health
    handle @api {
        reverse_proxy $BIFROST_CONTAINER:8080
    }

    handle {
        respond "Not Found" 404
    }
}
EOF_CADDY

  docker volume inspect model-provider-caddy-data >/dev/null 2>&1 || \
    docker volume create model-provider-caddy-data >/dev/null
  docker volume inspect model-provider-caddy-config >/dev/null 2>&1 || \
    docker volume create model-provider-caddy-config >/dev/null

  log "Starting HTTPS endpoint"
  docker run -d \
    --name "$CADDY_CONTAINER" \
    --restart unless-stopped \
    --network "$NETWORK" \
    -p 80:80 \
    -p 443:443 \
    -p 443:443/udp \
    -v "$CADDYFILE:/etc/caddy/Caddyfile:ro" \
    -v model-provider-caddy-data:/data \
    -v model-provider-caddy-config:/config \
    "$CADDY_IMAGE" \
    >/dev/null

  BASE_URL="https://$DOMAIN"
  wait_url "$BASE_URL/health" || {
    docker logs --tail 100 "$CADDY_CONTAINER" >&2 || true
    die "HTTPS health check failed; verify DNS and inbound TCP 80/443"
  }
else
  BASE_URL="http://127.0.0.1:$PORT"
fi

# -----------------------------------------------------------------------------
# Smoke tests through Bifrost. They verify the actual protocol facades, not
# merely that the containers are running.
# -----------------------------------------------------------------------------
smoke() {
  local kind="$1" url="$2" header="$3" jq_path="$4" body response
  local extra_headers=()
  [[ -n "$kind" ]] && extra_headers=( -H 'anthropic-version: 2023-06-01' )
  body="{\"model\":\"$PUBLIC_MODEL_ID\",\"messages\":[{\"role\":\"user\",\"content\":\"Reply exactly: OK\"}],\"max_tokens\":8}"
  for _ in {1..60}; do
    response="$(curl -sS --max-time 20 "$url" -H "$header" -H 'Content-Type: application/json' \
      "${extra_headers[@]}" -d "$body" 2>/dev/null || true)"
    jq -e "$jq_path" >/dev/null 2>&1 <<<"$response" && return 0
    sleep 2
  done
  printf '%s\n' "$response" >&2
  return 1
}

log "OpenAI smoke test"
# Empty first argument prevents the Anthropic version header.
smoke "" "$LOCAL_GATEWAY/openai/v1/chat/completions" \
  "Authorization: Bearer $PROVIDER_API_KEY" '.choices[0].message.content' || {
    docker logs --tail 100 "$LLAMA_CONTAINER" >&2 || true
    docker logs --tail 100 "$BIFROST_CONTAINER" >&2 || true
    die "OpenAI smoke test failed"
  }

log "Anthropic smoke test"
smoke "anthropic" "$LOCAL_GATEWAY/anthropic/v1/messages" \
  "x-api-key: $PROVIDER_API_KEY" '.content[0].text' || {
    docker logs --tail 100 "$BIFROST_CONTAINER" >&2 || true
    die "Anthropic smoke test failed"
  }

LAN_IP="$(hostname -I 2>/dev/null | awk '{print $1}' || true)"

cat <<EOF_RESULT

Provider is ready.

Model:
  $PUBLIC_MODEL_ID

OpenAI:
  export OPENAI_BASE_URL=$BASE_URL/openai/v1
  export OPENAI_API_KEY=$PROVIDER_API_KEY
  export OPENAI_MODEL=$PUBLIC_MODEL_ID

Anthropic:
  export ANTHROPIC_BASE_URL=$BASE_URL/anthropic
  export ANTHROPIC_API_KEY=$PROVIDER_API_KEY
  export ANTHROPIC_AUTH_TOKEN=$PROVIDER_API_KEY
  export ANTHROPIC_MODEL=$PUBLIC_MODEL_ID

Credentials:
  $SECRETS

Resolved image digests:
  $STATE/images.lock

Bifrost admin:
  $LOCAL_GATEWAY
  user: $BIFROST_ADMIN_USERNAME
  password: $BIFROST_ADMIN_PASSWORD
EOF_RESULT

if [[ "$MODE" == "lan" && -n "$LAN_IP" ]]; then
  cat <<EOF_LAN

LAN:
  OPENAI_BASE_URL=http://$LAN_IP:$PORT/openai/v1
  ANTHROPIC_BASE_URL=http://$LAN_IP:$PORT/anthropic
EOF_LAN
fi
