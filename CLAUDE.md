# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

LoadCell is a desktop HTTP load-testing app: Go backend (engine + persistence) glued to a Svelte/D3 frontend via [Wails v2.12](https://wails.io). The Go and frontend pieces live in one repo and ship as a single native binary that embeds `frontend/dist` (see `//go:embed all:frontend/dist` in `main.go`).

## Commands

Wails CLI must be installed once: `go install github.com/wailsapp/wails/v2/cmd/wails@latest` (v2.10+ is required; older v2.x only links webkit2gtk-4.0).

- `wails dev` — hot-reloading dev mode (Vite watches the frontend, Go side rebuilds on save).
- `wails build` — produce a redistributable in `build/bin/`. CI uses `wails build -platform <darwin/universal|linux/amd64|windows/amd64> -clean -trimpath`.
- `go run ./cmd/check <url> <concurrency> <secs> [rampUpSecs]` — drive the engine from the CLI; prints every metrics snapshot. Useful for engine-only changes without booting the WebView.
- `cd frontend && npm run check` — `svelte-check` typecheck over the Svelte/TS sources.
- `cd frontend && npm run build` / `npm run dev` — invoked by Wails via `wails.json`; rarely run by hand.
- `cd tools/echo-server && node server.js` — local Express target on `:4466` with nine routes that mix status codes and latency profiles; the README table lists them.

Linux dev requires `libgtk-3-dev libwebkit2gtk-4.1-dev build-essential pkg-config`. The release workflow runs on `ubuntu-latest` (24.04) and links webkit2gtk-4.1; end users also need `libwebkit2gtk-4.1-0` on the host at runtime.

No automated test suite exists. `cmd/check` is the smoke test for the engine.

## Architecture

### Go ⇄ Svelte boundary
The Go side exposes its API through `App` (`app.go`), which is `Bind`-ed in `main.go`. Wails auto-generates TypeScript bindings into `frontend/wailsjs/go/main/App.js` and shared models into `frontend/wailsjs/go/models.ts` — the Svelte app imports those directly. Anything you add to `App` becomes a callable from `App.svelte`; anything emitted via `runtime.EventsEmit(ctx, "...", payload)` is consumed via `EventsOn`. Today there is exactly **one** event channel: `"metrics"` (live snapshots from the engine). Everything else is request/response RPC.

Files in the root Go package (`package main`):
- `app.go` — Wails-bound surface: `StartTest`, `StopTest`, `ListRequests`, `SaveRequest`, `DeleteRequest`, `ListRuns`, `SaveRun`, `DeleteRun`, `SendSample`.
- `requests.go` — JSON-backed CRUD for `SavedRequest` (the Postman-style sidebar).
- `runs.go` — JSON-backed CRUD for `SavedRun` (every completed test snapshot + history).
- `sample.go` — single-shot "Send" handler for the request builder; caps body at 1 MiB.

Both stores live under `os.UserConfigDir()/loadcell/` (`requests.json`, `runs.json`), use `sync.Mutex` + atomic tmp+rename writes, and assign 12-char base64 IDs via `newID()` in `requests.go`.

### Engine (`engine/engine.go`)
Standalone package — no Wails dependency, so `cmd/check` and tests can drive it. Single `Engine` runs **one test at a time**; `Start` errors if already running.

The shape:
- N worker goroutines fire HTTP requests in tight loops against `Config.URL`.
- Each result (latency + status bucket) goes over a buffered channel to **one** aggregator goroutine that owns all mutable state (counters + latency slice). This is the only goroutine that mutates metrics state — don't introduce locking elsewhere.
- Aggregator emits a `Metrics` snapshot every 500 ms via the `onMetrics` callback; the final snapshot fires with `Running: false` **after** engine state is reset so a UI that auto-restarts on completion doesn't see "a test is already running".
- Responses are bucketed: 2xx/3xx (success), 4xx-not-429 (client), 429 (rate-limited), 5xx (server), transport-level (network).

Three load profiles — switch on `Config.Mode`:
- `"constant"` — pre-spawn `Concurrency` workers.
- `"ramp"` — spawn 1, then evenly add the remaining `Concurrency-1` over `RampUpSecs` via a wg-tracked ticker goroutine.
- `"curve"` — pre-spawn `ceil(peakUsers * (1+Noise))` workers; each worker has a slot index and only runs when `slot < desired`. A 100 ms scheduler interpolates `desired` along `Config.Curve` (linear / exponential / step segments) and applies `±Noise` jitter. `evalCurve` + `interpolateSegment` define the curve math.

Currently `aggregate` sorts the entire latency slice on every tick to compute p50/p95/p99 — fine for short runs, would need a streaming histogram (e.g. HDR) for long runs. The comment in `buildMetrics` flags this.

### Frontend (`frontend/src/App.svelte`)
~5k-line single-file Svelte component holding everything: curve editor, D3 throughput chart, saved-requests sidebar, run history, info sheet, and the request/response editor. There is no router or component library — `NumberFlow.svelte` is the only sibling component. Re-organizing this file is a major undertaking; small changes should stay scoped.

The frontend builds its own per-tick `Sample` history (`runs.go` mirrors this shape) by diffing successive `Metrics` snapshots; saved runs preserve that derived history so charts re-hydrate without re-running.

## Release

`.github/workflows/release.yml` is the only ship path — manual `workflow_dispatch` with a version tag, builds all three platforms, uploads three artifacts (`loadcell-darwin-universal.tar.gz`, `loadcell-linux-amd64.tar.gz`, `loadcell-windows-amd64.zip`), and publishes a GitHub release. macOS binaries are **not** notarized; the README documents the `xattr -dr com.apple.quarantine` workaround.

`.github/workflows/pages.yml` publishes the static landing page in `site/` to GitHub Pages.
