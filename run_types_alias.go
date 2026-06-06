package main

import "loadcell/model"

// Backward-compatible aliases so the Wails main.SavedRun surface stays stable
// while run types live in loadcell/model.
type Sample = model.Sample
type RunConfig = model.RunConfig
type SavedRun = model.SavedRun
