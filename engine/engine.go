// Package engine implements the LoadCell load-generation core.
//
// Design: N worker goroutines fire HTTP GETs in a tight loop against a single
// target. Each result is sent over a buffered channel to a single aggregator
// goroutine that owns all mutable state (counters + latency slice). A 500ms
// ticker prompts the aggregator to build a Metrics snapshot and hand it to a
// caller-supplied callback. The package has no Wails dependency so it can be
// driven by any shell (Wails, CLI, tests).
//
// Two load profiles:
//
//   - "constant": Concurrency workers fire for DurationSecs.
//   - "ramp":     Start with Concurrency workers, linearly add up to
//                 RampToConcurrency over DurationSecs.
//
// Responses are bucketed by status code so the caller can observe a realistic
// breakdown (2xx success, 4xx client, 429 rate-limited, 5xx server, network).
package engine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"loadcell/tmpl"
)

// Load profile names. Empty string is treated as ModeConstant.
const (
	ModeConstant = "constant"
	ModeRamp     = "ramp"
	ModeCurve    = "curve"
)

// Curve segment shapes — describes how to interpolate from the previous point
// to a given CurvePoint.
const (
	CurveLinear      = "linear"
	CurveExponential = "exponential"
	CurveStep        = "step"
)

// CurvePoint is one (time, users) anchor on a load curve. The CurveIn field
// describes how the segment ending at this point is shaped relative to the
// previous point.
type CurvePoint struct {
	TimeSecs float64 `json:"timeSecs"`
	Users    float64 `json:"users"`
	CurveIn  string  `json:"curveIn"`  // "linear" | "exponential" | "step"
	Exponent float64 `json:"exponent"` // for exponential; defaults to 2.0
}

// StepConfig is one HTTP request within a compound flow.
// Empty Method defaults to GET. Templated values render fresh per call.
type StepConfig struct {
	URL     string            `json:"url"`
	Method  string            `json:"method"`
	Headers map[string]string `json:"headers"`
	Body    string            `json:"body"`
}

// Config is the test definition supplied by the caller.
//
// Request shape:
//   - Steps non-empty: compound flow. Each worker fires step 0, 1, …, N-1
//     and loops back to step 0. The single-request URL/Method/Headers/Body
//     fields are ignored.
//   - Steps empty: legacy single-request mode, using URL/Method/Headers/Body.
//
// Load profiles:
//   - Mode == "constant": Concurrency workers fire for DurationSecs.
//   - Mode == "ramp":     workers ramp 1 → Concurrency over RampUpSecs, then
//                          sustain at Concurrency until DurationSecs.
//   - Mode == "curve":    workers follow the Curve (interactive editor) for
//                          DurationSecs. Noise jitters the desired count by
//                          ±(Noise * value) every scheduler tick.
type Config struct {
	URL     string            `json:"url"`
	Method  string            `json:"method"`  // empty → GET
	Headers map[string]string `json:"headers"` // applied via Header.Set on each request
	Body    string            `json:"body"`    // raw body, repeated verbatim per request

	// Steps replaces URL/Method/Headers/Body when non-empty. Each worker
	// walks the slice in order, then wraps back to index 0. Per-step
	// templates have independent {{seq}} counters.
	Steps []StepConfig `json:"steps,omitempty"`

	Mode        string `json:"mode"`
	Concurrency int    `json:"concurrency"` // target (peak) worker count for constant/ramp
	// RampUpSecs is how long it takes to reach Concurrency in ramp mode.
	// Must be 1 <= RampUpSecs <= DurationSecs. Ignored for non-ramp modes.
	RampUpSecs   int `json:"rampUpSecs"`
	DurationSecs int `json:"durationSecs"`

	// Curve mode fields.
	Curve []CurvePoint `json:"curve"` // ordered by TimeSecs ascending; first point usually at t=0
	Noise float64      `json:"noise"` // 0..1; jitter applied to desired worker count each tick
}

// stepSpec is one parsed step in a flow (or the single step of a non-flow
// run). Templates are pre-parsed in Start so workers don't re-parse per
// request; placeholders like {{uuid}} render fresh on every call so each
// request can be unique (cache-miss testing). Each step holds an
// independent {{seq}} counter — step-to-step value sharing is out of
// scope for the MVP (would need a variable system).
type stepSpec struct {
	urlTmpl  *tmpl.Template
	method   string
	headers  []headerSpec
	bodyTmpl *tmpl.Template
}

// headerSpec keeps a parallel key + value-template list. Header keys are
// kept static; only values are templated.
type headerSpec struct {
	key string
	val *tmpl.Template
}

// Metrics is one live snapshot of the running test, emitted periodically and
// once more (with Running=false) when the test ends.
//
// TotalRequests = Successful + ClientErrors + RateLimited + ServerErrors + NetworkErrors
// Errors        = TotalRequests - Successful (everything not 2xx/3xx)
type Metrics struct {
	ElapsedSecs        float64 `json:"elapsedSecs"`
	TotalRequests      int     `json:"totalRequests"`
	Successful         int     `json:"successful"`    // 2xx / 3xx
	ClientErrors       int     `json:"clientErrors"`  // 4xx except 429
	RateLimited        int     `json:"rateLimited"`   // 429
	ServerErrors       int     `json:"serverErrors"`  // 5xx
	NetworkErrors      int     `json:"networkErrors"` // transport-level (timeout, conn refused, DNS, ...)
	Errors             int     `json:"errors"`        // sum of all non-success buckets
	RPS                float64 `json:"rps"`
	ErrorRate          float64 `json:"errorRate"`
	P50Ms              float64 `json:"p50Ms"`
	P95Ms              float64 `json:"p95Ms"`
	P99Ms              float64 `json:"p99Ms"`
	CurrentConcurrency int     `json:"currentConcurrency"`
	Running            bool    `json:"running"`
}

type statusBucket int

const (
	bucketSuccess statusBucket = iota
	bucketClientErr
	bucketRateLimit
	bucketServerErr
	bucketNetworkErr
	bucketCount
)

func classify(resp *http.Response, err error) statusBucket {
	if err != nil || resp == nil {
		return bucketNetworkErr
	}
	code := resp.StatusCode
	switch {
	case code == http.StatusTooManyRequests:
		return bucketRateLimit
	case code >= 500:
		return bucketServerErr
	case code >= 400:
		return bucketClientErr
	default:
		return bucketSuccess
	}
}

type result struct {
	latencyMs float64
	bucket    statusBucket
	// stepIdx records which step of a flow produced this result. Always 0
	// in single-request mode. Currently the aggregator ignores it, but
	// storing it now keeps per-step metrics cheap to add later.
	stepIdx int
}

// Engine drives a single load test at a time.
type Engine struct {
	mu        sync.Mutex
	running   bool
	cancel    context.CancelFunc
	onMetrics func(Metrics)
}

// New returns a fresh Engine.
func New() *Engine {
	return &Engine{}
}

// OnMetrics registers a callback invoked with each live snapshot. The callback
// runs on the aggregator goroutine, so it should not block for long.
func (e *Engine) OnMetrics(fn func(Metrics)) {
	e.mu.Lock()
	e.onMetrics = fn
	e.mu.Unlock()
}

// IsRunning reports whether a test is currently in flight.
func (e *Engine) IsRunning() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.running
}

// Start validates cfg and kicks off a test in the background. It returns
// immediately. Returns an error if cfg is invalid or a test is already running.
func (e *Engine) Start(cfg Config) error {
	if len(cfg.Steps) > 0 {
		for i, s := range cfg.Steps {
			if s.URL == "" {
				return fmt.Errorf("step %d: url is required", i+1)
			}
		}
	} else if cfg.URL == "" {
		return errors.New("url is required")
	}
	if cfg.Concurrency < 1 {
		return errors.New("concurrency must be >= 1")
	}
	if cfg.DurationSecs < 1 {
		return errors.New("durationSecs must be >= 1")
	}
	if cfg.Mode == "" {
		cfg.Mode = ModeConstant
	}
	if cfg.Mode != ModeConstant && cfg.Mode != ModeRamp && cfg.Mode != ModeCurve {
		return errors.New("mode must be 'constant', 'ramp', or 'curve'")
	}
	if cfg.Mode == ModeRamp {
		if cfg.RampUpSecs < 1 {
			return errors.New("rampUpSecs must be >= 1")
		}
		if cfg.RampUpSecs > cfg.DurationSecs {
			return errors.New("rampUpSecs cannot exceed durationSecs")
		}
		if cfg.Concurrency < 2 {
			return errors.New("ramp mode requires concurrency >= 2 (otherwise there is nothing to ramp)")
		}
	}
	if cfg.Mode == ModeCurve {
		if len(cfg.Curve) < 2 {
			return errors.New("curve must have at least 2 points")
		}
		for i := 1; i < len(cfg.Curve); i++ {
			if cfg.Curve[i].TimeSecs <= cfg.Curve[i-1].TimeSecs {
				return errors.New("curve points must have strictly increasing time")
			}
		}
		peakUsers := 0.0
		for _, p := range cfg.Curve {
			if p.Users < 0 {
				return errors.New("curve users must be >= 0")
			}
			if p.Users > peakUsers {
				peakUsers = p.Users
			}
		}
		if peakUsers <= 0 {
			return errors.New("curve has 0 peak workers — add at least one point with users > 0")
		}
		if cfg.Noise < 0 || cfg.Noise > 1 {
			return errors.New("noise must be between 0 and 1 inclusive")
		}
	}

	// Build the step list. Legacy single-request mode is treated as a
	// one-step flow so the worker loop downstream is the same shape for
	// both cases.
	rawSteps := cfg.Steps
	if len(rawSteps) == 0 {
		rawSteps = []StepConfig{{
			URL: cfg.URL, Method: cfg.Method, Headers: cfg.Headers, Body: cfg.Body,
		}}
	}
	steps := make([]stepSpec, 0, len(rawSteps))
	for i, s := range rawSteps {
		sp, err := buildStepSpec(s)
		if err != nil {
			if len(cfg.Steps) > 0 {
				return fmt.Errorf("step %d: %w", i+1, err)
			}
			return err
		}
		steps = append(steps, sp)
	}

	e.mu.Lock()
	if e.running {
		e.mu.Unlock()
		return errors.New("a test is already running")
	}
	e.running = true
	maxWorkers := cfg.Concurrency
	if cfg.Mode == ModeCurve {
		// Peak users across the whole curve sets the max worker pool size.
		peak := 0.0
		for _, p := range cfg.Curve {
			if p.Users > peak {
				peak = p.Users
			}
		}
		// Reserve some slack for noise spikes above the deterministic peak.
		maxWorkers = int(math.Ceil(peak * (1 + cfg.Noise)))
		if maxWorkers < 1 {
			maxWorkers = 1
		}
	}
	ctx, cancel := context.WithTimeout(
		context.Background(),
		time.Duration(cfg.DurationSecs)*time.Second,
	)
	e.cancel = cancel
	cb := e.onMetrics
	e.mu.Unlock()

	transport := &http.Transport{
		MaxIdleConns:        maxWorkers * 2,
		MaxIdleConnsPerHost: maxWorkers * 2,
		IdleConnTimeout:     90 * time.Second,
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
	}

	results := make(chan result, 4096)
	startTime := time.Now()

	var wg sync.WaitGroup
	var currentConcurrency atomic.Int64

	spawnWorker := func() {
		wg.Add(1)
		go func() {
			currentConcurrency.Add(1)
			defer currentConcurrency.Add(-1)
			defer wg.Done()
			runWorker(ctx, client, steps, results)
		}()
	}

	if cfg.Mode == ModeCurve {
		// Curve mode: pre-spawn maxWorkers gated by slot index. A scheduler
		// goroutine updates `desired` based on the interpolated curve + noise.
		var desired atomic.Int64
		rng := rand.New(rand.NewSource(time.Now().UnixNano()))

		for i := 0; i < maxWorkers; i++ {
			slot := i
			wg.Add(1)
			go func() {
				defer wg.Done()
				runCurveWorker(ctx, slot, &desired, client, steps, results, &currentConcurrency)
			}()
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			ticker := time.NewTicker(100 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					t := time.Since(startTime).Seconds()
					target := evalCurve(cfg.Curve, t)
					if cfg.Noise > 0 {
						jitter := (rng.Float64()*2 - 1) * cfg.Noise
						target = target * (1 + jitter)
						if target < 0 {
							target = 0
						}
					}
					if target > float64(maxWorkers) {
						target = float64(maxWorkers)
					}
					desired.Store(int64(target + 0.5))
				}
			}
		}()
	} else if cfg.Mode == ModeRamp {
		// Spawn worker #1 immediately, then evenly spread the remaining
		// Concurrency-1 workers across RampUpSecs. After RampUpSecs the worker
		// count holds at Concurrency until the duration timeout fires.
		spawnWorker()
		remaining := cfg.Concurrency - 1
		interval := time.Duration(cfg.RampUpSecs) * time.Second / time.Duration(remaining)
		// Ramp scheduler is wg-tracked so wg.Wait can't fire (and close results)
		// while the scheduler might still spawn another worker.
		wg.Add(1)
		go func() {
			defer wg.Done()
			ticker := time.NewTicker(interval)
			defer ticker.Stop()
			added := 0
			for added < remaining {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					spawnWorker()
					added++
				}
			}
		}()
	} else {
		for i := 0; i < cfg.Concurrency; i++ {
			spawnWorker()
		}
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	go func() {
		final := e.aggregate(results, startTime, cb, func() int { return int(currentConcurrency.Load()) })
		transport.CloseIdleConnections()
		e.mu.Lock()
		e.running = false
		e.cancel = nil
		e.mu.Unlock()
		cancel()
		// Emit the final "Done" snapshot only after the engine state is reset,
		// so a UI consumer that immediately clicks Start in response sees an
		// idle engine instead of "a test is already running".
		if cb != nil {
			cb(final)
		}
	}()

	return nil
}

// Stop cancels a running test. No-op if nothing is running.
func (e *Engine) Stop() {
	e.mu.Lock()
	c := e.cancel
	e.mu.Unlock()
	if c != nil {
		c()
	}
}

// buildStepSpec pre-parses one step's templates so the worker hot path
// never re-parses. Defaults Method to GET when empty.
func buildStepSpec(s StepConfig) (stepSpec, error) {
	method := s.Method
	if method == "" {
		method = http.MethodGet
	}
	urlTmpl, err := tmpl.Parse(s.URL)
	if err != nil {
		return stepSpec{}, fmt.Errorf("url: %w", err)
	}
	bodyTmpl, err := tmpl.Parse(s.Body)
	if err != nil {
		return stepSpec{}, fmt.Errorf("body: %w", err)
	}
	var hdrs []headerSpec
	if len(s.Headers) > 0 {
		hdrs = make([]headerSpec, 0, len(s.Headers))
		for k, v := range s.Headers {
			vt, err := tmpl.Parse(v)
			if err != nil {
				return stepSpec{}, fmt.Errorf("header %q: %w", k, err)
			}
			hdrs = append(hdrs, headerSpec{key: k, val: vt})
		}
	}
	return stepSpec{
		urlTmpl:  urlTmpl,
		method:   method,
		headers:  hdrs,
		bodyTmpl: bodyTmpl,
	}, nil
}

// runWorker fires the steps in order, wrapping back to step 0 after the
// last one. Single-request mode is just a 1-step flow, so this is the
// only worker body.
func runWorker(ctx context.Context, client *http.Client, steps []stepSpec, out chan<- result) {
	i := 0
	for doRequest(ctx, client, steps[i], i, out) {
		i = (i + 1) % len(steps)
	}
}

// doRequest fires a single request and either sends its result or drops it on
// shutdown. Returns false if the worker should stop (cancelled, bad URL, etc.).
func doRequest(ctx context.Context, client *http.Client, step stepSpec, stepIdx int, out chan<- result) bool {
	if ctx.Err() != nil {
		return false
	}
	// Render templates fresh on every request so {{uuid}}, {{seq}}, etc.
	// produce a new value per call. For static templates Render returns
	// the original literal string with no copy.
	renderedURL := step.urlTmpl.Render()
	renderedBody := step.bodyTmpl.Render()
	var body io.Reader
	if renderedBody != "" {
		// Fresh reader per request — strings.NewReader is cheap to allocate
		// and avoids consuming a shared reader across iterations.
		body = strings.NewReader(renderedBody)
	}
	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, step.method, renderedURL, body)
	if err != nil {
		// Malformed URL/method won't fix itself — emit one error and exit so we
		// don't spin a CPU.
		select {
		case out <- result{latencyMs: 0, bucket: bucketNetworkErr, stepIdx: stepIdx}:
		case <-ctx.Done():
		}
		return false
	}
	for _, h := range step.headers {
		req.Header.Set(h.key, h.val.Render())
	}
	resp, err := client.Do(req)
	latencyMs := float64(time.Since(start).Microseconds()) / 1000.0
	// Cancellation mid-flight (duration elapsed or Stop pressed) — drop the
	// result so we don't pollute the error count with shutdown noise.
	if err != nil && ctx.Err() != nil {
		if resp != nil {
			resp.Body.Close()
		}
		return false
	}
	bucket := classify(resp, err)
	if resp != nil {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
	select {
	case out <- result{latencyMs: latencyMs, bucket: bucket, stepIdx: stepIdx}:
	case <-ctx.Done():
		return false
	}
	return true
}

// runCurveWorker is the worker body for curve mode. A worker is "active" only
// while its slot index is below the scheduler's `desired` value; otherwise it
// idles in 50ms ticks so it can resume the moment the curve climbs back.
func runCurveWorker(
	ctx context.Context,
	slot int,
	desired *atomic.Int64,
	client *http.Client,
	steps []stepSpec,
	out chan<- result,
	active *atomic.Int64,
) {
	wasActive := false
	setActive := func(b bool) {
		if b == wasActive {
			return
		}
		if b {
			active.Add(1)
		} else {
			active.Add(-1)
		}
		wasActive = b
	}
	defer setActive(false)

	stepIdx := 0
	for {
		if ctx.Err() != nil {
			return
		}
		if int64(slot) >= desired.Load() {
			setActive(false)
			select {
			case <-ctx.Done():
				return
			case <-time.After(50 * time.Millisecond):
			}
			continue
		}
		setActive(true)
		if !doRequest(ctx, client, steps[stepIdx], stepIdx, out) {
			return
		}
		stepIdx = (stepIdx + 1) % len(steps)
	}
}

// evalCurve interpolates the desired worker count at time t along the curve.
// Times outside the curve are clamped to the endpoints.
func evalCurve(points []CurvePoint, t float64) float64 {
	if len(points) == 0 {
		return 0
	}
	if t <= points[0].TimeSecs {
		return points[0].Users
	}
	for i := 1; i < len(points); i++ {
		if t <= points[i].TimeSecs {
			return interpolateSegment(points[i-1], points[i], t)
		}
	}
	return points[len(points)-1].Users
}

func interpolateSegment(p0, p1 CurvePoint, t float64) float64 {
	dt := p1.TimeSecs - p0.TimeSecs
	if dt <= 0 {
		return p1.Users
	}
	progress := (t - p0.TimeSecs) / dt
	if progress < 0 {
		progress = 0
	} else if progress > 1 {
		progress = 1
	}
	switch p1.CurveIn {
	case CurveStep:
		if progress >= 1.0 {
			return p1.Users
		}
		return p0.Users
	case CurveExponential:
		exp := p1.Exponent
		if exp <= 0 {
			exp = 2.0
		}
		progress = math.Pow(progress, exp)
	}
	return p0.Users + (p1.Users-p0.Users)*progress
}

// aggregate consumes results until the channel closes, emitting a live
// snapshot to emitTick every 500ms while the run is active, and returns the
// final snapshot (with Running=false) once all workers have exited.
func (e *Engine) aggregate(in <-chan result, startTime time.Time, emitTick func(Metrics), getConcurrency func() int) Metrics {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	var latencies []float64
	var total int
	var counts [bucketCount]int

	for {
		select {
		case r, ok := <-in:
			if !ok {
				return buildMetrics(latencies, total, counts, startTime, false, getConcurrency())
			}
			total++
			counts[r.bucket]++
			latencies = append(latencies, r.latencyMs)
		case <-ticker.C:
			if emitTick != nil {
				emitTick(buildMetrics(latencies, total, counts, startTime, true, getConcurrency()))
			}
		}
	}
}

func buildMetrics(latencies []float64, total int, counts [bucketCount]int, startTime time.Time, running bool, currentConcurrency int) Metrics {
	elapsed := time.Since(startTime).Seconds()
	m := Metrics{
		ElapsedSecs:        elapsed,
		TotalRequests:      total,
		Successful:         counts[bucketSuccess],
		ClientErrors:       counts[bucketClientErr],
		RateLimited:        counts[bucketRateLimit],
		ServerErrors:       counts[bucketServerErr],
		NetworkErrors:      counts[bucketNetworkErr],
		Running:            running,
		CurrentConcurrency: currentConcurrency,
	}
	m.Errors = total - m.Successful
	if elapsed > 0 {
		m.RPS = float64(total) / elapsed
	}
	if total > 0 {
		m.ErrorRate = float64(m.Errors) / float64(total)
	}
	if len(latencies) > 0 {
		// Copy + sort each tick — replace with a streaming histogram (e.g. HDR)
		// once runs grow long enough that O(n log n) per snapshot bites.
		sorted := make([]float64, len(latencies))
		copy(sorted, latencies)
		sort.Float64s(sorted)
		m.P50Ms = percentile(sorted, 50)
		m.P95Ms = percentile(sorted, 95)
		m.P99Ms = percentile(sorted, 99)
	}
	return m
}

func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(p / 100 * float64(len(sorted)))
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}
