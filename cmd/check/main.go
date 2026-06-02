// cmd/check is a throwaway smoke test for the engine. It runs a short load
// test against a target URL and prints every snapshot. Not built into the
// Wails binary — invoke with `go run ./cmd/check [url] [concurrency] [secs]`.
package main

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"loadcell/engine"
)

func main() {
	url := "https://example.com"
	concurrency := 10
	secs := 5
	rampUpSecs := 0
	if len(os.Args) > 1 {
		url = os.Args[1]
	}
	if len(os.Args) > 2 {
		if n, err := strconv.Atoi(os.Args[2]); err == nil {
			concurrency = n
		}
	}
	if len(os.Args) > 3 {
		if n, err := strconv.Atoi(os.Args[3]); err == nil {
			secs = n
		}
	}
	if len(os.Args) > 4 {
		if n, err := strconv.Atoi(os.Args[4]); err == nil {
			rampUpSecs = n
		}
	}

	eng := engine.New()
	done := make(chan struct{})
	eng.OnMetrics(func(m engine.Metrics) {
		fmt.Printf("[%4.1fs] conc=%3d req=%d 2xx=%d 4xx=%d 429=%d 5xx=%d net=%d rps=%7.1f p50=%5.1fms p95=%5.1fms p99=%5.1fms running=%v\n",
			m.ElapsedSecs, m.CurrentConcurrency, m.TotalRequests,
			m.Successful, m.ClientErrors, m.RateLimited, m.ServerErrors, m.NetworkErrors,
			m.RPS, m.P50Ms, m.P95Ms, m.P99Ms, m.Running)
		if !m.Running {
			select {
			case <-done:
			default:
				close(done)
			}
		}
	})

	cfg := engine.Config{URL: url, Concurrency: concurrency, DurationSecs: secs}
	if rampUpSecs > 0 {
		cfg.Mode = engine.ModeRamp
		cfg.RampUpSecs = rampUpSecs
	}
	if err := eng.Start(cfg); err != nil {
		fmt.Fprintln(os.Stderr, "start failed:", err)
		os.Exit(1)
	}

	select {
	case <-done:
	case <-time.After(time.Duration(secs+10) * time.Second):
		fmt.Fprintln(os.Stderr, "timed out waiting for final snapshot")
		os.Exit(1)
	}
}
