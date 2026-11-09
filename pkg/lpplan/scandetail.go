package lpplan

import (
	"context"
	"math"

	"github.com/dorman99/plutus/pkg/lpscan"
)

// ScanDetailer adds lpplan detail to lpscan rows: at DetailFull the same
// plan /lp-plan would print (balanced risk, no amount), at DetailBasic the
// token info only (one API call).
func ScanDetailer(src Source) lpscan.Detailer {
	return scanDetailer{src: src, pl: New(src)}
}

type scanDetailer struct {
	src Source
	pl  *Planner
}

func (d scanDetailer) Detail(ctx context.Context, coin string, level lpscan.DetailLevel) (*lpscan.Detail, error) {
	if level == lpscan.DetailBasic {
		info, err := d.src.TokenInfo(ctx, coin)
		if err != nil {
			return nil, err
		}
		return detailFromSummary(summary(nil, info, coin), lpscan.DetailBasic), nil
	}
	plan, err := d.pl.Plan(ctx, Params{CA: coin})
	if err != nil {
		return nil, err
	}
	return DetailFromPlan(plan), nil
}

func detailFromSummary(t TokenSummary, level lpscan.DetailLevel) *lpscan.Detail {
	return &lpscan.Detail{
		Level:           level,
		GTScore:         t.GTScore,
		HoldersCount:    t.HoldersCount,
		Top10Pct:        t.Top10Pct,
		IsHoneypot:      t.IsHoneypot,
		MintAuthority:   t.MintAuthority,
		FreezeAuthority: t.FreezeAuthority,
		Warnings:        []string{},
	}
}

// DetailFromPlan condenses a Plan into a scan row's detail. It is pure.
// net_7d_pct = est_fee_7d_pct + il_low_pct; triggered_exits lists the exit
// signals already true at plan time (TURNOVER_DROP, DAILY_MOVE).
func DetailFromPlan(p *Plan) *lpscan.Detail {
	d := detailFromSummary(p.Token, lpscan.DetailFull)
	d.Warnings = append(d.Warnings, p.Warnings...)
	pd := &lpscan.PlanDetail{
		Verdict:        string(p.Verdict.Decision),
		Reasons:        []lpscan.PlanReason{},
		ChosenPool:     p.ChosenPool,
		ExitSignals:    []string{},
		TriggeredExits: []string{},
	}
	for _, r := range p.Verdict.Reasons {
		pd.Reasons = append(pd.Reasons, lpscan.PlanReason{Code: r.Code, Tier: string(r.Tier), Message: r.Message})
	}

	var chosen *PoolRow
	for i := range p.Pools {
		if p.Pools[i].Chosen {
			chosen = &p.Pools[i]
		}
	}
	if chosen != nil {
		pd.ChosenPair, pd.ChosenDex = chosen.Pair, chosen.Dex
		pd.ChosenFeePct, pd.ChosenLiquidityUSD = chosen.FeePct, ptr(chosen.LiquidityUSD)
	}

	if s := p.Strategy; s != nil {
		pd.Mode, pd.Sigma7dPct = s.Mode, s.Sigma7dPct
		pd.EstFeeYieldD, pd.EstFee7dPct = s.EstFeeYieldD, s.EstFee7dPct
		pd.ILLowPct, pd.ILHighPct = s.ILLowPct, s.ILHighPct
		if s.EstFee7dPct != nil && s.ILLowPct != nil {
			pd.Net7dPct = ptr(*s.EstFee7dPct + *s.ILLowPct)
		}
	}
	for _, sig := range p.ExitSignals {
		pd.ExitSignals = append(pd.ExitSignals, sig.Message)
		if chosen == nil || sig.Value == nil {
			continue
		}
		switch sig.Code {
		case "TURNOVER_DROP":
			if chosen.TurnoverD < *sig.Value {
				pd.TriggeredExits = append(pd.TriggeredExits, sig.Code)
			}
		case "DAILY_MOVE":
			if pc := chosen.PriceChange24hPct; pc != nil && math.Abs(*pc) > *sig.Value {
				pd.TriggeredExits = append(pd.TriggeredExits, sig.Code)
			}
		}
	}
	d.Plan = pd
	return d
}
