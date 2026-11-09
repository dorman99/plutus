package lpplan

import (
	"context"
	"math"
	"testing"

	"github.com/dorman99/plutus/pkg/geckoterminal"
	"github.com/dorman99/plutus/pkg/lpscan"
)

func TestDetailFromPlan(t *testing.T) {
	in := input(pool("0xp1", nil))
	in.Info.Top10Pct, in.Info.GTScore = f(30.8), f(79.7)
	p := Analyze(in, Params{CA: ca}, now)
	d := DetailFromPlan(p)

	if d.Level != lpscan.DetailFull || d.IsHoneypot != "no" || *d.Top10Pct != 30.8 || *d.GTScore != 79.7 {
		t.Fatalf("info = %+v", d)
	}
	pd := d.Plan
	if pd == nil || pd.Verdict != "go" || *pd.ChosenPool != "0xp1" || pd.ChosenPair != "ABC/USDG" || *pd.ChosenFeePct != 0.3 {
		t.Fatalf("plan = %+v", pd)
	}
	s := p.Strategy
	if pd.Mode != s.Mode || *pd.EstFee7dPct != *s.EstFee7dPct || *pd.ILLowPct != *s.ILLowPct {
		t.Errorf("strategy not copied: %+v", pd)
	}
	if want := *s.EstFee7dPct + *s.ILLowPct; math.Abs(*pd.Net7dPct-want) > 1e-12 {
		t.Errorf("net 7d = %v, want %v", *pd.Net7dPct, want)
	}
	if len(pd.ExitSignals) != len(p.ExitSignals) || len(pd.TriggeredExits) != 0 {
		t.Errorf("exits = %v triggered = %v", pd.ExitSignals, pd.TriggeredExits)
	}
}

func TestDetailFromPlanTriggeredExits(t *testing.T) {
	// turnover/d 0.4 (below 0.5) and a 20% day on a low-volatility pool.
	in := input(pool("0xp1", func(p *geckoterminal.Pool) {
		p.Name = "ABC / USDG 3%"
		p.VolumeUSD = map[string]*float64{"h24": f(40_000), "h6": f(10_000)}
		p.PriceChangePct["h24"] = f(20)
	}))
	pd := DetailFromPlan(Analyze(in, Params{CA: ca}, now)).Plan
	if pd.Verdict != "wait" || len(pd.TriggeredExits) != 2 || pd.TriggeredExits[0] != "TURNOVER_DROP" || pd.TriggeredExits[1] != "DAILY_MOVE" {
		t.Errorf("verdict=%s triggered=%v", pd.Verdict, pd.TriggeredExits)
	}
}

func TestDetailFromPlanAvoid(t *testing.T) {
	in := input(pool("0xp1", func(p *geckoterminal.Pool) { p.ReserveUSD = f(10_000) }))
	pd := DetailFromPlan(Analyze(in, Params{CA: ca}, now)).Plan
	if pd.Verdict != "avoid" || pd.ChosenPool != nil || pd.Mode != "" || pd.Net7dPct != nil || len(pd.Reasons) != 1 {
		t.Errorf("plan = %+v", pd)
	}
}

func TestScanDetailerBasicMakesOneCall(t *testing.T) {
	// Token and pools would fail: basic detail must only read token info.
	src := &fake{
		in:       Input{Info: &geckoterminal.TokenInfo{IsHoneypot: "no", Top10Pct: f(18)}},
		tokenErr: context.DeadlineExceeded, poolsErr: context.DeadlineExceeded,
	}
	d, err := ScanDetailer(src).Detail(context.Background(), ca, lpscan.DetailBasic)
	if err != nil {
		t.Fatal(err)
	}
	if d.Level != lpscan.DetailBasic || d.Plan != nil || *d.Top10Pct != 18 || src.ohlcvCallCount != 0 {
		t.Errorf("detail = %+v", d)
	}
	if _, err := ScanDetailer(src).Detail(context.Background(), ca, lpscan.DetailFull); err == nil {
		t.Error("full detail should surface the token error")
	}
}
