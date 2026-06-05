<script lang="ts">
  import { onMount, onDestroy, afterUpdate } from 'svelte';
  import * as d3 from 'd3';
  import {
    Plus,
    Trash,
    CaretDown,
    Pencil,
    FloppyDisk,
    Copy,
    Play,
    Stop,
    CheckCircle,
    X,
    PaperPlaneRight,
    CircleNotch,
    Check,
  } from 'phosphor-svelte';
  import {
    StartTest,
    StopTest,
    ListRequests,
    SaveRequest,
    DeleteRequest,
    ListRuns,
    SaveRun,
    DeleteRun,
    SendSample,
    ListFlows,
    SaveFlow,
    DeleteFlow,
  } from '../wailsjs/go/main/App.js';
  import { EventsOn, BrowserOpenURL } from '../wailsjs/runtime/runtime.js';
  import { engine, main } from '../wailsjs/go/models';
  import NumberFlow from './NumberFlow.svelte';
  import logoUrl from './assets/images/loadcell.png';

  type Metrics = {
    elapsedSecs: number;
    totalRequests: number;
    successful: number;
    clientErrors: number;
    rateLimited: number;
    serverErrors: number;
    networkErrors: number;
    errors: number;
    rps: number;
    errorRate: number;
    p50Ms: number;
    p95Ms: number;
    p99Ms: number;
    currentConcurrency: number;
    running: boolean;
  };

  type Sample = {
    t: number;
    tickRps: number;
    tickRpsOk: number;
    p50: number;
    p95: number;
    p99: number;
    conc: number;
  };

  type FlowRunStep = { name: string; method: string; url: string };

  type RunConfig = {
    mode: 'constant' | 'curve';
    concurrency: number;
    durationSecs: number;
    curve?: CurvePt[];
    noise?: number;
    // Populated only when the run was a compound flow. Single-request
    // runs leave these undefined.
    flowId?: string;
    flowName?: string;
    steps?: FlowRunStep[];
  };

  type Run = {
    id: string;
    startedAt: number;          // ms epoch
    name: string;
    method: string;
    url: string;
    config: RunConfig;
    metrics: Metrics;
    history: Sample[];
  };

  type HeaderRow = { key: string; value: string };

  const METHODS = ['GET', 'POST', 'PUT', 'PATCH', 'DELETE', 'HEAD', 'OPTIONS'] as const;

  // Custom method dropdown — native <select> opens a system popup that
  // WKWebView renders modally, which makes the UI hard to drive via
  // accessibility tooling. A plain button+panel works around that.
  let methodOpen = false;
  function toggleMethod() {
    methodOpen = !methodOpen;
  }
  function pickMethod(m: string) {
    reqMethod = m;
    methodOpen = false;
  }
  function handleDocClick(e: MouseEvent) {
    if (!methodOpen) return;
    const t = e.target as HTMLElement | null;
    if (t && !t.closest('.method-wrap')) methodOpen = false;
  }
  function handleDocKey(e: KeyboardEvent) {
    if (methodOpen && e.key === 'Escape') methodOpen = false;
    if (infoOpen && e.key === 'Escape') infoOpen = false;
  }

  // ─── Request-builder state ───────────────────────────────────────────
  let requests: main.SavedRequest[] = [];
  let currentId = '';
  let reqName = '';
  let reqMethod = 'GET';
  let reqUrl = 'http://127.0.0.1:4466';
  let reqHeaders: HeaderRow[] = [{ key: '', value: '' }];
  let reqBody = '';
  let tab: 'headers' | 'body' = 'headers';
  let saveHint = '';
  let tokensExpanded = false;

  // Compound flows. SavedFlow is an ordered list of SavedRequest IDs;
  // activeFlow being non-null switches the loadtest pane into flow mode
  // (StartTest receives a Steps array instead of single URL/method/etc).
  type SavedFlowT = {
    id: string;
    name: string;
    stepIds: string[];
    createdAt: string;
    updatedAt: string;
  };
  let flows: SavedFlowT[] = [];
  let activeFlow: SavedFlowT | null = null;
  // Resolved SavedRequest objects for activeFlow.stepIds, in order. Computed
  // reactively so renames/edits to underlying requests flow through.
  $: activeFlowSteps = activeFlow
    ? (activeFlow.stepIds
        .map((id) => requests.find((r) => r.id === id))
        .filter((r): r is main.SavedRequest => !!r))
    : [];

  // Compose modal state. composeEditingId="" means "creating new", otherwise
  // an edit of an existing flow.
  let composeOpen = false;
  let composeEditingId = '';
  let composeName = '';
  let composeStepIds: string[] = [];

  // Sample-send state — null until the first Send. While `sending` is true
  // the Send button shows a spinner and the existing response (if any) stays
  // visible so the user can compare consecutive hits.
  let sampleResp: main.SampleResponse | null = null;
  let sampleErr = '';
  let sending = false;
  let respTab: 'body' | 'headers' = 'body';

  async function sendSample() {
    if (sending) return;
    sampleErr = '';
    sending = true;
    try {
      const req = main.SavedRequest.createFrom({
        id: currentId,
        name: reqName,
        method: reqMethod,
        url: reqUrl,
        headers: reqHeaders
          .filter((h) => h.key.trim() !== '')
          .map((h) => ({ key: h.key, value: h.value })),
        body: reqBody,
      });
      sampleResp = await SendSample(req);
      respTab = 'body';
      if (sampleResp.error) sampleErr = sampleResp.error;
    } catch (e: any) {
      sampleErr = typeof e === 'string' ? e : e?.message ?? String(e);
    } finally {
      sending = false;
    }
  }

  // Reset response when the user navigates to a different saved request so
  // they don't see Heavy report's body while looking at Login.
  function clearSample() {
    sampleResp = null;
    sampleErr = '';
  }

  // Pretty-print + classify the response body. Returns { text, html } where
  // `text` is the safe-to-display string (pretty-printed JSON or raw) and
  // `html` is non-null only when the body parsed as JSON — in that case it's
  // an HTML-escaped + class-tagged string suitable for {@html ...} render.
  function formatBody(
    body: string,
    contentType: string
  ): { text: string; html: string | null } {
    if (!body) return { text: '', html: null };
    const ct = (contentType || '').toLowerCase();
    const looksJson =
      ct.includes('json') ||
      body.trimStart().startsWith('{') ||
      body.trimStart().startsWith('[');
    if (looksJson) {
      try {
        const pretty = JSON.stringify(JSON.parse(body), null, 2);
        return { text: pretty, html: highlightJson(pretty) };
      } catch {
        // not actually JSON; fall through
      }
    }
    return { text: body, html: null };
  }

  // Tiny inline JSON highlighter. Tokens are classed so the CSS palette
  // (keys / strings / numbers / bools / null) can match the rest of the app.
  // Always HTML-escape *before* any wrapping so a malicious response body
  // can't inject markup.
  function highlightJson(s: string): string {
    const escaped = s
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;');
    return escaped.replace(
      /("(?:\\u[a-fA-F0-9]{4}|\\[^u]|[^\\"])*"(\s*:)?|\b(?:true|false|null)\b|-?\d+(?:\.\d+)?(?:[eE][+-]?\d+)?)/g,
      (match) => {
        let cls = 'jh-num';
        if (match.startsWith('"')) {
          cls = /:\s*$/.test(match) ? 'jh-key' : 'jh-str';
        } else if (match === 'true' || match === 'false') {
          cls = 'jh-bool';
        } else if (match === 'null') {
          cls = 'jh-null';
        }
        return `<span class="${cls}">${match}</span>`;
      }
    );
  }

  // HTML-escape — used before wrapping anything as highlight markup so a
  // pasted "<script>" in the URL/body can't inject DOM.
  function escapeHtml(s: string): string {
    return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
  }

  // Token recognizer. Mirrors the Go parser (tmpl/tmpl.go) — anything not
  // matched here is treated as literal text, so a typo like "{randInt:1:5}"
  // simply doesn't get highlighted, signaling to the user that it won't
  // render as a token. Whitespace inside the braces is tolerated to match
  // Go-side trimming.
  const TOKEN_RE = /\{\{\s*(uuid|seq|nowMs|randInt:\s*-?\d+\s*:\s*-?\d+)\s*\}\}/g;

  function highlightTokens(htmlOrText: string): string {
    return htmlOrText.replace(TOKEN_RE, (match, inner: string) => {
      if (inner.startsWith('randInt:')) {
        const nums = inner
          .slice('randInt:'.length)
          .split(':')
          .map((p) => parseInt(p.trim(), 10));
        // Reject inverted ranges (min > max) — the Go parser also rejects
        // these, so the highlight stays honest about what will render.
        if (nums.length !== 2 || nums.some(Number.isNaN) || nums[0] > nums[1]) {
          return match;
        }
      }
      return `<span class="lt-tok">${match}</span>`;
    });
  }

  // Request body highlighting — overlay strategy: a styled <pre> sits behind
  // a transparent-text <textarea>. Both share font/padding/wrap so glyph
  // positions line up; scroll is mirrored on every input/scroll event.
  let bodyOverlayEl: HTMLPreElement | null = null;
  let bodyTextareaEl: HTMLTextAreaElement | null = null;

  // URL overlay — same idea applied to a single-line <input>. Horizontal
  // scroll on the input gets mirrored to the overlay so tokens stay aligned.
  let urlOverlayEl: HTMLDivElement | null = null;
  let urlInputEl: HTMLInputElement | null = null;

  // Header value overlays — one per header row. Refs and highlight HTML are
  // parallel to reqHeaders by index. Adding/removing rows reshapes the
  // arrays via the reactive .map below.
  let hValueOverlayEls: (HTMLDivElement | null)[] = [];
  let hValueInputEls: (HTMLInputElement | null)[] = [];

  $: reqBodyIsJson = (() => {
    const t = reqBody.trimStart();
    return t.startsWith('{') || t.startsWith('[');
  })();
  // Layer order: HTML-escape → JSON highlight (if JSON-shaped) → token
  // highlight. The token regex matches {{...}} sequences that survive both
  // earlier steps untouched, so they compose without re-escaping.
  $: reqBodyHighlight = reqBody
    ? highlightTokens(reqBodyIsJson ? highlightJson(reqBody) : escapeHtml(reqBody))
    : '';
  $: reqUrlHighlight = reqUrl ? highlightTokens(escapeHtml(reqUrl)) : '';
  $: hValueHighlights = reqHeaders.map((h) =>
    h.value ? highlightTokens(escapeHtml(h.value)) : ''
  );

  // Keep overlay scroll in lockstep with the textarea so highlighted spans
  // stay aligned with what the user is typing.
  function syncBodyOverlayScroll() {
    if (!bodyOverlayEl || !bodyTextareaEl) return;
    bodyOverlayEl.scrollTop = bodyTextareaEl.scrollTop;
    bodyOverlayEl.scrollLeft = bodyTextareaEl.scrollLeft;
  }
  // Re-sync after value changes too (programmatic edits like Format).
  $: if (bodyOverlayEl && bodyTextareaEl) {
    void reqBody;
    requestAnimationFrame(syncBodyOverlayScroll);
  }

  // Single-line URL input scrolls horizontally as it overflows; mirror that
  // to the overlay so the highlighted spans track the caret.
  function syncUrlOverlayScroll() {
    if (!urlOverlayEl || !urlInputEl) return;
    urlOverlayEl.scrollLeft = urlInputEl.scrollLeft;
  }
  $: if (urlOverlayEl && urlInputEl) {
    void reqUrl;
    requestAnimationFrame(syncUrlOverlayScroll);
  }

  // Per-row header-value overlay sync. Index-keyed because the row count
  // changes when the user adds/removes headers.
  function syncHeaderOverlay(i: number) {
    const o = hValueOverlayEls[i];
    const inp = hValueInputEls[i];
    if (!o || !inp) return;
    o.scrollLeft = inp.scrollLeft;
  }
  $: hValueHighlights.forEach((_, i) => {
    if (hValueOverlayEls[i] && hValueInputEls[i]) {
      requestAnimationFrame(() => syncHeaderOverlay(i));
    }
  });

  function formatRequestBody() {
    try {
      const pretty = JSON.stringify(JSON.parse(reqBody), null, 2);
      reqBody = pretty;
    } catch {
      // user knows they have invalid JSON; no-op
    }
  }

  // Clipboard helpers — each copy button is identified by a string key so
  // multiple "Copied!" badges don't all light up at once.
  let copiedKey = '';
  let copyResetTimer: number | undefined;
  async function copyToClipboard(text: string, key: string) {
    try {
      await navigator.clipboard.writeText(text);
      copiedKey = key;
      if (copyResetTimer) clearTimeout(copyResetTimer);
      copyResetTimer = window.setTimeout(() => {
        if (copiedKey === key) copiedKey = '';
      }, 1400);
    } catch (e) {
      // Clipboard can fail in non-secure contexts; fall back silently.
      console.warn('clipboard write failed:', e);
    }
  }
  function headersAsText(h: Record<string, string> | null | undefined): string {
    if (!h) return '';
    return Object.entries(h)
      .map(([k, v]) => `${k}: ${v}`)
      .join('\n');
  }

  function statusClass(status: number): string {
    if (status === 0) return 's-err';
    if (status >= 500) return 's-5xx';
    if (status === 429) return 's-429';
    if (status >= 400) return 's-4xx';
    if (status >= 300) return 's-3xx';
    if (status >= 200) return 's-2xx';
    return '';
  }

  function fmtBytes(n: number): string {
    if (n < 1024) return `${n} B`;
    if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
    return `${(n / (1024 * 1024)).toFixed(2)} MB`;
  }

  // ─── Load-profile state ──────────────────────────────────────────────
  type CurveType = 'linear' | 'exponential' | 'step';
  type CurvePt = { timeSecs: number; users: number; curveIn: CurveType; exponent: number };

  // Ceiling on concurrent workers — clamps the editor + protects the engine
  // from spawning thousands of goroutines by accident. User-settable from
  // the info sheet, persisted via localStorage so the choice survives
  // restarts. 2000 is the "now you're on your own" threshold above which we
  // warn but still allow.
  const MAX_WORKERS_DEFAULT = 2000;
  const MAX_WORKERS_FLOOR = 10;
  const MAX_WORKERS_CEIL = 20000; // a hard sanity ceiling for typed input
  function loadMaxWorkers(): number {
    if (typeof localStorage === 'undefined') return MAX_WORKERS_DEFAULT;
    const raw = parseInt(localStorage.getItem('lc.maxWorkers') ?? '', 10);
    if (!Number.isFinite(raw) || raw < MAX_WORKERS_FLOOR) return MAX_WORKERS_DEFAULT;
    return Math.min(MAX_WORKERS_CEIL, raw);
  }
  let MAX_WORKERS = loadMaxWorkers();
  $: if (typeof localStorage !== 'undefined') {
    localStorage.setItem('lc.maxWorkers', String(MAX_WORKERS));
  }

  let mode: 'constant' | 'curve' = 'constant';
  let concurrency = 10;          // peak workers, fixed mode
  let durationSecs = 10;         // total test length
  let curvePoints: CurvePt[] = [
    { timeSecs: 0, users: 0, curveIn: 'linear', exponent: 2 },
    { timeSecs: 5, users: 10, curveIn: 'linear', exponent: 2 },
    { timeSecs: 10, users: 10, curveIn: 'linear', exponent: 2 },
  ];
  let noise = 0;                  // 0..1
  let selectedPtIdx = -1;
  let draggingPtIdx = -1;
  let dragMoved = false;
  let dragStartClientX = 0;
  let dragStartClientY = 0;
  let curveSvgEl: SVGSVGElement | null = null;

  // Friendly time formatting for the preview chart + caption. Always rounds
  // to 1 decimal so dragged float values don't bleed full IEEE precision into
  // the UI ("5.132259148269519s" → "5.1s").
  function fmtDur(s: number): string {
    if (!Number.isFinite(s) || s <= 0) return '0s';
    const rounded = Math.round(s * 10) / 10;
    if (rounded < 60) {
      return rounded === Math.floor(rounded) ? `${rounded}s` : `${rounded.toFixed(1)}s`;
    }
    const m = rounded / 60;
    if (m === Math.floor(m)) return `${m} ${m === 1 ? 'min' : 'mins'}`;
    return `${m.toFixed(1)} mins`;
  }

  function clampUsers(n: number): number {
    if (!Number.isFinite(n)) return 0;
    return Math.max(0, Math.min(MAX_WORKERS, Math.round(n)));
  }

  // Geometry for the preview chart / interactive curve editor.
  const PV = { w: 460, h: 220, l: 48, r: 22, t: 28, b: 32 };
  $: PV_innerW = PV.w - PV.l - PV.r;
  $: PV_innerH = PV.h - PV.t - PV.b;

  // Y scale derives from the curve's peak (with some headroom) so dragging up
  // is always meaningful.
  $: curveMaxUsers = Math.max(
    1,
    ...curvePoints.map((p) => p.users),
    mode === 'constant' ? concurrency : 0
  );
  // y-axis headroom factors in the noise envelope so the +noise dotted line
  // never clips above the top edge. Capped at 1.6× MAX_WORKERS so silly noise
  // values can't push the axis off into infinity.
  $: yAxisMax = Math.min(
    Math.ceil(MAX_WORKERS * 1.6),
    Math.max(
      4,
      Math.ceil(
        Math.max(curveMaxUsers, mode === 'constant' ? concurrency : 0) *
          (1 + Math.max(0, noise)) *
          1.1
      )
    )
  );
  $: safeDur = Math.max(1, Number.isFinite(durationSecs) ? durationSecs : 1);
  $: xAtT = (t: number) => PV.l + (t / safeDur) * PV_innerW;
  $: yAtN = (n: number) => PV.t + (1 - n / yAxisMax) * PV_innerH;
  $: tAtX = (x: number) => clamp(((x - PV.l) / PV_innerW) * safeDur, 0, safeDur);
  $: nAtY = (y: number) =>
    clamp((1 - (y - PV.t) / PV_innerH) * yAxisMax, 0, yAxisMax);

  function clamp(v: number, lo: number, hi: number) {
    return v < lo ? lo : v > hi ? hi : v;
  }

  // Build the visible polyline that passes through every keypoint, honouring
  // each segment's curve type. Exponential segments get sampled so the curve
  // is visibly … curved.
  function buildCurvePath(pts: CurvePt[], close: boolean): string {
    if (pts.length === 0) return '';
    const cmds: string[] = [];
    cmds.push(`M ${xAtT(pts[0].timeSecs).toFixed(1)} ${yAtN(pts[0].users).toFixed(1)}`);
    for (let i = 1; i < pts.length; i++) {
      const a = pts[i - 1];
      const b = pts[i];
      const x1 = xAtT(b.timeSecs);
      if (b.curveIn === 'step') {
        cmds.push(`L ${x1.toFixed(1)} ${yAtN(a.users).toFixed(1)}`);
        cmds.push(`L ${x1.toFixed(1)} ${yAtN(b.users).toFixed(1)}`);
      } else if (b.curveIn === 'exponential') {
        const exp = b.exponent > 0 ? b.exponent : 2;
        const samples = 28;
        for (let s = 1; s <= samples; s++) {
          const p = s / samples;
          const eased = Math.pow(p, exp);
          const t = a.timeSecs + (b.timeSecs - a.timeSecs) * p;
          const u = a.users + (b.users - a.users) * eased;
          cmds.push(`L ${xAtT(t).toFixed(1)} ${yAtN(u).toFixed(1)}`);
        }
      } else {
        cmds.push(`L ${x1.toFixed(1)} ${yAtN(b.users).toFixed(1)}`);
      }
    }
    // Tail: hold the last user count until duration ends.
    const last = pts[pts.length - 1];
    if (last.timeSecs < safeDur) {
      cmds.push(`L ${xAtT(safeDur).toFixed(1)} ${yAtN(last.users).toFixed(1)}`);
    }
    if (close) {
      cmds.push(`L ${xAtT(safeDur).toFixed(1)} ${yAtN(0).toFixed(1)}`);
      cmds.push(`L ${xAtT(0).toFixed(1)} ${yAtN(0).toFixed(1)}`);
      cmds.push('Z');
    }
    return cmds.join(' ');
  }

  // Path strings depend on yAxisMax + safeDur via xAtT/yAtN, but Svelte 3's
  // compile-time reactivity can't see through buildCurvePath. The comma
  // expression registers both as explicit deps so the line redraws when
  // either axis rescales (noise change, duration change, etc.).
  $: curveLinePath = (yAxisMax, safeDur, buildCurvePath(curvePoints, false));
  $: curveAreaPath = (yAxisMax, safeDur, buildCurvePath(curvePoints, true));

  // ─── Noise envelope ───────────────────────────────────────────────
  // Sample the deterministic curve at fixed intervals across [0, safeDur],
  // then scale each sample by (1 ± noise) to get the upper/lower bounds.
  function evalAtT(pts: CurvePt[], t: number): number {
    if (pts.length === 0) return 0;
    if (t <= pts[0].timeSecs) return pts[0].users;
    for (let i = 1; i < pts.length; i++) {
      if (t <= pts[i].timeSecs) {
        const a = pts[i - 1];
        const b = pts[i];
        const dt = b.timeSecs - a.timeSecs;
        if (dt <= 0) return b.users;
        let p = (t - a.timeSecs) / dt;
        if (p < 0) p = 0;
        else if (p > 1) p = 1;
        if (b.curveIn === 'step') return p >= 1 ? b.users : a.users;
        if (b.curveIn === 'exponential') {
          const exp = b.exponent > 0 ? b.exponent : 2;
          p = Math.pow(p, exp);
        }
        return a.users + (b.users - a.users) * p;
      }
    }
    return pts[pts.length - 1].users;
  }

  function sampleEnvelopePoints(
    pts: CurvePt[],
    n: number,
    scale: number
  ): string[] {
    const samples = 80;
    const out: string[] = [];
    for (let i = 0; i <= samples; i++) {
      const t = (i / samples) * safeDur;
      const u = evalAtT(pts, t);
      const scaled = Math.max(0, u * scale);
      out.push(`${xAtT(t).toFixed(1)},${yAtN(scaled).toFixed(1)}`);
    }
    return out;
  }

  $: noiseUpperPts = noise > 0
    ? (yAxisMax, safeDur, sampleEnvelopePoints(curvePoints, 0, 1 + noise))
    : [];
  $: noiseLowerPts = noise > 0
    ? (yAxisMax, safeDur, sampleEnvelopePoints(curvePoints, 0, 1 - noise))
    : [];
  $: noiseUpperPath = noiseUpperPts.length > 0 ? `M ${noiseUpperPts.join(' L ')}` : '';
  $: noiseLowerPath = noiseLowerPts.length > 0 ? `M ${noiseLowerPts.join(' L ')}` : '';
  // Closed polygon: forward upper → reverse lower → close.
  $: noiseHatchPath =
    noiseUpperPts.length > 0
      ? `M ${noiseUpperPts.join(' L ')} L ${noiseLowerPts.slice().reverse().join(' L ')} Z`
      : '';

  // Constant-mode preview is just a flat line at `concurrency`.
  $: previewLine =
    `M ${xAtT(0)} ${yAtN(concurrency)} L ${xAtT(safeDur)} ${yAtN(concurrency)}`;
  $: previewArea =
    `M ${xAtT(0)} ${yAtN(concurrency)} L ${xAtT(safeDur)} ${yAtN(concurrency)} ` +
    `L ${xAtT(safeDur)} ${yAtN(0)} L ${xAtT(0)} ${yAtN(0)} Z`;

  $: previewCaption =
    mode === 'curve'
      ? describeCurve(curvePoints, safeDur, noise)
      : `Hold ${concurrency} parallel workers for ${fmtDur(safeDur)}.`;

  function describeCurve(pts: CurvePt[], dur: number, n: number): string {
    if (pts.length < 2) return 'Add at least two points.';
    const peak = Math.max(...pts.map((p) => p.users));
    const last = pts[pts.length - 1];
    const lastTail = dur - last.timeSecs;
    const segs = pts.slice(1).map((p, i) => {
      const a = pts[i];
      const shape =
        p.curveIn === 'exponential'
          ? `exp(${(p.exponent || 2).toFixed(1)})`
          : p.curveIn;
      return `${Math.round(a.users)}→${Math.round(p.users)} ${shape} in ${fmtDur(p.timeSecs - a.timeSecs)}`;
    });
    const noiseSuffix = n > 0 ? ` with ±${Math.round(n * 100)}% noise` : '';
    const tail = lastTail > 0 ? `, then hold ${Math.round(last.users)} for ${fmtDur(lastTail)}` : '';
    return `Peak ${peak} workers — ${segs.join(' · ')}${tail}${noiseSuffix}.`;
  }

  // ─── Curve editor interaction ─────────────────────────────────────
  function svgCoordsFromEvent(e: PointerEvent): { x: number; y: number } | null {
    if (!curveSvgEl) return null;
    const rect = curveSvgEl.getBoundingClientRect();
    const sx = ((e.clientX - rect.left) / rect.width) * PV.w;
    const sy = ((e.clientY - rect.top) / rect.height) * PV.h;
    return { x: sx, y: sy };
  }

  function onHandlePointerDown(e: PointerEvent, idx: number) {
    if (running) return;
    // Only left-button starts a drag/selection. Right-click is reserved for
    // the contextmenu handler (which deletes); letting it set draggingPtIdx
    // here means a subsequent pointerup would re-select the just-deleted
    // index and crash the point-edit panel.
    if (e.button !== 0) return;
    e.stopPropagation();
    (e.target as Element).setPointerCapture(e.pointerId);
    draggingPtIdx = idx;
    dragMoved = false;
    dragStartClientX = e.clientX;
    dragStartClientY = e.clientY;
  }

  function onCurvePointerMove(e: PointerEvent) {
    if (draggingPtIdx < 0) return;
    if (!dragMoved) {
      const dx = Math.abs(e.clientX - dragStartClientX);
      const dy = Math.abs(e.clientY - dragStartClientY);
      if (dx > 3 || dy > 3) dragMoved = true;
    }
    const coords = svgCoordsFromEvent(e);
    if (!coords) return;
    const t = tAtX(coords.x);
    const u = nAtY(coords.y);
    const pts = curvePoints.slice();
    const idx = draggingPtIdx;
    if (idx === 0) {
      pts[idx] = { ...pts[idx], users: clampUsers(u) };
    } else {
      const minT = pts[idx - 1].timeSecs + 0.1;
      const maxT = idx < pts.length - 1 ? pts[idx + 1].timeSecs - 0.1 : safeDur;
      // Round to one decimal so dragged values stay tidy.
      const tSnapped = Math.round(Math.max(minT, Math.min(maxT, t)) * 10) / 10;
      pts[idx] = {
        ...pts[idx],
        timeSecs: tSnapped,
        users: clampUsers(u),
      };
    }
    curvePoints = pts;
  }

  function onCurvePointerUp(e: PointerEvent) {
    if (draggingPtIdx < 0) return;
    const wasIdx = draggingPtIdx;
    draggingPtIdx = -1;
    if (!dragMoved) {
      // Defense: the captured handle may have been deleted between pointerdown
      // and pointerup (right-click delete races). Don't select a stale index.
      if (wasIdx >= curvePoints.length) {
        selectedPtIdx = -1;
        return;
      }
      selectedPtIdx = selectedPtIdx === wasIdx ? -1 : wasIdx;
    }
  }

  function onCurveBackgroundClick(e: MouseEvent) {
    if (running) return;
    const target = e.target as Element;
    // Ignore clicks on handles (those toggle selection via pointerup).
    if (target.closest('.lc-handle')) return;
    if (!curveSvgEl) return;
    const rect = curveSvgEl.getBoundingClientRect();
    const sx = ((e.clientX - rect.left) / rect.width) * PV.w;
    const sy = ((e.clientY - rect.top) / rect.height) * PV.h;
    if (sx < PV.l || sx > PV.w - PV.r || sy < PV.t || sy > PV.h - PV.b) return;
    const t = tAtX(sx);
    const u = nAtY(sy);
    const pts = curvePoints.slice();
    let insertAt = pts.findIndex((p) => p.timeSecs > t);
    if (insertAt === -1) insertAt = pts.length;
    pts.splice(insertAt, 0, {
      timeSecs: Math.round(t * 10) / 10,
      users: clampUsers(u),
      curveIn: 'linear',
      exponent: 2,
    });
    curvePoints = pts;
    selectedPtIdx = insertAt;
  }

  function setSelectedCurve(c: CurveType) {
    if (selectedPtIdx <= 0) return; // first point has no curveIn
    const pts = curvePoints.slice();
    pts[selectedPtIdx] = { ...pts[selectedPtIdx], curveIn: c };
    curvePoints = pts;
  }

  function setSelectedExponent(v: number) {
    if (selectedPtIdx <= 0) return;
    const pts = curvePoints.slice();
    pts[selectedPtIdx] = { ...pts[selectedPtIdx], exponent: v };
    curvePoints = pts;
  }

  function onExponentInput(e: Event) {
    const v = parseFloat((e.target as HTMLInputElement).value);
    if (Number.isFinite(v)) setSelectedExponent(v);
  }

  function deletePointAt(idx: number) {
    if (idx <= 0) return;                // first point is anchored to t=0
    if (curvePoints.length <= 2) return; // engine needs at least 2 points
    const pts = curvePoints.slice();
    pts.splice(idx, 1);
    curvePoints = pts;
    if (selectedPtIdx === idx) selectedPtIdx = -1;
    else if (selectedPtIdx > idx) selectedPtIdx -= 1;
  }

  function deleteSelected() {
    deletePointAt(selectedPtIdx);
  }

  function onHandleContextMenu(e: MouseEvent, idx: number) {
    e.preventDefault();
    e.stopPropagation();
    if (running) return;
    deletePointAt(idx);
  }

  function suppressContextMenu(e: MouseEvent) {
    // Block the OS menu inside the chart area so right-click feels like a
    // first-class editor gesture.
    e.preventDefault();
  }

  function resetCurve() {
    curvePoints = [
      { timeSecs: 0, users: 0, curveIn: 'linear', exponent: 2 },
      { timeSecs: Math.max(2, Math.floor(safeDur / 2)), users: 10, curveIn: 'linear', exponent: 2 },
      { timeSecs: safeDur, users: 10, curveIn: 'linear', exponent: 2 },
    ];
    selectedPtIdx = -1;
    noise = 0;
    lastClampDur = safeDur;
  }

  // Numeric handle on the curve's peak. Scales all points proportionally so
  // the shape is preserved. If the curve is currently flat at 0, raise the
  // non-anchor points to the new peak so the user gets a meaningful curve
  // instead of an inert one.
  function onCurvePeakInput(e: Event) {
    const raw = parseInt((e.target as HTMLInputElement).value, 10);
    if (!Number.isFinite(raw)) return;
    const target = clampUsers(raw);
    const cur = curveMaxUsers;
    if (cur <= 0) {
      const pts = curvePoints.map((p, i) => ({
        ...p,
        users: i === 0 ? p.users : target, // keep the t=0 anchor as-is
      }));
      curvePoints = pts;
      return;
    }
    const ratio = target / cur;
    curvePoints = curvePoints.map((p) => ({
      ...p,
      users: clampUsers(p.users * ratio),
    }));
  }

  // Keep the keypoints inside the chart when the user shrinks Test duration.
  // Out-of-range points get dropped, and a closing point is added at the new
  // duration so the last segment still terminates somewhere visible.
  let lastClampDur = durationSecs;
  $: maybeClampCurveToDuration(durationSecs);

  function maybeClampCurveToDuration(d: number) {
    if (!Number.isFinite(d) || d <= 0 || running) return;
    if (d === lastClampDur) return;
    lastClampDur = d;
    const needsUpdate = curvePoints.some((p, i) => i > 0 && p.timeSecs > d);
    if (!needsUpdate) return;
    const kept = curvePoints.filter((p, i) => i === 0 || p.timeSecs <= d);
    const last = kept[kept.length - 1];
    if (last.timeSecs < d) {
      kept.push({
        timeSecs: d,
        users: last.users,
        curveIn: 'linear',
        exponent: 2,
      });
    }
    // Always keep at least two points (engine validation requires this).
    if (kept.length < 2) {
      kept.push({
        timeSecs: d,
        users: kept[0].users,
        curveIn: 'linear',
        exponent: 2,
      });
    }
    curvePoints = kept;
    // If the user had a point selected that fell off, clear the selection.
    if (selectedPtIdx >= curvePoints.length) selectedPtIdx = -1;
  }

  $: gridYTicks = (() => {
    const out: number[] = [];
    const step = niceStep(yAxisMax);
    for (let v = 0; v <= yAxisMax + step * 0.0001; v += step) out.push(v);
    return out;
  })();

  function niceStep(max: number): number {
    if (max <= 0) return 1;
    const exp = Math.floor(Math.log10(max));
    const base = Math.pow(10, exp);
    const norm = max / base;
    let s: number;
    if (norm < 1.5) s = 0.2 * base;
    else if (norm < 3) s = 0.5 * base;
    else if (norm < 7) s = 1 * base;
    else s = 2 * base;
    return Math.max(1, s);
  }

  // ─── Run state ───────────────────────────────────────────────────────
  let running = false;
  let errorMsg = '';
  let metrics: Metrics | null = null;
  let history: Sample[] = [];
  let prevMetrics: Metrics | null = null;
  let plannedDuration = durationSecs;
  let unsub: (() => void) | null = null;

  // Saved runs — in-memory only for now. Each completed run snapshots
  // metrics + history + request + config so the user can tab back to it.
  let runs: Run[] = [];
  // -1 = view live data, 0..N-1 = view runs[i]. Starts at -1 so the first
  // Start jumps straight to the live tab.
  let activeRunIdx = -1;
  let runStartTs = 0;

  // Top-level view router. Auto-switches to 'results' when a run starts or
  // a saved-run tab is selected; user can navigate freely otherwise.
  type View = 'request' | 'loadtest' | 'results';
  let view: View = 'request';

  // Info sheet — opened by clicking the brand mark in the topnav.
  let infoOpen = false;
  function openInfo() { infoOpen = true; }
  function closeInfo() { infoOpen = false; }
  function openRobiWork() { BrowserOpenURL('https://robi.work'); }
  function openSponsor() { BrowserOpenURL('https://github.com/sponsors/RobiMez'); }

  function snapshotRun(m: Metrics) {
    if (history.length === 0) return;
    // Flow runs: snapshot the resolved step list and flow name into
    // config so the saved-run card can show the flow's identity instead
    // of stale single-request fields. URL/Method on the run itself stay
    // empty for flow runs — the sidebar branches on config.steps.
    const flowConfig = activeFlow
      ? {
          flowId: activeFlow.id,
          flowName: activeFlow.name,
          steps: activeFlowSteps.map((r) => ({
            name: r.name || 'Untitled',
            method: r.method,
            url: r.url,
          })),
        }
      : {};
    const run: Run = {
      id: '', // backend assigns
      startedAt: runStartTs || Date.now(),
      name: activeFlow ? activeFlow.name : reqName || 'Untitled',
      method: activeFlow ? '' : reqMethod,
      url: activeFlow ? '' : reqUrl,
      config: {
        mode,
        concurrency,
        durationSecs,
        curve: mode === 'curve' ? curvePoints.map((p) => ({ ...p })) : undefined,
        noise: mode === 'curve' ? noise : undefined,
        ...flowConfig,
      },
      metrics: { ...m },
      history: history.map((s) => ({ ...s })),
    };
    // Optimistic: insert immediately, then replace with backend-persisted
    // copy when SaveRun resolves so the user-visible id matches disk.
    runs = [run, ...runs];
    activeRunIdx = 0;
    SaveRun(run as any).then((persisted) => {
      const i = runs.findIndex((r) => r === run);
      if (i >= 0) {
        const next = runs.slice();
        next[i] = persisted as unknown as Run;
        runs = next;
        // Keep activeRunIdx pointed at the same run if it was selected.
      }
    }).catch((e) => {
      console.error('SaveRun failed:', e);
    });
  }

  function selectRun(i: number) {
    activeRunIdx = i;
    hoverIdx = -1;
    view = 'results';
  }

  function deleteRun(i: number) {
    if (i < 0 || i >= runs.length) return;
    const doomed = runs[i];
    const next = runs.slice();
    next.splice(i, 1);
    runs = next;
    if (activeRunIdx === i) {
      // Prefer the next-older run, else the live view if available.
      activeRunIdx = runs.length > 0 ? Math.min(i, runs.length - 1) : -1;
    } else if (activeRunIdx > i) {
      activeRunIdx -= 1;
    }
    hoverIdx = -1;
    if (doomed?.id) {
      DeleteRun(doomed.id).catch((e) => console.error('DeleteRun failed:', e));
    }
  }

  function fmtRunTime(ts: number): string {
    const d = new Date(ts);
    const h = String(d.getHours()).padStart(2, '0');
    const m = String(d.getMinutes()).padStart(2, '0');
    return `${h}:${m}`;
  }

  onMount(async () => {
    document.addEventListener('click', handleDocClick);
    document.addEventListener('keydown', handleDocKey);
    unsub = EventsOn('metrics', (m: Metrics) => {
      let tickRps = 0;
      let tickRpsOk = 0;
      if (prevMetrics) {
        const dt = m.elapsedSecs - prevMetrics.elapsedSecs;
        const dn = m.totalRequests - prevMetrics.totalRequests;
        const dnOk = m.successful - prevMetrics.successful;
        tickRps = dt > 0 ? dn / dt : 0;
        tickRpsOk = dt > 0 ? dnOk / dt : 0;
      } else {
        tickRps = m.rps;
        tickRpsOk =
          m.totalRequests > 0
            ? m.rps * (m.successful / m.totalRequests)
            : m.rps;
      }
      history = [
        ...history,
        {
          t: m.elapsedSecs,
          tickRps,
          tickRpsOk,
          p50: m.p50Ms,
          p95: m.p95Ms,
          p99: m.p99Ms,
          conc: m.currentConcurrency,
        },
      ];
      const wasRunning = running;
      prevMetrics = m;
      metrics = m;
      running = m.running;
      // When the engine flips running false, snapshot the just-completed
      // run into the tabs list and switch the view to it.
      if (wasRunning && !running) snapshotRun(m);
    });

    try {
      requests = await ListRequests();
      if (requests.length > 0) loadRequest(requests[0]);
      else newRequest();
    } catch (e: any) {
      console.error('Failed to load requests:', e);
      newRequest();
    }

    try {
      const saved = await ListRuns();
      // Backend gives newest-first; cast through unknown because frontend Run
      // uses literal types ('constant' | 'curve', etc.) that the generated
      // SavedRun loosens to string.
      runs = (saved ?? []) as unknown as Run[];
    } catch (e: any) {
      console.error('Failed to load runs:', e);
    }

    await refreshFlows();
  });

  onDestroy(() => {
    if (unsub) unsub();
    document.removeEventListener('click', handleDocClick);
    document.removeEventListener('keydown', handleDocKey);
    if (chartResizeObs) chartResizeObs.disconnect();
  });

  // ─── Chart sizing — track chart-wrap rect → drive viewBox ─────────────
  let chartResizeObs: ResizeObserver | null = null;
  let chartResizeRaf = 0;

  function observeChartWrap(el: HTMLDivElement | null) {
    if (chartResizeObs) {
      chartResizeObs.disconnect();
      chartResizeObs = null;
    }
    if (!el || typeof ResizeObserver === 'undefined') return;
    chartResizeObs = new ResizeObserver(() => {
      // Coalesce rapid fires (drag-resize) into one rAF.
      if (chartResizeRaf) cancelAnimationFrame(chartResizeRaf);
      chartResizeRaf = requestAnimationFrame(() => {
        chartResizeRaf = 0;
        if (!chartWrapEl) return;
        const wrapRect = chartWrapEl.getBoundingClientRect();
        const legend = chartWrapEl.querySelector('.chart-legend') as HTMLElement | null;
        const legendH = legend ? legend.getBoundingClientRect().height + 8 : 28;
        // chart-wrap padding (14px sides, 14px top, 10px bottom) is already
        // outside the SVG since the SVG fills the content box; account for
        // the legend row above the SVG.
        const w = Math.max(320, Math.round(wrapRect.width - 28));
        const h = Math.max(220, Math.round(wrapRect.height - 14 - 10 - legendH));
        if (w !== CW || h !== CH) {
          CW = w;
          CH = h;
          chartInitialized = false; // force setupChart to re-run
        }
      });
    });
    chartResizeObs.observe(el);
  }

  // Re-observe whenever the binding to chartWrapEl appears (Results view mount).
  $: observeChartWrap(chartWrapEl);

  // ─── Request CRUD ────────────────────────────────────────────────────
  function newRequest() {
    activeFlow = null;
    currentId = '';
    reqName = '';
    reqMethod = 'GET';
    reqUrl = 'http://127.0.0.1:4466';
    reqHeaders = [{ key: '', value: '' }];
    reqBody = '';
    tab = 'headers';
    saveHint = '';
  }

  function loadRequest(r: main.SavedRequest) {
    currentId = r.id;
    reqName = r.name;
    reqMethod = r.method || 'GET';
    reqUrl = r.url;
    reqHeaders =
      r.headers && r.headers.length > 0
        ? r.headers.map((h) => ({ key: h.key, value: h.value }))
        : [{ key: '', value: '' }];
    reqBody = r.body || '';
    tab = reqBody ? 'body' : 'headers';
    saveHint = '';
    clearSample();
  }

  // Sidebar click handler. Loads the request, then routes:
  //  - from Results: jump to Load test (user is picking what to test next).
  //  - from Request or Load test: stay put (user is editing or sequencing).
  function pickRequest(r: main.SavedRequest) {
    // Leaving flow mode: a single-request selection takes precedence.
    activeFlow = null;
    loadRequest(r);
    if (view === 'results') view = 'loadtest';
  }

  async function refreshList() {
    requests = await ListRequests();
  }

  function trimmedHeaders(): main.HeaderKV[] {
    return reqHeaders
      .filter((h) => h.key.trim() !== '')
      .map((h) =>
        main.HeaderKV.createFrom({ key: h.key.trim(), value: h.value })
      );
  }

  async function saveCurrent() {
    try {
      const req = main.SavedRequest.createFrom({
        id: currentId,
        name: reqName.trim() || 'Untitled',
        method: reqMethod,
        url: reqUrl,
        headers: trimmedHeaders(),
        body: reqBody,
        createdAt: '',
        updatedAt: '',
      });
      const saved = await SaveRequest(req);
      currentId = saved.id;
      reqName = saved.name;
      await refreshList();
      flashSaved('Saved');
    } catch (e: any) {
      saveHint = `Save failed: ${e?.message ?? e}`;
    }
  }

  async function saveAsNew() {
    currentId = '';
    await saveCurrent();
  }

  async function deleteRequest(id: string) {
    if (!confirm('Delete this saved request?')) return;
    try {
      await DeleteRequest(id);
      if (id === currentId) newRequest();
      await refreshList();
    } catch (e: any) {
      saveHint = `Delete failed: ${e?.message ?? e}`;
    }
  }

  // ─── Flow operations ─────────────────────────────────────────────
  async function refreshFlows() {
    try {
      const fs = await ListFlows();
      flows = (fs ?? []) as SavedFlowT[];
      // If a flow is currently selected and its definition changed on disk,
      // refresh activeFlow from the list so step changes propagate.
      if (activeFlow) {
        const updated = flows.find((f) => f.id === activeFlow!.id);
        activeFlow = updated || null;
        if (!activeFlow && view === 'loadtest') view = 'request';
      }
    } catch (e: any) {
      console.error('Failed to load flows:', e);
    }
  }

  function selectFlow(f: SavedFlowT) {
    // Entering flow mode: clear single-request selection so the load-test
    // pane shows the flow header instead of an unrelated request card.
    currentId = '';
    activeFlow = f;
    activeRunIdx = -1;
    view = 'loadtest';
  }

  async function deleteFlowEntry(id: string) {
    if (!confirm('Delete this saved flow?')) return;
    try {
      await DeleteFlow(id);
      if (activeFlow?.id === id) {
        activeFlow = null;
        if (view === 'loadtest') view = 'request';
      }
      await refreshFlows();
    } catch (e: any) {
      console.error('Delete flow failed:', e);
    }
  }

  // ─── Compose modal ───────────────────────────────────────────────
  function openCompose(f?: SavedFlowT) {
    composeEditingId = f?.id || '';
    composeName = f?.name || '';
    composeStepIds = f ? [...f.stepIds] : [];
    composeOpen = true;
  }

  function closeCompose() {
    composeOpen = false;
    composeEditingId = '';
    composeName = '';
    composeStepIds = [];
  }

  function composeAddStep(reqId: string) {
    composeStepIds = [...composeStepIds, reqId];
  }

  function composeRemoveStep(idx: number) {
    composeStepIds = composeStepIds.filter((_, i) => i !== idx);
  }

  function composeMoveStep(idx: number, dir: -1 | 1) {
    const next = idx + dir;
    if (next < 0 || next >= composeStepIds.length) return;
    const copy = [...composeStepIds];
    [copy[idx], copy[next]] = [copy[next], copy[idx]];
    composeStepIds = copy;
  }

  async function saveCompose() {
    const name = composeName.trim();
    if (!name) return;
    if (composeStepIds.length === 0) return;
    try {
      const saved = await SaveFlow({
        id: composeEditingId,
        name,
        stepIds: composeStepIds,
        createdAt: '',
        updatedAt: '',
      } as any);
      await refreshFlows();
      // Open the (new or just-edited) flow so the user sees the result.
      const fresh = flows.find((f) => f.id === (saved as any).id);
      if (fresh) selectFlow(fresh);
      closeCompose();
    } catch (e: any) {
      console.error('Save flow failed:', e);
    }
  }

  // composeAvailable: saved requests not yet in the step list. Same
  // request can appear in the steps list multiple times (e.g. fetch
  // → update → fetch again), so we don't filter out used IDs.
  $: composeAvailable = requests;

  let flashTimer: any = null;
  function flashSaved(msg: string) {
    saveHint = msg;
    if (flashTimer) clearTimeout(flashTimer);
    flashTimer = setTimeout(() => {
      saveHint = '';
    }, 1600);
  }

  function addHeader() {
    reqHeaders = [...reqHeaders, { key: '', value: '' }];
  }

  function removeHeader(i: number) {
    reqHeaders = reqHeaders.filter((_, idx) => idx !== i);
    if (reqHeaders.length === 0) reqHeaders = [{ key: '', value: '' }];
  }

  $: filledHeaderCount = reqHeaders.filter((h) => h.key.trim() !== '').length;

  // ─── Load test ───────────────────────────────────────────────────────
  function buildHeaderMap(): Record<string, string> {
    const m: Record<string, string> = {};
    for (const h of reqHeaders) {
      const k = h.key.trim();
      if (k) m[k] = h.value;
    }
    return m;
  }

  async function start() {
    errorMsg = '';
    metrics = null;
    history = [];
    prevMetrics = null;
    chartInitialized = false;
    plannedDuration = durationSecs;
    runStartTs = Date.now();
    activeRunIdx = -1;
    hoverIdx = -1;
    try {
      // Flow mode: build a Steps[] from the resolved request list. The
      // engine ignores url/method/headers/body when Steps is non-empty,
      // but we leave them as placeholders since createFrom validates
      // shape rather than meaning.
      const stepsPayload = activeFlow
        ? activeFlowSteps.map((r) => ({
            url: r.url,
            method: r.method,
            headers: (r.headers || []).reduce<Record<string, string>>(
              (acc, h) => {
                if (h.key) acc[h.key] = h.value;
                return acc;
              },
              {}
            ),
            body: r.body || '',
          }))
        : [];
      await StartTest(
        engine.Config.createFrom({
          url: activeFlow ? '' : reqUrl,
          method: activeFlow ? '' : reqMethod,
          headers: activeFlow ? {} : buildHeaderMap(),
          body: activeFlow ? '' : reqBody,
          steps: stepsPayload,
          mode,
          concurrency,
          rampUpSecs: 0,
          durationSecs,
          curve: mode === 'curve' ? curvePoints.map((p) => ({
            timeSecs: p.timeSecs,
            users: p.users,
            curveIn: p.curveIn,
            exponent: p.exponent,
          })) : [],
          noise: mode === 'curve' ? noise : 0,
        })
      );
      running = true;
      // Only flip the view after a successful start; otherwise the error
      // message gets stranded on Load test while the user is staring at
      // Results.
      view = 'results';
    } catch (e: any) {
      errorMsg = typeof e === 'string' ? e : e?.message ?? String(e);
      running = false;
    }
  }

  function stop() {
    StopTest();
  }

  // ─── Formatters & chart geometry ─────────────────────────────────────
  const fmt = (n: number, d: number = 1) =>
    Number.isFinite(n) ? n.toFixed(d) : '0';
  const pct = (n: number, total: number) =>
    total > 0 ? ((n / total) * 100).toFixed(1) : '0.0';
  const pctNum = (n: number, total: number) =>
    total > 0 ? (n / total) * 100 : 0;

  // ─── D3 chart ────────────────────────────────────────────────────
  // Chart viewBox is sized to its container via ResizeObserver so the SVG
  // fills the actual chart-wrap (no letterboxing). Seeded with a sensible
  // default for the first paint before observe() fires.
  let CW = 900;
  let CH = 360;
  const PAD = { l: 58, r: 58, t: 22, b: 36 };
  $: innerW = CW - PAD.l - PAD.r;
  $: innerH = CH - PAD.t - PAD.b;
  const T_DUR = 380; // animation duration (ms)

  let chartEl: SVGSVGElement | null = null;
  let chartWrapEl: HTMLDivElement | null = null;
  let chartInitialized = false;

  // Hover state — D3 owns the SVG, so the guide line + dots are drawn by D3
  // into a dedicated `.lc-hover` group; the tooltip is HTML rendered by Svelte
  // and absolutely positioned inside `.chart-wrap`.
  let hoverIdx = -1;
  let tooltipLeft = 0;
  let tooltipFlip = false;
  let cachedXScale: d3.ScaleLinear<number, number> | null = null;
  let cachedYRpsScale: d3.ScaleLinear<number, number> | null = null;
  let cachedYLatScale: d3.ScaleLinear<number, number> | null = null;

  // ─── Display routing — live vs saved run ─────────────────────────────
  // When activeRunIdx === -1, the metrics card and chart read live state.
  // Otherwise they read from runs[activeRunIdx], so tabs feel like
  // looking at a different test without disturbing the live data.
  $: activeRun = activeRunIdx >= 0 && runs[activeRunIdx] ? runs[activeRunIdx] : null;
  $: displayMetrics = activeRun ? activeRun.metrics : metrics;
  $: displayHistory = activeRun ? activeRun.history : history;
  $: displayMethod = activeRun ? activeRun.method : reqMethod;
  $: displayUrl    = activeRun ? activeRun.url    : reqUrl;
  // Flow-aware shadows of the single-request display fields. For a flow
  // run, the saved name lives in config.flowName and there's no single
  // method/url — the header branches on displayIsFlow.
  $: displayIsFlow = activeRun
    ? !!(activeRun.config?.steps && activeRun.config.steps.length > 0)
    : !!activeFlow;
  $: displayFlowName = activeRun
    ? activeRun.config?.flowName ?? activeRun.name
    : (activeFlow?.name ?? '');
  $: displayFlowStepCount = activeRun
    ? (activeRun.config?.steps?.length ?? 0)
    : (activeFlow?.stepIds.length ?? 0);
  $: displayFlowSteps = activeRun
    ? (activeRun.config?.steps ?? [])
    : activeFlowSteps.map((r) => ({
        name: r.name || 'Untitled',
        method: r.method,
        url: r.url,
      }));
  $: displayIsLive = !activeRun;
  $: displayState  = activeRun ? 'done' : running ? 'running' : 'done';
  $: displayModeStr = activeRun
    ? activeRun.config.mode === 'curve'
      ? `curve · peak ${Math.max(0, ...(activeRun.config.curve ?? []).map((p) => p.users))}`
      : `fixed ${activeRun.config.concurrency}`
    : mode === 'curve'
      ? `curve · peak ${Math.max(0, ...curvePoints.map((p) => p.users))}`
      : `fixed ${concurrency}`;
  $: displayPeakConc = Math.max(0, ...displayHistory.map((s) => s.conc));
  // Active run's planned duration when viewing a saved snapshot; live one
  // otherwise. Drives the chart's x-axis extent so a saved run doesn't get
  // squashed by a stale live `plannedDuration` from the previous test.
  $: displayPlannedDuration = activeRun
    ? activeRun.config.durationSecs
    : plannedDuration;

  // ─── Mini throughput-per-tick sparkline (right column of the card) ───
  const SPARK_W = 200;
  const SPARK_H = 60;
  $: sparkPath = (() => {
    if (displayHistory.length === 0) return { line: '', area: '' };
    const maxT = Math.max(1, ...displayHistory.map((s) => s.t));
    const maxV = Math.max(1, ...displayHistory.map((s) => s.tickRps));
    const sx = (t: number) => (t / maxT) * SPARK_W;
    const sy = (v: number) => SPARK_H - (v / maxV) * (SPARK_H - 2) - 1;
    const line = d3
      .line<Sample>()
      .x((s) => sx(s.t))
      .y((s) => sy(s.tickRps))
      .curve(d3.curveMonotoneX)(displayHistory);
    const area = d3
      .area<Sample>()
      .x((s) => sx(s.t))
      .y0(SPARK_H)
      .y1((s) => sy(s.tickRps))
      .curve(d3.curveMonotoneX)(displayHistory);
    return { line: line ?? '', area: area ?? '' };
  })();
  $: sparkSpan = displayHistory.length > 0
    ? `0 → ${Math.max(0, ...displayHistory.map((s) => s.t)).toFixed(0)}s`
    : '';

  // Tiny sparkline used in the sidebar run cards. Fixed 80×22 viewBox; we
  // scale x by index (not time) so short runs still fill the width.
  function miniSparkPath(hist: Sample[]): { line: string; area: string } | null {
    if (!hist || hist.length < 2) return null;
    const w = 80;
    const h = 22;
    const maxV = Math.max(1, ...hist.map((s) => s.tickRps));
    const sx = (i: number) => (i / (hist.length - 1)) * w;
    const sy = (v: number) => h - (v / maxV) * (h - 2) - 1;
    const line = d3
      .line<Sample>()
      .x((_, i) => sx(i))
      .y((s) => sy(s.tickRps))
      .curve(d3.curveMonotoneX)(hist);
    const area = d3
      .area<Sample>()
      .x((_, i) => sx(i))
      .y0(h)
      .y1((s) => sy(s.tickRps))
      .curve(d3.curveMonotoneX)(hist);
    return { line: line ?? '', area: area ?? '' };
  }

  function fmtAxis(v: d3.NumberValue): string {
    const n = +v;
    if (n === 0) return '0';
    if (n >= 1000) return (n / 1000).toFixed(n >= 10000 ? 0 : 1) + 'k';
    if (n >= 10) return n.toFixed(0);
    return n.toFixed(1);
  }

  function setupChart() {
    if (!chartEl) return;
    const svg = d3.select(chartEl);
    svg.selectAll('*').remove();

    // Clip so bars/lines that briefly overshoot during transitions stay inside.
    svg
      .append('defs')
      .append('clipPath')
      .attr('id', 'lc-plot-clip')
      .append('rect')
      .attr('x', PAD.l)
      .attr('y', PAD.t - 2)
      .attr('width', innerW)
      .attr('height', innerH + 4);

    // Transparent rect first so the plot area captures pointermove even where
    // there's no bar — without it the SVG only fires on visible elements.
    svg
      .append('rect')
      .attr('class', 'lc-hover-pad')
      .attr('x', PAD.l)
      .attr('y', PAD.t)
      .attr('width', innerW)
      .attr('height', innerH)
      .attr('fill', 'transparent')
      .style('pointer-events', 'all');

    svg.append('g').attr('class', 'lc-grid');
    svg.append('g').attr('class', 'lc-bars-ok').attr('clip-path', 'url(#lc-plot-clip)');
    svg.append('g').attr('class', 'lc-bars-fail').attr('clip-path', 'url(#lc-plot-clip)');
    svg.append('g').attr('class', 'lc-lines').attr('clip-path', 'url(#lc-plot-clip)');
    svg.append('g').attr('class', 'lc-axis-x');
    svg.append('g').attr('class', 'lc-axis-y-rps');
    svg.append('g').attr('class', 'lc-axis-y-lat');
    svg.append('g').attr('class', 'lc-hover').style('pointer-events', 'none');

    svg
      .append('text')
      .attr('class', 'lc-axis-title')
      .attr('x', PAD.l - 6)
      .attr('y', PAD.t - 6)
      .attr('text-anchor', 'end')
      .text('rps');
    svg
      .append('text')
      .attr('class', 'lc-axis-title')
      .attr('x', CW - PAD.r + 6)
      .attr('y', PAD.t - 6)
      .attr('text-anchor', 'start')
      .text('ms');

    chartInitialized = true;
  }

  function updateChart() {
    if (!chartInitialized || !chartEl) return;
    const svg = d3.select(chartEl);
    const ease = d3.easeCubicOut;

    const xMax = Math.max(displayPlannedDuration || 1, ...displayHistory.map((s) => s.t));
    const rpsMax = Math.max(10, ...displayHistory.map((s) => s.tickRps));
    const latMax = Math.max(
      10,
      ...displayHistory.map((s) => Math.max(s.p50, s.p95, s.p99))
    );

    const x = d3.scaleLinear().domain([0, xMax]).range([PAD.l, CW - PAD.r]);
    const yRpsScale = d3
      .scaleLinear()
      .domain([0, rpsMax])
      .nice()
      .range([CH - PAD.b, PAD.t]);
    const yLatScale = d3
      .scaleLinear()
      .domain([0, latMax])
      .nice()
      .range([CH - PAD.b, PAD.t]);

    cachedXScale = x;
    cachedYRpsScale = yRpsScale;
    cachedYLatScale = yLatScale;

    // Grid lines aligned to rps ticks.
    const gridTicks = yRpsScale.ticks(5);
    const grid = svg
      .select<SVGGElement>('.lc-grid')
      .selectAll<SVGLineElement, number>('line')
      .data(gridTicks);
    grid.exit().remove();
    grid
      .enter()
      .append('line')
      .attr('class', 'lc-gridline')
      .merge(grid as any)
      .transition()
      .duration(T_DUR)
      .ease(ease)
      .attr('x1', PAD.l)
      .attr('x2', CW - PAD.r)
      .attr('y1', (d) => yRpsScale(d))
      .attr('y2', (d) => yRpsScale(d));

    // Bar width: enough room for breathing, max ~20px.
    const bw = Math.max(
      3,
      Math.min(20, (innerW / Math.max(displayHistory.length, 1)) * 0.62)
    );
    const y0 = yRpsScale(0);

    // Stacked bars — success at the bottom, failure on top.
    const drawStack = (
      cls: string,
      yTopFn: (d: Sample) => number,
      yBotFn: (d: Sample) => number
    ) => {
      const sel = svg
        .select<SVGGElement>(cls)
        .selectAll<SVGRectElement, Sample>('rect')
        .data(displayHistory, (d) => String(d.t));
      sel
        .exit()
        .transition()
        .duration(T_DUR)
        .attr('y', y0)
        .attr('height', 0)
        .remove();
      const ent = sel
        .enter()
        .append('rect')
        .attr('x', (d) => x(d.t) - bw / 2)
        .attr('width', bw)
        .attr('y', y0)
        .attr('height', 0);
      ent
        .merge(sel as any)
        .transition()
        .duration(T_DUR)
        .ease(ease)
        .attr('x', (d) => x(d.t) - bw / 2)
        .attr('width', bw)
        .attr('y', (d) => yTopFn(d))
        .attr('height', (d) => Math.max(0, yBotFn(d) - yTopFn(d)));
    };

    drawStack('.lc-bars-ok', (d) => yRpsScale(d.tickRpsOk), () => y0);
    drawStack(
      '.lc-bars-fail',
      (d) => yRpsScale(d.tickRps),
      (d) => yRpsScale(d.tickRpsOk)
    );

    // Smooth latency lines.
    const lineFor = (key: 'p50' | 'p95' | 'p99') =>
      d3
        .line<Sample>()
        .x((d) => x(d.t))
        .y((d) => yLatScale(d[key]))
        .curve(d3.curveMonotoneX);

    const drawLine = (
      key: 'p50' | 'p95' | 'p99',
      cls: string,
      width: number
    ) => {
      const linesGroup = svg.select<SVGGElement>('.lc-lines');
      let path = linesGroup.select<SVGPathElement>(`.${cls}`);
      if (path.empty()) {
        path = linesGroup
          .append('path')
          .attr('class', `lc-line ${cls}`)
          .attr('fill', 'none')
          .attr('stroke-width', width)
          .attr('stroke-linecap', 'round')
          .attr('stroke-linejoin', 'round');
      }
      path
        .datum(displayHistory)
        .transition()
        .duration(T_DUR)
        .ease(ease)
        .attr('d', lineFor(key));
    };

    drawLine('p99', 'lc-line-p99', 1.5);
    drawLine('p95', 'lc-line-p95', 1.6);
    drawLine('p50', 'lc-line-p50', 2.2);

    // Axes (transition between scale changes).
    svg
      .select<SVGGElement>('.lc-axis-x')
      .attr('transform', `translate(0,${CH - PAD.b})`)
      .transition()
      .duration(T_DUR)
      .ease(ease)
      .call(
        d3
          .axisBottom(x)
          .ticks(6)
          .tickSize(4)
          .tickFormat((d) => `${(+d).toFixed(0)}s`)
      );
    svg
      .select<SVGGElement>('.lc-axis-y-rps')
      .attr('transform', `translate(${PAD.l},0)`)
      .transition()
      .duration(T_DUR)
      .ease(ease)
      .call(d3.axisLeft(yRpsScale).ticks(5).tickSize(4).tickFormat(fmtAxis));
    svg
      .select<SVGGElement>('.lc-axis-y-lat')
      .attr('transform', `translate(${CW - PAD.r},0)`)
      .transition()
      .duration(T_DUR)
      .ease(ease)
      .call(d3.axisRight(yLatScale).ticks(5).tickSize(4).tickFormat(fmtAxis));

    // Re-anchor hover guide to fresh scales (no-op if not hovering).
    drawHoverGuide();
  }

  // ─── Chart hover tooltip ─────────────────────────────────────────────
  function drawHoverGuide() {
    if (!chartEl) return;
    const g = d3.select(chartEl).select<SVGGElement>('.lc-hover');
    g.selectAll('*').remove();
    if (
      hoverIdx < 0 ||
      !displayHistory[hoverIdx] ||
      !cachedXScale ||
      !cachedYRpsScale ||
      !cachedYLatScale
    )
      return;
    const s = displayHistory[hoverIdx];
    const hx = cachedXScale(s.t);
    g.append('line')
      .attr('class', 'lc-hover-guide')
      .attr('x1', hx)
      .attr('x2', hx)
      .attr('y1', PAD.t)
      .attr('y2', CH - PAD.b);
    g.append('circle')
      .attr('class', 'lc-hover-dot lc-hd-ok')
      .attr('cx', hx)
      .attr('cy', cachedYRpsScale(s.tickRpsOk))
      .attr('r', 3.5);
    if (s.tickRps > s.tickRpsOk + 0.001) {
      g.append('circle')
        .attr('class', 'lc-hover-dot lc-hd-fail')
        .attr('cx', hx)
        .attr('cy', cachedYRpsScale(s.tickRps))
        .attr('r', 3.5);
    }
    g.append('circle')
      .attr('class', 'lc-hover-dot lc-hd-p50')
      .attr('cx', hx)
      .attr('cy', cachedYLatScale(s.p50))
      .attr('r', 3);
    g.append('circle')
      .attr('class', 'lc-hover-dot lc-hd-p95')
      .attr('cx', hx)
      .attr('cy', cachedYLatScale(s.p95))
      .attr('r', 3);
    g.append('circle')
      .attr('class', 'lc-hover-dot lc-hd-p99')
      .attr('cx', hx)
      .attr('cy', cachedYLatScale(s.p99))
      .attr('r', 3);
  }

  function onChartPointerMove(e: PointerEvent) {
    if (!chartEl || displayHistory.length === 0 || !cachedXScale) return;
    const rect = chartEl.getBoundingClientRect();
    if (rect.width === 0) return;
    const cssX = e.clientX - rect.left;
    // Convert CSS px → viewBox units. With preserveAspectRatio="xMidYMid meet"
    // and width:100%, height:auto, the SVG renders at native aspect, so the
    // x mapping is linear with no letterboxing.
    const vbX = (cssX / rect.width) * CW;
    let best = -1;
    let bestDist = Infinity;
    for (let i = 0; i < displayHistory.length; i++) {
      const sx = cachedXScale(displayHistory[i].t);
      const d = Math.abs(sx - vbX);
      if (d < bestDist) {
        bestDist = d;
        best = i;
      }
    }
    if (best !== hoverIdx) hoverIdx = best;
  }

  function onChartPointerLeave() {
    if (hoverIdx !== -1) hoverIdx = -1;
  }

  // Position the HTML tooltip in CSS pixels relative to .chart-wrap.
  $: if (
    hoverIdx >= 0 &&
    displayHistory[hoverIdx] &&
    chartEl &&
    chartWrapEl &&
    cachedXScale
  ) {
    const rect = chartEl.getBoundingClientRect();
    const wrap = chartWrapEl.getBoundingClientRect();
    const hx = cachedXScale(displayHistory[hoverIdx].t);
    const cssX = (hx / CW) * rect.width + (rect.left - wrap.left);
    tooltipLeft = cssX;
    tooltipFlip = cssX > rect.width * 0.62;
  }

  // Redraw the guide whenever hover changes.
  $: if (chartInitialized) {
    void hoverIdx; // explicit dep
    drawHoverGuide();
  }

  // Clear hover when data resets.
  $: if (displayHistory.length === 0 && hoverIdx !== -1) hoverIdx = -1;

  afterUpdate(() => {
    if (!chartEl) {
      chartInitialized = false;
      return;
    }
    if (!chartInitialized) setupChart();
    if (displayHistory.length > 0) updateChart();
  });
</script>

<main class="app">
  <nav class="topnav">
    <button class="brand" type="button" on:click={openInfo} title="About LoadCell">
      <img class="brand-mark" src={logoUrl} alt="" aria-hidden="true" />
      <span class="brand-wordmark">LoadCell</span>
    </button>
    <div class="nav-segs" role="tablist" aria-label="App sections">
      <button
        type="button"
        class="nav-seg"
        class:active={view === 'request'}
        role="tab"
        aria-selected={view === 'request'}
        on:click={() => (view = 'request')}
      >Request</button>
      <button
        type="button"
        class="nav-seg"
        class:active={view === 'loadtest'}
        role="tab"
        aria-selected={view === 'loadtest'}
        on:click={() => (view = 'loadtest')}
      >Load test</button>
      <button
        type="button"
        class="nav-seg"
        class:active={view === 'results'}
        role="tab"
        aria-selected={view === 'results'}
        on:click={() => (view = 'results')}
      >Results</button>
    </div>
    <div class="nav-status">
      {#if running}
        <span class="status-pip live" aria-hidden="true"></span>
        <span class="status-text">Running</span>
      {:else if runs.length > 0}
        <span class="status-text muted">{runs.length} {runs.length === 1 ? 'run' : 'runs'}</span>
      {/if}
    </div>
  </nav>

  <div class="workspace" data-view={view}>
    {#if view === 'request' || view === 'loadtest' || view === 'results'}
      <aside class="sidebar">
        <div class="side-section">
          <div class="side-head">
            <span class="side-title">Saved requests</span>
            <button class="side-new" type="button" on:click={newRequest} title="New request">
              <Plus size={11} weight="duotone" /> New
            </button>
          </div>
          {#if requests.length === 0}
            <p class="empty">Nothing saved yet. Build a request and hit <strong>Save</strong>.</p>
          {:else}
            <ul class="req-list">
              {#each requests as r (r.id)}
                <li class:active={r.id === currentId}>
                  <button class="req-row" type="button" on:click={() => pickRequest(r)}>
                    <span class="method m-{r.method.toLowerCase()}">{r.method}</span>
                    <span class="req-name">{r.name || 'Untitled'}</span>
                  </button>
                  <button class="req-del" type="button" on:click={() => deleteRequest(r.id)} title="Delete">
                    <Trash size={14} />
                  </button>
                </li>
              {/each}
            </ul>
          {/if}
        </div>

        <div class="side-section flows-section">
          <div class="side-head">
            <span class="side-title">Saved flows</span>
            <button
              class="side-new"
              type="button"
              on:click={() => openCompose()}
              disabled={requests.length === 0}
              title={requests.length === 0 ? 'Save at least one request first' : 'Compose a new flow'}
            >
              <Plus size={11} weight="duotone" /> New
            </button>
          </div>
          {#if requests.length === 0}
            <p class="empty">Save some requests first, then compose them into a flow.</p>
          {:else if flows.length === 0}
            <p class="empty">No flows yet. <button class="empty-link" type="button" on:click={() => openCompose()}>Compose one</button> from your saved requests.</p>
          {:else}
            <ul class="flow-list">
              {#each flows as f (f.id)}
                <li class:active={activeFlow?.id === f.id}>
                  <button class="flow-row" type="button" on:click={() => selectFlow(f)}>
                    <span class="flow-badge">{f.stepIds.length}</span>
                    <span class="flow-name">{f.name || 'Untitled'}</span>
                  </button>
                  <button class="flow-edit" type="button" on:click|stopPropagation={() => openCompose(f)} title="Edit steps">
                    <Pencil size={12} weight="duotone" />
                  </button>
                  <button class="flow-del" type="button" on:click|stopPropagation={() => deleteFlowEntry(f.id)} title="Delete flow">
                    <Trash size={14} />
                  </button>
                </li>
              {/each}
            </ul>
          {/if}
        </div>

        <div class="side-section runs-section">
          <div class="side-head">
            <span class="side-title">Saved runs</span>
            {#if runs.length > 0}
              <span class="side-count">{runs.length}</span>
            {/if}
          </div>
          {#if runs.length === 0 && !running}
            <p class="empty">No runs yet. Start a load test to fill this list.</p>
          {:else}
            <ul class="run-list">
              {#if running}
                <li class="run-card-mini live" class:active={view === 'results' && activeRunIdx === -1} class:is-flow={!!activeFlow}>
                  <button class="rcm-main" type="button" on:click={() => selectRun(-1)}>
                    <div class="rcm-head">
                      <span class="rcm-live-dot" aria-hidden="true"></span>
                      {#if activeFlow}
                        <span class="rcm-flow-badge">FLOW · {activeFlow.stepIds.length}</span>
                      {/if}
                      <span class="rcm-name">Live · {activeFlow ? activeFlow.name : reqName || 'Untitled'}</span>
                      <span class="rcm-time">running</span>
                    </div>
                    {#if metrics}
                      {@const liveSp = miniSparkPath(history)}
                      <div class="rcm-mid">
                        <span class="rcm-pct"><strong>{pct(metrics.successful, metrics.totalRequests)}</strong>%</span>
                        {#if liveSp}
                          <svg class="rcm-spark" viewBox="0 0 80 22" preserveAspectRatio="none" aria-hidden="true">
                            {#if liveSp.area}<path d={liveSp.area} fill="rgba(159, 184, 173, 0.55)" />{/if}
                            {#if liveSp.line}<path d={liveSp.line} fill="none" stroke="var(--accent)" stroke-width="1" vector-effect="non-scaling-stroke" />{/if}
                          </svg>
                        {/if}
                      </div>
                      <div class="rcm-foot">
                        <span>{metrics.elapsedSecs.toFixed(0)}s</span>
                        <span>{metrics.totalRequests.toLocaleString()} reqs</span>
                        <span>{metrics.rps.toFixed(0)} rps</span>
                      </div>
                    {/if}
                  </button>
                </li>
              {/if}
              {#each runs as r, i (r.id)}
                {@const sp = miniSparkPath(r.history)}
                {@const isFlow = !!(r.config?.steps && r.config.steps.length > 0)}
                <li class="run-card-mini" class:active={view === 'results' && activeRunIdx === i} class:is-flow={isFlow}>
                  <button class="rcm-main" type="button" on:click={() => selectRun(i)}>
                    <div class="rcm-head">
                      {#if isFlow}
                        <span class="rcm-flow-badge">FLOW · {r.config.steps?.length ?? 0}</span>
                      {:else}
                        <span class="method m-{r.method.toLowerCase()}">{r.method}</span>
                      {/if}
                      <span class="rcm-name">{r.name || 'Untitled'}</span>
                      <span class="rcm-time">{fmtRunTime(r.startedAt)}</span>
                    </div>
                    <div class="rcm-mid">
                      <span class="rcm-pct"><strong>{pct(r.metrics.successful, r.metrics.totalRequests)}</strong>%</span>
                      {#if sp}
                        <svg class="rcm-spark" viewBox="0 0 80 22" preserveAspectRatio="none" aria-hidden="true">
                          {#if sp.area}<path d={sp.area} fill="rgba(159, 184, 173, 0.55)" />{/if}
                          {#if sp.line}<path d={sp.line} fill="none" stroke="var(--accent)" stroke-width="1" vector-effect="non-scaling-stroke" />{/if}
                        </svg>
                      {/if}
                    </div>
                    <div class="rcm-foot">
                      <span>{Math.round(r.metrics.elapsedSecs)}s</span>
                      <span>{r.metrics.totalRequests.toLocaleString()} reqs</span>
                      <span>{r.metrics.rps.toFixed(0)} rps</span>
                    </div>
                  </button>
                  <button type="button" class="rcm-del" on:click|stopPropagation={() => deleteRun(i)} title="Delete run" aria-label="Delete run">
                    <Trash size={11} />
                  </button>
                </li>
              {/each}
            </ul>
          {/if}
        </div>
      </aside>
    {/if}

    {#if view === 'request'}
    <section class="builder request-pane">
      <div class="builder-head">
        <input
          class="name-input"
          type="text"
          placeholder="Request name"
          bind:value={reqName}
          disabled={running}
        />
        <div class="save-row">
          {#if saveHint}
            <span class="save-hint">
              <CheckCircle size={13} weight="duotone" />
              {saveHint}
            </span>
          {/if}
          <button class="btn btn-ghost" type="button" on:click={saveCurrent} disabled={running}>
            <FloppyDisk size={13} weight="duotone" /> Save
          </button>
          {#if currentId}
            <button class="btn btn-ghost" type="button" on:click={saveAsNew} disabled={running}>
              <Copy size={13} weight="duotone" /> Save as new
            </button>
          {/if}
          <button
            class="btn btn-primary"
            type="button"
            on:click={sendSample}
            disabled={running || sending || !reqUrl.trim()}
            title="Send one request and show the response"
          >
            {#if sending}
              <span class="btn-spin"><CircleNotch size={13} weight="duotone" /></span>
            {:else}
              <PaperPlaneRight size={13} weight="duotone" />
            {/if}
            Send
          </button>
        </div>
      </div>

      <div class="url-row">
        <div class="method-wrap">
          <button
            type="button"
            class="method-trigger m-{reqMethod.toLowerCase()}"
            on:click={toggleMethod}
            disabled={running}
          >
            <span>{reqMethod}</span>
            <span class="method-arrow"><CaretDown size={10} weight="duotone" /></span>
          </button>
          {#if methodOpen}
            <div class="method-menu">
              {#each METHODS as m}
                <button
                  type="button"
                  class="method-opt"
                  class:active={m === reqMethod}
                  on:click={() => pickMethod(m)}
                >{m}</button>
              {/each}
            </div>
          {/if}
        </div>
        <div class="url-input-wrap" class:highlighted={!!reqUrlHighlight}>
          {#if reqUrlHighlight}
            <div
              class="url-input-overlay"
              bind:this={urlOverlayEl}
              aria-hidden="true"
            >{@html reqUrlHighlight}</div>
          {/if}
          <input
            class="url-input"
            type="text"
            bind:value={reqUrl}
            bind:this={urlInputEl}
            on:scroll={syncUrlOverlayScroll}
            on:input={syncUrlOverlayScroll}
            placeholder="https://..."
            spellcheck="false"
            disabled={running}
          />
        </div>
      </div>

      <div class="tokens-hint">
        <span class="tokens-hint-label">Dynamic tokens</span>
        <code title="Random UUID v4 — each occurrence rolls independently">{'{{uuid}}'}</code>
        <code title="Counter that grows 1, 2, 3 per request — shared across occurrences in one render">{'{{seq}}'}</code>
        <code title="Current Unix time in milliseconds — snapshotted once per request">{'{{nowMs}}'}</code>
        <code title="Random integer in [min, max] inclusive — each occurrence rolls independently">{'{{randInt:1:1000}}'}</code>
        <button
          type="button"
          class="tokens-toggle"
          class:open={tokensExpanded}
          on:click={() => (tokensExpanded = !tokensExpanded)}
          aria-expanded={tokensExpanded}
        >
          {tokensExpanded ? 'Hide' : 'How to use'}
          <CaretDown size={10} weight="duotone" />
        </button>
      </div>

      {#if tokensExpanded}
        <div class="tokens-help">
          <p class="tokens-help-intro">
            Drop these anywhere in the URL, header values, or body. Each request renders them to fresh values — useful for defeating caches and tagging requests for tracing.
          </p>
          <dl class="tokens-help-list">
            <dt><code>{'{{uuid}}'}</code></dt>
            <dd>
              Random UUID v4. Each occurrence in one template renders an independent value.
              <span class="tokens-help-ex">→ <code>b693b66f-950a-40dc-b913-60486727cb37</code></span>
            </dd>

            <dt><code>{'{{seq}}'}</code></dt>
            <dd>
              Counter that grows <code>1, 2, 3, …</code> per request. Multiple <code>{'{{seq}}'}</code> in the same render all return the same number, so URL/body/header stay consistent for the same request.
              <span class="tokens-help-ex">→ <code>1</code>, <code>2</code>, <code>3</code>, …</span>
            </dd>

            <dt><code>{'{{nowMs}}'}</code></dt>
            <dd>
              Current Unix time in milliseconds. Snapshotted once per request so all occurrences share the same instant.
              <span class="tokens-help-ex">→ <code>1780623659570</code></span>
            </dd>

            <dt><code>{'{{randInt:min:max}}'}</code></dt>
            <dd>
              Random integer in <code>[min, max]</code> (both inclusive). Each occurrence rolls independently. Min must be ≤ max.
              <span class="tokens-help-ex">→ <code>{'{{randInt:1:1000}}'}</code> renders as <code>441</code></span>
            </dd>
          </dl>
          <p class="tokens-help-foot">
            Unknown tokens (typos like <code>{'{{uid}}'}</code>) fail with an inline error instead of silently sending the literal text.
          </p>
        </div>
      {/if}

      <div class="tabs">
        <button
          class="tab"
          class:active={tab === 'headers'}
          type="button"
          on:click={() => (tab = 'headers')}
        >
          Headers
          {#if filledHeaderCount > 0}<span class="tab-badge">{filledHeaderCount}</span>{/if}
        </button>
        <button
          class="tab"
          class:active={tab === 'body'}
          type="button"
          on:click={() => (tab = 'body')}
        >
          Body
          {#if reqBody.length > 0}<span class="tab-badge">{reqBody.length}B</span>{/if}
        </button>
      </div>

      <div class="tab-content">
        {#if tab === 'headers'}
          <div class="headers">
            {#each reqHeaders as h, i}
              <div class="hrow">
                <input
                  type="text"
                  placeholder="Header name"
                  bind:value={h.key}
                  spellcheck="false"
                  disabled={running}
                />
                <div class="h-value-wrap" class:highlighted={!!hValueHighlights[i]}>
                  {#if hValueHighlights[i]}
                    <div
                      class="h-value-overlay"
                      bind:this={hValueOverlayEls[i]}
                      aria-hidden="true"
                    >{@html hValueHighlights[i]}</div>
                  {/if}
                  <input
                    type="text"
                    placeholder="Value"
                    bind:value={h.value}
                    bind:this={hValueInputEls[i]}
                    on:scroll={() => syncHeaderOverlay(i)}
                    on:input={() => syncHeaderOverlay(i)}
                    spellcheck="false"
                    disabled={running}
                  />
                </div>
                <button class="hdel" type="button" on:click={() => removeHeader(i)} disabled={running} title="Remove">
                  <X size={12} weight="duotone" />
                </button>
              </div>
            {/each}
            <button class="add-header" type="button" on:click={addHeader} disabled={running}>
              <Plus size={11} weight="duotone" /> Add header
            </button>
          </div>
        {:else}
          <div class="body-input-wrap" class:highlighted={!!reqBodyHighlight}>
            {#if reqBodyHighlight}
              <pre
                class="body-input-overlay"
                bind:this={bodyOverlayEl}
                aria-hidden="true"
              >{@html reqBodyHighlight}<br /></pre>
            {/if}
            <textarea
              class="body-input"
              bind:value={reqBody}
              bind:this={bodyTextareaEl}
              on:scroll={syncBodyOverlayScroll}
              placeholder={'{ "example": "JSON body sent on every request" }'}
              spellcheck="false"
              disabled={running}
            ></textarea>
            {#if reqBodyIsJson}
              <button
                type="button"
                class="body-format-btn"
                on:click={formatRequestBody}
                disabled={running}
                title="Pretty-print JSON"
              >Format</button>
            {/if}
          </div>
        {/if}
      </div>

      {#if sampleResp || sampleErr || sending}
        <div class="section-sep" role="separator" aria-label="Response">
          <span class="section-sep-label">Response</span>
        </div>
        <div class="response">
          <div class="resp-head">
            {#if sending && !sampleResp}
              <span class="skel skel-chip" aria-hidden="true"></span>
              <span class="skel skel-text" style:width="80px" aria-hidden="true"></span>
              <span class="skel skel-text" style:width="60px" aria-hidden="true"></span>
              <span class="resp-meta resp-sending"><CircleNotch size={11} weight="duotone" /> sending…</span>
            {:else if sampleErr && !sampleResp}
              <span class="resp-status s-err">ERROR</span>
              <span class="resp-msg">{sampleErr}</span>
            {:else if sampleResp}
              <span class="resp-status {statusClass(sampleResp.status)}">
                {sampleResp.status > 0 ? sampleResp.status : 'ERR'}
              </span>
              {#if sampleResp.statusText}
                <span class="resp-text">{sampleResp.statusText.replace(/^\d+\s*/, '')}</span>
              {/if}
              <span class="resp-sep" aria-hidden="true">·</span>
              <span class="resp-meta">{sampleResp.elapsedMs.toFixed(1)} ms</span>
              <span class="resp-sep" aria-hidden="true">·</span>
              <span class="resp-meta">{fmtBytes(sampleResp.bodyBytes)}</span>
              {#if sampleResp.bodyTruncated}
                <span class="resp-truncated">truncated</span>
              {/if}
              {#if sampleResp.error}
                <span class="resp-msg">{sampleResp.error}</span>
              {/if}
            {/if}
          </div>
          {#if sending && !sampleResp}
            <div class="resp-skel-body">
              <span class="skel skel-line" style:width="92%"></span>
              <span class="skel skel-line" style:width="74%"></span>
              <span class="skel skel-line" style:width="86%"></span>
              <span class="skel skel-line" style:width="62%"></span>
              <span class="skel skel-line" style:width="78%"></span>
            </div>
          {:else if sampleResp && !sampleResp.error}
            <div class="resp-tabs">
              <button
                type="button"
                class="resp-tab"
                class:active={respTab === 'body'}
                on:click={() => (respTab = 'body')}
              >Body</button>
              <button
                type="button"
                class="resp-tab"
                class:active={respTab === 'headers'}
                on:click={() => (respTab = 'headers')}
              >Headers <span class="resp-tab-badge">{Object.keys(sampleResp.headers ?? {}).length}</span></button>
              <div class="resp-tab-actions">
                {#if respTab === 'body'}
                  {@const fbAct = formatBody(sampleResp.body, sampleResp.contentType)}
                  <button
                    type="button"
                    class="copy-btn"
                    class:copied={copiedKey === 'resp-body'}
                    on:click={() => copyToClipboard(fbAct.text, 'resp-body')}
                    title="Copy body"
                  >
                    {#if copiedKey === 'resp-body'}
                      <Check size={11} weight="duotone" /> Copied
                    {:else}
                      <Copy size={11} weight="duotone" /> Copy
                    {/if}
                  </button>
                {:else}
                  <button
                    type="button"
                    class="copy-btn"
                    class:copied={copiedKey === 'resp-headers-all'}
                    on:click={() => copyToClipboard(headersAsText(sampleResp.headers), 'resp-headers-all')}
                    title="Copy all headers as text"
                  >
                    {#if copiedKey === 'resp-headers-all'}
                      <Check size={11} weight="duotone" /> Copied
                    {:else}
                      <Copy size={11} weight="duotone" /> Copy all
                    {/if}
                  </button>
                {/if}
              </div>
            </div>
            <div class="resp-content">
              {#if respTab === 'body'}
                {@const fb = formatBody(sampleResp.body, sampleResp.contentType)}
                {#if fb.html}
                  <pre class="resp-body json">{@html fb.html}</pre>
                {:else}
                  <pre class="resp-body">{fb.text}</pre>
                {/if}
              {:else}
                <div class="resp-headers">
                  {#each Object.entries(sampleResp.headers ?? {}) as [k, v]}
                    <div class="rh-row">
                      <span class="rh-k">{k}</span>
                      <span class="rh-v">{v}</span>
                      <button
                        type="button"
                        class="rh-copy"
                        class:copied={copiedKey === `rh-${k}`}
                        on:click={() => copyToClipboard(v, `rh-${k}`)}
                        title={`Copy ${k} value`}
                        aria-label={`Copy ${k}`}
                      >
                        {#if copiedKey === `rh-${k}`}
                          <Check size={11} weight="duotone" />
                        {:else}
                          <Copy size={11} weight="duotone" />
                        {/if}
                      </button>
                    </div>
                  {/each}
                </div>
              {/if}
            </div>
          {/if}
        </div>
      {/if}
    </section>
    {:else if view === 'loadtest'}
    <section class="builder loadtest-pane" class:flow-mode={!!activeFlow}>
      {#if activeFlow}
        <div class="lt-flow">
          <div class="lt-flow-head">
            <span class="lt-flow-label">Flow</span>
            <span class="lt-flow-name">{activeFlow.name}</span>
            <span class="lt-flow-count">{activeFlow.stepIds.length} step{activeFlow.stepIds.length === 1 ? '' : 's'}</span>
            <button
              type="button"
              class="lt-flow-edit"
              on:click={() => activeFlow && openCompose(activeFlow)}
              disabled={running}
              title="Edit flow"
            >Edit</button>
          </div>
          <ol class="lt-flow-steps">
            {#each activeFlowSteps as step, i}
              <li class="lt-flow-step">
                <span class="lt-flow-step-num">{i + 1}</span>
                <span class="method m-{step.method.toLowerCase()}">{step.method}</span>
                <span class="lt-flow-step-name">{step.name || 'Untitled'}</span>
                <span class="lt-flow-step-url" title={step.url}>{step.url}</span>
              </li>
            {/each}
            {#if activeFlowSteps.length < activeFlow.stepIds.length}
              <li class="lt-flow-step missing">
                <span class="lt-flow-step-num">!</span>
                <span class="lt-flow-step-name">{activeFlow.stepIds.length - activeFlowSteps.length} step(s) reference deleted requests — edit the flow to fix</span>
              </li>
            {/if}
          </ol>
        </div>
      {:else}
        <div class="lt-request">
          <span class="lt-request-label">Request</span>
          <span class="method m-{reqMethod.toLowerCase()}">{reqMethod}</span>
          <span class="lt-request-name">{reqName || 'Untitled'}</span>
          <span class="lt-request-url" title={reqUrl}>{reqUrl}</span>
          <button
            type="button"
            class="lt-request-edit"
            on:click={() => (view = 'request')}
            disabled={running}
            title="Edit request"
          >Edit</button>
        </div>
      {/if}
      <div class="profile">
        <div class="profile-head">
          <h3 class="profile-title">Load profile</h3>
          <div class="mode-toggle" role="tablist">
            <button
              type="button"
              class="seg"
              class:active={mode === 'constant'}
              on:click={() => (mode = 'constant')}
              disabled={running}
            >Fixed</button>
            <button
              type="button"
              class="seg"
              class:active={mode === 'curve'}
              on:click={() => (mode = 'curve')}
              disabled={running}
            >Curve</button>
          </div>
        </div>

        <div class="profile-body">
          <div class="profile-inputs">
            {#if mode === 'constant'}
              <label class="field">
                <span class="k">Concurrency <span class="k-hint">max {MAX_WORKERS}</span></span>
                <input type="number" bind:value={concurrency} min="1" max={MAX_WORKERS} disabled={running} />
              </label>
            {:else}
              <label class="field">
                <span class="k">Peak workers <span class="k-hint">max {MAX_WORKERS}</span></span>
                <input
                  type="number"
                  min="0"
                  max={MAX_WORKERS}
                  value={curveMaxUsers}
                  on:input={onCurvePeakInput}
                  disabled={running}
                />
              </label>
            {/if}
            <label class="field">
              <span class="k">Test duration (s)</span>
              <input type="number" bind:value={durationSecs} min="1" disabled={running} />
            </label>
            <div class="curve-tools" class:lp-muted={mode !== 'curve'}>
              <span class="ct-title">Curve</span>
              <button class="ct-btn" type="button" on:click={resetCurve} disabled={mode !== 'curve' || running}>Reset</button>
            </div>
            <p class="ct-hint" class:lp-muted={mode !== 'curve'}>
              Click empty space to add a point · drag to move · right-click to delete · click a point to edit its easing
            </p>
            <div class="noise-row" class:lp-muted={mode !== 'curve'}>
              <span class="k">Noise <span class="noise-val">{Math.round(noise * 100)}%</span></span>
              <input
                type="range"
                class="noise-slider"
                min="0"
                max="0.5"
                step="0.01"
                bind:value={noise}
                disabled={mode !== 'curve' || running}
              />
            </div>
            <div class="point-edit" class:lp-muted={!(mode === 'curve' && selectedPtIdx >= 0 && curvePoints[selectedPtIdx])}>
              {#if mode === 'curve' && selectedPtIdx >= 0 && curvePoints[selectedPtIdx]}
                {@const sp = curvePoints[selectedPtIdx]}
                <div class="pe-head">
                  <span class="pe-title">Point @ {fmtDur(sp.timeSecs)}, {sp.users} users</span>
                  {#if selectedPtIdx > 0 && curvePoints.length > 2}
                    <button class="pe-del" type="button" on:click={deleteSelected} disabled={running} title="Delete point">
                      <Trash size={12} />
                    </button>
                  {/if}
                </div>
                <div class="pe-row">
                  <span class="pe-label">Curve in</span>
                  <div class="pe-segs">
                    <button class="pe-seg" class:active={sp.curveIn === 'linear'} on:click={() => setSelectedCurve('linear')} disabled={selectedPtIdx === 0 || running} type="button">Linear</button>
                    <button class="pe-seg" class:active={sp.curveIn === 'exponential'} on:click={() => setSelectedCurve('exponential')} disabled={selectedPtIdx === 0 || running} type="button">Expo</button>
                    <button class="pe-seg" class:active={sp.curveIn === 'step'} on:click={() => setSelectedCurve('step')} disabled={selectedPtIdx === 0 || running} type="button">Step</button>
                  </div>
                </div>
                {#if sp.curveIn === 'exponential' && selectedPtIdx > 0}
                  <div class="pe-row">
                    <span class="pe-label">Exponent</span>
                    <input
                      type="range"
                      class="pe-slider"
                      min="0.3"
                      max="5"
                      step="0.1"
                      value={sp.exponent}
                      on:input={onExponentInput}
                      disabled={running}
                    />
                    <span class="pe-val">{sp.exponent.toFixed(1)}</span>
                  </div>
                {/if}
              {:else}
                <div class="pe-head">
                  <span class="pe-title pe-placeholder">
                    {mode === 'curve' ? 'Click a point in the curve to edit its easing' : 'Switch to Curve mode to edit points'}
                  </span>
                </div>
                <div class="pe-row">
                  <span class="pe-label">Curve in</span>
                  <div class="pe-segs">
                    <button class="pe-seg" type="button" disabled>Linear</button>
                    <button class="pe-seg" type="button" disabled>Expo</button>
                    <button class="pe-seg" type="button" disabled>Step</button>
                  </div>
                </div>
              {/if}
            </div>
          </div>

          <div class="profile-preview">
            <svg
              bind:this={curveSvgEl}
              viewBox={`0 0 ${PV.w} ${PV.h}`}
              class="preview curve-canvas"
              class:editable={mode === 'curve' && !running}
              role="img"
              aria-label="Load profile editor"
              on:pointermove={onCurvePointerMove}
              on:pointerup={onCurvePointerUp}
              on:pointercancel={onCurvePointerUp}
              on:click={onCurveBackgroundClick}
              on:contextmenu={suppressContextMenu}
            >
              <defs>
                <linearGradient id="lc-prev-fill" x1="0" x2="0" y1="0" y2="1">
                  <stop offset="0%"   stop-color="rgba(168, 194, 182, 0.55)" />
                  <stop offset="100%" stop-color="rgba(168, 194, 182, 0.06)" />
                </linearGradient>
                <!-- Diagonal hatching for the noise envelope -->
                <pattern id="lc-noise-hatch" patternUnits="userSpaceOnUse" width="6" height="6" patternTransform="rotate(45)">
                  <rect width="6" height="6" fill="transparent" />
                  <line x1="0" y1="0" x2="0" y2="6" stroke="rgba(138, 94, 54, 0.42)" stroke-width="0.9" />
                </pattern>
              </defs>
              <rect x="0" y="0" width={PV.w} height={PV.h} rx="6" fill="rgba(72, 89, 65, 0.04)" stroke="var(--line)" />

              <!-- Y gridlines + tick labels -->
              {#each gridYTicks as v}
                <line
                  x1={PV.l}
                  x2={PV.l + PV_innerW}
                  y1={yAtN(v)}
                  y2={yAtN(v)}
                  stroke="rgba(72,89,65,0.07)"
                  stroke-dasharray="2 4"
                />
                <text x={PV.l - 6} y={yAtN(v) + 3} text-anchor="end" class="pv-label">{v}</text>
              {/each}

              <!-- bottom + left axis lines -->
              <line x1={PV.l} x2={PV.l + PV_innerW} y1={PV.t + PV_innerH} y2={PV.t + PV_innerH} stroke="rgba(72,89,65,0.30)" />
              <line x1={PV.l} x2={PV.l} y1={PV.t} y2={PV.t + PV_innerH} stroke="rgba(72,89,65,0.22)" />

              {#if mode === 'constant'}
                <path d={previewArea} fill="url(#lc-prev-fill)" />
                <path d={previewLine} fill="none" stroke="var(--accent)" stroke-width="2" stroke-linejoin="round" stroke-linecap="round" />
              {:else}
                <path d={curveAreaPath} fill="url(#lc-prev-fill)" />
                {#if noise > 0}
                  <!-- Hatched envelope between (curve × (1-noise)) and (curve × (1+noise)) -->
                  <path d={noiseHatchPath} fill="url(#lc-noise-hatch)" stroke="none" />
                  <!-- Dotted bounds -->
                  <path
                    d={noiseUpperPath}
                    fill="none"
                    stroke="var(--rate)"
                    stroke-width="1.25"
                    stroke-dasharray="3 3"
                    stroke-linecap="round"
                    opacity="0.85"
                  />
                  <path
                    d={noiseLowerPath}
                    fill="none"
                    stroke="var(--rate)"
                    stroke-width="1.25"
                    stroke-dasharray="3 3"
                    stroke-linecap="round"
                    opacity="0.85"
                  />
                {/if}
                <path d={curveLinePath} fill="none" stroke="var(--accent)" stroke-width="2" stroke-linejoin="round" stroke-linecap="round" />

                <!-- Keypoint handles -->
                {#each curvePoints as p, i (i)}
                  <g class="lc-handle" class:selected={i === selectedPtIdx} class:dragging={i === draggingPtIdx}>
                    <circle
                      cx={xAtT(p.timeSecs)}
                      cy={yAtN(p.users)}
                      r="9"
                      class="lc-handle-hit"
                      on:pointerdown={(e) => onHandlePointerDown(e, i)}
                      on:contextmenu={(e) => onHandleContextMenu(e, i)}
                    >
                      <title>{i === 0 || curvePoints.length <= 2 ? 'Drag to move' : 'Drag to move · right-click to delete'}</title>
                    </circle>
                    <circle
                      cx={xAtT(p.timeSecs)}
                      cy={yAtN(p.users)}
                      r="5.5"
                      class="lc-handle-dot"
                      pointer-events="none"
                    />
                  </g>
                {/each}
              {/if}

              <!-- x-axis end labels -->
              <text x={PV.l} y={PV.h - 8} text-anchor="start" class="pv-label">0</text>
              <text x={PV.l + PV_innerW} y={PV.h - 8} text-anchor="end" class="pv-label">{fmtDur(safeDur)}</text>
            </svg>
            <div class="preview-caption">
              <span class="preview-caption-title">Preview</span>
              <p>{previewCaption}</p>
            </div>
          </div>
        </div>

        <div class="controls">
          <button class="btn btn-primary" type="button" on:click={start} disabled={running}>
            <Play size={13} weight="duotone" /> Start load test
          </button>
          <button class="btn" type="button" on:click={stop} disabled={!running}>
            <Stop size={13} weight="duotone" /> Stop
          </button>
        </div>

        {#if errorMsg}
          <p class="error" role="alert">{errorMsg}</p>
        {/if}
      </div>
    </section>
    {:else if view === 'results'}
    <section class="results-pane">
      {#if displayMetrics || running || runs.length > 0}
      <div class="run-header">
        {#if displayIsFlow}
          <span class="rcm-flow-badge hr-flow-badge">FLOW · {displayFlowStepCount}</span>
          <span class="hr-url" title={displayFlowName}>{displayFlowName}</span>
        {:else}
          <span class="status-method method m-{displayMethod.toLowerCase()}">{displayMethod}</span>
          <span class="hr-url" title={displayUrl}>{displayUrl}</span>
        {/if}
        <span class="hr-sep" aria-hidden="true">·</span>
        <span class="hr-state" class:running={displayIsLive && running}>{displayState}</span>
        <span class="hr-sep" aria-hidden="true">·</span>
        <span class="hr-mode">{displayModeStr}</span>
        {#if displayPeakConc > 0}
          <span class="hr-sep" aria-hidden="true">·</span>
          <span class="hr-peak">peak {displayPeakConc}</span>
        {/if}
        {#if displayIsLive && metrics}
          <span class="hr-sep" aria-hidden="true">·</span>
          <span class="hr-now">workers now <strong><NumberFlow value={metrics.currentConcurrency} /></strong></span>
        {/if}
      </div>

      {#if displayMetrics}
        {@const m = displayMetrics}
        {@const tot = Math.max(1, m.totalRequests)}
        <div class="run-card">
          <div class="rc-success">
            <div class="rc-label">Success</div>
            <div class="rc-success-num">
              <NumberFlow
                value={pctNum(m.successful, m.totalRequests)}
                format={{ maximumFractionDigits: 1, minimumFractionDigits: 1 }}
              />
              <span class="rc-success-pct">%</span>
            </div>
            <div class="rc-bar" role="img" aria-label="Status code breakdown">
              <span class="rc-bar-seg rc-bar-ok"   style:width="{(m.successful     / tot) * 100}%"></span>
              <span class="rc-bar-seg rc-bar-warn" style:width="{(m.clientErrors   / tot) * 100}%"></span>
              <span class="rc-bar-seg rc-bar-rate" style:width="{(m.rateLimited    / tot) * 100}%"></span>
              <span class="rc-bar-seg rc-bar-err"  style:width="{(m.serverErrors   / tot) * 100}%"></span>
              <span class="rc-bar-seg rc-bar-net"  style:width="{(m.networkErrors  / tot) * 100}%"></span>
            </div>
            <div class="rc-bar-legend">
              <span class="rc-bl"><span class="rc-bl-sw rc-bl-ok"></span>2XX {pct(m.successful, tot)}%</span>
              <span class="rc-bl"><span class="rc-bl-sw rc-bl-warn"></span>4XX {pct(m.clientErrors, tot)}%</span>
              <span class="rc-bl"><span class="rc-bl-sw rc-bl-rate"></span>429 {pct(m.rateLimited, tot)}%</span>
              <span class="rc-bl"><span class="rc-bl-sw rc-bl-err"></span>5XX {pct(m.serverErrors, tot)}%</span>
              {#if m.networkErrors > 0}
                <span class="rc-bl"><span class="rc-bl-sw rc-bl-net"></span>NET {pct(m.networkErrors, tot)}%</span>
              {/if}
            </div>
          </div>

          <div class="rc-stats">
            <div class="rc-cell">
              <span class="rc-k">Elapsed</span>
              <span class="rc-v"><NumberFlow value={m.elapsedSecs} format={{ maximumFractionDigits: 1, minimumFractionDigits: 1 }} /><span class="rc-u">s</span></span>
            </div>
            <div class="rc-cell">
              <span class="rc-k">Requests</span>
              <span class="rc-v"><NumberFlow value={m.totalRequests} /></span>
            </div>
            <div class="rc-cell">
              <span class="rc-k">RPS</span>
              <span class="rc-v"><NumberFlow value={m.rps} format={{ maximumFractionDigits: 1, minimumFractionDigits: 1 }} /></span>
            </div>
            <div class="rc-cell">
              <span class="rc-k">p50</span>
              <span class="rc-v"><NumberFlow value={m.p50Ms} format={{ maximumFractionDigits: 1, minimumFractionDigits: 1 }} /><span class="rc-u">ms</span></span>
            </div>
            <div class="rc-cell">
              <span class="rc-k">p95</span>
              <span class="rc-v"><NumberFlow value={m.p95Ms} format={{ maximumFractionDigits: 1, minimumFractionDigits: 1 }} /><span class="rc-u">ms</span></span>
            </div>
            <div class="rc-cell">
              <span class="rc-k">p99</span>
              <span class="rc-v"><NumberFlow value={m.p99Ms} format={{ maximumFractionDigits: 1, minimumFractionDigits: 1 }} /><span class="rc-u">ms</span></span>
            </div>
            <div class="rc-cell">
              <span class="rc-k">Failed</span>
              <span class="rc-v"><NumberFlow value={m.errors} /></span>
            </div>
          </div>

          <div class="rc-spark">
            <div class="rc-spark-head">
              <div class="rc-label">Throughput / Tick</div>
              <span class="rc-spark-range">{sparkSpan}</span>
            </div>
            <svg viewBox={`0 0 ${SPARK_W} ${SPARK_H}`} preserveAspectRatio="none" class="spark" role="img" aria-label="Throughput per tick">
              <defs>
                <linearGradient id="lc-spark-grad" x1="0" x2="0" y1="0" y2="1">
                  <stop offset="0%"   stop-color="rgba(159, 184, 173, 0.55)" />
                  <stop offset="100%" stop-color="rgba(159, 184, 173, 0.06)" />
                </linearGradient>
              </defs>
              {#if sparkPath.area}
                <path d={sparkPath.area} fill="url(#lc-spark-grad)" />
              {/if}
              {#if sparkPath.line}
                <path d={sparkPath.line} fill="none" stroke="var(--accent)" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" vector-effect="non-scaling-stroke" />
              {/if}
            </svg>
          </div>
        </div>
      {/if}

      <div class="chart-wrap" bind:this={chartWrapEl}>
        <div class="chart-legend">
          <span class="lg"><span class="sw sw-ok"></span>2xx / tick</span>
          <span class="lg"><span class="sw sw-fail"></span>failed / tick</span>
          <span class="lg"><span class="sw sw-p50"></span>p50</span>
          <span class="lg"><span class="sw sw-p95"></span>p95</span>
          <span class="lg"><span class="sw sw-p99"></span>p99</span>
        </div>
        <svg
          bind:this={chartEl}
          viewBox={`0 0 ${CW} ${CH}`}
          preserveAspectRatio="xMidYMid meet"
          class="chart"
          role="img"
          aria-label="Throughput and latency over time"
          on:pointermove={onChartPointerMove}
          on:pointerleave={onChartPointerLeave}
        ></svg>
        {#if hoverIdx >= 0 && displayHistory[hoverIdx]}
          {@const s = displayHistory[hoverIdx]}
          {@const total = s.tickRps}
          {@const fail = Math.max(0, s.tickRps - s.tickRpsOk)}
          <div
            class="chart-tooltip"
            class:flip={tooltipFlip}
            style:left="{tooltipLeft}px"
          >
            <div class="tt-time">{s.t.toFixed(1)}s</div>
            <div class="tt-row">
              <span class="tt-sw sw-ok"></span>
              <span class="tt-k">2xx</span>
              <span class="tt-v">{fmt(s.tickRpsOk, 0)}</span>
              <span class="tt-p">{pct(s.tickRpsOk, total)}%</span>
            </div>
            <div class="tt-row">
              <span class="tt-sw sw-fail"></span>
              <span class="tt-k">failed</span>
              <span class="tt-v">{fmt(fail, 0)}</span>
              <span class="tt-p">{pct(fail, total)}%</span>
            </div>
            <div class="tt-divider"></div>
            <div class="tt-row">
              <span class="tt-sw sw-p50"></span>
              <span class="tt-k">p50</span>
              <span class="tt-v">{fmt(s.p50, 1)}<span class="tt-u">ms</span></span>
            </div>
            <div class="tt-row">
              <span class="tt-sw sw-p95"></span>
              <span class="tt-k">p95</span>
              <span class="tt-v">{fmt(s.p95, 1)}<span class="tt-u">ms</span></span>
            </div>
            <div class="tt-row">
              <span class="tt-sw sw-p99"></span>
              <span class="tt-k">p99</span>
              <span class="tt-v">{fmt(s.p99, 1)}<span class="tt-u">ms</span></span>
            </div>
          </div>
        {/if}
      </div>
      {:else}
        <div class="results-empty">
          <div class="re-mark" aria-hidden="true">
            <img src={logoUrl} alt="" />
          </div>
          <h2 class="re-title">No results yet</h2>
          <p class="re-desc">
            Pick a saved request, configure a load profile, and start a test.<br />
            Throughput, latency percentiles, and per-status breakdowns will appear here.
          </p>
          <div class="re-actions">
            <button
              type="button"
              class="btn btn-primary"
              on:click={() => (view = 'loadtest')}
            >
              <Play size={13} weight="duotone" /> Open Load test
            </button>
            <button
              type="button"
              class="btn btn-ghost"
              on:click={() => (view = 'request')}
            >Edit request</button>
          </div>
        </div>
      {/if}
    </section>
    {/if}
  </div>

  {#if infoOpen}
    <div
      class="info-backdrop"
      role="presentation"
      on:click={closeInfo}
    >
      <div
        class="info-sheet"
        role="dialog"
        aria-modal="true"
        aria-labelledby="info-title"
        on:click|stopPropagation
      >
        <button
          type="button"
          class="info-close"
          on:click={closeInfo}
          aria-label="Close"
        >
          <X size={14} weight="duotone" />
        </button>

        <div class="info-mark" aria-hidden="true">
          <img src={logoUrl} alt="" />
        </div>
        <h2 id="info-title" class="info-title">LoadCell</h2>
        <p class="info-subtitle">A desktop load tester for HTTP APIs.</p>

        <p class="info-body">
          Build requests, sketch a load profile, fire a test, then see throughput,
          latency percentiles, and per-status breakdowns over time. Runs are
          saved locally so you can switch between past tests.
        </p>

        <div class="info-setting">
          <label class="info-setting-label" for="info-max-workers">
            Max concurrency
            <span class="info-setting-hint">workers cap (default 500)</span>
          </label>
          <input
            id="info-max-workers"
            class="info-setting-input"
            type="number"
            min={MAX_WORKERS_FLOOR}
            max={MAX_WORKERS_CEIL}
            step="50"
            bind:value={MAX_WORKERS}
            disabled={running}
          />
          {#if MAX_WORKERS > 2000}
            <p class="info-warn" role="alert">
              <strong>Above 2000.</strong> If your laptop melts or starts crying
             or your kernel panics, know that you brought it on yourself lol :) .
            </p>
          {/if}
        </div>

        <button
          type="button"
          class="info-link"
          on:click={openRobiWork}
          title="Open robi.work"
        >
          Built by Robi · robi.work →
        </button>

        <button
          type="button"
          class="info-sponsor"
          on:click={openSponsor}
          title="Sponsor on GitHub"
        >
          <svg viewBox="0 0 24 24" fill="currentColor" aria-hidden="true" focusable="false">
            <path d="M12 21s-7-4.35-7-10a4 4 0 0 1 7-2.65A4 4 0 0 1 19 11c0 5.65-7 10-7 10Z" />
          </svg>
          Sponsor on GitHub
        </button>
      </div>
    </div>
  {/if}

  {#if composeOpen}
    <div class="modal-backdrop" on:click|self={closeCompose} role="presentation">
      <div class="modal compose-modal" role="dialog" aria-modal="true" aria-label="Compose flow">
        <div class="modal-head">
          <h3 class="modal-title">{composeEditingId ? 'Edit flow' : 'New flow'}</h3>
          <button class="modal-close" type="button" on:click={closeCompose} title="Close">
            <X size={14} weight="duotone" />
          </button>
        </div>
        <div class="modal-body">
          <label class="compose-name">
            <span class="k">Name</span>
            <input
              type="text"
              bind:value={composeName}
              placeholder="e.g. checkout-journey"
              spellcheck="false"
            />
          </label>
          <div class="compose-panes">
            <div class="compose-pane">
              <div class="compose-pane-head">
                <span class="compose-pane-title">Saved requests</span>
                <span class="compose-pane-hint">click to add →</span>
              </div>
              {#if composeAvailable.length === 0}
                <p class="compose-empty">No saved requests yet.</p>
              {:else}
                <ul class="compose-avail">
                  {#each composeAvailable as r (r.id)}
                    <li>
                      <button class="compose-avail-row" type="button" on:click={() => composeAddStep(r.id)}>
                        <span class="method m-{r.method.toLowerCase()}">{r.method}</span>
                        <span class="compose-avail-name">{r.name || 'Untitled'}</span>
                      </button>
                    </li>
                  {/each}
                </ul>
              {/if}
            </div>
            <div class="compose-pane">
              <div class="compose-pane-head">
                <span class="compose-pane-title">Steps in order</span>
                <span class="compose-pane-hint">{composeStepIds.length} step{composeStepIds.length === 1 ? '' : 's'}</span>
              </div>
              {#if composeStepIds.length === 0}
                <p class="compose-empty">Pick requests on the left to build a flow.</p>
              {:else}
                <ol class="compose-steps">
                  {#each composeStepIds as sid, i (i + ':' + sid)}
                    {@const r = requests.find((x) => x.id === sid)}
                    <li>
                      <span class="compose-step-num">{i + 1}</span>
                      {#if r}
                        <span class="method m-{r.method.toLowerCase()}">{r.method}</span>
                        <span class="compose-step-name">{r.name || 'Untitled'}</span>
                      {:else}
                        <span class="method m-deleted">DEL</span>
                        <span class="compose-step-name compose-step-missing">deleted request</span>
                      {/if}
                      <div class="compose-step-actions">
                        <button type="button" on:click={() => composeMoveStep(i, -1)} disabled={i === 0} title="Move up" aria-label="Move up">↑</button>
                        <button type="button" on:click={() => composeMoveStep(i, 1)} disabled={i === composeStepIds.length - 1} title="Move down" aria-label="Move down">↓</button>
                        <button type="button" on:click={() => composeRemoveStep(i)} title="Remove" aria-label="Remove">
                          <X size={11} weight="duotone" />
                        </button>
                      </div>
                    </li>
                  {/each}
                </ol>
              {/if}
            </div>
          </div>
          <p class="compose-note">
            <strong>How flows run:</strong> Each worker fires step 1 → 2 → … → N in order, then loops. Workers are independent, so different workers may be on different steps at the same moment — no barrier between steps. Good for modeling real user journeys; not for "everyone fire step 1, then everyone fire step 2".
          </p>
        </div>
        <div class="modal-foot">
          <button class="btn btn-ghost" type="button" on:click={closeCompose}>Cancel</button>
          <button
            class="btn btn-primary"
            type="button"
            on:click={saveCompose}
            disabled={!composeName.trim() || composeStepIds.length === 0}
          >Save flow</button>
        </div>
      </div>
    </div>
  {/if}
</main>

<style>
  main.app {
    display: grid;
    grid-template-rows: 48px 1fr;
    height: 100vh;
    width: 100%;
    overflow: hidden;
    text-align: left;
    font-family: "Lexend", -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
    color: var(--text);
  }

  /* ─── Top navigation ───────────────────────────────────────────── */
  .topnav {
    height: 48px;
    display: grid;
    grid-template-columns: 1fr auto 1fr;
    align-items: center;
    padding: 0 16px;
    background: var(--bg);
    border-bottom: 1px solid var(--line);
    -webkit-app-region: drag;
  }
  .brand {
    appearance: none;
    background: transparent;
    border: none;
    color: inherit;
    font: inherit;
    cursor: pointer;
    display: inline-flex;
    align-items: center;
    gap: 8px;
    justify-self: start;
    padding: 4px 8px 4px 60px; /* leave room for macOS traffic lights */
    border-radius: 6px;
    -webkit-app-region: no-drag;
    transition: background 120ms;
  }
  .brand:hover { background: rgba(72, 89, 65, 0.06); }
  .brand-mark {
    width: 22px;
    height: 22px;
    object-fit: contain;
    display: block;
  }
  .brand-wordmark {
    font-size: 13px;
    font-weight: 600;
    color: var(--text);
    letter-spacing: -0.01em;
  }
  .nav-segs {
    display: inline-flex;
    gap: 2px;
    background: var(--surface-2);
    border: 1px solid var(--line);
    border-radius: 6px;
    padding: 2px;
    justify-self: center;
    -webkit-app-region: no-drag;
  }
  .nav-seg {
    appearance: none;
    background: transparent;
    border: none;
    color: var(--muted);
    font: inherit;
    font-size: 12px;
    font-weight: 500;
    padding: 5px 14px;
    border-radius: 4px;
    cursor: pointer;
    transition: background 120ms, color 120ms;
  }
  .nav-seg:hover:not(:disabled):not(.active) {
    color: var(--text);
    background: rgba(72, 89, 65, 0.06);
  }
  .nav-seg.active {
    background: var(--inset);
    color: var(--accent-strong);
    box-shadow: 0 1px 2px rgba(72, 89, 65, 0.08);
  }
  .nav-seg:disabled {
    opacity: 0.4;
    cursor: not-allowed;
  }
  .nav-status {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    justify-self: end;
    color: var(--muted);
    font-size: 11px;
    font-weight: 500;
    -webkit-app-region: no-drag;
  }
  .status-pip {
    width: 7px;
    height: 7px;
    border-radius: 50%;
    background: var(--accent);
    box-shadow: 0 0 0 0 rgba(72, 89, 65, 0.4);
  }
  .status-pip.live {
    animation: lc-pulse 1.4s ease-in-out infinite;
  }
  .status-text { color: var(--text); font-weight: 500; }
  .status-text.muted { color: var(--muted); font-weight: 500; }
  @keyframes lc-pulse {
    0%, 100% { box-shadow: 0 0 0 0 rgba(72, 89, 65, 0.40); }
    50%      { box-shadow: 0 0 0 5px rgba(72, 89, 65, 0); }
  }

  /* ─── Workspace ───────────────────────────────────────────────── */
  .workspace {
    display: grid;
    grid-template-columns: 296px 1fr;
    gap: 14px;
    padding: 14px;
    min-height: 0;
    overflow: hidden;
  }

  /* ─── Sidebar ──────────────────────────────────────────────────── */
  .sidebar {
    background: var(--surface);
    border: 1px solid var(--line);
    border-radius: 8px;
    padding: 12px;
    display: flex;
    flex-direction: column;
    gap: 8px;
    min-height: 0;
    overflow: hidden;
  }
  .side-section {
    display: flex;
    flex-direction: column;
    gap: 8px;
    min-height: 0;
  }
  .side-section + .side-section {
    margin-top: 4px;
    padding-top: 12px;
    border-top: 1px solid var(--line);
  }
  .runs-section {
    flex: 1 1 auto;
    min-height: 0;
    overflow: hidden;
  }
  .side-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 2px 4px;
  }
  .side-count {
    font-size: 10px;
    color: var(--muted);
    background: rgba(72, 89, 65, 0.10);
    padding: 1px 6px;
    border-radius: 999px;
    font-weight: 500;
    font-variant-numeric: tabular-nums;
  }
  .side-title {
    font-size: 10px;
    text-transform: uppercase;
    letter-spacing: 0.12em;
    color: var(--muted);
    opacity: 0.65;
    font-weight: 500;
  }
  .side-new {
    appearance: none;
    background: #fff;
    color: var(--text);
    border: 1px solid var(--line-strong);
    padding: 4px 10px 4px 8px;
    border-radius: 4px;
    font: inherit;
    font-size: 11px;
    font-weight: 500;
    cursor: pointer;
    transition: all 120ms;
    display: inline-flex;
    align-items: center;
    gap: 5px;
  }
  .side-new:hover {
    background: rgba(159, 184, 173, 0.10);
    border-color: var(--accent);
    color: var(--accent-strong);
  }
  .empty {
    color: var(--muted);
    opacity: 0.55;
    font-size: 12px;
    font-weight: 300;
    margin: 16px 4px;
    line-height: 1.6;
  }
  .req-list {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 4px;
    overflow-y: auto;
    min-height: 0;
    flex: 1;
  }
  .req-list li {
    display: flex;
    align-items: stretch;
    border: 1px solid var(--line);
    border-radius: 5px;
    background: #fff;
    transition: background 120ms, border-color 120ms;
  }
  .req-list li.active {
    background: var(--sage-tint);
    border-color: rgba(72, 89, 65, 0.3);
  }
  .req-list li:hover:not(.active) {
    background: #fff;
    border-color: rgba(72, 89, 65, 0.16);
    box-shadow: 0 1px 2px rgba(72, 89, 65, 0.04);
  }
  .req-row {
    flex: 1;
    display: flex;
    align-items: center;
    gap: 8px;
    background: transparent;
    border: none;
    color: inherit;
    padding: 8px 10px;
    cursor: pointer;
    text-align: left;
    font: inherit;
    border-radius: 4px;
    min-width: 0;
  }
  .req-name {
    font-size: 13px;
    font-weight: 400;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    color: var(--text);
  }
  .req-del {
    appearance: none;
    background: transparent;
    border: none;
    color: var(--muted);
    opacity: 0.4;
    padding: 0 10px;
    cursor: pointer;
    border-radius: 4px;
    transition: all 120ms;
    display: inline-flex;
    align-items: center;
    justify-content: center;
  }
  .req-del:hover {
    color: var(--err);
    opacity: 1;
    background: rgba(120, 50, 50, 0.10);
  }

  /* ─── Saved-flow list (sidebar) ──────────────────────────────────── */
  /* Flows section is content-sized — never shrinks under the runs
     section, and renders each flow as a distinct card so the boundary
     between rows is unambiguous (the previous flat-row style let the
     second row visually run into the SAVED RUNS header below). */
  .flows-section {
    flex: 0 0 auto;
  }
  .flow-list {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 4px;
    /* 3 rows × ~40px + 2 gaps × 4px = 128px. Anything past that scrolls
       so the section can't push the saved-runs list off-screen. */
    max-height: 132px;
    overflow-y: auto;
  }
  .flow-list li {
    display: flex;
    align-items: stretch;
    border: 1px solid var(--line);
    border-radius: 5px;
    background: #fff;
    transition: background 120ms, border-color 120ms;
  }
  .flow-list li.active {
    background: var(--sage-tint);
    border-color: rgba(72, 89, 65, 0.3);
  }
  .flow-list li:hover:not(.active) {
    background: #fff;
    border-color: rgba(72, 89, 65, 0.16);
    box-shadow: 0 1px 2px rgba(72, 89, 65, 0.04);
  }
  .flow-row {
    flex: 1;
    display: flex;
    align-items: center;
    gap: 9px;
    background: transparent;
    border: none;
    color: inherit;
    padding: 8px 4px 8px 10px;
    cursor: pointer;
    text-align: left;
    font: inherit;
    min-width: 0;
  }
  .flow-badge {
    flex: 0 0 auto;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    min-width: 22px;
    height: 22px;
    padding: 0 6px;
    background: var(--inset);
    border: 1px solid var(--line);
    border-radius: 3px;
    font-size: 11px;
    font-weight: 600;
    color: var(--accent-strong);
    font-variant-numeric: tabular-nums;
  }
  .flow-list li.active .flow-badge {
    background: rgba(255, 255, 255, 0.85);
    border-color: rgba(72, 89, 65, 0.3);
  }
  .flow-name {
    font-size: 13px;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    color: var(--text);
    min-width: 0;
  }
  .flow-edit,
  .flow-del {
    flex: 0 0 auto;
    appearance: none;
    background: transparent;
    border: none;
    color: var(--muted);
    opacity: 0.4;
    width: 26px;
    cursor: pointer;
    transition: all 120ms;
    display: inline-flex;
    align-items: center;
    justify-content: center;
  }
  .flow-edit:hover {
    color: var(--accent-strong);
    opacity: 1;
    background: rgba(72, 89, 65, 0.08);
  }
  .flow-del {
    margin-right: 4px;
  }
  .flow-del:hover {
    color: var(--err);
    opacity: 1;
    background: rgba(120, 50, 50, 0.10);
  }
  .empty-link {
    appearance: none;
    background: none;
    border: none;
    padding: 0;
    font: inherit;
    color: var(--accent-strong);
    text-decoration: underline;
    cursor: pointer;
  }

  /* ─── Saved-run cards (sidebar) ─────────────────────────────────── */
  .run-list {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 6px;
    overflow-y: auto;
    min-height: 0;
    flex: 1;
  }
  .run-card-mini {
    position: relative;
    display: flex;
    align-items: stretch;
    background: var(--inset);
    border: 1px solid var(--line);
    border-radius: 6px;
    transition: background 120ms, border-color 120ms;
  }
  .run-card-mini.active {
    background: var(--sage-tint);
    border-color: var(--accent);
  }
  .run-card-mini:hover:not(.active) {
    background: rgba(72, 89, 65, 0.04);
    border-color: var(--line-strong);
  }
  .run-card-mini.live {
    border-style: dashed;
  }
  .rcm-main {
    flex: 1;
    appearance: none;
    background: transparent;
    border: none;
    font: inherit;
    color: var(--text);
    text-align: left;
    padding: 8px 10px;
    cursor: pointer;
    display: flex;
    flex-direction: column;
    gap: 5px;
    min-width: 0;
  }
  .rcm-head {
    display: flex;
    align-items: center;
    gap: 6px;
    min-width: 0;
  }
  /* FLOW badge for compound-run cards. Visually parallel to the method
     chip (same height, same tracking) so the layout stays consistent
     between single-request and flow runs. */
  .rcm-flow-badge {
    flex: 0 0 auto;
    display: inline-flex;
    align-items: center;
    padding: 2px 7px;
    background: var(--sage-tint);
    border: 1px solid rgba(72, 89, 65, 0.25);
    border-radius: 3px;
    font-size: 9.5px;
    font-weight: 600;
    letter-spacing: 0.06em;
    color: var(--accent-strong);
    font-variant-numeric: tabular-nums;
    text-transform: uppercase;
  }
  .rcm-name {
    flex: 1;
    font-size: 12px;
    font-weight: 500;
    color: var(--text);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    min-width: 0;
  }
  .rcm-time {
    font-size: 10px;
    color: var(--muted);
    font-variant-numeric: tabular-nums;
    flex-shrink: 0;
  }
  .rcm-mid {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .rcm-pct {
    font-size: 11px;
    color: var(--muted);
    font-variant-numeric: tabular-nums;
    white-space: nowrap;
    flex: 0 0 50px;
  }
  .rcm-pct strong {
    font-size: 13px;
    color: var(--accent-strong);
    font-weight: 600;
  }
  .rcm-spark {
    flex: 1;
    height: 22px;
    display: block;
  }
  .rcm-foot {
    display: flex;
    justify-content: space-between;
    gap: 6px;
    font-size: 10px;
    color: var(--muted);
    font-variant-numeric: tabular-nums;
    font-weight: 500;
  }
  .rcm-live-dot {
    width: 7px;
    height: 7px;
    border-radius: 50%;
    background: var(--accent);
    animation: lc-pulse 1.4s ease-in-out infinite;
    flex-shrink: 0;
  }
  .rcm-del {
    appearance: none;
    background: transparent;
    border: none;
    border-left: 1px solid transparent;
    color: var(--muted);
    cursor: pointer;
    padding: 0 8px;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    opacity: 0;
    transition: opacity 120ms, color 120ms, background 120ms;
  }
  .run-card-mini:hover .rcm-del { opacity: 1; }
  .rcm-del:hover {
    color: var(--err);
    background: rgba(120, 50, 50, 0.10);
  }

  /* ─── Method badges (shared across sidebar + selects + metrics) ──── */
  .method {
    display: inline-block;
    font-size: 10px;
    font-weight: 700;
    letter-spacing: 0.05em;
    padding: 3px 7px;
    border-radius: 3px;
    font-family: "Lexend", ui-monospace, "SF Mono", Menlo, monospace;
    font-variant-numeric: tabular-nums;
    min-width: 46px;
    text-align: center;
    flex-shrink: 0;
  }
  .m-get     { background: rgba(159, 184, 173, 0.30); color: #2d4538; }
  .m-post    { background: rgba(72, 89, 65, 0.88);    color: #ffffff; }
  .m-put     { background: rgba(138, 114, 54, 0.22);  color: #5a4a26; }
  .m-patch   { background: rgba(138, 114, 54, 0.22);  color: #5a4a26; }
  .m-delete  { background: rgba(120, 50, 50, 0.18);   color: #4a2020; }
  .m-head    { background: rgba(72, 89, 65, 0.10);    color: var(--muted); }
  .m-options { background: rgba(72, 89, 65, 0.10);    color: var(--muted); }

  /* ─── Builder ─────────────────────────────────────────────────── */
  .builder {
    background: var(--surface);
    border: 1px solid var(--line);
    border-radius: 8px;
    padding: 18px;
    display: flex;
    flex-direction: column;
    gap: 14px;
    min-height: 0;
    overflow-y: auto;
  }
  .builder.loadtest-pane {
    overflow-y: auto;
  }
  .lt-request {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 10px 12px;
    background: var(--inset);
    border: 1px solid var(--line);
    border-radius: 6px;
    font-size: 12px;
    min-width: 0;
  }
  .lt-request-label {
    font-size: 10px;
    text-transform: uppercase;
    letter-spacing: 0.12em;
    color: var(--muted);
    font-weight: 500;
    flex-shrink: 0;
  }
  .lt-request-name {
    color: var(--text);
    font-weight: 500;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    flex-shrink: 0;
    max-width: 220px;
  }
  .lt-request-url {
    color: var(--muted);
    font-size: 11px;
    font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    flex: 1;
    min-width: 0;
  }
  .lt-request-edit {
    appearance: none;
    background: transparent;
    border: 1px solid var(--line-strong);
    color: var(--muted);
    font: inherit;
    font-size: 11px;
    font-weight: 500;
    padding: 3px 10px;
    border-radius: 4px;
    cursor: pointer;
    flex-shrink: 0;
    transition: all 120ms;
  }
  .lt-request-edit:hover:not(:disabled) {
    border-color: var(--accent);
    color: var(--accent-strong);
    background: rgba(72, 89, 65, 0.05);
  }
  .lt-request-edit:disabled {
    opacity: 0.4;
    cursor: not-allowed;
  }

  /* ─── Flow run header (load-test pane, flow mode) ───────────────── */
  .lt-flow {
    background: #fff;
    border: 1px solid var(--line);
    border-radius: 6px;
    padding: 14px 16px;
    display: flex;
    flex-direction: column;
    gap: 12px;
  }
  .lt-flow-head {
    display: flex;
    align-items: center;
    gap: 10px;
  }
  .lt-flow-label {
    display: inline-flex;
    align-items: center;
    padding: 3px 8px;
    text-transform: uppercase;
    letter-spacing: 0.08em;
    font-size: 10px;
    color: var(--accent-strong);
    font-weight: 700;
    background: var(--sage-tint);
    border: 1px solid rgba(72, 89, 65, 0.30);
    border-radius: 3px;
  }
  .lt-flow-name {
    font-size: 15px;
    font-weight: 600;
    color: var(--text);
  }
  .lt-flow-count {
    margin-left: 0;
    padding: 1px 8px;
    background: rgba(72, 89, 65, 0.12);
    border-radius: 999px;
    font-size: 11px;
    color: var(--accent-strong);
    font-weight: 500;
  }
  .lt-flow-edit {
    margin-left: auto;
    appearance: none;
    background: #fff;
    border: 1px solid var(--line-strong);
    border-radius: 4px;
    color: var(--text);
    padding: 4px 12px;
    font: inherit;
    font-size: 11.5px;
    cursor: pointer;
    transition: border-color 120ms;
  }
  .lt-flow-edit:hover:not(:disabled) {
    border-color: rgba(72, 89, 65, 0.45);
  }
  .lt-flow-edit:disabled {
    opacity: 0.4;
    cursor: not-allowed;
  }
  .lt-flow-steps {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .lt-flow-step {
    display: grid;
    grid-template-columns: 28px auto 1fr auto;
    align-items: center;
    gap: 10px;
    padding: 8px 10px;
    background: var(--inset);
    border: 1px solid var(--line);
    border-radius: 4px;
  }
  .lt-flow-step-num {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    min-width: 22px;
    height: 22px;
    padding: 0 6px;
    background: #fff;
    border: 1px solid var(--line);
    border-radius: 3px;
    font-size: 11px;
    font-weight: 600;
    color: var(--accent-strong);
    font-variant-numeric: tabular-nums;
  }
  .lt-flow-step.missing .lt-flow-step-num {
    background: rgba(120, 50, 50, 0.12);
    border-color: rgba(120, 50, 50, 0.3);
    color: var(--err);
  }
  .lt-flow-step-name {
    font-size: 13px;
    color: var(--text);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .lt-flow-step.missing .lt-flow-step-name {
    color: var(--err);
    font-style: italic;
  }
  .lt-flow-step-url {
    font-family: ui-monospace, "SF Mono", Menlo, monospace;
    font-size: 11.5px;
    color: var(--muted);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    min-width: 0;
  }

  /* ─── Compose modal ──────────────────────────────────────────────── */
  .modal-backdrop {
    position: fixed;
    inset: 0;
    background: rgba(20, 30, 25, 0.45);
    display: flex;
    align-items: center;
    justify-content: center;
    z-index: 100;
    backdrop-filter: blur(2px);
  }
  .modal {
    background: var(--bg, #fff);
    border: 1px solid var(--line-strong);
    border-radius: 8px;
    box-shadow: 0 20px 50px rgba(20, 30, 25, 0.2);
    width: min(880px, 92vw);
    max-height: 86vh;
    display: flex;
    flex-direction: column;
  }
  .modal-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 14px 18px;
    border-bottom: 1px solid var(--line);
  }
  .modal-title {
    margin: 0;
    font-size: 15px;
    font-weight: 600;
    color: var(--text);
  }
  .modal-close {
    appearance: none;
    background: transparent;
    border: none;
    color: var(--muted);
    padding: 4px;
    cursor: pointer;
    border-radius: 3px;
    display: inline-flex;
    align-items: center;
    justify-content: center;
  }
  .modal-close:hover {
    color: var(--text);
    background: rgba(72, 89, 65, 0.08);
  }
  .modal-body {
    padding: 18px;
    overflow-y: auto;
    display: flex;
    flex-direction: column;
    gap: 16px;
  }
  .modal-foot {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
    padding: 12px 18px;
    border-top: 1px solid var(--line);
  }
  .compose-name {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  .compose-name .k {
    font-size: 11px;
    color: var(--muted);
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }
  .compose-name input {
    height: 34px;
    padding: 0 10px;
    background: var(--inset);
    color: var(--text);
    border: 1px solid var(--line-strong);
    border-radius: 4px;
    font: inherit;
    font-size: 13px;
  }
  .compose-name input:focus {
    outline: none;
    border-color: var(--accent);
    box-shadow: 0 0 0 3px rgba(159, 184, 173, 0.15);
  }
  .compose-note {
    margin: 0;
    padding: 10px 12px;
    background: var(--inset);
    border: 1px solid var(--line);
    border-left: 3px solid rgba(72, 89, 65, 0.35);
    border-radius: 4px;
    font-size: 11.5px;
    line-height: 1.55;
    color: var(--muted);
  }
  .compose-note strong {
    color: var(--text);
    font-weight: 600;
  }
  .compose-panes {
    display: grid;
    grid-template-columns: 1fr 1.1fr;
    gap: 12px;
    min-height: 320px;
  }
  .compose-pane {
    background: var(--inset);
    border: 1px solid var(--line);
    border-radius: 6px;
    display: flex;
    flex-direction: column;
    min-height: 0;
  }
  .compose-pane-head {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 8px 12px;
    border-bottom: 1px solid var(--line);
  }
  .compose-pane-title {
    font-size: 11px;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    color: var(--muted);
  }
  .compose-pane-hint {
    font-size: 11px;
    color: var(--muted);
    opacity: 0.7;
  }
  .compose-empty {
    margin: 0;
    padding: 16px 12px;
    font-size: 12px;
    color: var(--muted);
    text-align: center;
  }
  .compose-avail {
    list-style: none;
    margin: 0;
    padding: 4px;
    display: flex;
    flex-direction: column;
    gap: 2px;
    overflow-y: auto;
    flex: 1;
  }
  .compose-avail-row {
    display: flex;
    align-items: center;
    gap: 8px;
    width: 100%;
    padding: 7px 10px;
    background: transparent;
    border: none;
    border-radius: 4px;
    color: var(--text);
    font: inherit;
    font-size: 13px;
    text-align: left;
    cursor: pointer;
    transition: background 120ms;
  }
  .compose-avail-row:hover {
    background: rgba(72, 89, 65, 0.10);
  }
  .compose-avail-name {
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .compose-steps {
    list-style: none;
    margin: 0;
    padding: 4px;
    display: flex;
    flex-direction: column;
    gap: 4px;
    overflow-y: auto;
    flex: 1;
  }
  .compose-steps li {
    display: grid;
    grid-template-columns: 22px auto 1fr auto;
    align-items: center;
    gap: 8px;
    padding: 6px 8px;
    background: rgba(255, 255, 255, 0.7);
    border: 1px solid var(--line);
    border-radius: 4px;
  }
  .compose-step-num {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    min-width: 20px;
    height: 20px;
    padding: 0 5px;
    background: var(--inset);
    border: 1px solid var(--line);
    border-radius: 3px;
    font-size: 10.5px;
    font-weight: 600;
    color: var(--accent-strong);
    font-variant-numeric: tabular-nums;
  }
  .compose-step-name {
    font-size: 12.5px;
    color: var(--text);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .compose-step-missing {
    color: var(--err);
    font-style: italic;
  }
  .compose-step-actions {
    display: flex;
    gap: 2px;
  }
  .compose-step-actions button {
    appearance: none;
    background: transparent;
    border: 1px solid transparent;
    color: var(--muted);
    width: 22px;
    height: 22px;
    border-radius: 3px;
    cursor: pointer;
    font-size: 12px;
    display: inline-flex;
    align-items: center;
    justify-content: center;
  }
  .compose-step-actions button:hover:not(:disabled) {
    background: rgba(72, 89, 65, 0.10);
    border-color: rgba(72, 89, 65, 0.18);
    color: var(--text);
  }
  .compose-step-actions button:disabled {
    opacity: 0.3;
    cursor: not-allowed;
  }
  .compose-step-actions button:last-child:hover:not(:disabled) {
    background: rgba(120, 50, 50, 0.10);
    border-color: rgba(120, 50, 50, 0.20);
    color: var(--err);
  }

  .results-pane {
    display: flex;
    flex-direction: column;
    gap: 12px;
    min-height: 0;
    overflow: hidden;
    background: var(--surface);
    border: 1px solid var(--line);
    border-radius: 8px;
    padding: 14px;
  }
  .results-empty {
    margin: auto;
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 14px;
    color: var(--muted);
    text-align: center;
    padding: 32px;
    max-width: 440px;
  }
  .re-mark {
    width: 64px;
    height: 64px;
    border-radius: 14px;
    background: var(--inset);
    border: 1px solid var(--line);
    display: inline-flex;
    align-items: center;
    justify-content: center;
    opacity: 0.85;
  }
  .re-mark img {
    width: 40px;
    height: 40px;
    object-fit: contain;
    opacity: 0.9;
  }
  .re-title {
    margin: 4px 0 0;
    font-size: 18px;
    font-weight: 600;
    color: var(--text);
    letter-spacing: -0.01em;
  }
  .re-desc {
    margin: 0;
    color: var(--muted);
    font-size: 13px;
    line-height: 1.55;
  }
  .re-actions {
    display: inline-flex;
    gap: 8px;
    margin-top: 6px;
  }
  .builder-head {
    display: flex;
    align-items: center;
    gap: 10px;
  }
  .name-input {
    flex: 1;
    height: 34px;
    padding: 0 10px;
    background: #fff;
    border: 1px solid var(--line);
    color: var(--text);
    border-radius: 4px;
    font: inherit;
    font-size: 16px;
    font-weight: 500;
    letter-spacing: -0.005em;
    transition: border-color 120ms, box-shadow 120ms;
  }
  .name-input::placeholder {
    color: var(--muted);
    opacity: 0.4;
    font-weight: 300;
  }
  .name-input:hover:not(:disabled) {
    border-color: var(--line-strong);
  }
  .name-input:focus {
    border-color: rgba(72, 89, 65, 0.45);
    outline: none;
  }
  .save-row {
    display: flex;
    align-items: center;
    gap: 6px;
  }
  .save-hint {
    font-size: 11px;
    font-weight: 500;
    color: var(--accent);
    margin-right: 4px;
    display: inline-flex;
    align-items: center;
    gap: 4px;
  }

  .url-row {
    display: flex;
    gap: 6px;
  }
  .method-wrap {
    position: relative;
  }
  .method-trigger {
    height: 38px;
    padding: 0 12px;
    min-width: 110px;
    border: 1px solid var(--line-strong);
    border-radius: 4px;
    background: var(--inset);
    color: var(--text);
    font: inherit;
    font-weight: 600;
    font-size: 12px;
    letter-spacing: 0.04em;
    cursor: pointer;
    display: inline-flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    transition: all 120ms;
  }
  .method-trigger:hover:not(:disabled) {
    background: var(--sage-tint-2);
    border-color: rgba(159, 184, 173, 0.5);
  }
  .method-trigger {
    background: var(--inset);
  }
  .method-trigger.m-get     { color: #2d4538; }
  .method-trigger.m-post    { color: var(--accent); }
  .method-trigger.m-put,
  .method-trigger.m-patch   { color: #5a4a26; }
  .method-trigger.m-delete  { color: #4a2020; }
  .method-trigger.m-head,
  .method-trigger.m-options { color: var(--muted); }
  .method-arrow {
    color: var(--muted);
    opacity: 0.55;
    display: inline-flex;
    align-items: center;
  }
  .method-menu {
    position: absolute;
    top: calc(100% + 4px);
    left: 0;
    z-index: 30;
    min-width: 134px;
    padding: 4px;
    background: #ffffff;
    border: 1px solid var(--line-strong);
    border-radius: 6px;
    box-shadow: 0 8px 28px rgba(72, 89, 65, 0.18);
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  .method-opt {
    appearance: none;
    background: transparent;
    border: none;
    padding: 7px 10px;
    border-radius: 3px;
    cursor: pointer;
    text-align: left;
    font: inherit;
    font-weight: 600;
    font-size: 12px;
    letter-spacing: 0.04em;
    color: var(--text);
    transition: background 120ms;
  }
  .method-opt:hover {
    background: rgba(72, 89, 65, 0.06);
  }
  .method-opt.active {
    background: var(--sage-tint);
    color: var(--accent);
  }
  /* URL input + overlay: single-line variant of the body's overlay
     pattern. .url-input-wrap holds both; .url-input-overlay sits behind
     a transparent-text <input> so spans line up under the caret. */
  .url-input-wrap {
    position: relative;
    flex: 1;
    display: flex;
  }
  .url-input,
  .url-input-overlay {
    width: 100%;
    height: 38px;
    padding: 0 12px;
    border: 1px solid var(--line-strong);
    border-radius: 4px;
    font: inherit;
    font-size: 13px;
    font-family: "Lexend", ui-monospace, "SF Mono", Menlo, monospace;
    font-weight: 400;
    line-height: 36px; /* 38 - 2*border */
    box-sizing: border-box;
  }
  .url-input {
    background: var(--inset);
    color: var(--text);
  }
  .url-input:focus {
    outline: none;
    border-color: var(--accent);
    box-shadow: 0 0 0 3px rgba(159, 184, 173, 0.15);
  }
  .url-input-overlay {
    position: absolute;
    inset: 0;
    margin: 0;
    background: var(--inset);
    color: var(--text);
    border-color: var(--line-strong);
    white-space: pre;
    overflow: hidden;
    pointer-events: none;
  }
  .url-input-wrap.highlighted .url-input {
    background: transparent;
    color: transparent;
    caret-color: var(--text);
    position: relative;
    z-index: 1;
  }
  .url-input-wrap.highlighted .url-input-overlay {
    /* Hide the overlay's own border so the underlying input's border
       (including its focus ring) is the single source of truth. Keep
       the overlay's --inset background so the input area looks the
       same as the non-highlighted state. */
    border-color: transparent;
  }

  /* Recognized template token — used by both URL and body overlays. Must
     stay metric-neutral or the overlay's glyphs drift right of the input's
     glyphs (selection/caret misalign with what the user sees). Safe props:
     background, color, border-radius (no width change), text-decoration,
     box-shadow. UNSAFE: padding, border-width, font-weight (bold widens
     glyphs in proportional fonts), font-size, letter-spacing, font-style. */
  :global(.lt-tok) {
    background: rgba(159, 184, 173, 0.38);
    color: var(--accent-strong);
    border-radius: 2px;
    box-shadow: inset 0 -1px 0 rgba(72, 89, 65, 0.35);
  }
  .tokens-hint {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 6px;
    margin-top: 6px;
    font-size: 11px;
    color: var(--muted);
  }
  .tokens-hint-label {
    text-transform: uppercase;
    letter-spacing: 0.04em;
    font-size: 10px;
    opacity: 0.75;
  }
  .tokens-hint code {
    padding: 1px 6px;
    background: var(--inset);
    border: 1px solid var(--line);
    border-radius: 3px;
    font-family: ui-monospace, "SF Mono", Menlo, monospace;
    font-size: 11px;
    color: var(--text);
    cursor: help;
  }
  .tokens-toggle {
    margin-left: auto;
    display: inline-flex;
    align-items: center;
    gap: 4px;
    padding: 2px 8px;
    background: transparent;
    border: 1px solid transparent;
    border-radius: 3px;
    font: inherit;
    font-size: 11px;
    color: var(--muted);
    cursor: pointer;
    transition: background 120ms, border-color 120ms, color 120ms;
  }
  .tokens-toggle :global(svg) {
    transition: transform 160ms;
  }
  .tokens-toggle.open :global(svg) {
    transform: rotate(180deg);
  }
  .tokens-toggle:hover {
    background: var(--inset);
    border-color: var(--line);
    color: var(--text);
  }

  .tokens-help {
    margin-top: 8px;
    padding: 12px 14px;
    background: var(--inset);
    border: 1px solid var(--line);
    border-radius: 4px;
    font-size: 12px;
    color: var(--text);
    line-height: 1.5;
  }
  .tokens-help-intro,
  .tokens-help-foot {
    margin: 0;
    color: var(--muted);
  }
  .tokens-help-foot {
    margin-top: 10px;
    font-size: 11px;
  }
  .tokens-help-list {
    display: grid;
    grid-template-columns: max-content 1fr;
    column-gap: 16px;
    row-gap: 10px;
    margin: 12px 0 0 0;
    align-items: start;
  }
  .tokens-help-list dt {
    margin: 0;
    padding-top: 1px;
  }
  .tokens-help-list dt code {
    display: inline-block;
    padding: 2px 6px;
    background: var(--bg, #fff);
    border: 1px solid var(--line);
    border-radius: 3px;
    font-family: ui-monospace, "SF Mono", Menlo, monospace;
    font-size: 11.5px;
    color: var(--text);
    white-space: nowrap;
  }
  .tokens-help-list dd {
    margin: 0;
    color: var(--text);
  }
  .tokens-help-list dd code {
    padding: 0 4px;
    background: rgba(0, 0, 0, 0.04);
    border-radius: 2px;
    font-family: ui-monospace, "SF Mono", Menlo, monospace;
    font-size: 11.5px;
  }
  .tokens-help-ex {
    display: block;
    margin-top: 3px;
    color: var(--muted);
    font-size: 11px;
  }
  input:disabled, select:disabled, textarea:disabled {
    opacity: 0.55;
  }

  .tabs {
    display: flex;
    gap: 4px;
    border-bottom: 1px solid var(--line);
    margin-top: 2px;
  }
  .tab {
    appearance: none;
    background: transparent;
    color: var(--muted);
    opacity: 0.7;
    border: none;
    padding: 9px 14px;
    font: inherit;
    font-size: 12px;
    font-weight: 500;
    cursor: pointer;
    border-bottom: 2px solid transparent;
    transition: all 140ms;
    display: inline-flex;
    align-items: center;
    gap: 6px;
    margin-bottom: -1px;
  }
  .tab:hover:not(.active) {
    color: var(--text);
    opacity: 1;
  }
  .tab.active {
    color: var(--text);
    opacity: 1;
    border-bottom-color: var(--accent);
  }
  .tab-badge {
    font-size: 10px;
    padding: 1px 6px;
    background: rgba(72, 89, 65, 0.10);
    color: var(--muted);
    border-radius: 8px;
    font-weight: 500;
    font-variant-numeric: tabular-nums;
  }
  .tab.active .tab-badge {
    background: var(--sage-tint);
    color: var(--accent);
  }

  .tab-content {
    min-height: 124px;
  }

  .headers {
    display: flex;
    flex-direction: column;
    gap: 5px;
  }
  .hrow {
    display: grid;
    grid-template-columns: 1fr 1.5fr 30px;
    gap: 5px;
  }
  .hrow input {
    height: 32px;
    padding: 0 10px;
    background: var(--inset);
    color: var(--text);
    border: 1px solid var(--line);
    border-radius: 4px;
    font: inherit;
    font-size: 12px;
    font-family: "Lexend", ui-monospace, monospace;
  }
  .hrow input:focus {
    outline: none;
    border-color: var(--accent);
    box-shadow: 0 0 0 2px rgba(159, 184, 173, 0.15);
  }
  /* Header-value overlay: same metric-neutral pattern as the URL input.
     The wrap fills the grid column; the overlay shares font/padding so
     glyph positions line up with the underlying input. */
  .h-value-wrap {
    position: relative;
    display: flex;
  }
  .h-value-wrap > input,
  .h-value-overlay {
    width: 100%;
    height: 32px;
    padding: 0 10px;
    border: 1px solid var(--line);
    border-radius: 4px;
    font: inherit;
    font-size: 12px;
    font-family: "Lexend", ui-monospace, monospace;
    line-height: 30px;
    box-sizing: border-box;
  }
  .h-value-overlay {
    position: absolute;
    inset: 0;
    margin: 0;
    background: var(--inset);
    color: var(--text);
    white-space: pre;
    overflow: hidden;
    pointer-events: none;
  }
  .h-value-wrap.highlighted > input {
    background: transparent;
    color: transparent;
    caret-color: var(--text);
    position: relative;
    z-index: 1;
  }
  .h-value-wrap.highlighted .h-value-overlay {
    border-color: transparent;
  }
  .hdel {
    appearance: none;
    background: transparent;
    border: 1px solid var(--line);
    color: var(--muted);
    opacity: 0.55;
    border-radius: 4px;
    cursor: pointer;
    transition: all 120ms;
    display: inline-flex;
    align-items: center;
    justify-content: center;
  }
  .hdel:hover:not(:disabled) {
    color: var(--err);
    opacity: 1;
    border-color: rgba(120, 50, 50, 0.5);
  }
  .add-header {
    align-self: flex-start;
    appearance: none;
    background: transparent;
    border: 1px dashed var(--line-strong);
    color: var(--muted);
    padding: 6px 12px 6px 10px;
    border-radius: 4px;
    font: inherit;
    font-size: 11px;
    font-weight: 500;
    cursor: pointer;
    margin-top: 2px;
    transition: all 120ms;
    display: inline-flex;
    align-items: center;
    gap: 5px;
  }
  .add-header:hover:not(:disabled) {
    background: var(--sage-tint-2);
    border-color: var(--accent);
    color: var(--accent);
  }

  /* Request body input — when reqBody parses as JSON, the wrap gets the
     `.highlighted` class and a <pre> overlay shows colored tokens behind
     the (now transparent) <textarea>. Both share box/font metrics so the
     spans line up precisely under the caret. */
  .body-input-wrap {
    position: relative;
    width: 100%;
  }
  .body-input,
  .body-input-overlay {
    width: 100%;
    min-height: 124px;
    padding: 12px;
    border: 1px solid transparent;
    border-radius: 4px;
    font-size: 12px;
    font-family: ui-monospace, "SF Mono", Menlo, monospace;
    line-height: 1.55;
    box-sizing: border-box;
    white-space: pre-wrap;
    word-break: break-word;
    overflow-wrap: anywhere;
    tab-size: 2;
  }
  .body-input {
    position: relative;
    background: var(--inset);
    color: var(--text);
    border-color: var(--line);
    resize: vertical;
    display: block;
  }
  .body-input:focus {
    outline: none;
    border-color: var(--accent);
    box-shadow: 0 0 0 2px rgba(159, 184, 173, 0.15);
  }
  .body-input-overlay {
    position: absolute;
    inset: 0;
    margin: 0;
    background: var(--inset);
    color: var(--text);
    pointer-events: none;
    overflow: hidden;
  }
  .body-input-wrap.highlighted .body-input {
    background: transparent;
    color: transparent;
    caret-color: var(--text);
    position: relative;
    z-index: 1;
  }
  .body-input-overlay :global(.jh-key)  { color: var(--accent-strong); font-weight: 500; }
  .body-input-overlay :global(.jh-str)  { color: #6b5526; }
  .body-input-overlay :global(.jh-num)  { color: var(--rate); }
  .body-input-overlay :global(.jh-bool) { color: var(--accent); font-weight: 600; }
  .body-input-overlay :global(.jh-null) { color: var(--muted-2); font-style: italic; }
  .body-format-btn {
    position: absolute;
    top: 6px;
    right: 8px;
    appearance: none;
    background: var(--inset);
    border: 1px solid var(--line-strong);
    color: var(--muted);
    font: inherit;
    font-size: 10px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.08em;
    padding: 3px 8px;
    border-radius: 4px;
    cursor: pointer;
    z-index: 2;
    transition: all 120ms;
  }
  .body-format-btn:hover:not(:disabled) {
    border-color: var(--accent);
    color: var(--accent-strong);
  }

  .divider {
    border: none;
    border-top: 1px solid var(--line);
    margin: 4px 0;
  }

  /* ─── Load profile ────────────────────────────────────────────── */
  .profile-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
  }
  .profile-title {
    margin: 0;
    font-size: 10px;
    text-transform: uppercase;
    letter-spacing: 0.12em;
    color: var(--muted);
    opacity: 0.7;
    font-weight: 500;
  }
  .mode-toggle {
    display: inline-flex;
    background: var(--inset);
    border: 1px solid var(--line);
    border-radius: 6px;
    padding: 2px;
  }
  .seg {
    appearance: none;
    background: transparent;
    color: var(--muted);
    border: none;
    padding: 6px 14px;
    font: inherit;
    font-size: 11px;
    font-weight: 500;
    letter-spacing: 0.02em;
    cursor: pointer;
    border-radius: 4px;
    transition: all 140ms;
  }
  .seg.active {
    background: var(--accent);
    color: #ffffff;
    font-weight: 600;
  }
  .seg:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }

  .profile-body {
    display: grid;
    grid-template-columns: 1fr 472px;
    gap: 16px;
    margin-top: 10px;
    align-items: start;
  }
  .profile-inputs {
    display: flex;
    flex-direction: column;
    gap: 10px;
  }

  /* Curve editor controls in the left sidebar */
  .curve-tools {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 8px 10px;
    background: var(--inset);
    border: 1px solid var(--line);
    border-radius: 4px;
  }
  /* Curve-only blocks stay mounted in Fixed mode so the panel doesn't
     jump in height when toggling Fixed/Curve. Dim them so they read as
     inactive; their own buttons/inputs already have disabled attrs. */
  .lp-muted {
    opacity: 0.55;
    filter: saturate(0.6);
  }
  .pe-placeholder {
    color: var(--muted);
    font-style: italic;
    font-weight: 400;
  }
  .ct-title {
    font-size: 11px;
    text-transform: uppercase;
    letter-spacing: 0.10em;
    color: var(--muted);
    font-weight: 600;
    flex: 1;
  }
  .ct-btn {
    appearance: none;
    background: transparent;
    border: 1px solid var(--line-strong);
    color: var(--text);
    padding: 3px 9px;
    border-radius: 3px;
    font: inherit;
    font-size: 11px;
    font-weight: 500;
    cursor: pointer;
  }
  .ct-btn:hover:not(:disabled) {
    background: var(--sage-tint-2);
    border-color: var(--accent);
    color: var(--accent);
  }
  .ct-btn:disabled {
    opacity: 0.4;
    cursor: not-allowed;
  }
  .ct-hint {
    margin: -2px 0 0 0;
    font-size: 11px;
    color: var(--muted);
    line-height: 1.5;
    font-style: italic;
  }

  .noise-row {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .noise-row .k {
    display: flex;
    justify-content: space-between;
    align-items: baseline;
  }
  .noise-val {
    font-size: 11px;
    color: var(--accent);
    font-weight: 600;
    letter-spacing: 0;
    text-transform: none;
    font-variant-numeric: tabular-nums;
  }
  .noise-slider, .pe-slider {
    -webkit-appearance: none;
    appearance: none;
    width: 100%;
    height: 4px;
    background: rgba(72, 89, 65, 0.18);
    border-radius: 2px;
    outline: none;
  }
  .noise-slider::-webkit-slider-thumb, .pe-slider::-webkit-slider-thumb {
    -webkit-appearance: none;
    appearance: none;
    width: 14px;
    height: 14px;
    border-radius: 50%;
    background: var(--accent);
    cursor: pointer;
    border: 2px solid var(--bg);
    box-shadow: 0 1px 2px rgba(0, 0, 0, 0.15);
  }
  .noise-slider:disabled, .pe-slider:disabled {
    opacity: 0.5;
  }

  .point-edit {
    background: var(--sage-tint-2);
    border: 1px solid rgba(72, 89, 65, 0.25);
    border-radius: 4px;
    padding: 10px;
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .pe-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
  }
  .pe-title {
    font-size: 12px;
    font-weight: 500;
    color: var(--accent);
    font-variant-numeric: tabular-nums;
  }
  .pe-del {
    appearance: none;
    background: transparent;
    border: 1px solid rgba(120, 50, 50, 0.30);
    color: var(--err);
    border-radius: 3px;
    padding: 4px 6px;
    cursor: pointer;
    display: inline-flex;
    align-items: center;
  }
  .pe-del:hover:not(:disabled) {
    background: rgba(120, 50, 50, 0.10);
  }
  .pe-row {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .pe-label {
    font-size: 11px;
    color: var(--muted);
    min-width: 64px;
  }
  .pe-segs {
    display: inline-flex;
    background: var(--inset);
    border: 1px solid var(--line);
    border-radius: 4px;
    padding: 2px;
    flex: 1;
  }
  .pe-seg {
    flex: 1;
    appearance: none;
    background: transparent;
    border: none;
    padding: 4px 6px;
    font: inherit;
    font-size: 11px;
    font-weight: 500;
    color: var(--muted);
    border-radius: 3px;
    cursor: pointer;
  }
  .pe-seg.active {
    background: var(--accent);
    color: #ffffff;
  }
  .pe-val {
    font-size: 11px;
    font-variant-numeric: tabular-nums;
    color: var(--text);
    min-width: 28px;
    text-align: right;
  }

  .curve-canvas.editable {
    cursor: crosshair;
  }
  :global(.curve-canvas .lc-handle .lc-handle-hit) {
    fill: transparent;
    cursor: grab;
  }
  :global(.curve-canvas .lc-handle.dragging .lc-handle-hit) {
    cursor: grabbing;
  }
  :global(.curve-canvas .lc-handle .lc-handle-dot) {
    fill: var(--bg);
    stroke: var(--accent);
    stroke-width: 2;
    transition: r 120ms, stroke-width 120ms;
  }
  :global(.curve-canvas .lc-handle:hover .lc-handle-dot),
  :global(.curve-canvas .lc-handle.dragging .lc-handle-dot) {
    stroke-width: 3;
  }
  :global(.curve-canvas .lc-handle.selected .lc-handle-dot) {
    fill: var(--accent);
    stroke: var(--accent-strong);
  }

  .profile-preview {
    background: var(--inset);
    border: 1px solid var(--line);
    border-radius: 6px;
    padding: 12px;
    display: flex;
    flex-direction: column;
    gap: 10px;
  }
  .preview {
    width: 100%;
    height: auto;
    display: block;
  }
  .preview-caption {
    border-top: 1px solid var(--line);
    padding-top: 10px;
  }
  .preview-caption-title {
    display: block;
    font-size: 10px;
    text-transform: uppercase;
    letter-spacing: 0.12em;
    color: var(--muted);
    opacity: 0.7;
    font-weight: 600;
    margin-bottom: 4px;
  }
  .preview-caption p {
    margin: 0;
    font-size: 12px;
    color: var(--text);
    line-height: 1.5;
    font-weight: 400;
  }
  :global(.preview .pv-label) {
    font: 10px "Lexend", ui-monospace, monospace;
    font-variant-numeric: tabular-nums;
    fill: var(--muted);
  }
  :global(.preview .pv-label-strong) {
    font: 10px "Lexend", ui-monospace, monospace;
    font-weight: 600;
    font-variant-numeric: tabular-nums;
    fill: var(--text);
  }

  /* Legacy class kept in case anything still uses it. */
  .profile-grid {
    display: grid;
    grid-template-columns: repeat(3, 1fr);
    gap: 10px;
    margin-top: 8px;
  }
  .field {
    display: flex;
    flex-direction: column;
    gap: 5px;
  }
  .field .k {
    font-size: 10px;
    text-transform: uppercase;
    letter-spacing: 0.12em;
    color: var(--muted);
    opacity: 0.7;
    font-weight: 500;
    display: flex;
    justify-content: space-between;
    align-items: baseline;
  }
  .k-hint {
    font-size: 9px;
    text-transform: none;
    letter-spacing: 0;
    font-weight: 400;
    opacity: 0.6;
    font-variant-numeric: tabular-nums;
  }
  .field input {
    height: 34px;
    padding: 0 12px;
    background: var(--inset);
    color: var(--text);
    border: 1px solid var(--line);
    border-radius: 4px;
    font: inherit;
    font-size: 13px;
    font-variant-numeric: tabular-nums;
  }
  .field input:focus {
    outline: none;
    border-color: var(--accent);
    box-shadow: 0 0 0 2px rgba(159, 184, 173, 0.15);
  }
  .field-with-arrow {
    position: relative;
  }
  .field-with-arrow::before {
    content: '→';
    position: absolute;
    left: -10px;
    top: 70%;
    transform: translateY(-50%);
    color: var(--muted);
    opacity: 0.4;
    font-size: 14px;
    pointer-events: none;
  }

  .controls {
    display: flex;
    gap: 6px;
    margin-top: 4px;
  }
  .btn {
    height: 36px;
    padding: 0 16px;
    border: 1px solid var(--line-strong);
    background: var(--surface-2);
    color: var(--text);
    border-radius: 4px;
    cursor: pointer;
    font: inherit;
    font-size: 13px;
    font-weight: 500;
    transition: all 140ms;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: 7px;
  }
  .btn:hover:not(:disabled) {
    background: rgba(72, 89, 65, 0.08);
    border-color: rgba(72, 89, 65, 0.30);
  }
  .btn:disabled {
    opacity: 0.35;
    cursor: not-allowed;
  }
  .btn-ghost {
    padding: 0 12px;
    font-size: 12px;
    background: #fff;
  }
  .btn-primary {
    background: var(--accent);
    border-color: var(--accent);
    color: #ffffff;
    font-weight: 600;
  }
  .btn-primary:hover:not(:disabled) {
    background: var(--accent-strong);
    border-color: var(--accent-strong);
  }
  .btn-spin {
    display: inline-flex;
    animation: lc-spin 0.9s linear infinite;
  }
  @keyframes lc-spin {
    to { transform: rotate(360deg); }
  }

  /* ─── Sample response panel ─────────────────────────────────────── */
  .section-sep {
    position: relative;
    display: flex;
    align-items: center;
    justify-content: center;
    margin: 18px 0 10px;
    height: 14px;
  }
  .section-sep::before {
    content: '';
    position: absolute;
    left: 0;
    right: 0;
    top: 50%;
    height: 1px;
    background: var(--line);
  }
  .section-sep-label {
    position: relative;
    z-index: 1;
    background: var(--surface);
    color: var(--muted);
    font-size: 10px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.16em;
    padding: 0 10px;
  }
  .response {
    display: flex;
    flex-direction: column;
    gap: 10px;
    background: var(--inset);
    border: 1px solid var(--line);
    border-radius: 8px;
    padding: 12px;
    min-height: 0;
  }
  .resp-head {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
    min-width: 0;
  }
  .resp-label {
    font-size: 10px;
    text-transform: uppercase;
    letter-spacing: 0.12em;
    color: var(--muted);
    font-weight: 500;
    margin-right: 4px;
  }
  .resp-status {
    display: inline-block;
    font-size: 11px;
    font-weight: 700;
    letter-spacing: 0.05em;
    padding: 3px 8px;
    border-radius: 4px;
    font-variant-numeric: tabular-nums;
  }
  .s-2xx { background: rgba(72, 89, 65, 0.15); color: var(--accent-strong); }
  .s-3xx { background: rgba(159, 184, 173, 0.30); color: var(--accent-strong); }
  .s-4xx { background: rgba(138, 114, 54, 0.18); color: #5a4a26; }
  .s-429 { background: rgba(138, 94, 54, 0.20); color: #5a3a1f; }
  .s-5xx { background: rgba(120, 50, 50, 0.18); color: #4a2020; }
  .s-err { background: rgba(120, 50, 50, 0.18); color: var(--err); }
  .resp-text {
    color: var(--text);
    font-size: 12px;
    font-weight: 500;
  }
  .resp-sep { color: var(--muted-2); opacity: 0.5; }
  .resp-meta {
    color: var(--muted);
    font-size: 12px;
    font-variant-numeric: tabular-nums;
  }
  .resp-truncated {
    color: var(--rate);
    font-size: 10px;
    text-transform: uppercase;
    letter-spacing: 0.08em;
    font-weight: 600;
  }
  .resp-msg {
    color: var(--err);
    font-size: 12px;
    font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
    overflow-wrap: anywhere;
  }
  .resp-tabs {
    display: flex;
    align-items: center;
    gap: 2px;
    border-bottom: 1px solid var(--line);
  }
  .resp-tab-actions {
    margin-left: auto;
    display: inline-flex;
    gap: 4px;
    padding-bottom: 4px;
  }
  .copy-btn {
    appearance: none;
    background: transparent;
    border: 1px solid var(--line);
    color: var(--muted);
    font: inherit;
    font-size: 10px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.06em;
    padding: 3px 8px;
    border-radius: 4px;
    cursor: pointer;
    display: inline-flex;
    align-items: center;
    gap: 4px;
    transition: all 120ms;
  }
  .copy-btn:hover {
    border-color: var(--accent);
    color: var(--accent-strong);
    background: rgba(72, 89, 65, 0.05);
  }
  .copy-btn.copied {
    border-color: var(--accent);
    color: var(--accent-strong);
    background: var(--sage-tint);
  }

  /* Loading skeleton — used while sending is true and no response yet. */
  .skel {
    display: inline-block;
    border-radius: 4px;
    background: linear-gradient(
      90deg,
      rgba(72, 89, 65, 0.07) 25%,
      rgba(72, 89, 65, 0.13) 50%,
      rgba(72, 89, 65, 0.07) 75%
    );
    background-size: 200% 100%;
    animation: lc-shimmer 1.2s ease-in-out infinite;
  }
  .skel-chip { width: 48px; height: 18px; }
  .skel-text { height: 12px; }
  .skel-line { display: block; height: 12px; margin: 6px 0; }
  .resp-sending {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    margin-left: auto;
  }
  .resp-sending :global(svg) {
    animation: lc-spin 0.9s linear infinite;
  }
  .resp-skel-body {
    background: var(--surface-2);
    border: 1px solid var(--line);
    border-radius: 4px;
    padding: 12px;
  }
  @keyframes lc-shimmer {
    0%   { background-position: 100% 0; }
    100% { background-position: -100% 0; }
  }
  .resp-tab {
    appearance: none;
    background: transparent;
    border: none;
    color: var(--muted);
    font: inherit;
    font-size: 12px;
    font-weight: 500;
    padding: 6px 10px;
    cursor: pointer;
    border-bottom: 2px solid transparent;
    margin-bottom: -1px;
    display: inline-flex;
    align-items: center;
    gap: 6px;
  }
  .resp-tab:hover:not(.active) { color: var(--text); }
  .resp-tab.active {
    color: var(--accent-strong);
    border-bottom-color: var(--accent);
  }
  .resp-tab-badge {
    background: var(--surface-2);
    color: var(--muted);
    font-size: 10px;
    padding: 1px 6px;
    border-radius: 999px;
    font-variant-numeric: tabular-nums;
  }
  .resp-content {
    max-height: 360px;
    overflow: auto;
    background: var(--surface-2);
    border: 1px solid var(--line);
    border-radius: 4px;
  }
  .resp-body {
    margin: 0;
    padding: 10px 12px;
    font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
    font-size: 12px;
    line-height: 1.5;
    color: var(--text);
    white-space: pre-wrap;
    word-break: break-word;
  }
  /* JSON tokens — palette tuned to the rest of the app:
     keys lean into the brand forest, strings warm ochre, numbers muted rust,
     bools sage, null low-key gray. */
  .resp-body.json :global(.jh-key)  { color: var(--accent-strong); font-weight: 500; }
  .resp-body.json :global(.jh-str)  { color: #6b5526; }
  .resp-body.json :global(.jh-num)  { color: var(--rate); }
  .resp-body.json :global(.jh-bool) { color: var(--accent); font-weight: 600; }
  .resp-body.json :global(.jh-null) { color: var(--muted-2); font-style: italic; }
  .resp-headers {
    display: flex;
    flex-direction: column;
    padding: 4px 0;
  }
  .rh-row {
    display: grid;
    grid-template-columns: minmax(140px, 220px) 1fr auto;
    align-items: center;
    gap: 12px;
    padding: 5px 12px;
    font-size: 12px;
    font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  }
  .rh-row:hover { background: rgba(72, 89, 65, 0.04); }
  .rh-k {
    color: var(--muted);
    overflow-wrap: anywhere;
  }
  .rh-v {
    color: var(--text);
    overflow-wrap: anywhere;
  }
  .rh-copy {
    appearance: none;
    background: transparent;
    border: none;
    color: var(--muted-2);
    cursor: pointer;
    padding: 4px 6px;
    border-radius: 3px;
    opacity: 0;
    transition: opacity 120ms, color 120ms, background 120ms;
    display: inline-flex;
    align-items: center;
  }
  .rh-row:hover .rh-copy { opacity: 1; }
  .rh-copy:hover { color: var(--accent-strong); background: rgba(72, 89, 65, 0.06); }
  .rh-copy.copied {
    opacity: 1;
    color: var(--accent-strong);
    background: var(--sage-tint);
  }

  .error {
    margin: 8px 0 0;
    padding: 10px 14px;
    background: rgba(120, 50, 50, 0.10);
    border: 1px solid rgba(120, 50, 50, 0.40);
    border-radius: 4px;
    color: var(--err);
    font-size: 12px;
  }

  /* ─── Metrics (now inside .results-pane) ─────────────────────── */

  /* Run tabs — Live + each saved snapshot */
  .run-tabs {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
  }
  .run-tab {
    display: inline-flex;
    align-items: stretch;
    background: var(--surface-2);
    border: 1px solid var(--line);
    border-radius: 999px;
    overflow: hidden;
    transition: all 140ms;
  }
  .run-tab:hover:not(.active) {
    background: rgba(72, 89, 65, 0.08);
    border-color: var(--line-strong);
  }
  .run-tab.active {
    background: var(--sage-tint);
    border-color: var(--accent);
  }
  .run-tab-main {
    appearance: none;
    background: transparent;
    border: none;
    color: var(--muted);
    font: inherit;
    font-size: 11px;
    font-weight: 500;
    cursor: pointer;
    padding: 5px 11px 5px 9px;
    display: inline-flex;
    align-items: center;
    gap: 7px;
    transition: color 140ms;
  }
  .run-tab.active .run-tab-main { color: var(--accent-strong); }
  .run-tab-method {
    font-size: 9px;
    padding: 2px 5px;
    min-width: 36px;
  }
  .run-tab-label {
    font-weight: 500;
    color: inherit;
    max-width: 140px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .run-tab-time {
    color: var(--muted);
    opacity: 0.65;
    font-variant-numeric: tabular-nums;
  }
  .run-tab.active .run-tab-time { opacity: 0.85; }
  .run-tab-x {
    appearance: none;
    background: transparent;
    border: none;
    color: var(--muted);
    padding: 0 8px;
    cursor: pointer;
    opacity: 0;
    display: inline-flex;
    align-items: center;
    border-left: 1px solid transparent;
    transition: all 120ms;
  }
  .run-tab:hover .run-tab-x { opacity: 0.55; }
  .run-tab-x:hover {
    opacity: 1 !important;
    color: var(--err);
    background: rgba(120, 50, 50, 0.12);
    border-left-color: var(--line);
  }
  /* Live tab is a single button — no inner main/x split */
  .run-tab-live {
    appearance: none;
    border: 1px solid var(--line);
    background: var(--surface-2);
    color: var(--muted);
    font: inherit;
    font-size: 11px;
    font-weight: 500;
    padding: 5px 13px;
    border-radius: 999px;
    cursor: pointer;
    display: inline-flex;
    align-items: center;
    gap: 7px;
    transition: all 140ms;
  }
  .run-tab-live:hover:not(.active) {
    background: rgba(72, 89, 65, 0.08);
    border-color: var(--line-strong);
  }
  .run-tab-live.active {
    background: var(--accent);
    color: #ffffff;
    border-color: var(--accent);
  }
  .run-tab-dot {
    width: 7px;
    height: 7px;
    border-radius: 50%;
    background: var(--sage);
  }
  .run-tab-dot.live {
    background: var(--sage);
    box-shadow: 0 0 0 0 rgba(159, 184, 173, 0.55);
    animation: pulse 1.6s ease-out infinite;
  }
  .run-tab-live.active .run-tab-dot.live { background: #ffffff; }

  /* Header — POST · url · done · curve · peak 500 · workers now 0 */
  .run-header {
    display: flex;
    align-items: center;
    gap: 10px;
    flex-wrap: wrap;
    font-size: 12px;
    color: var(--muted);
    font-weight: 400;
  }
  .run-header .status-method {
    margin: 0;
  }
  /* Header-sized FLOW badge — scales up from the sidebar variant so it
     sits proportionally next to the method-chip-replaced .hr-url. */
  .hr-flow-badge {
    padding: 4px 10px;
    font-size: 10.5px;
  }
  .hr-url {
    color: var(--text);
    font-family: "Lexend", ui-monospace, monospace;
    font-weight: 400;
    max-width: 460px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .hr-sep {
    opacity: 0.32;
    user-select: none;
  }
  .hr-state {
    color: var(--muted);
    font-weight: 500;
  }
  .hr-state.running {
    color: var(--accent);
    position: relative;
    padding-left: 12px;
  }
  .hr-state.running::before {
    content: '';
    position: absolute;
    left: 0; top: 50%;
    transform: translateY(-50%);
    width: 6px; height: 6px;
    border-radius: 50%;
    background: var(--sage);
    box-shadow: 0 0 0 0 rgba(159, 184, 173, 0.5);
    animation: pulse 1.6s ease-out infinite;
  }
  .hr-mode, .hr-peak, .hr-now {
    color: var(--muted);
  }
  .hr-now strong {
    color: var(--text);
    font-weight: 600;
    font-variant-numeric: tabular-nums;
  }

  /* Card — Success | Stats | Sparkline */
  .run-card {
    display: grid;
    grid-template-columns: minmax(220px, 1.05fr) minmax(380px, 2fr) minmax(180px, 1fr);
    gap: 22px;
    padding: 18px 22px 20px;
    background: var(--surface);
    border: 1px solid var(--line);
    border-radius: 8px;
    align-items: stretch;
  }
  .rc-label {
    font-size: 10px;
    text-transform: uppercase;
    letter-spacing: 0.12em;
    color: var(--muted);
    font-weight: 600;
    opacity: 0.75;
    margin-bottom: 8px;
  }

  /* Left — Success hero with stacked breakdown */
  .rc-success {
    display: flex;
    flex-direction: column;
    padding-right: 18px;
    border-right: 1px solid var(--line);
  }
  .rc-success-num {
    font-size: 44px;
    font-weight: 600;
    letter-spacing: -0.02em;
    color: var(--text);
    line-height: 1.05;
    display: inline-flex;
    align-items: baseline;
    gap: 0;
    margin-bottom: 16px;
    font-variant-numeric: tabular-nums;
  }
  .rc-success-pct {
    font-size: 18px;
    font-weight: 500;
    color: var(--muted);
    margin-left: 2px;
  }
  .rc-bar {
    display: flex;
    height: 6px;
    border-radius: 3px;
    overflow: hidden;
    background: rgba(72, 89, 65, 0.08);
    margin-bottom: 10px;
  }
  .rc-bar-seg {
    height: 100%;
    transition: width 280ms ease;
  }
  .rc-bar-ok   { background: var(--accent); }
  .rc-bar-warn { background: var(--warn); }
  .rc-bar-rate { background: var(--rate); }
  .rc-bar-err  { background: var(--err); }
  .rc-bar-net  { background: var(--neterr); }
  .rc-bar-legend {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 4px 12px;
    font-size: 11px;
    color: var(--muted);
    font-variant-numeric: tabular-nums;
  }
  .rc-bl {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    font-weight: 500;
  }
  .rc-bl-sw {
    width: 8px;
    height: 8px;
    border-radius: 2px;
    flex-shrink: 0;
  }
  .rc-bl-ok   { background: var(--accent); }
  .rc-bl-warn { background: var(--warn); }
  .rc-bl-rate { background: var(--rate); }
  .rc-bl-err  { background: var(--err); }
  .rc-bl-net  { background: var(--neterr); }

  /* Middle — stat grid */
  .rc-stats {
    display: grid;
    grid-template-columns: repeat(4, minmax(0, 1fr));
    gap: 14px 22px;
    padding: 0 6px;
    align-content: start;
  }
  .rc-cell {
    display: flex;
    flex-direction: column;
    gap: 4px;
    min-width: 0;
  }
  .rc-k {
    font-size: 10px;
    text-transform: uppercase;
    letter-spacing: 0.12em;
    color: var(--muted);
    opacity: 0.75;
    font-weight: 600;
  }
  .rc-v {
    font-size: 22px;
    font-weight: 500;
    font-variant-numeric: tabular-nums;
    letter-spacing: -0.01em;
    color: var(--text);
    display: inline-flex;
    align-items: baseline;
    gap: 0;
  }
  .rc-u {
    font-size: 11px;
    font-weight: 400;
    color: var(--muted);
    opacity: 0.7;
    margin-left: 2px;
  }

  /* Right — sparkline */
  .rc-spark {
    display: flex;
    flex-direction: column;
    padding-left: 18px;
    border-left: 1px solid var(--line);
  }
  .rc-spark-head {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    margin-bottom: 8px;
  }
  .rc-spark-head .rc-label { margin-bottom: 0; }
  .rc-spark-range {
    font-size: 10px;
    color: var(--muted);
    font-variant-numeric: tabular-nums;
    opacity: 0.7;
  }
  .spark {
    width: 100%;
    height: 64px;
    flex: 1;
    display: block;
  }

  .status-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: 14px;
    gap: 12px;
  }
  .status {
    display: flex;
    align-items: center;
    gap: 10px;
    font-size: 13px;
    font-weight: 500;
    color: var(--text);
    min-width: 0;
  }
  .status-method {
    margin-left: 4px;
  }
  .status-url {
    font-family: "Lexend", ui-monospace, monospace;
    font-size: 12px;
    font-weight: 400;
    color: var(--muted);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    max-width: 480px;
  }
  .status-meta {
    font-size: 12px;
    color: var(--muted);
    font-variant-numeric: tabular-nums;
    white-space: nowrap;
    font-weight: 400;
  }
  .status-meta strong {
    color: var(--text);
    font-weight: 600;
  }
  .dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: rgba(72, 89, 65, 0.25);
  }
  .dot.running {
    background: var(--sage);
    box-shadow: 0 0 0 0 rgba(72, 89, 65, 0.5);
    animation: pulse 1.6s ease-out infinite;
  }
  @keyframes pulse {
    0%   { box-shadow: 0 0 0 0 rgba(72, 89, 65, 0.45); }
    70%  { box-shadow: 0 0 0 10px rgba(72, 89, 65, 0); }
    100% { box-shadow: 0 0 0 0 rgba(72, 89, 65, 0); }
  }

  .breakdown {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
    margin-bottom: 12px;
  }
  .chip {
    display: inline-flex;
    align-items: baseline;
    gap: 7px;
    padding: 6px 11px;
    border-radius: 4px;
    border: 1px solid;
    font-size: 12px;
    font-variant-numeric: tabular-nums;
    background: var(--surface-2);
  }
  .chip-k {
    font-size: 10px;
    text-transform: uppercase;
    letter-spacing: 0.08em;
    opacity: 0.85;
    font-weight: 600;
  }
  .chip-v {
    font-weight: 600;
    color: var(--text);
    font-variant-numeric: tabular-nums;
  }
  .chip-p {
    font-size: 10px;
    opacity: 0.65;
    font-variant-numeric: tabular-nums;
    font-weight: 400;
  }
  .chip-ok   { border-color: rgba(72, 89, 65, 0.45);   color: var(--accent);  background: var(--sage-tint-2); }
  .chip-warn { border-color: rgba(138, 114, 54, 0.55); color: var(--warn);    background: rgba(138, 114, 54, 0.08); }
  .chip-rate { border-color: rgba(138, 94, 54, 0.55);  color: var(--rate);    background: rgba(138, 94, 54, 0.08); }
  .chip-err  { border-color: rgba(120, 50, 50, 0.55);  color: var(--err);     background: rgba(120, 50, 50, 0.07); }
  .chip-net  { border-color: rgba(90, 36, 36, 0.50);  color: var(--neterr);  background: rgba(90, 36, 36, 0.06); }
  .chip-ok .chip-v,
  .chip-warn .chip-v,
  .chip-rate .chip-v,
  .chip-err .chip-v,
  .chip-net .chip-v {
    color: inherit;
  }

  .grid {
    display: grid;
    grid-template-columns: repeat(4, 1fr);
    gap: 8px;
  }
  .cell {
    display: flex;
    flex-direction: column;
    gap: 5px;
    padding: 14px 16px;
    background: var(--surface-2);
    border: 1px solid var(--line);
    border-radius: 6px;
  }
  .cell .k {
    font-size: 10px;
    text-transform: uppercase;
    letter-spacing: 0.12em;
    color: var(--muted);
    opacity: 0.7;
    font-weight: 500;
  }
  .cell .v {
    font-size: 24px;
    font-weight: 500;
    font-variant-numeric: tabular-nums;
    letter-spacing: -0.01em;
    color: var(--text);
    display: inline-flex;
    align-items: baseline;
    gap: 1px;
  }

  /* Inherit font + color into the <number-flow> custom element so animated
   * digits look like the surrounding text. */
  :global(number-flow) {
    font: inherit;
    color: inherit;
    --number-flow-mask-height: 0.18em;
    --number-flow-char-height: 1em;
  }
  .cell .u {
    font-size: 12px;
    margin-left: 4px;
    color: var(--muted);
    opacity: 0.55;
    font-weight: 400;
  }
  .cell-success {
    background: linear-gradient(180deg, rgba(159, 184, 173, 0.28), rgba(159, 184, 173, 0.10));
    border-color: rgba(72, 89, 65, 0.30);
  }
  .cell-success .v {
    color: var(--accent);
  }

  .chart-wrap {
    position: relative;
    padding: 14px 14px 10px;
    background: var(--surface-2);
    border: 1px solid var(--line);
    border-radius: 6px;
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
  }
  .chart-legend {
    display: flex;
    flex-wrap: wrap;
    gap: 14px;
    margin-bottom: 8px;
    font-size: 11px;
    color: var(--muted);
    font-weight: 500;
  }
  .lg {
    display: inline-flex;
    align-items: center;
    gap: 5px;
  }
  .sw {
    width: 10px;
    height: 10px;
    border-radius: 2px;
    display: inline-block;
  }
  .sw-ok   { background: #a8c2b6; border: 1px solid rgba(72, 89, 65, 0.55); }
  .sw-fail { background: #bda998; border: 1px solid rgba(120, 50, 50, 0.40); }
  .sw-p50  { background: var(--accent); }
  .sw-p95  { background: var(--warn); }
  .sw-p99  { background: var(--rate); }

  .chart {
    width: 100%;
    height: 100%;
    flex: 1;
    min-height: 0;
    display: block;
  }

  /* D3 chart geometry */
  :global(.chart .lc-bars-ok rect) {
    fill: #a8c2b6;                /* soft sage pastel */
    stroke: rgba(72, 89, 65, 0.55);
    stroke-width: 0.5;
  }
  :global(.chart .lc-bars-fail rect) {
    fill: #bda998;                /* soft dusty rose pastel */
    stroke: rgba(120, 50, 50, 0.40);
    stroke-width: 0.5;
  }
  :global(.chart .lc-gridline) {
    stroke: rgba(72, 89, 65, 0.08);
    stroke-dasharray: 2 4;
  }
  :global(.chart .lc-line-p50) { stroke: var(--accent); }
  :global(.chart .lc-line-p95) { stroke: var(--warn);   }
  :global(.chart .lc-line-p99) { stroke: var(--rate);   }

  /* Hover guide + per-series dots */
  :global(.chart .lc-hover-pad) {
    cursor: crosshair;
  }
  :global(.chart .lc-hover-guide) {
    stroke: var(--accent);
    stroke-width: 1;
    stroke-dasharray: 3 3;
    opacity: 0.55;
  }
  :global(.chart .lc-hover-dot) {
    stroke: #ffffff;
    stroke-width: 1.5;
  }
  :global(.chart .lc-hd-ok)   { fill: #a8c2b6; }
  :global(.chart .lc-hd-fail) { fill: #bda998; }
  :global(.chart .lc-hd-p50)  { fill: var(--accent); }
  :global(.chart .lc-hd-p95)  { fill: var(--warn);   }
  :global(.chart .lc-hd-p99)  { fill: var(--rate);   }

  /* HTML tooltip overlay */
  .chart-tooltip {
    position: absolute;
    top: 34px;
    transform: translateX(10px);
    z-index: 5;
    min-width: 168px;
    padding: 9px 11px 10px;
    background: #ffffff;
    border: 1px solid var(--line-strong);
    border-radius: 6px;
    box-shadow: 0 6px 20px rgba(72, 89, 65, 0.16);
    font-size: 11px;
    color: var(--text);
    pointer-events: none;
    font-variant-numeric: tabular-nums;
  }
  .chart-tooltip.flip {
    transform: translateX(calc(-100% - 10px));
  }
  .tt-time {
    font-size: 10px;
    font-weight: 600;
    color: var(--muted);
    letter-spacing: 0.08em;
    text-transform: uppercase;
    margin-bottom: 6px;
  }
  .tt-row {
    display: grid;
    grid-template-columns: 10px 44px 1fr 42px;
    align-items: baseline;
    gap: 6px;
    line-height: 1.5;
  }
  .tt-sw {
    width: 8px;
    height: 8px;
    border-radius: 2px;
    align-self: center;
  }
  .tt-sw.sw-ok   { background: #a8c2b6; }
  .tt-sw.sw-fail { background: #bda998; }
  .tt-sw.sw-p50  { background: var(--accent); }
  .tt-sw.sw-p95  { background: var(--warn);   }
  .tt-sw.sw-p99  { background: var(--rate);   }
  .tt-k {
    color: var(--muted);
    font-size: 10px;
    font-weight: 500;
    letter-spacing: 0.02em;
  }
  .tt-v {
    font-weight: 600;
    text-align: right;
    color: var(--text);
  }
  .tt-u {
    font-weight: 400;
    color: var(--muted);
    margin-left: 2px;
    font-size: 10px;
  }
  .tt-p {
    color: var(--muted);
    font-size: 10px;
    text-align: right;
    min-width: 38px;
  }
  .tt-divider {
    height: 1px;
    background: var(--line);
    margin: 6px 0;
  }

  /* D3 axes: text + tick lines + axis domain */
  :global(.chart .lc-axis-x text),
  :global(.chart .lc-axis-y-rps text),
  :global(.chart .lc-axis-y-lat text) {
    fill: var(--muted);
    font-family: "Lexend", ui-monospace, monospace;
    font-size: 10px;
    font-weight: 500;
    font-variant-numeric: tabular-nums;
  }
  :global(.chart .lc-axis-x .tick line),
  :global(.chart .lc-axis-y-rps .tick line),
  :global(.chart .lc-axis-y-lat .tick line) {
    stroke: rgba(72, 89, 65, 0.25);
  }
  :global(.chart .lc-axis-x .domain),
  :global(.chart .lc-axis-y-rps .domain),
  :global(.chart .lc-axis-y-lat .domain) {
    stroke: rgba(72, 89, 65, 0.30);
  }
  :global(.chart .lc-axis-title) {
    fill: var(--muted);
    font-family: "Lexend", ui-monospace, monospace;
    font-size: 10px;
    font-weight: 600;
    letter-spacing: 0.06em;
  }

  /* ─── Info sheet (logo-click modal) ─────────────────────────────── */
  .info-backdrop {
    position: fixed;
    inset: 0;
    z-index: 1000;
    background: rgba(31, 42, 29, 0.42);
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 32px;
    animation: lc-fade-in 140ms ease-out;
  }
  .info-sheet {
    position: relative;
    width: min(440px, 100%);
    background: var(--bg);
    border: 1px solid var(--line);
    border-radius: 12px;
    padding: 28px 28px 24px;
    box-shadow: 0 24px 64px rgba(31, 42, 29, 0.18);
    text-align: center;
    animation: lc-pop-in 180ms cubic-bezier(0.2, 0.7, 0.2, 1);
  }
  .info-close {
    position: absolute;
    top: 12px;
    right: 12px;
    appearance: none;
    background: transparent;
    border: 1px solid transparent;
    color: var(--muted);
    cursor: pointer;
    padding: 6px;
    border-radius: 4px;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    transition: all 120ms;
  }
  .info-close:hover {
    color: var(--text);
    border-color: var(--line);
    background: var(--surface);
  }
  .info-mark {
    width: 72px;
    height: 72px;
    margin: 0 auto 14px;
    border-radius: 16px;
    background: var(--inset);
    border: 1px solid var(--line);
    display: inline-flex;
    align-items: center;
    justify-content: center;
  }
  .info-mark img {
    width: 46px;
    height: 46px;
    object-fit: contain;
  }
  .info-title {
    margin: 0 0 4px;
    font-size: 20px;
    font-weight: 600;
    letter-spacing: -0.01em;
    color: var(--text);
  }
  .info-subtitle {
    margin: 0 0 14px;
    color: var(--muted);
    font-size: 13px;
  }
  .info-body {
    margin: 0 0 20px;
    color: var(--text);
    font-size: 13px;
    line-height: 1.6;
    text-align: left;
    padding: 12px 14px;
    background: var(--surface);
    border: 1px solid var(--line);
    border-radius: 8px;
  }
  .info-setting {
    margin: 0 0 18px;
    padding: 12px 14px;
    background: var(--surface);
    border: 1px solid var(--line);
    border-radius: 8px;
    text-align: left;
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .info-setting-label {
    display: flex;
    align-items: baseline;
    gap: 8px;
    font-size: 12px;
    font-weight: 500;
    color: var(--text);
  }
  .info-setting-hint {
    color: var(--muted);
    font-size: 10px;
    font-weight: 500;
    text-transform: uppercase;
    letter-spacing: 0.08em;
  }
  .info-setting-input {
    appearance: none;
    width: 100%;
    padding: 8px 10px;
    background: var(--inset);
    color: var(--text);
    border: 1px solid var(--line);
    border-radius: 4px;
    font: inherit;
    font-size: 13px;
    font-variant-numeric: tabular-nums;
    box-sizing: border-box;
  }
  .info-setting-input:focus {
    outline: none;
    border-color: var(--accent);
    box-shadow: 0 0 0 2px rgba(159, 184, 173, 0.15);
  }
  .info-warn {
    margin: 0;
    padding: 8px 10px;
    background: rgba(138, 94, 54, 0.10);
    border: 1px solid rgba(138, 94, 54, 0.40);
    border-radius: 4px;
    color: #5a3a1f;
    font-size: 11px;
    line-height: 1.5;
  }
  .info-warn strong {
    color: var(--err);
    font-weight: 700;
  }
  .info-link {
    appearance: none;
    background: var(--accent);
    color: #ffffff;
    border: 1px solid var(--accent);
    font: inherit;
    font-size: 12px;
    font-weight: 600;
    padding: 9px 18px;
    border-radius: 6px;
    cursor: pointer;
    transition: background 120ms, border-color 120ms;
  }
  .info-link:hover {
    background: var(--accent-strong);
    border-color: var(--accent-strong);
  }
  .info-sponsor {
    appearance: none;
    background: transparent;
    color: var(--text);
    border: 1px solid var(--line-strong);
    font: inherit;
    font-size: 12px;
    font-weight: 500;
    padding: 8px 16px;
    border-radius: 6px;
    cursor: pointer;
    margin-top: 8px;
    display: inline-flex;
    align-items: center;
    gap: 6px;
    transition: border-color 120ms, color 120ms, background 120ms;
  }
  .info-sponsor svg {
    width: 13px;
    height: 13px;
    color: #c44d4d;
  }
  .info-sponsor:hover {
    border-color: var(--accent);
    color: var(--accent-strong);
    background: rgba(72, 89, 65, 0.05);
  }
  .info-sponsor:hover svg {
    color: #b03939;
  }
  @keyframes lc-fade-in {
    from { opacity: 0; }
    to   { opacity: 1; }
  }
  @keyframes lc-pop-in {
    from { opacity: 0; transform: translateY(6px) scale(0.985); }
    to   { opacity: 1; transform: translateY(0) scale(1); }
  }
</style>
