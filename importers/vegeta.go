package importers

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"loadcell/engine"
	"loadcell/model"
)

// vegetaImporter understands two vegeta outputs:
//
//   - Encoded NDJSON from `vegeta encode -to=json results.bin`: one JSON
//     object per request, carrying timestamp, latency, method, URL, status,
//     and error. This is the rich format that gives us a real per-second
//     time-series plus the endpoint.
//   - Aggregate report from `vegeta report -type=json`: aggregate-only, no
//     per-request stream, so it produces a flat synthesized history at the
//     reported rate plus an accurate per-status-code summary.
type vegetaImporter struct{}

func (vegetaImporter) Name() string { return "vegeta" }

// vegetaReport mirrors the fields we use from vegeta's aggregate JSON
// report. Latency values are in nanoseconds.
type vegetaReport struct {
	Latencies struct {
		Mean float64 `json:"mean"`
		P50  float64 `json:"50th"`
		P90  float64 `json:"90th"`
		P95  float64 `json:"95th"`
		P99  float64 `json:"99th"`
		Max  float64 `json:"max"`
	} `json:"latencies"`
	Duration    float64        `json:"duration"` // ns of actual request span
	Wait        float64        `json:"wait"`
	Requests    int            `json:"requests"`
	Rate        float64        `json:"rate"`
	Throughput  float64        `json:"throughput"`
	Success     float64        `json:"success"` // ratio 0..1
	StatusCodes map[string]int `json:"status_codes"`
	Errors      []string       `json:"errors"`
}

// vegetaResult mirrors one line of `vegeta encode -to=json` output.
// Latency is nanoseconds.
type vegetaResult struct {
	Seq       uint64 `json:"seq"`
	Code      int    `json:"code"`
	Timestamp string `json:"timestamp"`
	Latency   int64  `json:"latency"`
	Method    string `json:"method"`
	URL       string `json:"url"`
	Error     string `json:"error"`
}

func (vegetaImporter) Detect(data []byte) bool {
	// Encoded NDJSON: first line has the per-request shape.
	if line, ok := firstJSONLine(data); ok {
		var probe struct {
			Seq       *uint64 `json:"seq"`
			Code      *int    `json:"code"`
			Timestamp string  `json:"timestamp"`
			Latency   *int64  `json:"latency"`
			Method    string  `json:"method"`
		}
		if json.Unmarshal(line, &probe) == nil &&
			probe.Seq != nil && probe.Code != nil && probe.Latency != nil &&
			probe.Timestamp != "" && probe.Method != "" {
			return true
		}
	}
	// Aggregate report: top-level object with latencies + status_codes.
	var probe struct {
		Latencies   *json.RawMessage `json:"latencies"`
		StatusCodes *json.RawMessage `json:"status_codes"`
		Requests    *int             `json:"requests"`
	}
	if json.Unmarshal(data, &probe) != nil {
		return false
	}
	return probe.Latencies != nil && probe.StatusCodes != nil && probe.Requests != nil
}

func (v vegetaImporter) Parse(data []byte) (model.SavedRun, error) {
	if line, ok := firstJSONLine(data); ok {
		var probe struct {
			Seq       *uint64 `json:"seq"`
			Timestamp string  `json:"timestamp"`
		}
		if json.Unmarshal(line, &probe) == nil && probe.Seq != nil && probe.Timestamp != "" {
			return v.parseEncoded(data)
		}
	}
	return v.parseReport(data)
}

// ── Encoded NDJSON (`vegeta encode -to=json`) ────────────────────────────

func (vegetaImporter) parseEncoded(data []byte) (model.SavedRun, error) {
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 1024*1024), 16*1024*1024)

	type secBucket struct {
		reqs      int
		ok        int
		latencies []float64
	}
	buckets := map[int]*secBucket{}
	var acc metricsAccumulator

	var minT, maxT time.Time
	var method, urlStr string
	seen := false

	for sc.Scan() {
		raw := bytes.TrimSpace(sc.Bytes())
		if len(raw) == 0 || raw[0] != '{' {
			continue
		}
		var r vegetaResult
		if err := json.Unmarshal(raw, &r); err != nil {
			continue
		}
		// A valid result line always carries a timestamp and a latency.
		if r.Timestamp == "" {
			continue
		}
		seen = true

		ts, _ := time.Parse(time.RFC3339Nano, r.Timestamp)
		if !ts.IsZero() {
			if minT.IsZero() || ts.Before(minT) {
				minT = ts
			}
			if ts.After(maxT) {
				maxT = ts
			}
		}
		if method == "" && r.Method != "" {
			method = r.Method
		}
		if urlStr == "" && r.URL != "" {
			urlStr = r.URL
		}

		// vegeta reports transport-level failures with code=0 (or an empty
		// code) and a non-empty error string. bucketForStatus(0) already
		// maps to bucketNetworkErr, which keeps colours consistent.
		status := r.Code
		if status == 0 && r.Error != "" {
			status = 0
		}
		latencyMs := float64(r.Latency) / 1e6
		acc.add(status, latencyMs)

		ok := statusIsSuccess(status)
		if !ts.IsZero() {
			sec := int(ts.Sub(minT).Seconds())
			b := buckets[sec]
			if b == nil {
				b = &secBucket{}
				buckets[sec] = b
			}
			b.reqs++
			if ok {
				b.ok++
			}
			b.latencies = append(b.latencies, latencyMs)
		}
	}
	if err := sc.Err(); err != nil {
		return model.SavedRun{}, fmt.Errorf("reading vegeta NDJSON: %w", err)
	}
	if !seen {
		return model.SavedRun{}, fmt.Errorf("no vegeta result records found")
	}

	elapsed := maxT.Sub(minT).Seconds()
	if elapsed <= 0 {
		elapsed = float64(len(buckets))
	}
	metrics := acc.summary(elapsed)

	secs := make([]int, 0, len(buckets))
	for s := range buckets {
		secs = append(secs, s)
	}
	sort.Ints(secs)
	history := make([]model.Sample, 0, len(secs))
	for _, s := range secs {
		b := buckets[s]
		history = append(history, model.Sample{
			T:         float64(s),
			TickRps:   float64(b.reqs),
			TickRpsOk: float64(b.ok),
			P50:       percentile(b.latencies, 50),
			P95:       percentile(b.latencies, 95),
			P99:       percentile(b.latencies, 99),
		})
	}

	conc := 0
	if metrics.RPS > 0 {
		conc = int(math.Round(metrics.RPS))
	}

	return model.SavedRun{
		StartedAt: minT.UnixMilli(),
		Name:      "vegeta import",
		Method:    strings.ToUpper(method),
		URL:       urlStr,
		Config: model.RunConfig{
			Mode:         "imported",
			Concurrency:  conc,
			DurationSecs: int(math.Round(elapsed)),
		},
		Metrics: metrics,
		History: history,
	}, nil
}

// ── Aggregate report (`vegeta report -type=json`) ────────────────────────

func (vegetaImporter) parseReport(data []byte) (model.SavedRun, error) {
	var r vegetaReport
	if err := json.Unmarshal(data, &r); err != nil {
		return model.SavedRun{}, fmt.Errorf("parsing vegeta JSON: %w", err)
	}

	const nsToMs = 1.0 / 1e6
	p50 := r.Latencies.P50 * nsToMs
	p95 := r.Latencies.P95 * nsToMs
	p99 := r.Latencies.P99 * nsToMs

	var buckets [bucketCount]int
	total := 0
	for code, n := range r.StatusCodes {
		status, _ := strconv.Atoi(code)
		// vegeta records transport errors under the "0" status code.
		buckets[bucketForStatus(status)] += n
		total += n
	}
	if total == 0 {
		total = r.Requests
		buckets[bucketSuccess] = total
	}
	// Reconcile with reported request count if status codes are incomplete.
	if total < r.Requests {
		buckets[bucketNetworkErr] += r.Requests - total
		total = r.Requests
	}

	success := buckets[bucketSuccess]
	errors := total - success
	elapsed := r.Duration / 1e9 // ns → s
	if elapsed <= 0 && r.Rate > 0 {
		elapsed = float64(total) / r.Rate
	}
	errRate := 0.0
	if total > 0 {
		errRate = float64(errors) / float64(total)
	}
	rps := r.Rate
	if rps == 0 && elapsed > 0 {
		rps = float64(total) / elapsed
	}

	metrics := engine.Metrics{
		ElapsedSecs:   elapsed,
		TotalRequests: total,
		Successful:    success,
		ClientErrors:  buckets[bucketClientErr],
		RateLimited:   buckets[bucketRateLimit],
		ServerErrors:  buckets[bucketServerErr],
		NetworkErrors: buckets[bucketNetworkErr],
		Errors:        errors,
		RPS:           rps,
		ErrorRate:     errRate,
		P50Ms:         p50,
		P95Ms:         p95,
		P99Ms:         p99,
		Running:       false,
	}

	dr := int(math.Max(1, math.Round(elapsed)))
	okRate := rps
	if total > 0 {
		okRate = rps * float64(success) / float64(total)
	}
	// Vegeta attacks are constant-rate by default, so spreading the
	// aggregate evenly across the duration is a faithful (if synthetic)
	// per-second timeline. Without this the chart only has two points and
	// renders as bars at the edges with nothing between.
	conc := int(math.Round(rps))
	if conc < 1 {
		conc = 1
	}
	history := make([]model.Sample, 0, dr+1)
	for t := 0; t <= dr; t++ {
		history = append(history, model.Sample{
			T:         float64(t),
			TickRps:   rps,
			TickRpsOk: okRate,
			P50:       p50,
			P95:       p95,
			P99:       p99,
			Conc:      conc,
		})
	}

	return model.SavedRun{
		Name:    "vegeta import",
		Method:  "GET",
		Config:  model.RunConfig{Mode: "imported", Concurrency: conc, DurationSecs: dr},
		Metrics: metrics,
		History: history,
	}, nil
}
