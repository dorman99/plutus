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

// Source selects which pool listings are fetched.
type Source string

const (
	SourceTrending Source = "trending" // trending_pools only
	SourceAll      Source = "all"      // pools, trending_pools, new_pools
)

// Kind is what a coin is: a tokenized stock or anything else (a meme coin).
type Kind string

const (
	KindMeme  Kind = "meme"
	KindStock Kind = "stock"
	KindAll   Kind = "all" // filter value only
)

// DetailLevel is how much per-coin detail a Detailer adds to the top rows.
type DetailLevel string

const (
	DetailBasic DetailLevel = "basic" // token info: holders, top 10, honeypot, GT score
	DetailFull  DetailLevel = "full"  // the full lpplan analysis
)

// Params are the scan inputs. Detail and MaxTop10 only apply when the
// Scanner has a Detailer.
type Params struct {
	MinMcap      float64     `json:"min_mcap"`
	MaxMcap      float64     `json:"max_mcap"`
	Window       Window      `json:"window"`
	MinLiquidity float64     `json:"min_liquidity"`
	MinVolume    float64     `json:"min_volume"`
	Top          int         `json:"top"`
	Pages        int         `json:"pages"`
	Source       Source      `json:"source"`
	Kind         Kind        `json:"kind"`
	Detail       DetailLevel `json:"detail"`
	MaxTop10     *float64    `json:"max_top10"` // drop coins whose top-10 holders own more (%); nil = off
}

const (
	MaxTop        = 50
	MaxPages      = 10
	MaxFullDetail = 10 // rows past this get basic detail: each full plan costs ~4 API calls
)

// DefaultParams returns the defaults: trending meme coins between $1M and
// $20M market cap, top 5 with full detail.
func DefaultParams() Params {
	return Params{
		MinMcap:      1_000_000,
		MaxMcap:      20_000_000,
		Window:       Window6h,
		MinLiquidity: 50_000,
		MinVolume:    25_000,
		Top:          5,
		Pages:        3,
		Source:       SourceTrending,
		Kind:         KindMeme,
		Detail:       DetailFull,
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
	case bad(p.MinMcap) || p.MinMcap < 0:
		return &ParamError{"min-mcap", "must be >= 0"}
	case bad(p.MaxMcap) || p.MaxMcap <= 0:
		return &ParamError{"max-mcap", "must be > 0"}
	case p.MinMcap > p.MaxMcap:
		return &ParamError{"min-mcap", "must not be above --max-mcap"}
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
	case p.Source != SourceTrending && p.Source != SourceAll:
		return &ParamError{"source", "must be one of trending, all"}
	case p.Kind != KindMeme && p.Kind != KindStock && p.Kind != KindAll:
		return &ParamError{"kind", "must be one of meme, stock, all"}
	case p.Detail != DetailBasic && p.Detail != DetailFull:
		return &ParamError{"detail", "must be one of basic, full"}
	case p.MaxTop10 != nil && (bad(*p.MaxTop10) || *p.MaxTop10 <= 0 || *p.MaxTop10 > 100):
		return &ParamError{"max-top10", "must be between 0 and 100"}
	}
	return nil
}
