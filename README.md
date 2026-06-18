# GPU Model Autoscaler

Dynamic GPU autoscaling infrastructure for self-hosted LLM inference. Proxies requests to vLLM workers, monitors latency/throughput, and auto-scales GPU instances up/down via cloud APIs.

## Architecture

```
Client → Proxy (Go) → vLLM Workers (GPU instances)
              ↓
       Autoscaler Controller → Cloud API (RunPod / Lambda Labs)
              ↓
       Prometheus → Grafana
```

## Features

- **Self-hosted LLMs** with vLLM for high-throughput inference
- **Load balancer** with least-connections + health checks
- **Autoscaling** based on queue depth, latency, and GPU utilization
- **Multi-cloud GPU support** — RunPod and Lambda Labs APIs
- **Request queuing** with Redis overflow buffer
- **Prometheus metrics** for inference latency, throughput, GPU utilization
- **Grafana dashboard** with real-time autoscaling decisions

## Quick Start

### Prerequisites

- Docker and Docker Compose
- Go 1.18+ (for building the proxy locally)
- k6 (for load testing)

### Run the stack

```bash
git clone https://github.com/LuisReinoso/gpu-model-autoscaler.git
cd gpu-model-autoscaler

# Start all services (stub mode, no GPU required)
docker compose up -d

# Verify everything is up
curl http://localhost:8080/health
curl http://localhost:8001/health
```

### Run a load test

```bash
# Install k6 if you don't have it: https://k6.io/docs/get-started/installation/
cd load-tests && k6 run load-test.js
```

### View metrics

- **Grafana**: http://localhost:3000 (admin/admin) — dashboard "GPU Autoscaler"
- **Prometheus**: http://localhost:9090
- **Proxy metrics**: http://localhost:8080/metrics
- **Worker list**: http://localhost:8080/admin/workers

## Stack

| Component | Tech |
|-----------|------|
| Inference | vLLM (Llama 3, Mistral, etc.) |
| Proxy/LB | Go + net/http |
| Queue | Redis |
| Metrics | Prometheus |
| Dashboard | Grafana |
| Cloud GPU | RunPod / Lambda Labs API |

## Endpoints

- `POST /v1/completions` — OpenAI-compatible completions
- `POST /v1/chat/completions` — OpenAI-compatible chat
- `GET /health` — proxy health (also `:8001` for worker health)
- `GET /metrics` — Prometheus metrics
- `GET /admin/workers` — current workers and their status
- `GET /admin/scale/up` — manual scale up trigger
- `GET /admin/scale/down` — manual scale down trigger

## Metrics

- `gpu_autoscaler_requests_total`
- `gpu_autoscaler_request_duration_seconds`
- `gpu_autoscaler_queue_depth`
- `gpu_autoscaler_active_workers`
- `gpu_autoscaler_gpu_utilization`
- `gpu_autoscaler_scale_events_total`

## Benchmarks

### Stub mode (no GPU, validates full pipeline)

```bash
cd load-tests && PROXY_URL=http://localhost:8080 k6 run load-test.js
```

| Metric | Value |
|--------|-------|
| Total Requests | 7,895 |
| Throughput | 78.8 req/s |
| Error Rate | 0.00% |
| P50 Latency | 1.91ms |
| P95 Latency | 3.29ms |

### GPU mode (RTX 4080 — Qwen 2.5 1.5B, real inference)

```bash
python3 -m vllm.entrypoints.openai.api_server \
  --model Qwen/Qwen2.5-1.5B-Instruct \
  --port 8001 --max-model-len 2048 --gpu-memory-utilization 0.5 --enforce-eager
# In another terminal:
k6 run load-tests/load-test.js
```

| Metric | Value |
|--------|-------|
| Total Requests | 765 |
| Throughput | 12.7 req/s |
| Error Rate | 0.00% |
| P50 Latency | 315ms |
| P95 Latency | 324ms |

### Reproduce

```bash
git clone https://github.com/LuisReinoso/gpu-model-autoscaler.git
cd gpu-model-autoscaler
docker compose up -d --build   # stub mode (no GPU needed)
k6 run load-tests/load-test.js
```

## License

MIT