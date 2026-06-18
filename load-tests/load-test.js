import http from 'k6/http';
import { check, sleep } from 'k6';
import { Rate, Trend, Counter } from 'k6/metrics';

const errorRate = new Rate('errors');
const latencyP50 = new Trend('latency_p50');
const latencyP95 = new Trend('latency_p95');
const latencyP99 = new Trend('latency_p99');
const throughput = new Counter('throughput');

export const options = {
  stages: [
    { duration: '30s', target: 10 },
    { duration: '1m',  target: 10 },
    { duration: '30s', target: 30 },
    { duration: '1m',  target: 30 },
    { duration: '30s', target: 50 },
    { duration: '1m',  target: 50 },
    { duration: '1m',  target: 0 },
  ],
  thresholds: {
    http_req_duration: ['p(95)<3000'],
    errors: ['rate<0.1'],
  },
};

const PROXY_URL = __ENV.PROXY_URL || 'http://localhost:8080';

const PROMPTS = [
  "Explain the concept of GPU autoscaling in 3 paragraphs.",
  "Write a Python function to calculate fibonacci numbers.",
  "What is the difference between CUDA and ROCm?",
  "Explain memory management in large language model inference.",
  "Compare TPU vs GPU for ML inference workloads.",
  "What are the key metrics for monitoring a GPU cluster?",
  "Explain the transformer architecture in simple terms.",
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
    max_tokens: 100,
    temperature: 0.7,
  });

  const params = {
    headers: {
      'Content-Type': 'application/json',
    },
    timeout: '30s',
  };

  const res = http.post(`${PROXY_URL}/v1/completions`, payload, params);

  const success = check(res, {
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

  errorRate.add(!success);
  throughput.add(1);

  const duration = res.timings.duration;
  latencyP50.add(duration);
  latencyP95.add(duration);
  latencyP99.add(duration);

  sleep(0.5);
}

export function handleSummary(data) {
  const totalReqs = data.metrics.http_reqs.values.count;
  const errorRateVal = data.metrics.errors.values.rate;
  const p50 = data.metrics.latency_p50.values.med;
  const p95 = data.metrics.latency_p95.values['p(95)'];
  const p99 = data.metrics.latency_p99.values['p(99)'];

  console.log('='.repeat(60));
  console.log('LOAD TEST SUMMARY');
  console.log('='.repeat(60));
  console.log(`Total Requests:   ${totalReqs}`);
  console.log(`Throughput:       ${data.metrics.http_req_duration.values.rate} req/s`);
  console.log(`Error Rate:       ${(errorRateVal * 100).toFixed(2)}%`);
  console.log(`P50 Latency:      ${p50.toFixed(2)}ms`);
  console.log(`P95 Latency:      ${p95.toFixed(2)}ms`);
  console.log(`P99 Latency:      ${p99.toFixed(2)}ms`);
  console.log('='.repeat(60));

  return {
    stdout: '',
    'summary.json': JSON.stringify(data, null, 2),
  };
}