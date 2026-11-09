// Package lpscan scans Robinhood Chain pools and ranks coins worth reviewing
// for a liquidity position.
//
// It only produces data: Scan and Rank return a *Result and never print.
// Rendering lives in package present, so any UI (CLI, MCP, web, bot) can
// reuse the same scan.
package lpscan

import (
	"fmt"
	"math"
	"time"
)

// Network is the GeckoTerminal network id scanned in stage 1.
const Network = "robinhood"

// Window is the time range used for volume, price change and transactions.
type Window string

const (
	Window5m  Window = "5m"
	Window1h  Window = "1h"
	Window6h  Window = "6h"
	Window24h Window = "24h"
)

var windows = map[Window]struct {
	key string
	dur time.Duration
}{
	Window5m:  {"m5", 5 * time.Minute},
	Window1h:  {"h1", time.Hour},
	Window6h:  {"h6", 6 * time.Hour},
	Window24h: {"h24", 24 * time.Hour},
}

// Valid reports whether w is one of the supported windows.
func (w Window) Valid() bool { _, ok := windows[w]; return ok }

// Duration is the window length.
func (w Window) Duration() time.Duration { return windows[w].dur }

// apiKey is the GeckoTerminal map key for w ("6h" -> "h6").
func (w Window) apiKey() string { return windows[w].key }

// Params are the scan inputs.
type Params struct {
	MaxMcap      float64 `json:"max_mcap"`
	Window       Window  `json:"window"`
	MinLiquidity float64 `json:"min_liquidity"`
	MinVolume    float64 `json:"min_volume"`
	Top          int     `json:"top"`
	Pages        int     `json:"pages"`
}

const (
	MaxTop   = 50
	MaxPages = 10
)

// DefaultParams returns the stage-1 defaults.
func DefaultParams() Params {
	return Params{
		MaxMcap:      5_000_000,
		Window:       Window6h,
		MinLiquidity: 50_000,
		MinVolume:    25_000,
		Top:          5,
		Pages:        3,
	}
}

// ParamError is an invalid parameter. Field uses the CLI flag spelling.
type ParamError struct {
	Field string
	Msg   string
}

func (e *ParamError) Error() string { return fmt.Sprintf("--%s %s", e.Field, e.Msg) }

// Validate returns a *ParamError for the first invalid field, or nil.
func (p Params) Validate() error {
	bad := func(v float64) bool { return math.IsNaN(v) || math.IsInf(v, 0) }
	switch {
	case bad(p.MaxMcap) || p.MaxMcap <= 0:
		return &ParamError{"max-mcap", "must be > 0"}
	case !p.Window.Valid():
		return &ParamError{"window", "must be one of 5m, 1h, 6h, 24h"}
	case bad(p.MinLiquidity) || p.MinLiquidity < 0:
		return &ParamError{"min-liquidity", "must be >= 0"}
	case bad(p.MinVolume) || p.MinVolume < 0:
		return &ParamError{"min-volume", "must be >= 0"}
	case p.Top < 1 || p.Top > MaxTop:
		return &ParamError{"top", fmt.Sprintf("must be between 1 and %d", MaxTop)}
	case p.Pages < 1 || p.Pages > MaxPages:
		return &ParamError{"pages", fmt.Sprintf("must be between 1 and %d", MaxPages)}
	}
	return nil
}
