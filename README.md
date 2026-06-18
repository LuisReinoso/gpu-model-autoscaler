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

```bash
# Requires: Docker, Go 1.21+, NVIDIA Container Toolkit (for local GPU)
docker compose up -d
```

Without GPU (stub mode):
```bash
GPU_MODE=stub docker compose up -d
```

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
- `GET /health` — proxy health
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

```bash
cd load-tests && k6 run load-test.js
```

**Results from stub mode (CPU only, no GPU):**

| Metric | Value |
|--------|-------|
| Total Requests | 7,895 |
| Throughput | 78.8 req/s |
| Error Rate | 0.00% |
| P50 Latency | 1.91ms |
| P95 Latency | 3.29ms |
| P99 Latency | 4-5ms |

_With real GPU (A100/H100) expect 10-50x higher throughput. The stub mode validates the full proxy → worker → metrics pipeline end-to-end._

## License

MIT