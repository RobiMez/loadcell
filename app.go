package main

import (
	"context"
	"errors"

	"loadcell/engine"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// App is the Wails-bound surface for the frontend.
type App struct {
	ctx      context.Context
	engine   *engine.Engine
	requests *requestStore
	runs     *runStore
}

// NewApp creates a new App application struct.
func NewApp() *App {
	return &App{
		engine:   engine.New(),
		requests: newRequestStore(),
		runs:     newRunStore(),
	}
}

// startup is called when the app starts. The context is saved so we can call
// the Wails runtime methods.
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
}

// StartTest validates cfg, starts the engine, and wires live metrics to a
// Wails event named "metrics". Returns an error if cfg is invalid or a test
// is already running.
func (a *App) StartTest(cfg engine.Config) error {
	if a.ctx == nil {
		return errors.New("app not started")
	}
	a.engine.OnMetrics(func(m engine.Metrics) {
		runtime.EventsEmit(a.ctx, "metrics", m)
	})
	return a.engine.Start(cfg)
}

// StopTest cancels a running test. No-op if nothing is running.
func (a *App) StopTest() {
	a.engine.Stop()
}

// ListRequests returns saved request templates, newest first.
func (a *App) ListRequests() ([]SavedRequest, error) {
	return a.requests.List()
}

// SaveRequest creates (when ID is empty) or updates the given request, and
// returns the persisted version with timestamps and ID populated.
func (a *App) SaveRequest(req SavedRequest) (SavedRequest, error) {
	return a.requests.Upsert(req)
}

// DeleteRequest removes a saved request by ID. Missing ID is not an error.
func (a *App) DeleteRequest(id string) error {
	if id == "" {
		return errors.New("id is required")
	}
	return a.requests.Delete(id)
}

// ListRuns returns persisted load-test runs, newest first.
func (a *App) ListRuns() ([]SavedRun, error) {
	return a.runs.List()
}

// SaveRun persists a completed run and returns it with id/startedAt filled.
func (a *App) SaveRun(r SavedRun) (SavedRun, error) {
	return a.runs.Save(r)
}

// DeleteRun removes a persisted run by ID.
func (a *App) DeleteRun(id string) error {
	if id == "" {
		return errors.New("id is required")
	}
	return a.runs.Delete(id)
}
