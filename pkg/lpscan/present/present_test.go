package present

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/dorman99/plutus/pkg/lpscan"
)

const caABC = "0x6162630000000000000000000000000000000000"

func f(v float64) *float64 { return &v }

// sample is a hand-built Result: presenters are tested without scanning.
func sample() *lpscan.Result {
	return &lpscan.Result{
		ScannedAt: time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC),
		Network:   "robinhood",
		Params:    lpscan.DefaultParams(),
		Stats:     lpscan.Stats{Scanned: 142, Passed: 23, Coins: 18},
		Results: []lpscan.Row{
			{Rank: 1, CoinSymbol: "ABC", CoinAddress: caABC, Pair: "ABC/WETH", LiquidityUSD: 412_300, VolumeUSD: 1_900_000,
				TurnoverD: 4.61, FeePct: f(0.3), EstFeeYieldD: f(1.383), PriceChangePct: 8.2, McapUSD: 3_100_000,
				Score: 4.26, Grade: lpscan.GradeStrong, Flags: []lpscan.Flag{}, PoolAddress: "0xpool1"},
			{Rank: 2, CoinSymbol: "XYZ", CoinAddress: "0x78797a0000000000000000000000000000000000", Pair: "XYZ/USDG",
				LiquidityUSD: 96_000, VolumeUSD: 310_500, TurnoverD: 3.23, PriceChangePct: -4, McapUSD: 1_200_000,
				Score: 3.11, Grade: lpscan.GradeSuspect, Flags: []lpscan.Flag{lpscan.FlagNew, lpscan.FlagSkew}, PoolAddress: "0xpool2"},
		},
		Pick:     &lpscan.Pick{Rank: 1, CoinSymbol: "ABC", CoinAddress: caABC, Reason: "highest est. yield/d among unflagged coins"},
		Warnings: []string{"trending_pools page 1 failed"},
		Legend:   lpscan.LegendData(),
	}
}

func TestTable(t *testing.T) {
	var buf bytes.Buffer
	if err := Table(&buf, sample()); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{
		"lpscan  network=robinhood  source=trending  kind=meme  window=6h  mcap=$1.0M-$20.0M  min-liq=$50.0K  min-vol=$25.0K  top=5  detail=full",
		"scanned 142 pools -> 23 passed filters -> 18 coins",
		"VOL(6h)", "CHG(6h)", "TURNOVER/d", "EST YIELD/d", "CA",
		"★1", "$412.3K", "$1.9M", "0.3%", "1.38%", "+8.2%", "strong", caABC,
		"NEW,SKEW", "—",
		"★ pick: ABC (" + caABC + ") — highest est. yield/d among unflagged coins",
		"Legend:", "FDV ", "NEW ", "SKEW ", "SWING ", "GRADE ", "suspect >5 or SKEW",
		"Candidates to review, not a safety check",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("table missing %q\n%s", want, out)
		}
	}
	if strings.Contains(out, "warning:") {
		t.Error("warnings must not be in the table output")
	}
}

func TestTableDetailed(t *testing.T) {
	r := sample()
	holders := 17027
	r.Results[0].Detail = &lpscan.Detail{Level: lpscan.DetailFull, GTScore: f(79.68), HoldersCount: &holders,
		Top10Pct: f(30.82), IsHoneypot: "no", Plan: &lpscan.PlanDetail{
			Verdict: "go", ChosenPool: strPtr("0xpool1"), ChosenPair: "ABC/WETH", ChosenFeePct: f(1), ChosenLiquidityUSD: f(366_385),
			Mode: "full_range", Sigma7dPct: f(61.48), EstFeeYieldD: f(1.394), EstFee7dPct: f(9.76), ILLowPct: f(-4.55), Net7dPct: f(5.21),
			TriggeredExits: []string{}}}
	r.Results[1].Detail = &lpscan.Detail{Level: lpscan.DetailFull, IsHoneypot: "unknown", Plan: &lpscan.PlanDetail{
		Verdict: "wait", Reasons: []lpscan.PlanReason{{Code: "LOW_TURNOVER", Tier: "wait", Message: "chosen pool turnover/d 0.37 is below the 0.5 exit threshold"}},
		ChosenPool: strPtr("0xpool2"), ChosenPair: "XYZ/USDG", ChosenFeePct: f(3.04), ChosenLiquidityUSD: f(68_701), Mode: "full_range",
		TriggeredExits: []string{"TURNOVER_DROP"}}}
	r.Pick.Reason = "highest net 7d (est. fees minus IL) among GO coins"

	var buf bytes.Buffer
	if err := Table(&buf, r); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{
		"VERDICT", "HOLDERS", "TOP10", "HONEYPOT", "NET 7d", "EXITS NOW",
		"★1", "GO", "79.7", "17027", "30.8%", "ABC/WETH 1% $366.4K", "61.5%", "full range", "1.39%", "9.8%", "-4.5%", "5.2%", caABC,
		"WAIT", "unknown", "TURNOVER_DROP",
		"GO 1 · WAIT 1",
		"XYZ: LOW_TURNOVER (wait): chosen pool turnover/d 0.37",
		"★ pick: ABC (" + caABC + ") — highest net 7d",
		"VERDICT GO no blocking signal", "NET 7d  est. 7-day fees", "KIND ", "never ★: top 10 holders above 80%",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("table missing %q\n%s", want, out)
		}
	}
	if strings.Contains(out, "SCORE") {
		t.Error("detailed layout should not print the score columns")
	}
}

func strPtr(s string) *string { return &s }

func TestTableZeroResultsStillPrintsLegend(t *testing.T) {
	r := sample()
	r.Results, r.Pick = []lpscan.Row{}, nil
	var buf bytes.Buffer
	if err := Table(&buf, r); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{NoMatches, "Legend:", "SWING", "not a safety check"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q\n%s", want, out)
		}
	}
	if strings.Contains(out, "COIN") {
		t.Error("printed an empty table")
	}
}

func TestTableNoPick(t *testing.T) {
	r := sample()
	r.Pick = nil
	var buf bytes.Buffer
	Table(&buf, r)
	if !strings.Contains(buf.String(), "★ pick: none — no clear candidate this run") || strings.Contains(buf.String(), "★1") {
		t.Errorf("no-pick output:\n%s", buf.String())
	}
}

func TestJSON(t *testing.T) {
	var buf bytes.Buffer
	if err := JSON(&buf, sample()); err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"scanned_at", "params", "stats", "results", "pick", "warnings", "legend"} {
		if _, ok := got[k]; !ok {
			t.Errorf("missing key %q", k)
		}
	}
	row := got["results"].([]any)[1].(map[string]any)
	if row["coin_address"] != "0x78797a0000000000000000000000000000000000" || row["pool_address"] != "0xpool2" {
		t.Errorf("row = %v", row)
	}
	if row["fee_pct"] != nil || row["est_fee_yield_d"] != nil {
		t.Errorf("unknown fee should be null: %v / %v", row["fee_pct"], row["est_fee_yield_d"])
	}
}

func TestWarnings(t *testing.T) {
	var buf bytes.Buffer
	Warnings(&buf, sample())
	if buf.String() != "warning: trending_pools page 1 failed\n" {
		t.Errorf("warnings = %q", buf.String())
	}
}

func TestMoney(t *testing.T) {
	for v, want := range map[float64]string{
		950: "$950", 50_000: "$50.0K", 412_300: "$412.3K", 999_960: "$1.0M",
		1_900_000: "$1.9M", 5_000_000: "$5.0M", 2_100_000_000: "$2.1B", 0: "$0",
	} {
		if got := Money(v); got != want {
			t.Errorf("Money(%v) = %s, want %s", v, got, want)
		}
	}
}
