import http from 'k6/http';
import { check, sleep } from 'k6';

export const options = {
  stages: [
    { duration: '20s', target: 10 },
    { duration: '30s', target: 30 },
    { duration: '30s', target: 50 },
    { duration: '20s', target: 0 },
  ],
  thresholds: {
    http_req_duration: ['p(95)<5000'],
    http_req_failed: ['rate<0.05'],
  },
};

const PROXY_URL = __ENV.PROXY_URL || 'http://localhost:8080';

const PROMPTS = [
  "Explain the concept of GPU autoscaling in simple terms.",
  "Write a Python function to calculate fibonacci numbers.",
  "Describe the differences between CUDA and ROCm.",
  "Explain memory management in large language model inference.",
  "Compare TPU vs GPU for ML inference workloads.",
  "What are the key metrics for monitoring a GPU cluster?",
  "Explain the transformer architecture briefly.",
  "How does tensor parallelism work in vLLM?",
  "Write a bash script to monitor GPU memory usage.",
  "Describe the challenges of serving LLMs at scale.",
];

function randomPrompt() {
  return PROMPTS[Math.floor(Math.random() * PROMPTS.length)];
}

export default function () {
  const payload = JSON.stringify({
    model: 'mistral-7b',
    prompt: randomPrompt(),
    max_tokens: 80,
    temperature: 0.7,
  });

  const params = {
    headers: {
      'Content-Type': 'application/json',
    },
    timeout: '30s',
  };

  const res = http.post(`${PROXY_URL}/v1/completions`, payload, params);

  check(res, {
    'status is 200': (r) => r.status === 200,
    'response has choices': (r) => {
      try {
        const body = JSON.parse(r.body);
        return body.choices && body.choices.length > 0;
      } catch (e) {
        return false;
      }
    },
  });

  sleep(0.3);
}

export function handleSummary(data) {
  const metrics = data.metrics;
  const totalReqs = metrics.http_reqs.values.count || 0;
  const duration = metrics.http_req_duration.values;
  const failedRate = metrics.http_req_failed ? metrics.http_req_failed.values.rate : 0;

  let output = '\n';
  output += '============================================================\n';
  output += 'LOAD TEST RESULTS\n';
  output += '============================================================\n';
  output += `Total Requests:   ${totalReqs}\n`;
  output += `Throughput:       ${(metrics.http_reqs.values.rate || 0).toFixed(2)} req/s\n`;
  output += `Failed Rate:      ${(failedRate * 100).toFixed(2)}%\n`;
  output += `P50 Latency:      ${duration.avg.toFixed(2)}ms\n`;
  output += `P90 Latency:      ${(duration['p(90)'] || 0).toFixed(2)}ms\n`;
  output += `P95 Latency:      ${(duration['p(95)'] || 0).toFixed(2)}ms\n`;
  output += `P99 Latency:      ${(duration['p(99)'] || 0).toFixed(2)}ms\n`;
  output += '============================================================\n';

  return {
    stdout: output,
    'summary.json': JSON.stringify({
      total_requests: totalReqs,
      throughput: (metrics.http_reqs.values.rate || 0).toFixed(2),
      error_rate: (failedRate * 100).toFixed(2),
      p50_latency_ms: duration.avg.toFixed(2),
      p95_latency_ms: (duration['p(95)'] || 0).toFixed(2),
      p99_latency_ms: (duration['p(99)'] || 0).toFixed(2),
    }, null, 2),
  };
}