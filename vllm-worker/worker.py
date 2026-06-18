import argparse
import asyncio
import time
import random
from concurrent.futures import ThreadPoolExecutor
from fastapi import FastAPI, Request
from fastapi.responses import StreamingResponse
from prometheus_client import Counter, Histogram, Gauge, generate_latest

executor = ThreadPoolExecutor(max_workers=8)

TOKENS_TOTAL = Counter("vllm_tokens_total", "Total tokens generated")
REQUESTS_TOTAL = Counter("vllm_requests_total", "Total inference requests")
LATENCY = Histogram("vllm_latency_seconds", "Request latency")
GPU_UTIL = Gauge("vllm_gpu_utilization", "GPU utilization percentage")

def fake_tokens(prompt: str, max_tokens: int):
    words = [
        "the", "model", "inference", "scalable", "infrastructure",
        "autoscaling", "GPU", "tokens", "processing", "pipeline",
        "throughput", "latency", "optimized", "distributed", "serving",
    ]
    tokens = []
    for _ in range(max_tokens):
        tokens.append(random.choice(words))
    return " ".join(tokens)


def run_inference(prompt: str, max_tokens: int):
    start = time.time()
    output = fake_tokens(prompt, min(max_tokens, 200))
    elapsed = time.time() - start

    REQUESTS_TOTAL.inc()
    TOKENS_TOTAL.inc(max_tokens)
    GPU_UTIL.set(random.uniform(40, 95))
    LATENCY.observe(elapsed)

    return {
        "id": f"cmpl-{random.randint(1000,9999)}",
        "object": "text_completion",
        "created": int(time.time()),
        "model": "mistral-7b-stub",
        "choices": [{"text": output, "index": 0, "finish_reason": "stop"}],
        "usage": {
            "prompt_tokens": len(prompt.split()),
            "completion_tokens": max_tokens,
            "total_tokens": len(prompt.split()) + max_tokens,
        },
    }


app = FastAPI()

@app.post("/v1/completions")
@app.post("/v1/chat/completions")
async def completions(request: Request):
    body = await request.json()
    max_tokens = body.get("max_tokens", 100)
    prompt = body.get("prompt", body.get("messages", [{}])[0].get("content", ""))
    loop = asyncio.get_event_loop()
    return await loop.run_in_executor(executor, run_inference, prompt, max_tokens)


@app.get("/health")
async def health():
    return {"status": "healthy", "gpu_utilization": GPU_UTIL._value.get()}


@app.get("/metrics")
async def metrics():
    return generate_latest()


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--host", default="0.0.0.0")
    parser.add_argument("--port", type=int, default=8000)
    parser.add_argument("--workers", type=int, default=1)
    args = parser.parse_args()
    import uvicorn
    uvicorn.run(app, host=args.host, port=args.port, workers=args.workers, log_level="info")