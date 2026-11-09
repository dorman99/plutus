// Package lpplan builds an LP decision brief for one Robinhood Chain coin:
// which pool to provide liquidity in, a go / wait / avoid verdict with every
// reason, and (unless avoid) a range, fee and IL estimate, deposit split and
// exit signals.
//
// It only produces data: Plan and Analyze return a *Plan and never print.
// Rendering lives in package present.
package lpplan

import (
	"fmt"
	"math"
	"regexp"
	"strings"

	"github.com/dorman99/plutus/pkg/lpscan"
)

// Network is the GeckoTerminal network id.
const Network = lpscan.Network

// Risk sets how wide the suggested range is.
type Risk string

const (
	RiskSafe       Risk = "safe"
	RiskBalanced   Risk = "balanced"
	RiskAggressive Risk = "aggressive"
)

// RangeK is the number of 7-day standard deviations each side of the price.
var RangeK = map[Risk]float64{
	RiskSafe:       2,
	RiskBalanced:   1,
	RiskAggressive: 0.5,
}

// Params are the plan inputs.
type Params struct {
	CA     string   `json:"ca"`
	Amount *float64 `json:"amount_usd"` // optional deposit in USD
	Risk   Risk     `json:"risk"`
}

// ParamError is an invalid argument. Field uses the CLI spelling.
type ParamError struct {
	Field string
	Msg   string
}

func (e *ParamError) Error() string {
	if e.Field == "ca" {
		return "CA " + e.Msg
	}
	return fmt.Sprintf("--%s %s", e.Field, e.Msg)
}

var caRe = regexp.MustCompile(`^0x[0-9a-fA-F]{40}$`)

// Normalize lowercases the CA and defaults the risk.
func (p Params) Normalize() Params {
	p.CA = strings.ToLower(strings.TrimSpace(p.CA))
	if p.Risk == "" {
		p.Risk = RiskBalanced
	}
	return p
}

// Validate returns a *ParamError for the first invalid field, or nil.
func (p Params) Validate() error {
	switch {
	case p.CA == "":
		return &ParamError{"ca", "is required"}
	case !caRe.MatchString(p.CA):
		return &ParamError{"ca", fmt.Sprintf("%q is not a contract address (0x + 40 hex)", p.CA)}
	}
	if sym, major := lpscan.Majors[strings.ToLower(p.CA)]; major {
		return &ParamError{"ca", fmt.Sprintf("%s is %s, a major token, not an LP coin", p.CA, sym)}
	}
	if p.Amount != nil && (math.IsNaN(*p.Amount) || math.IsInf(*p.Amount, 0) || *p.Amount <= 0) {
		return &ParamError{"amount", "must be > 0"}
	}
	if _, ok := RangeK[p.Risk]; !ok {
		return &ParamError{"risk", "must be one of safe, balanced, aggressive"}
	}
	return nil
}
