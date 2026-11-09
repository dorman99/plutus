package lpscan

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/dorman99/plutus/pkg/geckoterminal"
)

func fp(v float64) *float64 { return &v }

func TestKindOf(t *testing.T) {
	for name, want := range map[string]Kind{
		"United States Oil Fund • Robinhood Token": KindStock,
		"Hims &amp; Hers Health • Robinhood Token": KindStock,
		"NVIDIA • Robinhood Token ":                KindStock,
		"Delta":                                    KindMeme,
		"Robinhood Token Fan Club":                 KindMeme,
		"":                                         KindMeme,
	} {
		if got := KindOf(name); got != want {
			t.Errorf("KindOf(%q) = %s, want %s", name, got, want)
		}
	}
}

func TestDefaultsKeepTrendingMemesInMcapRange(t *testing.T) {
	pools := loadFixture(t)
	for i := range pools {
		if pools[i].BaseToken.Symbol == "GHI" {
			pools[i].BaseToken.Name = "Intel • Robinhood Token"
		}
	}
	p := DefaultParams()
	p.Top = 50
	r := Rank(pools, p, testNow)
	// JKL ($0.9M) under the minimum, MNO ($8M) now inside the $20M maximum,
	// GHI is a stock.
	if got, want := coins(r), "EFG,ABC,XYZ,MNO,BCD,DEF,HIJ"; got != want {
		t.Errorf("coins = %s, want %s", got, want)
	}
	if row(t, r, "ABC").Kind != KindMeme {
		t.Error("ABC should be meme")
	}

	p.Kind = KindStock
	if got := coins(Rank(pools, p, testNow)); got != "GHI" {
		t.Errorf("--kind stock = %s, want GHI", got)
	}
	p.Kind, p.MinMcap = KindAll, 0
	if got := coins(Rank(pools, p, testNow)); !strings.Contains(got, "GHI") || !strings.Contains(got, "JKL") {
		t.Errorf("--kind all --min-mcap 0 = %s", got)
	}
}

func TestScanTrendingOnlyByDefault(t *testing.T) {
	pools := loadFixture(t)
	var calls []string
	src := PoolSourceFunc(func(ctx context.Context, ep geckoterminal.Endpoint, page int) ([]geckoterminal.Pool, error) {
		calls = append(calls, fmt.Sprintf("%s:%d", ep, page))
		return pools, nil
	})
	if _, err := newTestScanner(src).Scan(context.Background(), DefaultParams()); err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(calls, ","), "trending_pools:1,trending_pools:2,trending_pools:3"; got != want {
		t.Errorf("calls = %s, want %s", got, want)
	}
}

func plan(verdict string, net *float64) *Detail {
	return &Detail{Level: DetailFull, IsHoneypot: "no", Top10Pct: fp(30),
		Plan: &PlanDetail{Verdict: verdict, Net7dPct: net}}
}

func TestFinalizeOrdersByVerdictThenNet7d(t *testing.T) {
	res := &Result{Params: legacyParams(), Results: []Row{
		{CoinSymbol: "AVOIDER", Score: 9, Detail: plan("avoid", nil)},
		{CoinSymbol: "WAITER", Score: 8, Detail: plan("wait", fp(27.7))},
		{CoinSymbol: "LOWNET", Score: 7, Detail: plan("go", fp(3.9))},
		{CoinSymbol: "NODETAIL", Score: 6},
		{CoinSymbol: "HIGHNET", Score: 5, Detail: plan("go", fp(10.3))},
		{CoinSymbol: "NONET", Score: 4, Detail: plan("go", nil)},
	}}
	Finalize(res)
	if got, want := coins(res), "HIGHNET,LOWNET,NONET,WAITER,AVOIDER,NODETAIL"; got != want {
		t.Errorf("order = %s, want %s", got, want)
	}
	if res.Results[0].Rank != 1 || res.Results[5].Rank != 6 {
		t.Error("ranks not renumbered")
	}
	if res.Pick == nil || res.Pick.CoinSymbol != "HIGHNET" || !strings.Contains(res.Pick.Reason, "net 7d") {
		t.Errorf("pick = %+v", res.Pick)
	}
}

func TestPickGuards(t *testing.T) {
	bundle := plan("go", fp(20))
	bundle.Top10Pct = fp(91.8)
	unknown := plan("go", fp(15))
	unknown.IsHoneypot = "unknown"
	res := &Result{Params: legacyParams(), Results: []Row{
		{CoinSymbol: "USO", Detail: bundle},
		{CoinSymbol: "UNK", Detail: unknown},
		{CoinSymbol: "ORBIO", Detail: plan("go", fp(10))},
	}}
	Finalize(res)
	if res.Pick == nil || res.Pick.CoinSymbol != "ORBIO" {
		t.Fatalf("pick = %+v, want ORBIO", res.Pick)
	}
	if !strings.Contains(res.Pick.Reason, "skipped USO, UNK") {
		t.Errorf("reason = %q", res.Pick.Reason)
	}
	if coins(res) != "USO,UNK,ORBIO" {
		t.Errorf("guarded coins must stay listed: %s", coins(res))
	}

	// No pickable GO: no pick, even with a WAIT row.
	res = &Result{Params: legacyParams(), Results: []Row{
		{CoinSymbol: "USO", Detail: bundle},
		{CoinSymbol: "RAETO", Detail: plan("wait", fp(27))},
	}}
	if Finalize(res); res.Pick != nil {
		t.Errorf("pick = %+v, want none", res.Pick)
	}
}

func TestMaxTop10Filter(t *testing.T) {
	bundle := plan("go", fp(20))
	bundle.Top10Pct = fp(91.8)
	p := legacyParams()
	p.MaxTop10 = fp(60)
	res := &Result{Params: p, Results: []Row{
		{CoinSymbol: "USO", Detail: bundle},
		{CoinSymbol: "DELTA", Detail: plan("go", fp(5))},
		{CoinSymbol: "NODETAIL"},
	}}
	Finalize(res)
	if got := coins(res); got != "DELTA,NODETAIL" {
		t.Errorf("coins = %s, want DELTA,NODETAIL", got)
	}
}

type fakeDetailer struct {
	levels map[string]DetailLevel
	fail   map[string]bool
	net    map[string]float64
}

func (f *fakeDetailer) Detail(ctx context.Context, coin string, level DetailLevel) (*Detail, error) {
	f.levels[coin] = level
	if f.fail[coin] {
		return nil, errors.New("HTTP 404")
	}
	if level == DetailBasic {
		return &Detail{Level: DetailBasic, IsHoneypot: "no"}, nil
	}
	d := plan("go", fp(f.net[coin]))
	d.Warnings = []string{"price history unavailable"}
	return d, nil
}

func TestScanEnrichesAndReorders(t *testing.T) {
	pools := loadFixture(t)
	src := PoolSourceFunc(func(ctx context.Context, ep geckoterminal.Endpoint, page int) ([]geckoterminal.Pool, error) {
		if page > 1 {
			return nil, nil
		}
		return pools, nil
	})
	byCoin := map[string]string{}
	for _, p := range pools {
		byCoin[p.BaseToken.Symbol] = p.BaseToken.Address
	}
	d := &fakeDetailer{levels: map[string]DetailLevel{}, fail: map[string]bool{byCoin["XYZ"]: true},
		net: map[string]float64{byCoin["GHI"]: 12, byCoin["ABC"]: 4}}

	p := legacyParams()
	p.Top = 3 // EFG, ABC, XYZ by score
	r, err := newTestScanner(src).WithDetailer(d).Scan(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	if got := coins(r); got != "ABC,EFG,XYZ" {
		t.Errorf("order = %s, want ABC (net 4), EFG (net 0), XYZ (no detail)", got)
	}
	if row(t, r, "XYZ").Detail != nil {
		t.Error("failed coin should have no detail")
	}
	joined := strings.Join(r.Warnings, "\n")
	if !strings.Contains(joined, "XYZ ("+byCoin["XYZ"]+"): detail unavailable: HTTP 404") ||
		!strings.Contains(joined, "ABC: price history unavailable") {
		t.Errorf("warnings = %v", r.Warnings)
	}
	if r.Pick == nil || r.Pick.CoinSymbol != "ABC" {
		t.Errorf("pick = %+v", r.Pick)
	}
}

func TestScanCapsFullDetail(t *testing.T) {
	var pools []geckoterminal.Pool
	base := only(loadFixture(t), "ABC")[0]
	for i := 0; i < MaxFullDetail+2; i++ {
		p := base
		p.Address = fmt.Sprintf("0xpool%02d", i)
		p.BaseToken.Address = fmt.Sprintf("0x%040d", i)
		p.BaseToken.Symbol = fmt.Sprintf("C%02d", i)
		pools = append(pools, p)
	}
	src := PoolSourceFunc(func(ctx context.Context, ep geckoterminal.Endpoint, page int) ([]geckoterminal.Pool, error) {
		if page > 1 {
			return nil, nil
		}
		return pools, nil
	})
	d := &fakeDetailer{levels: map[string]DetailLevel{}, fail: map[string]bool{}, net: map[string]float64{}}
	p := legacyParams()
	p.Top = MaxFullDetail + 2
	r, err := newTestScanner(src).WithDetailer(d).Scan(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	var full, basic int
	for _, lv := range d.levels {
		if lv == DetailFull {
			full++
		} else {
			basic++
		}
	}
	if full != MaxFullDetail || basic != 2 {
		t.Errorf("full=%d basic=%d, want %d and 2", full, basic, MaxFullDetail)
	}
	if !strings.Contains(strings.Join(r.Warnings, "\n"), "capped at 10") {
		t.Errorf("warnings = %v", r.Warnings)
	}
}

func TestValidateNewParams(t *testing.T) {
	cases := map[string]func(*Params){
		"--min-mcap must be >= 0":                 func(p *Params) { p.MinMcap = -1 },
		"--min-mcap must not be above --max-mcap": func(p *Params) { p.MinMcap = 30e6 },
		"--source must be one of trending, all":   func(p *Params) { p.Source = "new" },
		"--kind must be one of meme, stock, all":  func(p *Params) { p.Kind = "etf" },
		"--detail must be one of basic, full":     func(p *Params) { p.Detail = "none" },
		"--max-top10 must be between 0 and 100":   func(p *Params) { p.MaxTop10 = fp(120) },
	}
	for want, mod := range cases {
		p := DefaultParams()
		mod(&p)
		if err := p.Validate(); err == nil || err.Error() != want {
			t.Errorf("err = %v, want %q", err, want)
		}
	}
	if err := DefaultParams().Validate(); err != nil {
		t.Errorf("defaults invalid: %v", err)
	}
}
