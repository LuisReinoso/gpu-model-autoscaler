#!/bin/bash
MODEL=${MODEL_NAME:-"mistralai/Mistral-7B-Instruct-v0.3"}
TENSOR_PARALLEL=${TENSOR_PARALLEL:-1}
HOST="0.0.0.0"
PORT=8000

echo "[worker] starting vLLM with model=$MODEL tp=$TENSOR_PARALLEL"

if [ "$GPU_MODE" = "stub" ]; then
    echo "[worker] running in stub mode (no GPU)"
    exec python3 /app/worker.py --host $HOST --port $PORT
else
    exec python3 -m vllm.entrypoints.openai.api_server \
        --model "$MODEL" \
        --host "$HOST" \
        --port "$PORT" \
        --tensor-parallel-size "$TENSOR_PARALLEL"
fi