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

// FlowRunStep records one step of a compound flow as it existed at the
// time of the run. We snapshot the resolved name/method/url so the run
// history stays meaningful even after the underlying saved request is
// edited or deleted.
type FlowRunStep struct {
	Name   string `json:"name"`
	Method string `json:"method"`
	URL    string `json:"url"`
}

// RunConfig captures the slice of engine.Config the user picked for a run.
// Stored alongside the run so saved runs can be re-launched or inspected
// even if engine.Config grows new fields later.
//
// FlowID + FlowName + Steps are populated when the run was a compound
// flow; for single-request runs they're omitted via omitempty.
type RunConfig struct {
	Mode         string              `json:"mode"`
	Concurrency  int                 `json:"concurrency"`
	DurationSecs int                 `json:"durationSecs"`
	Curve        []engine.CurvePoint `json:"curve,omitempty"`
	Noise        float64             `json:"noise,omitempty"`
	FlowID       string              `json:"flowId,omitempty"`
	FlowName     string              `json:"flowName,omitempty"`
	Steps        []FlowRunStep       `json:"steps,omitempty"`
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
