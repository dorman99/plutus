package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/dorman99/plutus/pkg/geckoterminal"
)

const ca = "0x1111111111111111111111111111111111111111"

type fake struct {
	tokenErr error
	gotCA    string
}

func (s *fake) Token(_ context.Context, a string) (*geckoterminal.TokenDetail, error) {
	s.gotCA = a
	if s.tokenErr != nil {
		return nil, s.tokenErr
	}
	return &geckoterminal.TokenDetail{Address: a, Symbol: "ABC"}, nil
}
func (s *fake) TokenInfo(context.Context, string) (*geckoterminal.TokenInfo, error) {
	return &geckoterminal.TokenInfo{IsHoneypot: "no"}, nil
}
func (s *fake) TokenPools(context.Context, string) ([]geckoterminal.Pool, error) {
	v := func(x float64) *float64 { return &x }
	created := time.Now().Add(-30 * 24 * time.Hour)
	return []geckoterminal.Pool{{
		Address: "0xpool", Name: "ABC / USDG 0.3%", DexID: "uniswap-v3-robinhood", CreatedAt: &created,
		ReserveUSD: v(100_000), VolumeUSD: map[string]*float64{"h24": v(100_000), "h6": v(25_000)},
		PriceChangePct: map[string]*float64{"h24": v(1)},
		BaseToken:      geckoterminal.Token{Address: ca, Symbol: "ABC"}, QuoteToken: geckoterminal.Token{Address: "0x2", Symbol: "USDG"},
		BasePriceUSD: v(2), QuotePriceUSD: v(1), BasePriceQuote: v(2),
	}}, nil
}
func (s *fake) PoolOHLCV(context.Context, string) ([]geckoterminal.Candle, error) {
	out := make([]geckoterminal.Candle, 168)
	for i := range out {
		out[i] = geckoterminal.Candle{Time: time.Unix(int64(i*3600), 0), Close: 2 + float64(i%2)*0.02}
	}
	return out, nil
}

func runCLI(src *fake, args ...string) (int, string, string) {
	var out, errb bytes.Buffer
	code := run(context.Background(), args, &out, &errb, src)
	return code, out.String(), errb.String()
}

func TestCAPositionAndJSON(t *testing.T) {
	for _, args := range [][]string{
		{strings.ToUpper(ca[:2]) + ca[2:], "--json", "--amount", "500"},
		{"--json", "--amount", "500", ca},
	} {
		src := &fake{}
		code, out, errOut := runCLI(src, args...)
		if code != exitOK {
			t.Fatalf("%v: exit %d: %s", args, code, errOut)
		}
		if src.gotCA != ca {
			t.Errorf("CA = %q", src.gotCA)
		}
		var m struct {
			Params struct {
				Amount *float64 `json:"amount_usd"`
				Risk   string   `json:"risk"`
			} `json:"params"`
			Verdict struct{ Decision string } `json:"verdict"`
		}
		if err := json.Unmarshal([]byte(out), &m); err != nil {
			t.Fatal(err)
		}
		if m.Params.Amount == nil || *m.Params.Amount != 500 || m.Params.Risk != "balanced" {
			t.Errorf("params = %+v", m.Params)
		}
	}
}

func TestTableDefault(t *testing.T) {
	code, out, _ := runCLI(&fake{}, ca)
	if code != exitOK || !strings.Contains(out, "VERDICT") || !strings.Contains(out, "A decision brief") {
		t.Fatalf("exit %d\n%s", code, out)
	}
}

func TestBadArgs(t *testing.T) {
	cases := map[string][]string{
		"CA is required":            {"--json"},
		"not a contract address":    {"0x12"},
		"--risk must be one of":     {ca, "--risk", "yolo"},
		"--amount must be > 0":      {ca, "--amount", "-1"},
		"unexpected argument":       {ca, "0x2222222222222222222222222222222222222222"},
		"flag provided but not def": {ca, "--window", "6h"},
		"--timeout must be > 0":     {ca, "--timeout", "0s"},
	}
	for want, args := range cases {
		code, _, errOut := runCLI(&fake{}, args...)
		if code != exitParam || !strings.Contains(errOut, want) {
			t.Errorf("%v: exit %d, stderr %q, want %q", args, code, errOut, want)
		}
	}
}

func TestAPIError(t *testing.T) {
	code, _, errOut := runCLI(&fake{tokenErr: errors.New("HTTP 404")}, ca)
	if code != exitAPI || !strings.Contains(errOut, "HTTP 404") {
		t.Errorf("exit %d stderr %q", code, errOut)
	}
}
