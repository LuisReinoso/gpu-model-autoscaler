import argparse
import time
import random
from fastapi import FastAPI, Request
from fastapi.responses import StreamingResponse
from prometheus_client import Counter, Histogram, Gauge, generate_latest

app = FastAPI()

TOKENS_TOTAL = Counter("vllm_tokens_total", "Total tokens generated")
REQUESTS_TOTAL = Counter("vllm_requests_total", "Total inference requests")
LATENCY = Histogram("vllm_latency_seconds", "Request latency")
GPU_UTIL = Gauge("vllm_gpu_utilization", "GPU utilization percentage")


def fake_tokens(prompt: str, max_tokens: int):
    words = [
        "the", "model", "output", "demonstrates", "inference",
        "capabilities", "with", "scalable", "infrastructure",
        "autoscaling", "GPU", "tokens", "processing", "pipeline",
        "throughput", "latency", "optimized", "distributed", "serving",
    ]
    for _ in range(max_tokens):
        yield random.choice(words) + " "
        time.sleep(0.01)


@app.post("/v1/completions")
@app.post("/v1/chat/completions")
async def completions(request: Request):
    body = await request.json()
    max_tokens = body.get("max_tokens", 100)
    prompt = body.get("prompt", body.get("messages", [{}])[0].get("content", ""))

    REQUESTS_TOTAL.inc()

    start = time.time()
    output = "".join(fake_tokens(prompt, min(max_tokens, 200)))
    elapsed = time.time() - start
    token_count = max_tokens

    TOKENS_TOTAL.inc(token_count)
    GPU_UTIL.set(random.uniform(40, 95))
    LATENCY.observe(elapsed)

    return {
        "id": f"cmpl-{random.randint(1000, 9999)}",
        "object": "text_completion",
        "created": int(time.time()),
        "model": "mistral-7b-stub",
        "choices": [{"text": output, "index": 0, "finish_reason": "stop"}],
        "usage": {
            "prompt_tokens": len(prompt.split()),
            "completion_tokens": token_count,
            "total_tokens": len(prompt.split()) + token_count,
        },
    }


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
    args = parser.parse_args()

    import uvicorn
    uvicorn.run(app, host=args.host, port=args.port)