package lpplan

import (
	"context"
	"errors"
	"math"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/dorman99/plutus/pkg/geckoterminal"
	"github.com/dorman99/plutus/pkg/lpscan"
)

const (
	ca    = "0x1111111111111111111111111111111111111111"
	quote = "0x2222222222222222222222222222222222222222"
	usoCA = "0xa30fa36db767ad9ed3f7a60fc79526fb4d56d344"
)

var now = time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)

func f(v float64) *float64 { return &v }

// pool is a healthy concentrated pool: $100K liquidity, $100K 24h volume
// ($25K in 6h), 0.3% fee, flat price, balanced trades, 30 days old.
func pool(addr string, mod func(*geckoterminal.Pool)) geckoterminal.Pool {
	created := now.Add(-30 * 24 * time.Hour)
	p := geckoterminal.Pool{
		Address:        addr,
		Name:           "ABC / USDG 0.3%",
		DexID:          "uniswap-v3-robinhood",
		CreatedAt:      &created,
		ReserveUSD:     f(100_000),
		VolumeUSD:      map[string]*float64{"h24": f(100_000), "h6": f(25_000)},
		PriceChangePct: map[string]*float64{"h24": f(1)},
		Transactions:   map[string]*geckoterminal.Txns{"h24": {Buys: 50, Sells: 50}},
		BaseToken:      geckoterminal.Token{Address: ca, Symbol: "ABC"},
		QuoteToken:     geckoterminal.Token{Address: quote, Symbol: "USDG"},
		BasePriceUSD:   f(2),
		QuotePriceUSD:  f(1),
		BasePriceQuote: f(2),
	}
	if mod != nil {
		mod(&p)
	}
	return p
}

// candles alternate ±step in log price around 2, giving σ_hour ≈ step.
func candles(n int, step float64) []geckoterminal.Candle {
	out := make([]geckoterminal.Candle, n)
	for i := range out {
		c := 2.0
		if i%2 == 1 {
			c = 2 * math.Exp(step)
		}
		out[i] = geckoterminal.Candle{Time: now.Add(time.Duration(i-n) * time.Hour), Close: c}
	}
	return out
}

func input(pools ...geckoterminal.Pool) Input {
	return Input{
		Token:   &geckoterminal.TokenDetail{Address: ca, Symbol: "ABC", MarketCapUSD: f(1e6)},
		Info:    &geckoterminal.TokenInfo{IsHoneypot: "no"},
		Pools:   pools,
		Candles: candles(168, 0.005),
	}
}

func codes(p *Plan) string {
	var c []string
	for _, r := range p.Verdict.Reasons {
		c = append(c, r.Code)
	}
	return strings.Join(c, ",")
}

func TestHealthyPoolIsGo(t *testing.T) {
	p := Analyze(input(pool("0xp1", nil)), Params{CA: ca}, now)
	if p.Verdict.Decision != DecisionGo || len(p.Verdict.Reasons) != 0 {
		t.Fatalf("verdict = %+v", p.Verdict)
	}
	if p.ChosenPool == nil || *p.ChosenPool != "0xp1" || !p.Pools[0].Chosen {
		t.Errorf("chosen = %v", p.ChosenPool)
	}
	s := p.Strategy
	if s == nil || s.Mode != ModeConcentrated || s.Risk != RiskBalanced || s.PriceUnit != "USDG per ABC" {
		t.Fatalf("strategy = %+v", s)
	}
	if len(p.ExitSignals) != 4 {
		t.Errorf("exit signals = %+v", p.ExitSignals)
	}
	if s.Deposit != nil {
		t.Error("deposit without --amount")
	}
}

func TestVerdictCodes(t *testing.T) {
	cases := []struct {
		name     string
		mod      func(*geckoterminal.Pool)
		in       func(*Input)
		want     Decision
		wantCode string
	}{
		{"honeypot", nil, func(in *Input) { in.Info.IsHoneypot = "yes" }, DecisionAvoid, ReasonHoneypot},
		{"skew", func(p *geckoterminal.Pool) { p.Transactions["h24"] = &geckoterminal.Txns{Buys: 80, Sells: 20} }, nil, DecisionAvoid, ReasonSkew},
		{"weak", func(p *geckoterminal.Pool) { p.VolumeUSD = map[string]*float64{"h24": f(5_000), "h6": f(1_250)} }, nil, DecisionAvoid, ReasonWeak},
		{"suspect", func(p *geckoterminal.Pool) { p.VolumeUSD = map[string]*float64{"h24": f(600_000), "h6": f(150_000)} }, nil, DecisionWait, ReasonSuspect},
		{"swing", func(p *geckoterminal.Pool) { p.PriceChangePct["h24"] = f(-35) }, nil, DecisionWait, ReasonSwing},
		{"new", func(p *geckoterminal.Pool) { c := now.Add(-2 * 24 * time.Hour); p.CreatedAt = &c }, nil, DecisionWait, ReasonNew},
		{"fading", func(p *geckoterminal.Pool) { p.VolumeUSD["h6"] = f(10_000) }, nil, DecisionWait, ReasonFading},
		{"fee unknown", func(p *geckoterminal.Pool) { p.Name = "ABC / USDG" }, nil, DecisionWait, ReasonFeeUnknown},
		{"no history", nil, func(in *Input) { in.Candles = candles(10, 0.01) }, DecisionWait, ReasonNoHistory},
		{"il beats fees", func(p *geckoterminal.Pool) { p.Name = "ABC / USDG 0.01%" }, func(in *Input) { in.Candles = candles(168, 0.02) }, DecisionWait, ReasonILGTFees},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := input(pool("0xp1", tc.mod))
			if tc.in != nil {
				tc.in(&in)
			}
			p := Analyze(in, Params{CA: ca}, now)
			if p.Verdict.Decision != tc.want || !strings.Contains(codes(p), tc.wantCode) {
				t.Fatalf("verdict = %s %s, want %s with %s", p.Verdict.Decision, codes(p), tc.want, tc.wantCode)
			}
			if tc.want == DecisionAvoid && (p.Strategy != nil || len(p.ExitSignals) != 0) {
				t.Error("avoid must not carry a strategy or exit signals")
			}
			if tc.want == DecisionWait && p.Strategy == nil {
				t.Error("wait keeps the strategy")
			}
		})
	}
}

func TestAvoidBeatsWaitAndListsAll(t *testing.T) {
	in := input(pool("0xp1", func(p *geckoterminal.Pool) {
		p.Transactions["h24"] = &geckoterminal.Txns{Buys: 90, Sells: 10}
		p.PriceChangePct["h24"] = f(50)
		p.Name = "ABC / USDG"
	}))
	p := Analyze(in, Params{CA: ca}, now)
	if p.Verdict.Decision != DecisionAvoid {
		t.Fatalf("decision = %s", p.Verdict.Decision)
	}
	for _, c := range []string{ReasonSkew, ReasonSwing, ReasonFeeUnknown} {
		if !strings.Contains(codes(p), c) {
			t.Errorf("reasons %s missing %s", codes(p), c)
		}
	}
}

func TestNoDepth(t *testing.T) {
	p := Analyze(input(pool("0xp1", func(p *geckoterminal.Pool) { p.ReserveUSD = f(49_999) })), Params{CA: ca}, now)
	if p.Verdict.Decision != DecisionAvoid || codes(p) != ReasonNoDepth || p.ChosenPool != nil || p.Strategy != nil {
		t.Fatalf("plan = %+v", p.Verdict)
	}
	if len(p.Pools) != 1 {
		t.Error("shallow pool should still be listed")
	}
}

func TestChoosePool(t *testing.T) {
	lowFee := pool("0xlow", func(p *geckoterminal.Pool) { p.Name = "ABC / USDG 0.05%"; p.ReserveUSD = f(500_000) })
	highFee := pool("0xhigh", nil)
	noFee := pool("0xnofee", func(p *geckoterminal.Pool) { p.Name = "ABC / USDG"; p.VolumeUSD["h24"] = f(900_000) })
	shallow := pool("0xshallow", func(p *geckoterminal.Pool) { p.ReserveUSD = f(10_000); p.Name = "ABC / USDG 1%" })
	other := pool("0xother", func(p *geckoterminal.Pool) { p.BaseToken.Address = "0x9"; p.ReserveUSD = f(9e9) })

	rows := Rows([]geckoterminal.Pool{lowFee, highFee, noFee, shallow, other, highFee}, ca, now)
	if len(rows) != 4 {
		t.Fatalf("rows = %d (dedupe and CA filter)", len(rows))
	}
	if rows[0].PoolAddress != "0xlow" {
		t.Errorf("rows not sorted by liquidity: %s", rows[0].PoolAddress)
	}
	if i := Choose(rows); rows[i].PoolAddress != "0xhigh" {
		t.Errorf("chose %s, want highest est. yield", rows[i].PoolAddress)
	}

	// No fee anywhere: highest turnover.
	a := pool("0xa", func(p *geckoterminal.Pool) { p.Name = "ABC / USDG" })
	b := pool("0xb", func(p *geckoterminal.Pool) { p.Name = "ABC / USDG"; p.VolumeUSD["h24"] = f(300_000) })
	rows = Rows([]geckoterminal.Pool{a, b}, ca, now)
	if i := Choose(rows); rows[i].PoolAddress != "0xb" {
		t.Errorf("chose %s, want highest turnover", rows[i].PoolAddress)
	}
}

func TestCAAsQuote(t *testing.T) {
	p := pool("0xq", func(p *geckoterminal.Pool) {
		p.BaseToken, p.QuoteToken = geckoterminal.Token{Address: quote, Symbol: "MEME"}, geckoterminal.Token{Address: ca, Symbol: "ABC"}
		p.Name = "MEME / ABC 1%"
	})
	plan := Analyze(input(p), Params{CA: ca}, now)
	if len(plan.Pools) != 1 || plan.Pools[0].Pair != "MEME/ABC" || plan.Strategy.PriceUnit != "ABC per MEME" {
		t.Fatalf("plan = %+v", plan.Pools)
	}
}

func TestRangeMaths(t *testing.T) {
	step := 0.01
	in := input(pool("0xp1", nil))
	in.Candles = candles(168, step)
	p := Analyze(in, Params{CA: ca, Risk: RiskSafe}, now)
	s := p.Strategy
	// 167 alternating ±step returns (84 up, 83 down): mean step/167, exact sample stdev
	n := 167.0
	m := step / n
	sh := math.Sqrt((n*step*step - n*m*m) / (n - 1))
	s7 := sh * math.Sqrt(168)
	near := func(name string, got *float64, want float64) {
		t.Helper()
		if got == nil {
			t.Errorf("%s = nil, want %v", name, want)
		} else if math.Abs(*got-want) > 1e-9*math.Max(1, math.Abs(want)) {
			t.Errorf("%s = %v, want %v", name, *got, want)
		}
	}
	near("sigma_7d_pct", s.Sigma7dPct, s7*100)
	near("sigma_day_pct", s.SigmaDayPct, sh*math.Sqrt(24)*100)
	near("range_low", s.RangeLow, 2*math.Exp(-2*s7))
	near("range_high", s.RangeHigh, 2*math.Exp(2*s7))
	near("range_high_pct", s.RangeHighPct, (math.Exp(2*s7)-1)*100)
	// 1/(1 − e^(−kσ/2)) ≈ 8.2 here, so the ×3 cap applies
	conc := MaxConcentration
	if s.Concentration != conc {
		t.Errorf("concentration = %v, want %v", s.Concentration, conc)
	}
	near("base_fee_yield_d", s.BaseFeeYieldD, 1.0*0.3)
	near("est_fee_yield_d", s.EstFeeYieldD, 1.0*0.3*conc)
	near("est_fee_7d_pct", s.EstFee7dPct, 7*1.0*0.3*conc)
	// v3 edge IL for a geometric-centred range: (√pb+1)/(pb+1) − 1 at both edges
	pb := math.Exp(2 * s7)
	il := ((math.Sqrt(pb)+1)/(pb+1) - 1) * 100
	near("il_low_pct", s.ILLowPct, il)
	near("il_high_pct", s.ILHighPct, il)
}

// IL_GT_FEES must use the base yield: here the concentrated 7-day fees
// beat the IL but the base fees do not.
func TestILCheckUsesBaseYield(t *testing.T) {
	in := input(pool("0xp1", func(p *geckoterminal.Pool) { p.Name = "ABC / USDG 0.5%" }))
	in.Candles = candles(168, 0.01)
	p := Analyze(in, Params{CA: ca, Risk: RiskSafe}, now)
	s := p.Strategy
	worst := math.Abs(*s.ILLowPct)
	if !(*s.EstFee7dPct > worst && 7**s.BaseFeeYieldD < worst) {
		t.Fatalf("fixture no longer separates base %.2f / est %.2f around IL %.2f", 7**s.BaseFeeYieldD, *s.EstFee7dPct, worst)
	}
	if !strings.Contains(codes(p), ReasonILGTFees) {
		t.Errorf("reasons = %s, want %s", codes(p), ReasonILGTFees)
	}
}

func TestConcentrationCap(t *testing.T) {
	in := input(pool("0xp1", nil))
	in.Candles = candles(168, 0.0005)
	p := Analyze(in, Params{CA: ca, Risk: RiskAggressive}, now)
	if p.Strategy.Concentration != MaxConcentration {
		t.Errorf("concentration = %v, want cap %v", p.Strategy.Concentration, MaxConcentration)
	}
}

func TestFullRange(t *testing.T) {
	cases := map[string]struct {
		mod  func(*geckoterminal.Pool)
		in   func(*Input)
		want string
	}{
		"v2":         {func(p *geckoterminal.Pool) { p.DexID = "pons-v2-dex" }, nil, "v2 pool"},
		"too wide":   {nil, func(in *Input) { in.Candles = candles(168, 0.05) }, "too volatile"},
		"no history": {nil, func(in *Input) { in.Candles = nil }, "not enough price history"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			in := input(pool("0xp1", tc.mod))
			if tc.in != nil {
				tc.in(&in)
			}
			s := Analyze(in, Params{CA: ca}, now).Strategy
			if s == nil || s.Mode != ModeFullRange || s.FullRangeReason == nil || !strings.Contains(*s.FullRangeReason, tc.want) {
				t.Fatalf("strategy = %+v", s)
			}
			if s.RangeLow != nil || s.Concentration != 1 {
				t.Errorf("full range has a range: %+v", s)
			}
			if name == "no history" && s.ILLowPct != nil {
				t.Error("IL without volatility")
			}
			if name == "v2" {
				r := s.Sigma7dPct
				want := (2*math.Sqrt(math.Exp(*r/100))/(1+math.Exp(*r/100)) - 1) * 100
				if math.Abs(*s.ILHighPct-want) > 1e-9 {
					t.Errorf("full-range IL = %v, want %v", *s.ILHighPct, want)
				}
			}
		})
	}
}

func TestDeposit(t *testing.T) {
	p := Analyze(input(pool("0xp1", nil)), Params{CA: ca, Amount: f(1000)}, now)
	d := p.Strategy.Deposit
	if d == nil {
		t.Fatal("no deposit")
	}
	if math.Abs(d.BaseUSD+d.QuoteUSD-1000) > 1e-9 {
		t.Errorf("split %v + %v != 1000", d.BaseUSD, d.QuoteUSD)
	}
	if math.Abs(*d.BaseAmount-d.BaseUSD/2) > 1e-9 || math.Abs(*d.QuoteAmount-d.QuoteUSD) > 1e-9 {
		t.Errorf("amounts = %v %v", *d.BaseAmount, *d.QuoteAmount)
	}
	if math.Abs(*d.EstFeesDUSD-1000**p.Strategy.EstFeeYieldD/100) > 1e-9 || math.Abs(*d.ILLowUSD-10**p.Strategy.ILLowPct) > 1e-9 {
		t.Errorf("deposit $ = %+v", d)
	}
}

func TestInfoFlags(t *testing.T) {
	in := input(pool("0xp1", nil))
	in.Info = &geckoterminal.TokenInfo{IsHoneypot: "unknown", Top10Pct: f(85), MintAuthority: new(string)}
	p := Analyze(in, Params{CA: ca}, now)
	var got []string
	for _, n := range p.InfoFlags {
		got = append(got, n.Code)
	}
	if strings.Join(got, ",") != "TOP10,MINT,HONEYPOT_UNKNOWN" {
		t.Errorf("info flags = %v", got)
	}
	if p.Verdict.Decision != DecisionGo {
		t.Error("info flags must not decide the verdict")
	}
}

func TestParamsValidate(t *testing.T) {
	cases := map[string]Params{
		"CA is required":       {},
		"not a contract":       {CA: "0x123"},
		"major token":          {CA: "0x0BD7D308F8E1639FAB988DF18A8011F41EACAD73"},
		"--amount must be > 0": {CA: ca, Amount: f(0)},
		"--risk must be one":   {CA: ca, Risk: "yolo"},
	}
	for want, p := range cases {
		err := p.Normalize().Validate()
		var pe *ParamError
		if !errors.As(err, &pe) || !strings.Contains(err.Error(), want) {
			t.Errorf("%+v: err = %v, want %q", p, err, want)
		}
	}
	if err := (Params{CA: strings.ToUpper(ca[:2]) + ca[2:]}).Normalize().Validate(); err != nil {
		t.Errorf("valid CA rejected: %v", err)
	}
}

// fake is a Source with canned responses and call recording.
type fake struct {
	in             Input
	tokenErr       error
	infoErr        error
	poolsErr       error
	ohlcvErr       error
	ohlcvPool      string
	ohlcvCallCount int
}

func (s *fake) Token(context.Context, string) (*geckoterminal.TokenDetail, error) {
	return s.in.Token, s.tokenErr
}
func (s *fake) TokenInfo(context.Context, string) (*geckoterminal.TokenInfo, error) {
	return s.in.Info, s.infoErr
}
func (s *fake) TokenPools(context.Context, string) ([]geckoterminal.Pool, error) {
	return s.in.Pools, s.poolsErr
}
func (s *fake) PoolOHLCV(_ context.Context, pool string) ([]geckoterminal.Candle, error) {
	s.ohlcvCallCount++
	s.ohlcvPool = pool
	return s.in.Candles, s.ohlcvErr
}

func planner(src Source) *Planner { return &Planner{src: src, now: func() time.Time { return now }} }

func TestPlannerFetchesChosenPoolCandles(t *testing.T) {
	src := &fake{in: input(pool("0xa", func(p *geckoterminal.Pool) { p.Name = "ABC / USDG 0.05%" }), pool("0xb", nil))}
	p, err := planner(src).Plan(context.Background(), Params{CA: ca})
	if err != nil {
		t.Fatal(err)
	}
	if p.Warnings == nil {
		t.Error("warnings must be [] not null")
	}
	if src.ohlcvCallCount != 1 || src.ohlcvPool != "0xb" || *p.ChosenPool != "0xb" {
		t.Errorf("ohlcv calls=%d pool=%s", src.ohlcvCallCount, src.ohlcvPool)
	}
}

func TestPlannerSoftFailures(t *testing.T) {
	src := &fake{in: input(pool("0xp1", nil)), infoErr: errors.New("boom"), ohlcvErr: errors.New("bang")}
	p, err := planner(src).Plan(context.Background(), Params{CA: ca})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Warnings) != 2 || !strings.Contains(p.Warnings[0], "token info unavailable") || !strings.Contains(p.Warnings[1], "price history unavailable") {
		t.Errorf("warnings = %q", p.Warnings)
	}
	if p.Token.GTVerified != nil || p.Token.IsHoneypot != "unknown" || !strings.Contains(codes(p), ReasonNoHistory) {
		t.Errorf("plan = %+v %s", p.Token, codes(p))
	}
}

func TestPlannerHardFailures(t *testing.T) {
	for name, src := range map[string]*fake{
		"token": {in: input(pool("0xp1", nil)), tokenErr: errors.New("404")},
		"pools": {in: input(pool("0xp1", nil)), poolsErr: errors.New("500")},
	} {
		if _, err := planner(src).Plan(context.Background(), Params{CA: ca}); err == nil || !strings.Contains(err.Error(), name) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func TestPlannerNoDepthSkipsCandles(t *testing.T) {
	src := &fake{in: input(pool("0xp1", func(p *geckoterminal.Pool) { p.ReserveUSD = f(1000) }))}
	if _, err := planner(src).Plan(context.Background(), Params{CA: ca}); err != nil {
		t.Fatal(err)
	}
	if src.ohlcvCallCount != 0 {
		t.Error("NO_DEPTH must not fetch candles")
	}
}

// TestLiveFixture runs the whole analysis on data captured from the live API
// (USO, 2026-11-09).
func TestLiveFixture(t *testing.T) {
	open := func(n string) *os.File {
		fh, err := os.Open("testdata/" + n)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { fh.Close() })
		return fh
	}
	tok, err1 := geckoterminal.DecodeToken(open("token.json"))
	info, err2 := geckoterminal.DecodeTokenInfo(open("token_info.json"))
	pools, err3 := geckoterminal.DecodePools(open("token_pools.json"))
	cs, err4 := geckoterminal.DecodeOHLCV(open("ohlcv.json"))
	if err := errors.Join(err1, err2, err3, err4); err != nil {
		t.Fatal(err)
	}
	p := Analyze(Input{tok, info, pools, cs}, Params{CA: usoCA, Amount: f(500)}, now)
	if len(p.Pools) != 20 || p.ChosenPool == nil || *p.ChosenPool != "0x02175608f1b5e6b5ed221ccfdc7be197d111d915" {
		t.Fatalf("pools=%d chosen=%v", len(p.Pools), p.ChosenPool)
	}
	if p.Verdict.Decision != DecisionGo || p.Strategy == nil || p.Strategy.Deposit == nil {
		t.Fatalf("verdict = %s %s", p.Verdict.Decision, codes(p))
	}
	if p.Pools[0].Grade != lpscan.GradeStrong {
		t.Errorf("grade = %s", p.Pools[0].Grade)
	}
}
