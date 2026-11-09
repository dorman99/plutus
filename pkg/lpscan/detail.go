package lpscan

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// Detailer adds per-coin detail to the top rows of a scan. lpscan only
// defines the contract: lpplan.ScanDetailer implements it with the full
// lpplan analysis, so a scan row and /lp-plan always agree.
type Detailer interface {
	Detail(ctx context.Context, coin string, level DetailLevel) (*Detail, error)
}

// Pick guards: a coin the tool cannot vouch for is listed but never ★.
const (
	PickMaxTop10Pct = 80.0 // top-10 holders above: not pickable (looks like a bundle)
)

// Detail is a coin's token info and, at DetailFull, its plan summary.
// Info fields are null when GeckoTerminal did not send them.
type Detail struct {
	Level           DetailLevel `json:"level"`
	GTScore         *float64    `json:"gt_score"`
	HoldersCount    *int        `json:"holders_count"`
	Top10Pct        *float64    `json:"top10_pct"`
	IsHoneypot      string      `json:"is_honeypot"` // yes | no | unknown
	MintAuthority   *string     `json:"mint_authority"`
	FreezeAuthority *string     `json:"freeze_authority"`
	Plan            *PlanDetail `json:"plan"` // null at DetailBasic
	Warnings        []string    `json:"warnings"`
}

// PlanDetail is the part of an lpplan brief shown in a scan row. Percent
// fields are percentages; IL is negative for a loss versus holding.
type PlanDetail struct {
	Verdict            string       `json:"verdict"` // go | wait | avoid
	Reasons            []PlanReason `json:"reasons"`
	ChosenPool         *string      `json:"chosen_pool"`
	ChosenPair         string       `json:"chosen_pair"`
	ChosenDex          string       `json:"chosen_dex"`
	ChosenFeePct       *float64     `json:"chosen_fee_pct"`
	ChosenLiquidityUSD *float64     `json:"chosen_liquidity_usd"`
	Mode               string       `json:"mode"` // concentrated | full_range | "" (no strategy)
	Sigma7dPct         *float64     `json:"sigma_7d_pct"`
	EstFeeYieldD       *float64     `json:"est_fee_yield_d"`
	EstFee7dPct        *float64     `json:"est_fee_7d_pct"`
	ILLowPct           *float64     `json:"il_low_pct"`
	ILHighPct          *float64     `json:"il_high_pct"`
	Net7dPct           *float64     `json:"net_7d_pct"` // est_fee_7d_pct + il_low_pct
	ExitSignals        []string     `json:"exit_signals"`
	TriggeredExits     []string     `json:"triggered_exits"` // exit signal codes already true at scan time
}

// PlanReason is one verdict reason.
type PlanReason struct {
	Code    string `json:"code"`
	Tier    string `json:"tier"`
	Message string `json:"message"`
}

// enrich adds detail to every row, full for the first MaxFullDetail rows
// (when asked) and basic after that. A failing coin or running out of time
// is a warning, never a failed scan.
func enrich(ctx context.Context, d Detailer, res *Result) {
	level := res.Params.Detail
	for i := range res.Results {
		r := &res.Results[i]
		if ctx.Err() != nil {
			res.Warnings = append(res.Warnings, fmt.Sprintf("detail stopped at %s: scan timed out; it and the coins below have no detail", r.CoinSymbol))
			return
		}
		lv := level
		if lv == DetailFull && i >= MaxFullDetail {
			if i == MaxFullDetail {
				res.Warnings = append(res.Warnings, fmt.Sprintf("full detail is capped at %d coins; the rest get basic detail", MaxFullDetail))
			}
			lv = DetailBasic
		}
		det, err := d.Detail(ctx, r.CoinAddress, lv)
		if err != nil {
			res.Warnings = append(res.Warnings, fmt.Sprintf("%s (%s): detail unavailable: %v", r.CoinSymbol, r.CoinAddress, err))
			continue
		}
		for _, w := range det.Warnings {
			res.Warnings = append(res.Warnings, fmt.Sprintf("%s: %s", r.CoinSymbol, w))
		}
		r.Detail = det
	}
}

// Finalize applies the detail-based steps to ranked rows: the --max-top10
// filter, the final order (verdict, then net 7d), ranks and the pick. Rank
// calls it; Scan calls it again after adding detail. It is pure.
func Finalize(res *Result) {
	p := res.Params
	if p.MaxTop10 != nil {
		kept := res.Results[:0]
		for _, r := range res.Results {
			if r.Detail != nil && r.Detail.Top10Pct != nil && *r.Detail.Top10Pct > *p.MaxTop10 {
				continue
			}
			kept = append(kept, r)
		}
		res.Results = kept
	}

	rows := res.Results
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if va, vb := verdictOrder(a), verdictOrder(b); va != vb {
			return va < vb
		}
		na, nb := net7d(a), net7d(b)
		if (na == nil) != (nb == nil) {
			return na != nil
		}
		if na != nil && *na != *nb {
			return *na > *nb
		}
		return better(a, b)
	})
	for i := range rows {
		rows[i].Rank = i + 1
	}
	res.Pick = pick(rows)
}

func verdictOrder(r Row) int {
	if r.Detail == nil || r.Detail.Plan == nil {
		return 3
	}
	switch r.Detail.Plan.Verdict {
	case "go":
		return 0
	case "wait":
		return 1
	}
	return 2
}

func net7d(r Row) *float64 {
	if r.Detail == nil || r.Detail.Plan == nil {
		return nil
	}
	return r.Detail.Plan.Net7dPct
}

// pickable is false for a coin with a bundle-like holder share or an
// unknown (or positive) honeypot check. No detail: nothing to rule out.
func pickable(r Row) bool {
	d := r.Detail
	if d == nil {
		return true
	}
	if d.IsHoneypot != "no" {
		return false
	}
	return d.Top10Pct == nil || *d.Top10Pct <= PickMaxTop10Pct
}

// pick chooses the coin to review first. With plans: the GO coin with the
// highest net 7d that passes the pick guards. Without: graded decent or
// strong with no SKEW, SWING or FDV flag, highest est. daily fee yield (or
// highest score when no eligible row has a known fee).
func pick(rows []Row) *Pick {
	for _, r := range rows {
		if r.Detail != nil && r.Detail.Plan != nil {
			return pickByPlan(rows)
		}
	}

	var byYield, byScore *Row
	for i := range rows {
		r := &rows[i]
		if r.Grade != GradeDecent && r.Grade != GradeStrong {
			continue
		}
		if r.HasFlag(FlagSkew) || r.HasFlag(FlagSwing) || r.HasFlag(FlagFDV) || !pickable(*r) {
			continue
		}
		if byScore == nil {
			byScore = r // rows are already sorted by score
		}
		if r.EstFeeYieldD != nil && (byYield == nil || *r.EstFeeYieldD > *byYield.EstFeeYieldD) {
			byYield = r
		}
	}
	switch {
	case byYield != nil:
		return &Pick{byYield.Rank, byYield.CoinSymbol, byYield.CoinAddress,
			"highest est. yield/d among unflagged coins"}
	case byScore != nil:
		return &Pick{byScore.Rank, byScore.CoinSymbol, byScore.CoinAddress,
			"highest score among unflagged coins (fee tier unknown)"}
	}
	return nil
}

// pickByPlan relies on Finalize's order: GO rows first, by net 7d.
func pickByPlan(rows []Row) *Pick {
	var skipped []string
	for _, r := range rows {
		if verdictOrder(r) != 0 {
			break
		}
		if !pickable(r) {
			skipped = append(skipped, r.CoinSymbol)
			continue
		}
		reason := "highest net 7d (est. fees minus IL) among GO coins"
		if net7d(r) == nil {
			reason = "first GO coin (net 7d unknown)"
		}
		if len(skipped) > 0 {
			reason += fmt.Sprintf("; skipped %s (top 10 holders above %.0f%% or honeypot not confirmed no)", strings.Join(skipped, ", "), PickMaxTop10Pct)
		}
		return &Pick{r.Rank, r.CoinSymbol, r.CoinAddress, reason}
	}
	return nil
}
