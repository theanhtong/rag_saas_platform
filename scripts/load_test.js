import http from 'k6/http';
import { check, sleep } from 'k6';

// k6 load test configuration options
export const options = {
  stages: [
    { duration: '5s', target: 10 },  // ramp-up to 10 VUs
    { duration: '10s', target: 20 }, // maintain load with 20 VUs
    { duration: '5s', target: 0 },   // ramp-down
  ],
  thresholds: {
    http_req_duration: ['p(95)<500'], // 95% of requests must complete under 500ms
  },
};

const BASE_URL = 'http://localhost:8080';

export default function () {
  const url = `${BASE_URL}/v1/chat/completions`;
  const payload = JSON.stringify({
    model: 'gpt-4o',
    messages: [
      { role: 'user', content: 'What is the speed of light?' }
    ],
    stream: false,
  });

  const params = {
    headers: {
      'Content-Type': 'application/json',
      'X-API-Key': 'k6-load-test-key',
    },
  };

  const res = http.post(url, payload, params);

  check(res, {
    'status is 200 or 429': (r) => r.status === 200 || r.status === 429,
    'cache header present': (r) => r.headers['X-Cache'] !== undefined,
  });

  // verify sub-10ms SLA latency on Semantic Cache HITs
  if (res.headers['X-Cache'] === 'HIT') {
    check(res, {
      'cache hit latency < 10ms': (r) => r.timings.duration < 10,
    });
  }

  sleep(0.1);
}
