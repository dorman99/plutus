package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/dorman99/plutus/pkg/geckoterminal"
	"github.com/dorman99/plutus/pkg/lpscan"
)

func fixtureSource(t *testing.T) lpscan.PoolSource {
	t.Helper()
	f, err := os.Open("../../pkg/lpscan/testdata/pools.json")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	pools, err := geckoterminal.DecodePools(f)
	if err != nil {
		t.Fatal(err)
	}
	return lpscan.PoolSourceFunc(func(ctx context.Context, ep geckoterminal.Endpoint, page int) ([]geckoterminal.Pool, error) {
		if page > 1 {
			return nil, nil
		}
		return pools, nil
	})
}

func runCLI(t *testing.T, src lpscan.PoolSource, args ...string) (int, string, string) {
	t.Helper()
	return runCLIWith(t, src, nil, args...)
}

func runCLIWith(t *testing.T, src lpscan.PoolSource, det lpscan.Detailer, args ...string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := run(context.Background(), args, &out, &errOut, src, det)
	return code, out.String(), errOut.String()
}

// stubDetailer gives every coin a GO plan; top-10 share from top10.
type stubDetailer struct{ top10 map[string]float64 }

func (s stubDetailer) Detail(_ context.Context, coin string, level lpscan.DetailLevel) (*lpscan.Detail, error) {
	t10, net := s.top10[coin], 5.0
	return &lpscan.Detail{Level: level, IsHoneypot: "no", Top10Pct: &t10,
		Plan: &lpscan.PlanDetail{Verdict: "go", Net7dPct: &net, TriggeredExits: []string{}}}, nil
}

func TestInvalidFlagsExit2(t *testing.T) {
	tests := map[string][]string{
		"--window must be one of 5m, 1h, 6h, 24h": {"--window", "4h"},
		"--top must be between 1 and 50":          {"--top", "0"},
		"--max-mcap must be > 0":                  {"--max-mcap", "-1"},
		"-min-volume":                             {"--min-volume", "lots"},
		"--timeout must be > 0":                   {"--timeout", "0s"},
		"flag provided but not defined: -foo":     {"--foo"},
		"--min-mcap must not be above --max-mcap": {"--min-mcap", "30000000"},
		"--source must be one of trending, all":   {"--source", "new"},
		"--kind must be one of meme, stock, all":  {"--kind", "etf"},
		"--detail must be one of basic, full":     {"--detail", "none"},
		"--max-top10 must be between 0 and 100":   {"--max-top10", "120"},
	}
	for want, args := range tests {
		code, out, errOut := runCLI(t, nil, args...)
		if code != exitParam || !strings.Contains(errOut, want) || out != "" {
			t.Errorf("%v: code=%d stderr=%q", args, code, errOut)
		}
		if strings.Count(errOut, "\n") != 1 {
			t.Errorf("%v: want a one-line message, got %q", args, errOut)
		}
	}
}

func TestDefaultsPrintTable(t *testing.T) {
	code, out, _ := runCLI(t, fixtureSource(t))
	if code != exitOK {
		t.Fatalf("code = %d", code)
	}
	for _, want := range []string{"window=6h", "min-vol=$25.0K", "★2", "Legend:"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q\n%s", want, out)
		}
	}
}

func TestJSONOutput(t *testing.T) {
	code, out, _ := runCLI(t, fixtureSource(t), "--json", "--top", "3")
	if code != exitOK {
		t.Fatalf("code = %d", code)
	}
	var r lpscan.Result
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatal(err)
	}
	if len(r.Results) != 3 || r.Pick == nil || len(r.Results[0].CoinAddress) != 42 {
		t.Errorf("json result = %+v", r)
	}
}

func TestZeroMatchesExit0(t *testing.T) {
	code, out, _ := runCLI(t, fixtureSource(t), "--min-liquidity", "1000000000")
	if code != exitOK || !strings.Contains(out, "No coins matched.") {
		t.Errorf("code=%d out=%s", code, out)
	}
}

func TestAPIFailureExit1(t *testing.T) {
	src := lpscan.PoolSourceFunc(func(ctx context.Context, ep geckoterminal.Endpoint, page int) ([]geckoterminal.Pool, error) {
		return nil, errors.New("HTTP 503 after 4 attempt(s)")
	})
	code, out, errOut := runCLI(t, src)
	if code != exitAPI || out != "" || !strings.Contains(errOut, "503") {
		t.Errorf("code=%d out=%q stderr=%q", code, out, errOut)
	}
}

func TestDetailerFlagsAndJSON(t *testing.T) {
	abc := "0x6162630000000000000000000000000000000000"
	det := stubDetailer{top10: map[string]float64{abc: 91.8}}
	code, out, errOut := runCLIWith(t, fixtureSource(t), det, "--json", "--kind", "all", "--max-top10", "50", "--detail", "basic")
	if code != exitOK {
		t.Fatalf("code=%d stderr=%s", code, errOut)
	}
	var r lpscan.Result
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatal(err)
	}
	if r.Params.Kind != lpscan.KindAll || r.Params.Detail != lpscan.DetailBasic || r.Params.MaxTop10 == nil || *r.Params.MaxTop10 != 50 {
		t.Errorf("params = %+v", r.Params)
	}
	for _, row := range r.Results {
		if row.CoinAddress == abc {
			t.Error("ABC (top 10 = 91.8%) should be dropped by --max-top10 50")
		}
		if row.Detail == nil || row.Detail.Plan == nil {
			t.Errorf("%s has no detail", row.CoinSymbol)
		}
	}
	if !strings.Contains(out, `"net_7d_pct": 5`) || !strings.Contains(out, `"kind": "meme"`) {
		t.Error("json missing net_7d_pct or kind")
	}
}
