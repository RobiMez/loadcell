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

// k6Importer understands two k6 outputs:
//
//   - NDJSON stream from `k6 run --out json=out.json`: one JSON object per
//     line, including per-request Point samples. This is the rich format that
//     gives us a real time-series for the chart.
//   - The summary JSON from `handleSummary()` / `--summary-export=s.json`:
//     aggregate-only, so it yields a flat single-sample history.
type k6Importer struct{}

func (k6Importer) Name() string { return "k6" }

func (k6Importer) Detect(data []byte) bool {
	// NDJSON: first non-empty line is a k6 envelope with type+metric.
	if line, ok := firstJSONLine(data); ok {
		var env struct {
			Type   string `json:"type"`
			Metric string `json:"metric"`
		}
		if json.Unmarshal(line, &env) == nil && env.Metric != "" &&
			(env.Type == "Point" || env.Type == "Metric") {
			return true
		}
	}
	// Summary JSON: top-level object with a metrics map containing http_reqs.
	var s k6Summary
	if json.Unmarshal(data, &s) == nil && s.Metrics != nil {
		if _, ok := s.Metrics["http_reqs"]; ok {
			return true
		}
	}
	return false
}

// k6MetricFloats extracts the numeric fields of one k6 metric object,
// silently skipping nested objects (e.g. "thresholds") and non-numeric
// values that would otherwise break a typed unmarshal.
func k6MetricFloats(raw json.RawMessage) map[string]float64 {
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil {
		return nil
	}
	out := make(map[string]float64, len(m))
	for k, v := range m {
		var f float64
		if json.Unmarshal(v, &f) == nil {
			out[k] = f
		}
	}
	return out
}

func (k k6Importer) Parse(data []byte) (model.SavedRun, error) {
	if _, ok := firstJSONLine(data); ok && looksLikeNDJSON(data) {
		return k.parseNDJSON(data)
	}
	return k.parseSummary(data)
}

// ── NDJSON (`--out json`) ────────────────────────────────────────────────

// k6Point is the subset of a k6 NDJSON line we care about.
type k6Point struct {
	Type   string `json:"type"`
	Metric string `json:"metric"`
	Data   struct {
		Time  string  `json:"time"`
		Value float64 `json:"value"`
		Tags  struct {
			Status           string `json:"status"`
			ExpectedResponse string `json:"expected_response"`
			Name             string `json:"name"`
			URL              string `json:"url"`
			Method           string `json:"method"`
		} `json:"tags"`
	} `json:"data"`
}

func (k6Importer) parseNDJSON(data []byte) (model.SavedRun, error) {
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 1024*1024), 16*1024*1024)

	type secBucket struct {
		reqs      int
		ok        int
		latencies []float64
		vus       float64
	}
	buckets := map[int]*secBucket{}
	var acc metricsAccumulator

	var minT, maxT time.Time
	var method, urlStr, scenarioName string
	seenReq := false

	// pendingStatus lets us pair an http_req_duration sample with the status
	// from its own tags; each point already carries its tags so no real
	// pairing is needed, but we track status per duration point directly.
	for sc.Scan() {
		raw := bytes.TrimSpace(sc.Bytes())
		if len(raw) == 0 || raw[0] != '{' {
			continue
		}
		var p k6Point
		if err := json.Unmarshal(raw, &p); err != nil {
			continue
		}
		if p.Type != "Point" {
			continue
		}
		ts, _ := time.Parse(time.RFC3339Nano, p.Data.Time)
		if !ts.IsZero() {
			if minT.IsZero() || ts.Before(minT) {
				minT = ts
			}
			if ts.After(maxT) {
				maxT = ts
			}
		}

		switch p.Metric {
		case "http_reqs":
			seenReq = true
			status, _ := strconv.Atoi(p.Data.Tags.Status)
			if method == "" && p.Data.Tags.Method != "" {
				method = p.Data.Tags.Method
			}
			if urlStr == "" && p.Data.Tags.URL != "" {
				urlStr = p.Data.Tags.URL
			}
			if scenarioName == "" && p.Data.Tags.Name != "" {
				scenarioName = p.Data.Tags.Name
			}
			// Count is value (usually 1).
			n := int(p.Data.Value)
			if n <= 0 {
				n = 1
			}
			ok := statusIsSuccess(status)
			if p.Data.Tags.ExpectedResponse == "false" {
				ok = false
			}
			for i := 0; i < n; i++ {
				acc.buckets[bucketForStatus(status)]++
			}
			if !ts.IsZero() {
				sec := int(ts.Sub(minT).Seconds())
				b := buckets[sec]
				if b == nil {
					b = &secBucket{}
					buckets[sec] = b
				}
				b.reqs += n
				if ok {
					b.ok += n
				}
			}
		case "http_req_duration":
			acc.latencies = append(acc.latencies, p.Data.Value)
			if !ts.IsZero() {
				sec := int(ts.Sub(minT).Seconds())
				b := buckets[sec]
				if b == nil {
					b = &secBucket{}
					buckets[sec] = b
				}
				b.latencies = append(b.latencies, p.Data.Value)
			}
		case "vus":
			if !ts.IsZero() {
				sec := int(ts.Sub(minT).Seconds())
				b := buckets[sec]
				if b == nil {
					b = &secBucket{}
					buckets[sec] = b
				}
				if p.Data.Value > b.vus {
					b.vus = p.Data.Value
				}
			}
		}
	}
	if err := sc.Err(); err != nil {
		return model.SavedRun{}, fmt.Errorf("reading NDJSON: %w", err)
	}
	if !seenReq {
		return model.SavedRun{}, fmt.Errorf("no http_reqs samples found")
	}

	elapsed := maxT.Sub(minT).Seconds()
	if elapsed <= 0 {
		elapsed = float64(len(buckets))
	}
	metrics := acc.summary(elapsed)

	// Build ordered per-second history.
	secs := make([]int, 0, len(buckets))
	for s := range buckets {
		secs = append(secs, s)
	}
	sort.Ints(secs)
	history := make([]model.Sample, 0, len(secs))
	var lastVus float64
	for _, s := range secs {
		b := buckets[s]
		if b.vus > 0 {
			lastVus = b.vus
		}
		history = append(history, model.Sample{
			T:         float64(s),
			TickRps:   float64(b.reqs),
			TickRpsOk: float64(b.ok),
			P50:       percentile(b.latencies, 50),
			P95:       percentile(b.latencies, 95),
			P99:       percentile(b.latencies, 99),
			Conc:      int(math.Round(lastVus)),
		})
	}

	name := scenarioName
	if name == "" {
		name = "k6 import"
	}
	return model.SavedRun{
		StartedAt: minT.UnixMilli(),
		Name:      name,
		Method:    strings.ToUpper(method),
		URL:       urlStr,
		Config: model.RunConfig{
			Mode:         "imported",
			DurationSecs: int(math.Round(elapsed)),
		},
		Metrics: metrics,
		History: history,
	}, nil
}

// ── Summary JSON (`handleSummary` / `--summary-export`) ───────────────────

type k6Summary struct {
	Metrics map[string]json.RawMessage `json:"metrics"`
}

func (k6Importer) parseSummary(data []byte) (model.SavedRun, error) {
	var s k6Summary
	if err := json.Unmarshal(data, &s); err != nil {
		return model.SavedRun{}, fmt.Errorf("parsing summary JSON: %w", err)
	}
	reqs := k6MetricFloats(s.Metrics["http_reqs"])
	if reqs == nil {
		return model.SavedRun{}, fmt.Errorf("summary has no http_reqs metric")
	}
	total := int(reqs["count"])
	rate := reqs["rate"]
	elapsed := 0.0
	if rate > 0 {
		elapsed = float64(total) / rate
	}

	// http_req_failed is a Rate metric: "value" is the failed fraction (0..1).
	// (Its "passes"/"fails" counts are confusingly inverted — passes = failed
	// requests — so we derive the count from the rate instead.) The summary
	// doesn't break failures down by status, so they land in server errors.
	failed := 0
	if f := k6MetricFloats(s.Metrics["http_req_failed"]); f != nil {
		if v, ok := f["value"]; ok {
			failed = int(math.Round(v * float64(total)))
		} else if passes, ok := f["passes"]; ok {
			failed = int(passes)
		}
	}
	if failed > total {
		failed = total
	}
	success := total - failed

	dur := k6MetricFloats(s.Metrics["http_req_duration"])
	p50 := pickFirst(dur, "med", "p(50)", "avg")
	p95 := pickFirst(dur, "p(95)", "p(90)", "max")
	p99 := pickFirst(dur, "p(99)", "p(95)", "max")

	errRate := 0.0
	if total > 0 {
		errRate = float64(failed) / float64(total)
	}
	metrics := engine.Metrics{
		ElapsedSecs:   elapsed,
		TotalRequests: total,
		Successful:    success,
		ServerErrors:  failed,
		Errors:        failed,
		RPS:           rate,
		ErrorRate:     errRate,
		P50Ms:         p50,
		P95Ms:         p95,
		P99Ms:         p99,
		Running:       false,
	}

	// Summary has no time-series. Spread the aggregate evenly across the
	// duration so the chart renders as a constant strip rather than two
	// bars at the edges.
	dr := int(math.Max(1, math.Round(elapsed)))
	okRate := rate * float64(success) / math.Max(1, float64(total))
	conc := int(math.Round(rate))
	if conc < 1 {
		conc = 1
	}
	history := make([]model.Sample, 0, dr+1)
	for t := 0; t <= dr; t++ {
		history = append(history, model.Sample{
			T:         float64(t),
			TickRps:   rate,
			TickRpsOk: okRate,
			P50:       p50,
			P95:       p95,
			P99:       p99,
			Conc:      conc,
		})
	}

	return model.SavedRun{
		Name:    "k6 import (summary)",
		Config:  model.RunConfig{Mode: "imported", Concurrency: conc, DurationSecs: dr},
		Metrics: metrics,
		History: history,
	}, nil
}

func pickFirst(m map[string]float64, keys ...string) float64 {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			return v
		}
	}
	return 0
}

// ── small NDJSON helpers ─────────────────────────────────────────────────

func firstJSONLine(data []byte) ([]byte, bool) {
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 1024*1024), 16*1024*1024)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) > 0 && line[0] == '{' {
			out := make([]byte, len(line))
			copy(out, line)
			return out, true
		}
	}
	return nil, false
}

// looksLikeNDJSON returns true if the data has more than one JSON object line,
// or a single line that is a k6 Point/Metric envelope (as opposed to a single
// pretty-printed summary object).
func looksLikeNDJSON(data []byte) bool {
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 1024*1024), 16*1024*1024)
	jsonLines := 0
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var env struct {
			Type   string `json:"type"`
			Metric string `json:"metric"`
		}
		if json.Unmarshal(line, &env) == nil && env.Metric != "" && env.Type != "" {
			jsonLines++
			if jsonLines >= 1 {
				return true
			}
		}
	}
	return false
}
