#!/usr/bin/env bash

PROVIDER_STORAGE=${1:-/arch/model}
PROVIDER_PORT=${2:-8080}

#TODO add check health via nvidia-smi

if [ ! -f "$PROVIDER_STORAGE/Ternary-Bonsai-1.7B-Q2_0_g64.gguf" ]; then
  mkdir -p "$PROVIDER_STORAGE"
  hf download \
    prism-ml/Ternary-Bonsai-1.7B-gguf \
    Ternary-Bonsai-1.7B-Q2_0_g64.gguf \
    --local-dir "$PROVIDER_STORAGE"
  sha256sum "$PROVIDER_STORAGE/Ternary-Bonsai-1.7B-Q2_0_g64.gguf"
  # TODO check match 6d0ecb3d9055969b5cde332b6fdb60e67ed3599e9f73e56b977731e1467e5c91
fi

docker pull ghcr.io/ggml-org/llama.cpp:server-cuda
docker rm -f model-provider 2>/dev/null || true

cat > "/arch/models/models.ini" <<'EOF'
; https://github.com/ggml-org/llama.cpp/blob/master/docs/preset.md
version = 1
[*]
parallel = 1
batch-size = 512
ubatch-size = 128
flash-attn = auto
jinja = true
n-gpu-layers = all
; Prompt/KV cache
cache-prompt = true
[Ternary-Bonsai-1.7B]
alias = prism-ml/Ternary-Bonsai-1.7B
model = /models/Ternary-Bonsai-1.7B-Q2_0_g64.gguf
ctx-size = 4096 ; max 32768
tags = ternary,1.58bit,qwen3,gguf,q2_0,g64
load-on-startup = true
stop-timeout = 10
EOF

api_key=$(openssl rand -hex 32)
docker run -d \
  --name model-provider \
  --restart unless-stopped \
  --gpus all \
  -p "0.0.0.0:$PROVIDER_PORT:8080" \
  -v "$PROVIDER_STORAGE:/models:ro" \
  ghcr.io/ggml-org/llama.cpp:server-cuda \
  --api-key "$api_key" \
  --models-preset /models/models.ini \
  --models-max 1 \
  --host 0.0.0.0 \
  --port "$PROVIDER_PORT"

#TODO add check health via nvidia-smi

#TODO add heading check
curl -s http://localhost:8080/health | jq
#TODO add heading check
curl -s http://localhost:8080/v1/models | jq
#TODO add heading check logs
docker logs -f bonsai
echo "use:"
echo "  export OPENAI_BASE_URL=http://localhost:$PROVIDER_PORT/v1"
echo "  export OPENAI_API_KEY=$api_key"

