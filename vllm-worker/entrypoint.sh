#!/bin/bash
HOST="0.0.0.0"
PORT=8000

echo "[worker] starting stub vLLM on port $PORT (thread pool inside)"

exec python3 /app/worker.py --host $HOST --port $PORT