package lpplan

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/dorman99/plutus/pkg/geckoterminal"
	"github.com/dorman99/plutus/pkg/lpscan"
)

// Tunable thresholds. Grades, SKEW, SWING and NEW reuse lpscan's.
const (
	MinLiquidityUSD   = 50_000 // chosen pool must be at least this deep
	CandleLimit       = 168    // 7 days of hourly candles
	MinCandles        = 24     // fewer: no volatility estimate
	MaxRangeHighPct   = 60.0   // range wider than this: full range
	MaxConcentration  = 3.0    // cap on the concentrated fee multiplier (other LPs concentrate too)
	FadingShare       = 0.5    // 6h turnover/d below this share of 24h: FADING
	Top10WarnPct      = 80.0   // top-10 holders above: TOP10 note
	TurnoverExitMin   = 0.5    // exit signal: turnover/d below
	LiquidityExitDrop = 0.30   // exit signal: liquidity falls this share
)

// Reason codes.
const (
	ReasonHoneypot   = "HONEYPOT"
	ReasonNoDepth    = "NO_DEPTH"
	ReasonSkew       = "SKEW"
	ReasonWeak       = "WEAK"
	ReasonSuspect    = "SUSPECT"
	ReasonSwing      = "SWING"
	ReasonNew        = "NEW"
	ReasonFading     = "FADING"
	ReasonFeeUnknown = "FEE_UNKNOWN"
	ReasonILGTFees   = "IL_GT_FEES"
	ReasonNoHistory  = "NO_HISTORY"
)

const (
	ModeConcentrated = "concentrated"
	ModeFullRange    = "full_range"
)

const poolURLFormat = "https://www.geckoterminal.com/%s/pools/%s"

// Input is everything Analyze needs. Info is nil when the info call failed;
// Candles are the chosen pool's hourly candles (nil when not fetched).
type Input struct {
	Token   *geckoterminal.TokenDetail
	Info    *geckoterminal.TokenInfo
	Pools   []geckoterminal.Pool
	Candles []geckoterminal.Candle
}

// Rows builds one row per unique pool that trades ca (as base or quote),
// sorted by liquidity, highest first.
func Rows(pools []geckoterminal.Pool, ca string, now time.Time) []PoolRow {
	seen := map[string]bool{}
	rows := []PoolRow{}
	for _, pool := range pools {
		if seen[pool.Address] || (pool.BaseToken.Address != ca && pool.QuoteToken.Address != ca) {
			continue
		}
		seen[pool.Address] = true
		rows = append(rows, row(pool, now))
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].LiquidityUSD != rows[j].LiquidityUSD {
			return rows[i].LiquidityUSD > rows[j].LiquidityUSD
		}
		return rows[i].PoolAddress < rows[j].PoolAddress
	})
	return rows
}

func row(pool geckoterminal.Pool, now time.Time) PoolRow {
	liq := positive(pool.ReserveUSD)
	vol24 := positive(pool.VolumeUSD["h24"])
	vol6 := positive(pool.VolumeUSD["h6"])
	var turnover, turnover6 float64
	if liq > 0 {
		turnover = vol24 / liq
		turnover6 = vol6 / liq * 4
	}

	pc := finite(pool.PriceChangePct["h24"])
	absPC := 0.0
	if pc != nil {
		absPC = math.Abs(*pc)
	}
	score := turnover / (1 + absPC/100)

	flags := []lpscan.Flag{}
	if pool.CreatedAt != nil && now.Sub(*pool.CreatedAt) < lpscan.NewPoolAgeDays*24*time.Hour {
		flags = append(flags, lpscan.FlagNew)
	}
	skew := false
	if t := pool.Transactions["h24"]; t != nil {
		if total := t.Buys + t.Sells; total > 0 {
			skew = float64(t.Buys)/float64(total) > lpscan.SkewShare || float64(t.Sells)/float64(total) > lpscan.SkewShare
		}
	}
	if skew {
		flags = append(flags, lpscan.FlagSkew)
	}
	if absPC > lpscan.SwingPct {
		flags = append(flags, lpscan.FlagSwing)
	}

	fee := lpscan.ParseFeePct(pool.Name)
	var yield *float64
	if fee != nil {
		yield = ptr(turnover * *fee)
	}

	base, quote := lpscan.Symbols(pool)
	return PoolRow{
		PoolAddress:       pool.Address,
		Pair:              base + "/" + quote,
		BaseSymbol:        base,
		QuoteSymbol:       quote,
		Dex:               pool.DexID,
		FeePct:            fee,
		LiquidityUSD:      liq,
		Volume24hUSD:      vol24,
		TurnoverD:         turnover,
		EstFeeYieldD:      yield,
		PriceChange24hPct: pc,
		Score:             score,
		Grade:             lpscan.GradeFor(score, skew),
		Flags:             flags,
		PoolCreatedAt:     pool.CreatedAt,
		URL:               fmt.Sprintf(poolURLFormat, Network, pool.Address),
		turnover6hD:       turnover6,
		skew:              skew,
		basePrice:         finitePositive(pool.BasePriceQuote),
		baseUSD:           finitePositive(pool.BasePriceUSD),
		quoteUSD:          finitePositive(pool.QuotePriceUSD),
	}
}

// Choose returns the index of the pool to build the plan on, or -1: among
// pools with at least MinLiquidityUSD, the highest est. fee yield/d; when
// none has a known fee, the highest turnover/d. Ties go to liquidity.
func Choose(rows []PoolRow) int {
	best, bestFee := -1, false
	for i, r := range rows {
		if r.LiquidityUSD < MinLiquidityUSD {
			continue
		}
		hasFee := r.EstFeeYieldD != nil
		if best < 0 || (hasFee && !bestFee) {
			best, bestFee = i, hasFee
			continue
		}
		if hasFee != bestFee {
			continue
		}
		b := rows[best]
		a, cur := r.TurnoverD, b.TurnoverD
		if hasFee {
			a, cur = *r.EstFeeYieldD, *b.EstFeeYieldD
		}
		if a > cur || (a == cur && r.LiquidityUSD > b.LiquidityUSD) {
			best = i
		}
	}
	return best
}

// Analyze is pure: it turns fetched data into the Plan. now is the plan time.
func Analyze(in Input, p Params, now time.Time) *Plan {
	p = p.Normalize()
	plan := &Plan{
		ScannedAt:   now.UTC(),
		Network:     Network,
		Params:      p,
		Token:       summary(in.Token, in.Info, p.CA),
		Pools:       Rows(in.Pools, p.CA, now),
		ExitSignals: []Signal{},
		Warnings:    []string{},
		Legend:      LegendData(),
	}
	plan.InfoFlags = infoFlags(plan.Token, in.Info != nil)

	var reasons []Reason
	add := func(code string, tier Decision, format string, a ...any) {
		reasons = append(reasons, Reason{code, tier, fmt.Sprintf(format, a...)})
	}
	if plan.Token.IsHoneypot == "yes" {
		add(ReasonHoneypot, DecisionAvoid, "GeckoTerminal marks this token as a honeypot")
	}

	idx := Choose(plan.Pools)
	if idx < 0 {
		add(ReasonNoDepth, DecisionAvoid, "no pool has at least $%.0f liquidity", float64(MinLiquidityUSD))
		plan.Verdict = verdict(reasons)
		return plan
	}
	plan.Pools[idx].Chosen = true
	c := plan.Pools[idx]
	plan.ChosenPool = &c.PoolAddress

	if c.skew {
		add(ReasonSkew, DecisionAvoid, "buys or sells are above %.0f%% of 24h transactions: one-sided trading", lpscan.SkewShare*100)
	}
	if c.Grade == lpscan.GradeWeak {
		add(ReasonWeak, DecisionAvoid, "24h score %.3f is below %.1f: volume too low for the liquidity", c.Score, lpscan.GradeDecentMin)
	}
	if c.Score > lpscan.GradeSuspectMax {
		add(ReasonSuspect, DecisionWait, "24h score %.2f is above %.0f: turnover may not be organic", c.Score, lpscan.GradeSuspectMax)
	}
	if c.PriceChange24hPct != nil && math.Abs(*c.PriceChange24hPct) > lpscan.SwingPct {
		add(ReasonSwing, DecisionWait, "price moved %+.1f%% in 24h (above ±%.0f%%)", *c.PriceChange24hPct, lpscan.SwingPct)
	}
	if hasFlag(c.Flags, lpscan.FlagNew) {
		add(ReasonNew, DecisionWait, "pool is younger than %d days", lpscan.NewPoolAgeDays)
	}
	if c.TurnoverD > 0 && c.turnover6hD < FadingShare*c.TurnoverD {
		add(ReasonFading, DecisionWait, "volume is fading: 6h turnover/d %.2f vs 24h %.2f", c.turnover6hD, c.TurnoverD)
	}
	if c.FeePct == nil {
		add(ReasonFeeUnknown, DecisionWait, "fee tier unknown (not in the pool name): fee yield cannot be estimated")
	}

	sigmaH := sigmaHour(in.Candles)
	if sigmaH == nil {
		add(ReasonNoHistory, DecisionWait, "fewer than %d hourly candles: volatility and IL cannot be estimated", MinCandles)
	}

	s := strategy(c, p, sigmaH)
	// IL_GT_FEES uses the base (unconcentrated) yield: the concentration
	// bonus is the most uncertain number in the plan.
	if s != nil && s.BaseFeeYieldD != nil && s.ILLowPct != nil && s.ILHighPct != nil {
		worst := math.Max(math.Abs(*s.ILLowPct), math.Abs(*s.ILHighPct))
		if base7d := 7 * *s.BaseFeeYieldD; base7d < worst {
			add(ReasonILGTFees, DecisionWait, "base 7-day fees %.2f%% (before concentration) are below the IL at the range edge %.2f%%", base7d, worst)
		}
	}
	if s == nil {
		plan.Warnings = append(plan.Warnings, "chosen pool has no price; no strategy can be built")
	}

	plan.Verdict = verdict(reasons)
	if plan.Verdict.Decision == DecisionAvoid || s == nil {
		return plan
	}
	plan.Strategy = s
	plan.ExitSignals = exitSignals(c, s)
	return plan
}

func verdict(reasons []Reason) Verdict {
	v := Verdict{Decision: DecisionGo, Reasons: []Reason{}}
	for _, r := range reasons {
		v.Reasons = append(v.Reasons, r)
		if r.Tier == DecisionAvoid || (r.Tier == DecisionWait && v.Decision == DecisionGo) {
			v.Decision = r.Tier
		}
	}
	return v
}

func summary(t *geckoterminal.TokenDetail, info *geckoterminal.TokenInfo, ca string) TokenSummary {
	s := TokenSummary{Address: ca, IsHoneypot: "unknown"}
	if t != nil {
		s.Symbol, s.Name = t.Symbol, t.Name
		s.PriceUSD = finitePositive(t.PriceUSD)
		if m := finitePositive(t.MarketCapUSD); m != nil {
			s.McapUSD, s.McapSource = m, ptr(lpscan.McapSourceMarketCap)
		} else if f := finitePositive(t.FDVUSD); f != nil {
			s.McapUSD, s.McapSource = f, ptr(lpscan.McapSourceFDV)
		}
	}
	if info != nil {
		s.GTScore = finite(info.GTScore)
		s.GTVerified = ptr(info.GTVerified)
		s.HoldersCount = info.HoldersCount
		s.Top10Pct = finite(info.Top10Pct)
		s.IsHoneypot = info.IsHoneypot
		s.MintAuthority = info.MintAuthority
		s.FreezeAuthority = info.FreezeAuthority
	}
	return s
}

func infoFlags(t TokenSummary, haveInfo bool) []Note {
	notes := []Note{}
	if t.Top10Pct != nil && *t.Top10Pct > Top10WarnPct {
		notes = append(notes, Note{"TOP10", fmt.Sprintf("top 10 holders own %.1f%% of supply (above %.0f%%)", *t.Top10Pct, Top10WarnPct)})
	}
	if t.MintAuthority != nil {
		notes = append(notes, Note{"MINT", "mint authority is set: supply can be increased"})
	}
	if t.FreezeAuthority != nil {
		notes = append(notes, Note{"FREEZE", "freeze authority is set: transfers can be frozen"})
	}
	if t.IsHoneypot == "unknown" {
		msg := "honeypot status unknown"
		if !haveInfo {
			msg = "honeypot status unknown (token info unavailable)"
		}
		notes = append(notes, Note{"HONEYPOT_UNKNOWN", msg})
	}
	return notes
}

// sigmaHour is the sample standard deviation of hourly log returns, or nil
// with fewer than MinCandles usable closes.
func sigmaHour(candles []geckoterminal.Candle) *float64 {
	var closes []float64
	for _, c := range candles {
		if c.Close > 0 && !math.IsInf(c.Close, 0) && !math.IsNaN(c.Close) {
			closes = append(closes, c.Close)
		}
	}
	if len(closes) < MinCandles {
		return nil
	}
	rets := make([]float64, 0, len(closes)-1)
	for i := 1; i < len(closes); i++ {
		rets = append(rets, math.Log(closes[i]/closes[i-1]))
	}
	var mean float64
	for _, r := range rets {
		mean += r
	}
	mean /= float64(len(rets))
	var ss float64
	for _, r := range rets {
		ss += (r - mean) * (r - mean)
	}
	return ptr(math.Sqrt(ss / float64(len(rets)-1)))
}

func strategy(c PoolRow, p Params, sigmaH *float64) *Strategy {
	if c.basePrice == nil {
		return nil
	}
	k := RangeK[p.Risk]
	P := *c.basePrice
	s := &Strategy{
		Risk:          p.Risk,
		K:             k,
		Mode:          ModeConcentrated,
		Price:         P,
		PriceUnit:     c.QuoteSymbol + " per " + c.BaseSymbol,
		Concentration: 1,
	}

	var move float64 // k·σ_7d in log terms
	if sigmaH != nil {
		s7 := *sigmaH * math.Sqrt(CandleLimit)
		move = k * s7
		s.Sigma7dPct = ptr(s7 * 100)
		s.SigmaDayPct = ptr(*sigmaH * math.Sqrt(24) * 100)
	}
	switch {
	case sigmaH == nil:
		s.Mode, s.FullRangeReason = ModeFullRange, ptr("not enough price history to size a range")
	case strings.Contains(strings.ToLower(c.Dex), "v2"):
		s.Mode, s.FullRangeReason = ModeFullRange, ptr("v2 pool: liquidity is always full range")
	case move > math.Log(1+MaxRangeHighPct/100):
		s.Mode, s.FullRangeReason = ModeFullRange, ptr(fmt.Sprintf("a ±%gσ range would exceed +%.0f%%: too volatile to concentrate", k, MaxRangeHighPct))
	}

	baseShare := 0.5
	if sigmaH != nil {
		rLow, rHigh := math.Exp(-move), math.Exp(move)
		if s.Mode == ModeConcentrated {
			s.RangeLow, s.RangeHigh = ptr(P*rLow), ptr(P*rHigh)
			s.RangeLowPct, s.RangeHighPct = ptr((rLow-1)*100), ptr((rHigh-1)*100)
			s.Concentration = math.Min(MaxConcentration, 1/(1-math.Exp(-move/2)))
			lo, hi, share := v3IL(rLow, rHigh)
			s.ILLowPct, s.ILHighPct, baseShare = ptr(lo*100), ptr(hi*100), share
		} else {
			s.ILLowPct, s.ILHighPct = ptr(fullRangeIL(rLow)*100), ptr(fullRangeIL(rHigh)*100)
		}
	}
	if c.FeePct != nil {
		base := c.TurnoverD * *c.FeePct
		s.BaseFeeYieldD = ptr(base)
		s.EstFeeYieldD, s.EstFee7dPct = ptr(base*s.Concentration), ptr(7*base*s.Concentration)
	}

	if p.Amount != nil {
		a := *p.Amount
		d := &Deposit{
			AmountUSD:   a,
			BaseSymbol:  c.BaseSymbol,
			BaseUSD:     a * baseShare,
			QuoteSymbol: c.QuoteSymbol,
			QuoteUSD:    a * (1 - baseShare),
		}
		if c.baseUSD != nil {
			d.BaseAmount = ptr(d.BaseUSD / *c.baseUSD)
		}
		if c.quoteUSD != nil {
			d.QuoteAmount = ptr(d.QuoteUSD / *c.quoteUSD)
		}
		if s.EstFeeYieldD != nil {
			d.EstFeesDUSD = ptr(a * *s.EstFeeYieldD / 100)
		}
		if s.ILLowPct != nil {
			d.ILLowUSD, d.ILHighUSD = ptr(a**s.ILLowPct/100), ptr(a**s.ILHighPct/100)
		}
		s.Deposit = d
	}
	return s
}

// v3IL is the loss versus holding of a Uniswap v3 position over
// [rLow, rHigh] (prices relative to entry 1) when price ends at each edge,
// plus the base token's share of the deposit value at entry.
func v3IL(rLow, rHigh float64) (low, high, baseShare float64) {
	sa, sb := math.Sqrt(rLow), math.Sqrt(rHigh)
	x0 := 1 - 1/sb // base amount at price 1, liquidity 1
	y0 := 1 - sa   // quote amount
	hold := func(p float64) float64 { return x0*p + y0 }
	high = (sb-sa)/hold(rHigh) - 1        // all quote at the top edge
	low = (1/sa-1/sb)*rLow/hold(rLow) - 1 // all base at the bottom edge
	return low, high, x0 / (x0 + y0)
}

// fullRangeIL is constant-product IL for a price ratio r.
func fullRangeIL(r float64) float64 { return 2*math.Sqrt(r)/(1+r) - 1 }

func exitSignals(c PoolRow, s *Strategy) []Signal {
	sig := []Signal{}
	if s.Mode == ModeConcentrated && s.RangeLow != nil {
		sig = append(sig, Signal{"OUT_OF_RANGE",
			fmt.Sprintf("price leaves the range %.6g–%.6g %s: fees stop until you rebalance", *s.RangeLow, *s.RangeHigh, s.PriceUnit), nil})
	}
	sig = append(sig, Signal{"TURNOVER_DROP",
		fmt.Sprintf("turnover/d falls below %.1f (now %.2f): volume no longer pays for the position", TurnoverExitMin, c.TurnoverD), ptr(TurnoverExitMin)})
	floor := c.LiquidityUSD * (1 - LiquidityExitDrop)
	sig = append(sig, Signal{"LIQUIDITY_PULL",
		fmt.Sprintf("pool liquidity falls below $%.0f (%.0f%% under today's): possible liquidity pull", floor, LiquidityExitDrop*100), ptr(floor)})
	if s.SigmaDayPct != nil {
		mv := 2 * *s.SigmaDayPct
		sig = append(sig, Signal{"DAILY_MOVE",
			fmt.Sprintf("price moves more than %.1f%% in one day (2σ): re-run the plan and rebalance or exit", mv), ptr(mv)})
	}
	return sig
}

func hasFlag(fs []lpscan.Flag, f lpscan.Flag) bool {
	for _, x := range fs {
		if x == f {
			return true
		}
	}
	return false
}

func ptr[T any](v T) *T { return &v }

func finite(v *float64) *float64 {
	if v == nil || math.IsNaN(*v) || math.IsInf(*v, 0) {
		return nil
	}
	return v
}

func finitePositive(v *float64) *float64 {
	if v = finite(v); v == nil || *v <= 0 {
		return nil
	}
	return v
}

func positive(v *float64) float64 {
	if v = finitePositive(v); v == nil {
		return 0
	}
	return *v
}
