#!/usr/bin/env bash

set -Eeuo pipefail

# Install:
#   cp model-provider.sh ~/.local/bin/model-provider
#   or
#   scp model-provider.sh <ssh-alias>:/usr/local/bin/model-provider
# Commands:
#   up STORAGE PORT|DOMAIN    create or replace the provider stack
#   stop                      stop all provider services, preserving their state
#   start                     start already created provider services
#   down                      stop and remove services plus local state/cache files
#                             (preserves downloaded models and Docker images)
#
# Examples:
#   LAN:    model-provider up /arch/model 8080
#   Public: model-provider up /srv/model example.net
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
COMPAT_IMAGE="${COMPAT_IMAGE:-python:3.13.11-alpine3.23}"
CADDY_IMAGE="${CADDY_IMAGE:-caddy:2.11.3-alpine}"

NETWORK="model-provider"
LLAMA_CONTAINER="model-provider-llama"
BIFROST_CONTAINER="model-provider-bifrost"
COMPAT_CONTAINER="model-provider-compat"
CADDY_CONTAINER="model-provider-caddy"

log() { printf '\n==> %s\n' "$*"; }
die() { printf '\nERROR: %s\n' "$*" >&2; exit 1; }
need() { command -v "$1" >/dev/null 2>&1 || die "missing command: $1"; }

CONTAINERS=(
  "$LLAMA_CONTAINER"
  "$BIFROST_CONTAINER"
  "$COMPAT_CONTAINER"
  "$CADDY_CONTAINER"
)

usage() {
  cat <<EOF_USAGE
usage:
  $0 up STORAGE PORT|DOMAIN
  $0 stop
  $0 start
  $0 down

down uses \${MODEL_PROVIDER_STORAGE:-/arch/model}; it removes only the provider
containers, its Docker volumes/network, and \${MODEL_PROVIDER_STORAGE:-/arch/model}/.model-provider plus
\${MODEL_PROVIDER_STORAGE:-/arch/model}/models.ini. Downloaded GGUF models and Docker images are preserved.
EOF_USAGE
}

container_exists() {
  docker container inspect "$1" >/dev/null 2>&1
}

stop_services() {
  local container
  for container in "${CONTAINERS[@]}"; do
    container_exists "$container" && docker stop "$container" >/dev/null
  done
}

start_services() {
  local container
  for container in "${CONTAINERS[@]}"; do
    container_exists "$container" && docker start "$container" >/dev/null
  done
}

down_and_wipe() {
  local storage="${MODEL_PROVIDER_STORAGE:-/arch/model}"
  local container
  [[ -d "$storage" ]] || die "storage directory does not exist: $storage"
  storage="$(cd "$storage" && pwd -P)"
  [[ "$storage" != "/" ]] || die "invalid MODEL_PROVIDER_STORAGE"

  for container in "${CONTAINERS[@]}"; do
    container_exists "$container" && docker rm -f "$container" >/dev/null
  done
  docker volume rm \
    model-provider-bifrost-data \
    model-provider-caddy-data \
    model-provider-caddy-config \
    >/dev/null 2>&1 || true
  docker network rm "$NETWORK" >/dev/null 2>&1 || true
  rm -rf -- "$storage/.model-provider"
  rm -f -- "$storage/models.ini"
  log "Provider services and state removed; downloaded models and Docker images were preserved"
}

COMMAND="${1:-}"
case "$COMMAND" in
  stop)
    [[ $# -eq 1 ]] || { usage >&2; exit 2; }
    need docker
    docker info >/dev/null 2>&1 || die "Docker daemon is unavailable"
    stop_services
    log "Provider services stopped"
    exit 0
    ;;
  start)
    [[ $# -eq 1 ]] || { usage >&2; exit 2; }
    need docker
    docker info >/dev/null 2>&1 || die "Docker daemon is unavailable"
    start_services
    log "Provider services started"
    exit 0
    ;;
  down)
    [[ $# -eq 1 ]] || { usage >&2; exit 2; }
    need docker
    docker info >/dev/null 2>&1 || die "Docker daemon is unavailable"
    down_and_wipe
    exit 0
    ;;
  up)
    [[ $# -eq 3 ]] || { usage >&2; exit 2; }
    STORAGE="$2"
    TARGET="$3"
    ;;
  *)
    usage >&2
    exit 2
    ;;
esac

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
BIFROST_DATA="$STATE/bifrost-data"
COMPAT_SERVER="$STATE/openai_compat.py"
CADDYFILE="$STATE/Caddyfile"
mkdir -p "$STATE"
chmod 700 "$STATE"
mkdir -p "$BIFROST_DATA"
chmod 700 "$BIFROST_DATA"

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
# Governance and virtual-key authentication require a persistent config store.
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
      "disable_auth_on_inference": false
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
            "key_ids": ["llama-upstream"],
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
  "config_store": {
    "enabled": true,
    "type": "sqlite",
    "config": { "path": "/app/data/config.db" }
  }
}
EOF_BIFROST
jq -e . "$BIFROST_CONFIG" >/dev/null
chmod 644 "$BIFROST_CONFIG"

# -----------------------------------------------------------------------------
# OpenAI compatibility facade
# -----------------------------------------------------------------------------
# Bifrost is the authenticated provider router. This small stdlib-only facade
# fixes the remaining OpenAI wire differences that Bifrost v1.6.6 exposes on
# /v1: model-list object, model retrieval, error type, and stream usage chunks.
cat > "$COMPAT_SERVER" <<'PYTHON'
import copy
import http.client
import json
import os
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import unquote, urlsplit

UPSTREAM_HOST = os.environ.get("BIFROST_HOST", "model-provider-bifrost")
UPSTREAM_PORT = int(os.environ.get("BIFROST_PORT", "8080"))


def openai_error(message, error_type="invalid_request_error"):
    return {"error": {"message": message, "type": error_type, "code": None}}


class Handler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def log_message(self, _format, *_args):
        pass

    def request_body(self):
        length = int(self.headers.get("Content-Length", "0"))
        return self.rfile.read(length) if length else b""

    def upstream(self, method, path, body=b""):
        headers = {
            key: value for key, value in self.headers.items()
            if key.lower() not in {"host", "connection", "content-length"}
        }
        if body:
            headers["Content-Length"] = str(len(body))
        connection = http.client.HTTPConnection(UPSTREAM_HOST, UPSTREAM_PORT, timeout=300)
        connection.request(method, path, body=body, headers=headers)
        return connection, connection.getresponse()

    def send_json(self, status, payload):
        encoded = json.dumps(payload, separators=(",", ":")).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(encoded)))
        self.end_headers()
        self.wfile.write(encoded)

    def copy_response_headers(self, response):
        for key, value in response.getheaders():
            if key.lower() not in {"connection", "content-length", "transfer-encoding"}:
                self.send_header(key, value)

    def normal_json(self, status, payload):
        if status >= 400:
            error = payload.get("error") if isinstance(payload, dict) else None
            if isinstance(error, dict):
                error.setdefault("type", "invalid_request_error")
                error.setdefault("code", None)
            else:
                payload = openai_error(str(payload))
        return payload

    def list_models(self):
        connection, response = self.upstream("GET", "/openai/v1/models")
        raw = response.read()
        connection.close()
        try:
            payload = json.loads(raw)
        except json.JSONDecodeError:
            self.send_json(response.status, openai_error("invalid upstream model list", "server_error"))
            return
        if response.status >= 400:
            self.send_json(response.status, self.normal_json(response.status, payload))
            return
        data = payload.get("data", [])
        for model in data:
            if isinstance(model, dict):
                model.setdefault("object", "model")
        self.send_json(200, {"object": "list", "data": data})

    def retrieve_model(self, model_id):
        connection, response = self.upstream("GET", "/openai/v1/models")
        raw = response.read()
        connection.close()
        try:
            payload = json.loads(raw)
            models = payload.get("data", [])
        except json.JSONDecodeError:
            models = []
        for model in models:
            if isinstance(model, dict) and model.get("id") == model_id:
                model = dict(model)
                model.setdefault("object", "model")
                self.send_json(200, model)
                return
        self.send_json(404, openai_error("The model '%s' does not exist" % model_id, "invalid_request_error"))

    def stream(self, response, include_usage):
        self.send_response(response.status)
        self.copy_response_headers(response)
        self.send_header("Content-Type", "text/event-stream")
        self.send_header("Cache-Control", "no-cache")
        self.send_header("Connection", "close")
        self.end_headers()
        for raw_line in response:
            line = raw_line.decode("utf-8", "replace").rstrip("\r\n")
            if not line.startswith("data: "):
                self.wfile.write(raw_line)
                self.wfile.flush()
                continue
            value = line[6:]
            if value == "[DONE]":
                self.wfile.write(b"data: [DONE]\n\n")
                self.wfile.flush()
                self.close_connection = True
                break
            try:
                event = json.loads(value)
            except json.JSONDecodeError:
                self.wfile.write(raw_line)
                self.wfile.flush()
                continue
            for choice in event.get("choices", []):
                choice.setdefault("finish_reason", None)
            usage = event.get("usage")
            if include_usage and usage and event.get("choices"):
                choice_event = copy.deepcopy(event)
                choice_event["usage"] = None
                usage_event = copy.deepcopy(event)
                usage_event["choices"] = []
                self.wfile.write(("data: " + json.dumps(choice_event, separators=(",", ":")) + "\n\n").encode())
                self.wfile.write(("data: " + json.dumps(usage_event, separators=(",", ":")) + "\n\n").encode())
            else:
                self.wfile.write(("data: " + json.dumps(event, separators=(",", ":")) + "\n\n").encode())
            self.wfile.flush()

    def forward(self):
        body = self.request_body()
        path = urlsplit(self.path).path
        query = urlsplit(self.path).query
        if path == "/health":
            upstream_path = "/health"
        elif path.startswith("/v1/"):
            upstream_path = "/openai" + path
        else:
            self.send_json(404, openai_error("Not found", "invalid_request_error"))
            return
        if query:
            upstream_path += "?" + query
        request_payload = None
        include_usage = False
        if body:
            try:
                request_payload = json.loads(body)
                include_usage = bool(request_payload.get("stream_options", {}).get("include_usage"))
            except (ValueError, AttributeError):
                pass
        try:
            connection, response = self.upstream(self.command, upstream_path, body)
            content_type = response.getheader("Content-Type", "")
            if "text/event-stream" in content_type:
                self.stream(response, include_usage)
                connection.close()
                return
            raw = response.read()
            connection.close()
        except (OSError, http.client.HTTPException) as error:
            self.send_json(502, openai_error("upstream unavailable: %s" % error, "server_error"))
            return
        try:
            payload = json.loads(raw)
        except json.JSONDecodeError:
            payload = openai_error(raw.decode("utf-8", "replace"), "server_error")
        requested_model = request_payload.get("model") if isinstance(request_payload, dict) else None
        error = payload.get("error") if isinstance(payload, dict) else None
        if response.status >= 400 and isinstance(requested_model, str) and isinstance(error, dict):
            message = str(error.get("message", ""))
            if "resolve a provider" in message or "no keys found" in message:
                error["message"] = "The model '%s' does not exist or you do not have access to it." % requested_model
                error["type"] = "invalid_request_error"
                error["param"] = "model"
                error["code"] = "model_not_found"
        self.send_json(response.status, self.normal_json(response.status, payload))

    def do_GET(self):
        path = urlsplit(self.path).path
        if path == "/v1/models":
            self.list_models()
        elif path.startswith("/v1/models/"):
            self.retrieve_model(unquote(path[len("/v1/models/"):]))
        else:
            self.forward()

    def do_POST(self):
        self.forward()


ThreadingHTTPServer(("0.0.0.0", 8080), Handler).serve_forever()
PYTHON
chmod 644 "$COMPAT_SERVER"

# -----------------------------------------------------------------------------
# Runtime
# -----------------------------------------------------------------------------
log "Pulling pinned images"
docker pull "$LLAMA_IMAGE" >/dev/null
docker pull "$BIFROST_IMAGE" >/dev/null
docker pull "$COMPAT_IMAGE" >/dev/null
[[ "$MODE" == "public" ]] && docker pull "$CADDY_IMAGE" >/dev/null

# Record immutable digests actually used by this deployment.
{
  printf 'LLAMA_IMAGE=%s\n' "$(docker image inspect "$LLAMA_IMAGE" --format '{{index .RepoDigests 0}}')"
  printf 'BIFROST_IMAGE=%s\n' "$(docker image inspect "$BIFROST_IMAGE" --format '{{index .RepoDigests 0}}')"
  printf 'COMPAT_IMAGE=%s\n' "$(docker image inspect "$COMPAT_IMAGE" --format '{{index .RepoDigests 0}}')"
  [[ "$MODE" == "public" ]] && \
    printf 'CADDY_IMAGE=%s\n' "$(docker image inspect "$CADDY_IMAGE" --format '{{index .RepoDigests 0}}')"
} > "$STATE/images.lock"

docker rm -f \
  "$CADDY_CONTAINER" "$COMPAT_CONTAINER" "$BIFROST_CONTAINER" "$LLAMA_CONTAINER" \
  model-provider llama.cpp \
  >/dev/null 2>&1 || true

docker network inspect "$NETWORK" >/dev/null 2>&1 || docker network create "$NETWORK" >/dev/null
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
  BIFROST_HOST_PORT="${BIFROST_ADMIN_PORT:-8081}"
  COMPAT_PUBLISH=( -p "0.0.0.0:$PORT:8080" )
else
  BIFROST_HOST_PORT="${BIFROST_ADMIN_PORT:-8081}"
  COMPAT_HOST_PORT="${COMPAT_HOST_PORT:-8082}"
  COMPAT_PUBLISH=( -p "127.0.0.1:$COMPAT_HOST_PORT:8080" )
fi

log "Starting Bifrost"
docker run -d \
  --name "$BIFROST_CONTAINER" \
  --restart unless-stopped \
  --network "$NETWORK" \
  -p "127.0.0.1:$BIFROST_HOST_PORT:8080" \
  --env-file "$SECRETS" \
  -e APP_HOST=0.0.0.0 \
  -e APP_PORT=8080 \
  -v "$BIFROST_DATA:/app/data" \
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

log "Starting OpenAI compatibility facade"
docker run -d \
  --name "$COMPAT_CONTAINER" \
  --restart unless-stopped \
  --network "$NETWORK" \
  "${COMPAT_PUBLISH[@]}" \
  -e BIFROST_HOST="$BIFROST_CONTAINER" \
  -e BIFROST_PORT=8080 \
  -v "$COMPAT_SERVER:/app/server.py:ro" \
  "$COMPAT_IMAGE" \
  python /app/server.py \
  >/dev/null

LOCAL_API="http://127.0.0.1:${COMPAT_HOST_PORT:-$PORT}"
wait_url "$LOCAL_API/health" || {
  docker logs --tail 100 "$COMPAT_CONTAINER" >&2 || true
  die "OpenAI compatibility facade health check failed"
}

# -----------------------------------------------------------------------------
# HTTPS for public mode. Only provider APIs are Internet-facing; Bifrost
# management remains on 127.0.0.1:$BIFROST_HOST_PORT.
# -----------------------------------------------------------------------------
if [[ "$MODE" == "public" ]]; then
  cat > "$CADDYFILE" <<EOF_CADDY
$DOMAIN {
    encode zstd gzip

    @openai path /v1 /v1/* /health
    handle @openai {
        reverse_proxy $COMPAT_CONTAINER:8080
    }

    @anthropic path /anthropic /anthropic/*
    handle @anthropic {
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
# Smoke tests verify the public protocol facades, not merely that containers run.
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
smoke "" "$LOCAL_API/v1/chat/completions" \
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
  export OPENAI_BASE_URL=$BASE_URL/v1
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
  OPENAI_BASE_URL=http://$LAN_IP:$PORT/v1
  ANTHROPIC_BASE_URL=http://$LAN_IP:$PORT/anthropic
EOF_LAN
fi
