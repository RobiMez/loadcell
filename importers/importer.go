package importers

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"loadcell/engine"
	"loadcell/model"
)

// Importer converts a load-testing tool's native result output into a
// SavedRun so it can be visualised with the same charts/cards as a run that
// loadcell produced itself.
//
// Adding support for a new tool is intentionally cheap: implement this
// interface and register it in importerRegistry. Everything else — the App
// binding, the file picker, the visuals — stays the same.
type Importer interface {
	// Name is a short, stable identifier for the tool ("k6", "vegeta").
	Name() string
	// Detect reports whether data looks like this tool's output. It should be
	// cheap and conservative: only claim data it is confident about.
	Detect(data []byte) bool
	// Parse turns the raw file bytes into a SavedRun. The caller fills in ID
	// and StartedAt if the importer leaves them zero.
	Parse(data []byte) (model.SavedRun, error)
}

// importerRegistry lists every supported tool. Order matters only for
// detection ambiguity; put more specific formats first.
var importerRegistry = []Importer{
	k6Importer{},
	vegetaImporter{},
}

// ImportRun runs auto-detection over data and returns the parsed SavedRun.
// name is a fallback display name (e.g. the source filename) used when the
// adapter cannot derive a better one.
func ImportRun(name string, data []byte) (model.SavedRun, error) {
	if len(strings.TrimSpace(string(data))) == 0 {
		return model.SavedRun{}, fmt.Errorf("import file is empty")
	}
	for _, imp := range importerRegistry {
		if imp.Detect(data) {
			run, err := imp.Parse(data)
			if err != nil {
				return model.SavedRun{}, fmt.Errorf("%s import failed: %w", imp.Name(), err)
			}
			if strings.TrimSpace(run.Name) == "" {
				run.Name = name
			}
			return run, nil
		}
	}
	tools := make([]string, 0, len(importerRegistry))
	for _, imp := range importerRegistry {
		tools = append(tools, imp.Name())
	}
	return model.SavedRun{}, fmt.Errorf("unrecognised result format (supported: %s)", strings.Join(tools, ", "))
}

// ── Shared helpers used by adapters ──────────────────────────────────────

// statusBucket mirrors the engine's internal classification (which is
// unexported) so importers can tally requests into the same categories the
// live engine reports.
type statusBucket int

const (
	bucketSuccess statusBucket = iota
	bucketClientErr
	bucketRateLimit
	bucketServerErr
	bucketNetworkErr
	bucketCount
)

// bucketForStatus classifies an HTTP status code the same way the engine does,
// so imported runs colour-match native ones. status 0 means a transport-level
// failure (timeout, connection refused, ...).
func bucketForStatus(status int) statusBucket {
	switch {
	case status == 0:
		return bucketNetworkErr
	case status == 429:
		return bucketRateLimit
	case status >= 500:
		return bucketServerErr
	case status >= 400:
		return bucketClientErr
	default:
		return bucketSuccess
	}
}

// statusIsSuccess reports whether a code counts as a successful response.
func statusIsSuccess(status int) bool {
	return status >= 100 && status < 400
}

// percentile returns the p-th percentile (0..100) of values using linear
// interpolation. values need not be sorted. Returns 0 for empty input.
func percentile(values []float64, p float64) float64 {
	if len(values) == 0 {
		return 0
	}
	s := make([]float64, len(values))
	copy(s, values)
	sort.Float64s(s)
	if len(s) == 1 {
		return s[0]
	}
	rank := (p / 100) * float64(len(s)-1)
	lo := int(math.Floor(rank))
	hi := int(math.Ceil(rank))
	if lo == hi {
		return s[lo]
	}
	frac := rank - float64(lo)
	return s[lo] + (s[hi]-s[lo])*frac
}

// flatHistory synthesises a per-second time-series at constant values, used by
// adapters that only have aggregate data (no real time-series). It fills the
// whole [0, durationSecs] span so the chart renders as a clean steady band
// rather than two lonely points at the extremes. This is an honest depiction:
// a summary only tells us the averages, shown here sustained over the run.
func flatHistory(durationSecs int, rps, okRps, p50, p95, p99 float64) []model.Sample {
	n := durationSecs
	if n < 1 {
		n = 1
	}
	if n > 3600 {
		n = 3600 // guard against absurd durations
	}
	out := make([]model.Sample, 0, n+1)
	for s := 0; s <= n; s++ {
		out = append(out, model.Sample{
			T:         float64(s),
			TickRps:   rps,
			TickRpsOk: okRps,
			P50:       p50,
			P95:       p95,
			P99:       p99,
		})
	}
	return out
}

// errorBuckets accumulates per-bucket counts and derives an engine.Metrics
// summary. Adapters feed it one classified request at a time.
type metricsAccumulator struct {
	buckets   [bucketCount]int
	latencies []float64
}

func (a *metricsAccumulator) add(status int, latencyMs float64) {
	a.buckets[bucketForStatus(status)]++
	a.latencies = append(a.latencies, latencyMs)
}

// summary builds an engine.Metrics from the accumulated requests. elapsedSecs
// is the wall-clock span of the test (used to derive RPS).
func (a *metricsAccumulator) summary(elapsedSecs float64) engine.Metrics {
	total := 0
	for _, c := range a.buckets {
		total += c
	}
	success := a.buckets[bucketSuccess]
	errors := total - success
	rps := 0.0
	if elapsedSecs > 0 {
		rps = float64(total) / elapsedSecs
	}
	errRate := 0.0
	if total > 0 {
		errRate = float64(errors) / float64(total)
	}
	return engine.Metrics{
		ElapsedSecs:   elapsedSecs,
		TotalRequests: total,
		Successful:    success,
		ClientErrors:  a.buckets[bucketClientErr],
		RateLimited:   a.buckets[bucketRateLimit],
		ServerErrors:  a.buckets[bucketServerErr],
		NetworkErrors: a.buckets[bucketNetworkErr],
		Errors:        errors,
		RPS:           rps,
		ErrorRate:     errRate,
		P50Ms:         percentile(a.latencies, 50),
		P95Ms:         percentile(a.latencies, 95),
		P99Ms:         percentile(a.latencies, 99),
		Running:       false,
	}
}
