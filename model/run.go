package model

import "loadcell/engine"

// Sample is one tick of derived per-second telemetry. Mirrors the Sample
// type the frontend builds from successive Metrics snapshots.
type Sample struct {
	T         float64 `json:"t"`
	TickRps   float64 `json:"tickRps"`
	TickRpsOk float64 `json:"tickRpsOk"`
	P50       float64 `json:"p50"`
	P95       float64 `json:"p95"`
	P99       float64 `json:"p99"`
	Conc      int     `json:"conc"`
}

// RunConfig captures the slice of engine.Config the user picked for a run.
// Stored alongside the run so saved runs can be re-launched or inspected
// even if engine.Config grows new fields later.
type RunConfig struct {
	Mode         string              `json:"mode"`
	Concurrency  int                 `json:"concurrency"`
	DurationSecs int                 `json:"durationSecs"`
	Curve        []engine.CurvePoint `json:"curve,omitempty"`
	Noise        float64             `json:"noise,omitempty"`
}

// SavedRun is one completed load test, snapshotted at end and persisted.
type SavedRun struct {
	ID        string         `json:"id"`
	StartedAt int64          `json:"startedAt"` // unix millis
	Name      string         `json:"name"`
	Method    string         `json:"method"`
	URL       string         `json:"url"`
	Config    RunConfig      `json:"config"`
	Metrics   engine.Metrics `json:"metrics"`
	History   []Sample       `json:"history"`
}
