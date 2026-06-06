package importers

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"

	"loadcell/engine"
	"loadcell/model"
)

// vegetaImporter understands the JSON report from `vegeta report -type=json`.
// That report is aggregate-only (no per-request stream), so it produces a flat
// two-point history plus an accurate per-status-code summary.
//
// It exists primarily to demonstrate that the Importer interface is genuinely
// tool-agnostic: supporting a new tool is just another file like this one.
type vegetaImporter struct{}

func (vegetaImporter) Name() string { return "vegeta" }

// vegetaReport mirrors the fields we use from vegeta's JSON report. Latency
// values are in nanoseconds.
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

func (vegetaImporter) Detect(data []byte) bool {
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

func (vegetaImporter) Parse(data []byte) (model.SavedRun, error) {
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
	history := []model.Sample{
		{T: 0, TickRps: rps, TickRpsOk: okRate, P50: p50, P95: p95, P99: p99},
		{T: float64(dr), TickRps: rps, TickRpsOk: okRate, P50: p50, P95: p95, P99: p99},
	}

	return model.SavedRun{
		Name:    "vegeta import",
		Config:  model.RunConfig{Mode: "imported", DurationSecs: dr},
		Metrics: metrics,
		History: history,
	}, nil
}
