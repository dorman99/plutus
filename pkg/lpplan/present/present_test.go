package present

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/dorman99/plutus/pkg/lpplan"
	"github.com/dorman99/plutus/pkg/lpscan"
)

func f(v float64) *float64 { return &v }
func s(v string) *string   { return &v }

const (
	ca   = "0xa30fa36db767ad9ed3f7a60fc79526fb4d56d344"
	pool = "0x02175608f1b5e6b5ed221ccfdc7be197d111d915"
)

func goPlan() *lpplan.Plan {
	return &lpplan.Plan{
		ScannedAt: time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC),
		Network:   "robinhood",
		Params:    lpplan.Params{CA: ca, Amount: f(500), Risk: lpplan.RiskBalanced},
		Token:     lpplan.TokenSummary{Address: ca, Symbol: "USO", Name: "Oil", PriceUSD: f(144), McapUSD: f(2.4e6), McapSource: s("market_cap"), IsHoneypot: "unknown"},
		Pools: []lpplan.PoolRow{
			{PoolAddress: pool, Pair: "USO/USDG", Dex: "uniswap-v3-robinhood", FeePct: f(0.3), LiquidityUSD: 1.7e6, Volume24hUSD: 3.1e6,
				TurnoverD: 1.79, EstFeeYieldD: f(0.54), PriceChange24hPct: f(-2.9), Grade: lpscan.GradeStrong, Flags: []lpscan.Flag{}, Chosen: true},
			{PoolAddress: "0xother", Pair: "GAS/USO", Dex: "uniswap-v4-robinhood", LiquidityUSD: 1.2e5, Grade: lpscan.GradeWeak, Flags: []lpscan.Flag{lpscan.FlagNew}},
		},
		ChosenPool: s(pool),
		Verdict:    lpplan.Verdict{Decision: lpplan.DecisionGo, Reasons: []lpplan.Reason{}},
		Strategy: &lpplan.Strategy{Risk: lpplan.RiskBalanced, K: 1, Mode: lpplan.ModeConcentrated, Price: 144.3, PriceUnit: "USDG per USO",
			Sigma7dPct: f(10.3), SigmaDayPct: f(3.9), RangeLow: f(130.2), RangeHigh: f(160), RangeLowPct: f(-9.8), RangeHighPct: f(10.8),
			Concentration: 10, EstFeeYieldD: f(5.36), EstFee7dPct: f(37.5), ILLowPct: f(-2.64), ILHighPct: f(-2.64),
			Deposit: &lpplan.Deposit{AmountUSD: 500, BaseSymbol: "USO", BaseAmount: f(1.73), BaseUSD: 250, QuoteSymbol: "USDG", QuoteAmount: f(249.8), QuoteUSD: 250,
				EstFeesDUSD: f(26.8), ILLowUSD: f(-13.2), ILHighUSD: f(-13.2)}},
		ExitSignals: []lpplan.Signal{{Code: "OUT_OF_RANGE", Message: "price leaves the range"}},
		InfoFlags:   []lpplan.Note{{Code: "HONEYPOT_UNKNOWN", Message: "honeypot status unknown"}},
		Warnings:    []string{},
		Legend:      lpplan.LegendData(),
	}
}

func TestTableGo(t *testing.T) {
	var b bytes.Buffer
	if err := Table(&b, goPlan()); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	for _, want := range []string{
		ca, pool, "0xother", "★", "VERDICT  GO", "STRATEGY  (risk balanced, k=1)", "130.2 – 160",
		"deposit    $500", "EXIT / REBALANCE WHEN", "HONEYPOT_UNKNOWN", "NO_DEPTH", "A decision brief, not financial advice",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("table missing %q\n%s", want, out)
		}
	}
}

func TestTableWaitAndAvoid(t *testing.T) {
	p := goPlan()
	p.Verdict = lpplan.Verdict{Decision: lpplan.DecisionWait, Reasons: []lpplan.Reason{{Code: "SWING", Tier: lpplan.DecisionWait, Message: "moved"}}}
	var b bytes.Buffer
	Table(&b, p)
	if !strings.Contains(b.String(), "IF YOU STILL ENTER") || !strings.Contains(b.String(), "SWING        (wait) moved") {
		t.Errorf("wait table:\n%s", b.String())
	}

	p.Verdict.Decision, p.Strategy, p.ExitSignals = lpplan.DecisionAvoid, nil, []lpplan.Signal{}
	b.Reset()
	Table(&b, p)
	out := b.String()
	if strings.Contains(out, "STRATEGY") || strings.Contains(out, "IF YOU STILL ENTER") || strings.Contains(out, "EXIT /") {
		t.Errorf("avoid shows a strategy:\n%s", out)
	}
	if !strings.Contains(out, "VERDICT  AVOID") || !strings.Contains(out, "A decision brief") {
		t.Errorf("avoid table:\n%s", out)
	}
}

func TestTableFullRange(t *testing.T) {
	p := goPlan()
	p.Strategy.Mode, p.Strategy.FullRangeReason = lpplan.ModeFullRange, s("v2 pool: liquidity is always full range")
	p.Strategy.RangeLow, p.Strategy.RangeHigh, p.Strategy.RangeLowPct, p.Strategy.RangeHighPct = nil, nil, nil, nil
	var b bytes.Buffer
	if err := Table(&b, p); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), "full range: v2 pool") {
		t.Errorf("table:\n%s", b.String())
	}
}

func TestJSON(t *testing.T) {
	var b bytes.Buffer
	if err := JSON(&b, goPlan()); err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"scanned_at", "network", "params", "token", "pools", "chosen_pool", "verdict", "strategy", "exit_signals", "info_flags", "warnings", "legend"} {
		if _, ok := m[k]; !ok {
			t.Errorf("json missing %q", k)
		}
	}
	if strings.Contains(b.String(), "turnover6hD") || strings.Contains(b.String(), "basePrice") {
		t.Error("unexported fields leaked")
	}
}
