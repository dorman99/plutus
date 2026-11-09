package lpscan

import (
	"math"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/dorman99/plutus/pkg/geckoterminal"
)

// testdata/pools.json is a hand-designed GeckoTerminal response; each pool
// exercises one rule. Default params, window 6h:
//
//	EFG  score 8.00  suspect (score > 5)
//	ABC  score 4.26  strong, fee 0.3%  <- pick (second ABC pool scores lower)
//	XYZ  score 3.11  suspect, NEW, SKEW
//	GHI  score 1.84  strong, SWING
//	BCD  score 1.49  strong, FDV (market cap "0.0")
//	DEF  score 1.18  strong, FDV (market cap null)
//	HIJ  score 0.40  decent, no fee, no created date
//	JKL  score 0.34  decent, fee 0.3%
//	dropped: MNO mcap 8M, PQR liq 20K, STU vol 10K, WETH major base,
//	         VWX no price change, YZA mcap and fdv 0
var testNow = time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)

// legacyParams are the stage-1 defaults the fixture was designed for: no
// minimum market cap, $5M maximum, every endpoint and kind.
func legacyParams() Params {
	p := DefaultParams()
	p.MinMcap, p.MaxMcap = 0, 5_000_000
	p.Source, p.Kind = SourceAll, KindAll
	return p
}

func loadFixture(t *testing.T) []geckoterminal.Pool {
	t.Helper()
	f, err := os.Open("testdata/pools.json")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	pools, err := geckoterminal.DecodePools(f)
	if err != nil {
		t.Fatal(err)
	}
	return pools
}

func only(pools []geckoterminal.Pool, syms ...string) []geckoterminal.Pool {
	var out []geckoterminal.Pool
	for _, p := range pools {
		for _, s := range syms {
			if p.BaseToken.Symbol == s {
				out = append(out, p)
			}
		}
	}
	return out
}

func coins(r *Result) string {
	s := make([]string, len(r.Results))
	for i, row := range r.Results {
		s[i] = row.CoinSymbol
	}
	return strings.Join(s, ",")
}

func row(t *testing.T, r *Result, sym string) Row {
	t.Helper()
	for _, x := range r.Results {
		if x.CoinSymbol == sym {
			return x
		}
	}
	t.Fatalf("coin %s not in results %s", sym, coins(r))
	return Row{}
}

func near(a, b float64) bool { return math.Abs(a-b) < 0.005 }

func TestRankDefaults(t *testing.T) {
	r := Rank(loadFixture(t), legacyParams(), testNow)

	if got, want := r.Stats, (Stats{Scanned: 15, Passed: 9, Coins: 8}); got != want {
		t.Errorf("stats = %+v, want %+v", got, want)
	}
	if got, want := coins(r), "EFG,ABC,XYZ,GHI,BCD"; got != want {
		t.Errorf("coins = %s, want %s", got, want)
	}
	for i, x := range r.Results {
		if x.Rank != i+1 {
			t.Errorf("row %d rank = %d", i, x.Rank)
		}
	}

	abc := row(t, r, "ABC")
	if !near(abc.TurnoverD, 4.608) || !near(abc.Score, 4.259) {
		t.Errorf("ABC turnover_d=%.3f score=%.3f", abc.TurnoverD, abc.Score)
	}
	if abc.Pair != "ABC/WETH" || abc.Grade != GradeStrong || len(abc.Flags) != 0 {
		t.Errorf("ABC = %s %s %v", abc.Pair, abc.Grade, abc.Flags)
	}
	if abc.FeePct == nil || *abc.FeePct != 0.3 || abc.EstFeeYieldD == nil || !near(*abc.EstFeeYieldD, 1.382) {
		t.Errorf("ABC fee=%v yield=%v", abc.FeePct, abc.EstFeeYieldD)
	}
	if !strings.HasPrefix(abc.CoinAddress, "0x") || len(abc.CoinAddress) != 42 {
		t.Errorf("ABC coin address %q is not a full address", abc.CoinAddress)
	}
	if !strings.HasSuffix(abc.URL, abc.PoolAddress) {
		t.Errorf("ABC url %q", abc.URL)
	}

	if xyz := row(t, r, "XYZ"); xyz.Grade != GradeSuspect || strings.Join(flagStrings(xyz.Flags), ",") != "NEW,SKEW" || xyz.FeePct != nil {
		t.Errorf("XYZ = %s %v fee=%v", xyz.Grade, xyz.Flags, xyz.FeePct)
	}
	if efg := row(t, r, "EFG"); efg.Grade != GradeSuspect || len(efg.Flags) != 0 {
		t.Errorf("EFG = %s %v", efg.Grade, efg.Flags)
	}
	if ghi := row(t, r, "GHI"); !ghi.HasFlag(FlagSwing) {
		t.Errorf("GHI flags %v", ghi.Flags)
	}
	if bcd := row(t, r, "BCD"); !bcd.HasFlag(FlagFDV) || bcd.McapSource != McapSourceFDV || bcd.McapUSD != 1500000 {
		t.Errorf("BCD = %v %s %v", bcd.Flags, bcd.McapSource, bcd.McapUSD)
	}

	if r.Pick == nil || r.Pick.CoinSymbol != "ABC" || r.Pick.Rank != 2 {
		t.Fatalf("pick = %+v, want ABC rank 2", r.Pick)
	}
}

func flagStrings(fs []Flag) []string {
	s := make([]string, len(fs))
	for i, f := range fs {
		s[i] = string(f)
	}
	return s
}

func TestRankOneRowPerCoinUsesBestPool(t *testing.T) {
	p := legacyParams()
	p.Top = MaxTop
	r := Rank(loadFixture(t), p, testNow)
	n := 0
	for _, x := range r.Results {
		if x.CoinSymbol == "ABC" {
			n++
			if x.Pair != "ABC/WETH" {
				t.Errorf("ABC kept pool %s, want the higher-scoring ABC/WETH", x.Pair)
			}
		}
	}
	if n != 1 {
		t.Errorf("ABC appears %d times", n)
	}
	if got := coins(r); got != "EFG,ABC,XYZ,GHI,BCD,DEF,HIJ,JKL" {
		t.Errorf("all coins = %s", got)
	}
	if def := row(t, r, "DEF"); !def.HasFlag(FlagFDV) {
		t.Errorf("DEF (null market cap) flags %v", def.Flags)
	}
	if hij := row(t, r, "HIJ"); hij.HasFlag(FlagNew) || hij.PoolCreatedAt != nil {
		t.Errorf("HIJ (no created date) flags %v", hij.Flags)
	}
}

func TestRankParams(t *testing.T) {
	tests := []struct {
		name string
		mod  func(*Params)
		want string
	}{
		{"max-mcap lowers", func(p *Params) { p.MaxMcap = 1_000_000 }, "HIJ,JKL"},
		{"max-mcap raises", func(p *Params) { p.MaxMcap = 10_000_000; p.Top = MaxTop }, "EFG,ABC,XYZ,GHI,MNO,BCD,DEF,HIJ,JKL"},
		{"min-liquidity", func(p *Params) { p.MinLiquidity = 15_000; p.Top = MaxTop }, "PQR,EFG,ABC,XYZ,GHI,BCD,DEF,HIJ,JKL"},
		{"min-volume", func(p *Params) { p.MinVolume = 5_000; p.Top = MaxTop }, "EFG,ABC,XYZ,GHI,BCD,DEF,STU,HIJ,JKL"},
		{"top 2", func(p *Params) { p.Top = 2 }, "EFG,ABC"},
		{"top above passing coins", func(p *Params) { p.Top = 20 }, "EFG,ABC,XYZ,GHI,BCD,DEF,HIJ,JKL"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := legacyParams()
			tc.mod(&p)
			if got := coins(Rank(loadFixture(t), p, testNow)); got != tc.want {
				t.Errorf("coins = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestRankWindowSwitchesFields(t *testing.T) {
	p := legacyParams()
	p.Window = Window24h
	r := Rank(loadFixture(t), p, testNow)
	abc := row(t, r, "ABC")
	if abc.VolumeUSD != 1_425_000 || abc.PriceChangePct != 16.4 || abc.Buys != 900 {
		t.Errorf("24h ABC volume=%v change=%v buys=%v", abc.VolumeUSD, abc.PriceChangePct, abc.Buys)
	}
	// per-day turnover at 24h is not scaled
	if !near(abc.TurnoverD, 1_425_000/412_300.0) {
		t.Errorf("24h turnover_d = %v", abc.TurnoverD)
	}
}

func TestPick(t *testing.T) {
	pools := loadFixture(t)

	// yield beats score: HIJ scores higher but has no fee; JKL has a fee
	p := legacyParams()
	p.MaxMcap = 1_000_000
	if r := Rank(pools, p, testNow); r.Pick == nil || r.Pick.CoinSymbol != "JKL" {
		t.Errorf("pick = %+v, want JKL by yield", r.Pick)
	}

	// no eligible row has a fee: fall back to score
	r := Rank(only(pools, "XYZ", "HIJ"), legacyParams(), testNow)
	if r.Pick == nil || r.Pick.CoinSymbol != "HIJ" || !strings.Contains(r.Pick.Reason, "fee tier unknown") {
		t.Errorf("pick = %+v, want HIJ by score", r.Pick)
	}

	// only flagged rows: no pick
	if r := Rank(only(pools, "XYZ", "GHI", "BCD"), legacyParams(), testNow); r.Pick != nil {
		t.Errorf("pick = %+v, want nil", r.Pick)
	}
}

func TestRankZeroResults(t *testing.T) {
	p := legacyParams()
	p.MinLiquidity = 1e12
	r := Rank(loadFixture(t), p, testNow)
	if len(r.Results) != 0 || r.Results == nil || r.Pick != nil || r.Stats.Scanned != 15 {
		t.Errorf("zero results = %+v", r)
	}
}

func TestRankDedupesPoolsByAddress(t *testing.T) {
	pools := loadFixture(t)
	r := Rank(append(pools, pools...), legacyParams(), testNow)
	if r.Stats.Scanned != 15 || r.Stats.Passed != 9 {
		t.Errorf("stats with duplicates = %+v", r.Stats)
	}
}

func TestParseFeePct(t *testing.T) {
	tests := map[string]float64{
		"NVDA / WETH 0.05%":  0.05,
		"PONS / WETH 1%":     1,
		"WETH / USDG 0.046%": 0.046,
		"UNIWALL / WETH 10%": 10,
		"GIGATENEV / WETH":   -1,
		"":                   -1,
	}
	for name, want := range tests {
		got := ParseFeePct(name)
		switch {
		case want < 0 && got != nil:
			t.Errorf("%q = %v, want nil", name, *got)
		case want >= 0 && (got == nil || *got != want):
			t.Errorf("%q = %v, want %v", name, got, want)
		}
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		mod  func(*Params)
		want string
	}{
		{func(p *Params) {}, ""},
		{func(p *Params) { p.MaxMcap = 0 }, "--max-mcap must be > 0"},
		{func(p *Params) { p.Window = "4h" }, "--window must be one of 5m, 1h, 6h, 24h"},
		{func(p *Params) { p.MinLiquidity = -1 }, "--min-liquidity must be >= 0"},
		{func(p *Params) { p.MinVolume = math.NaN() }, "--min-volume must be >= 0"},
		{func(p *Params) { p.Top = 51 }, "--top must be between 1 and 50"},
		{func(p *Params) { p.Pages = 0 }, "--pages must be between 1 and 10"},
	}
	for _, tc := range tests {
		p := legacyParams()
		tc.mod(&p)
		err := p.Validate()
		got := ""
		if err != nil {
			got = err.Error()
		}
		if got != tc.want {
			t.Errorf("Validate() = %q, want %q", got, tc.want)
		}
	}
}
