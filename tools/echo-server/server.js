// Tiny Express target for LoadCell sanity checks. Exposes a handful of
// REST-shaped routes (GET / POST / PUT / DELETE) each with its own status
// mix and latency profile so LoadCell's chart shows distinctly different
// shapes per endpoint:
//
//   GET    /health              fast, almost always 200 — quiet baseline
//   GET    /users               mostly 200, occasional 5xx, medium latency
//   POST   /users               validation-heavy: 70% 2xx, 20% 4xx, 10% 5xx
//   PUT    /users/:id           85% 2xx, 10% 4xx (not found), 5% 5xx
//   DELETE /users/:id           80% 2xx, 15% 4xx, 5% 5xx
//   POST   /login               auth-heavy: 60% 2xx, 35% 4xx, 5% 429
//   GET    /products/search     90% 2xx, 8% 429, 2% 5xx, slow tail
//   POST   /checkout            risky: 50% 2xx, 30% 4xx, 15% 5xx, 5% 429
//   GET    /reports/heavy       95% 2xx but expensive (200-500ms)
//
// Anything else falls through to a catch-all that honors the legacy
// env-tunables (PROB_500/PROB_429/PROB_404 etc.) so old scripts still work.
//
//   node tools/echo-server/server.js
//
// Catch-all env knobs (only applied to the fallback handler):
//   PROB_500=0.03    PROB_429=0.05    PROB_404=0.02
//   LATENCY_MIN_MS=1 LATENCY_MAX_MS=8
//   SLOW_PROB=0.05   SLOW_MAX_MS=400

const express = require('express');

const PORT = 4466;
const PROB_500 = clampProb(parseFloat(process.env.PROB_500 ?? '0.03'));
const PROB_429 = clampProb(parseFloat(process.env.PROB_429 ?? '0.05'));
const PROB_404 = clampProb(parseFloat(process.env.PROB_404 ?? '0.02'));
const LATENCY_MIN_MS = Math.max(0, parseInt(process.env.LATENCY_MIN_MS ?? '1', 10));
const LATENCY_MAX_MS = Math.max(
  LATENCY_MIN_MS,
  parseInt(process.env.LATENCY_MAX_MS ?? '8', 10)
);
const SLOW_PROB = clampProb(parseFloat(process.env.SLOW_PROB ?? '0.05'));
const SLOW_MAX_MS = Math.max(0, parseInt(process.env.SLOW_MAX_MS ?? '400', 10));
const PROB_OK = Math.max(0, 1 - PROB_500 - PROB_429 - PROB_404);

function clampProb(p) {
  if (Number.isNaN(p)) return 0;
  if (p < 0) return 0;
  if (p > 1) return 1;
  return p;
}

const app = express();
// Capture body so we can inspect what LoadCell actually sent.
app.use(express.json({ limit: '1mb', strict: false }));
app.use(express.text({ limit: '1mb', type: '*/*' }));

let total = 0;
let lastWindowTotal = 0;
let firstHitAt = null;
let slowResponses = 0;
const counts = { ok: 0, c404: 0, e500: 0, r429: 0 };
const methodCounts = {};
const firstByMethod = {};

// Generic responder. Picks a status from the supplied bucket weights, then
// sleeps for a uniform [latMin..latMax] interval (with a slowProb chance of
// an extra slowMax tail) before sending.
function respond(req, res, opts) {
  if (firstHitAt === null) firstHitAt = Date.now();
  total++;
  methodCounts[req.method] = (methodCounts[req.method] || 0) + 1;
  const key = req.method + ' ' + req.route?.path || req.method + ' ' + req.path;
  if (!firstByMethod[key]) {
    firstByMethod[key] = {
      method: req.method,
      url: req.url,
      headers: { ...req.headers },
      bodyPreview:
        typeof req.body === 'string'
          ? req.body.slice(0, 200)
          : req.body
            ? JSON.stringify(req.body).slice(0, 200)
            : '',
      at: new Date().toISOString(),
    };
  }

  const {
    ok = 1,
    c4xx = 0,
    e5xx = 0,
    r429 = 0,
    latMin = 1,
    latMax = 8,
    slowProb = 0,
    slowMax = 200,
    okBody = 'ok',
    c4xxBody = 'bad request',
    e5xxBody = 'internal server error',
    r429Body = 'too many requests',
  } = opts;

  const r = Math.random();
  let status;
  let body;
  if (r < e5xx) {
    status = 500;
    body = e5xxBody;
    counts.e500++;
  } else if (r < e5xx + c4xx) {
    // Mix of 400/404 — route can override via c4xxStatus if it cares.
    status = opts.c4xxStatus ?? 400;
    body = c4xxBody;
    counts.c404++;
  } else if (r < e5xx + c4xx + r429) {
    status = 429;
    body = r429Body;
    res.set('Retry-After', '1');
    counts.r429++;
  } else {
    status = 200;
    body = okBody;
    counts.ok++;
  }

  const baseline = latMin + Math.random() * (latMax - latMin);
  const slow = slowProb > 0 && Math.random() < slowProb;
  if (slow) slowResponses++;
  const delay = baseline + (slow ? Math.random() * slowMax : 0);

  const send = () =>
    res
      .status(status)
      .type(typeof body === 'string' ? 'text/plain' : 'application/json')
      .send(body);
  if (delay > 0) setTimeout(send, delay);
  else send();
}

// ─── Explicit routes ────────────────────────────────────────────────

// Quiet baseline: nearly always 200, very fast. Good for warming up.
app.get('/health', (req, res) =>
  respond(req, res, {
    ok: 0.99,
    e5xx: 0.01,
    latMin: 1,
    latMax: 3,
    okBody: JSON.stringify({ status: 'ok' }),
  })
);

// Read-heavy: mostly 200, occasional 5xx blip, modest latency.
app.get('/users', (req, res) =>
  respond(req, res, {
    ok: 0.95,
    e5xx: 0.05,
    latMin: 2,
    latMax: 10,
    slowProb: 0.05,
    slowMax: 100,
    okBody: JSON.stringify({ users: [], page: 1 }),
  })
);

// Validation-heavy: client-error spike on bad bodies.
app.post('/users', (req, res) =>
  respond(req, res, {
    ok: 0.70,
    c4xx: 0.20,
    e5xx: 0.10,
    c4xxStatus: 422,
    latMin: 3,
    latMax: 15,
    okBody: JSON.stringify({ id: Math.floor(Math.random() * 10000) }),
    c4xxBody: 'validation failed',
  })
);

// Update: targets often missing → 404-heavy.
app.put('/users/:id', (req, res) =>
  respond(req, res, {
    ok: 0.85,
    c4xx: 0.10,
    e5xx: 0.05,
    c4xxStatus: 404,
    latMin: 5,
    latMax: 20,
    okBody: JSON.stringify({ id: req.params.id, updated: true }),
    c4xxBody: 'user not found',
  })
);

// Delete: similar 404 pattern.
app.delete('/users/:id', (req, res) =>
  respond(req, res, {
    ok: 0.80,
    c4xx: 0.15,
    e5xx: 0.05,
    c4xxStatus: 404,
    latMin: 3,
    latMax: 12,
    okBody: '',
    c4xxBody: 'user not found',
  })
);

// Auth: lots of 401s and a sprinkle of rate limits.
app.post('/login', (req, res) =>
  respond(req, res, {
    ok: 0.60,
    c4xx: 0.35,
    r429: 0.05,
    c4xxStatus: 401,
    latMin: 10,
    latMax: 50,
    okBody: JSON.stringify({ token: 'fake.jwt.token' }),
    c4xxBody: 'invalid credentials',
  })
);

// Search: rate-limit dominant, with a long tail.
app.get('/products/search', (req, res) =>
  respond(req, res, {
    ok: 0.90,
    r429: 0.08,
    e5xx: 0.02,
    latMin: 20,
    latMax: 80,
    slowProb: 0.20,
    slowMax: 300,
    okBody: JSON.stringify({ q: req.query.q ?? '', results: [] }),
  })
);

// Checkout: messy multi-failure mode.
app.post('/checkout', (req, res) =>
  respond(req, res, {
    ok: 0.50,
    c4xx: 0.30,
    e5xx: 0.15,
    r429: 0.05,
    c4xxStatus: 402,
    latMin: 50,
    latMax: 200,
    slowProb: 0.30,
    slowMax: 500,
    okBody: JSON.stringify({ orderId: `ord_${Date.now()}` }),
    c4xxBody: 'payment declined',
  })
);

// Heavy report: success rate is fine but each call is expensive.
app.get('/reports/heavy', (req, res) =>
  respond(req, res, {
    ok: 0.95,
    e5xx: 0.05,
    latMin: 200,
    latMax: 500,
    slowProb: 0.50,
    slowMax: 1500,
    okBody: JSON.stringify({ rows: 0, generatedAt: new Date().toISOString() }),
  })
);

// ─── Catch-all — preserves legacy env-tunable behavior ─────────────
app.use((req, res) =>
  respond(req, res, {
    ok: PROB_OK,
    c4xx: PROB_404,
    e5xx: PROB_500,
    r429: PROB_429,
    c4xxStatus: 404,
    latMin: LATENCY_MIN_MS,
    latMax: LATENCY_MAX_MS,
    slowProb: SLOW_PROB,
    slowMax: SLOW_MAX_MS,
    c4xxBody: 'not found',
  })
);

setInterval(() => {
  const windowCount = total - lastWindowTotal;
  lastWindowTotal = total;
  if (total === 0) return;
  const elapsedSec = firstHitAt ? (Date.now() - firstHitAt) / 1000 : 0;
  const avgRps = elapsedSec > 0 ? total / elapsedSec : 0;
  const ts = new Date().toISOString().slice(11, 19);
  const pct = (n) => `${((n / total) * 100).toFixed(1).padStart(4)}%`;
  const methodSummary = Object.entries(methodCounts)
    .sort((a, b) => b[1] - a[1])
    .map(([m, c]) => `${m}:${c}`)
    .join(' ');
  console.log(
    `[${ts}] total=${total.toString().padStart(8)}  ` +
      `2xx=${pct(counts.ok)} 4xx=${pct(counts.c404)} 429=${pct(counts.r429)} 5xx=${pct(counts.e500)}  ` +
      `slow=${pct(slowResponses)}  methods[${methodSummary}]  ` +
      `last1s=${windowCount.toString().padStart(6)}  avg=${avgRps.toFixed(1).padStart(8)} rps`
  );
}, 1000);

// Tiny side-channel on port 4467 for inspecting the first request seen per
// method+path. curl http://127.0.0.1:4467/ to see the headers/body LoadCell sent.
const inspectApp = express();
inspectApp.get('/', (_req, res) => res.json(firstByMethod));
inspectApp.listen(4467, '127.0.0.1', () => {
  console.log('inspect    → http://127.0.0.1:4467/ (first request per route)');
});

app.listen(PORT, '127.0.0.1', () => {
  const pp = (p) => `${(p * 100).toFixed(1)}%`;
  console.log(`echo-server listening on http://127.0.0.1:${PORT}`);
  console.log('routes:');
  console.log('  GET    /health              99% ok, fast');
  console.log('  GET    /users               95% ok, occasional 5xx');
  console.log('  POST   /users               70% 2xx, 20% 4xx, 10% 5xx');
  console.log('  PUT    /users/:id           85% 2xx, 10% 404, 5% 5xx');
  console.log('  DELETE /users/:id           80% 2xx, 15% 404, 5% 5xx');
  console.log('  POST   /login               60% 2xx, 35% 401, 5% 429');
  console.log('  GET    /products/search     90% 2xx, 8% 429, 2% 5xx, slow tail');
  console.log('  POST   /checkout            50% 2xx, 30% 402, 15% 5xx, 5% 429');
  console.log('  GET    /reports/heavy       95% 2xx, 200-500ms baseline');
  console.log(
    `fallback   → 200:${pp(PROB_OK)}  404:${pp(PROB_404)}  429:${pp(PROB_429)}  500:${pp(PROB_500)}`
  );
});
