package geckoterminal

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func open(t *testing.T, name string) *os.File {
	t.Helper()
	f, err := os.Open("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	return f
}

func TestDecodeTokenFixture(t *testing.T) {
	tok, err := DecodeToken(open(t, "token.json"))
	if err != nil {
		t.Fatal(err)
	}
	if tok.Address != "0xa30fa36db767ad9ed3f7a60fc79526fb4d56d344" || tok.Symbol != "USO" {
		t.Errorf("token = %+v", tok)
	}
	if tok.PriceUSD == nil || tok.MarketCapUSD == nil || tok.FDVUSD == nil || tok.Volume24hUSD == nil {
		t.Errorf("numbers missing: %+v", tok)
	}
}

func TestDecodeTokenEmpty(t *testing.T) {
	if _, err := DecodeToken(strings.NewReader(`{"data":{}}`)); err == nil {
		t.Error("want error for a response without a token")
	}
}

func TestDecodeTokenInfoFixture(t *testing.T) {
	info, err := DecodeTokenInfo(open(t, "token_info.json"))
	if err != nil {
		t.Fatal(err)
	}
	if info.GTScore == nil || info.HoldersCount == nil || *info.HoldersCount <= 0 || info.Top10Pct == nil {
		t.Errorf("info = %+v", info)
	}
	if info.IsHoneypot != "unknown" || info.MintAuthority != nil || info.FreezeAuthority != nil || !info.GTVerified {
		t.Errorf("info = %+v", info)
	}
}

func TestHoneypotForms(t *testing.T) {
	for raw, want := range map[string]string{
		`true`: "yes", `false`: "no", `"yes"`: "yes", `"No"`: "no",
		`"unknown"`: "unknown", `null`: "unknown", ``: "unknown",
	} {
		body := `{"data":{"attributes":{"is_honeypot":` + raw + `}}}`
		if raw == "" {
			body = `{"data":{"attributes":{}}}`
		}
		info, err := DecodeTokenInfo(strings.NewReader(body))
		if err != nil {
			t.Fatalf("%s: %v", raw, err)
		}
		if info.IsHoneypot != want {
			t.Errorf("is_honeypot %s = %q, want %q", raw, info.IsHoneypot, want)
		}
		if info.HoldersCount != nil || info.Top10Pct != nil {
			t.Errorf("%s: holders should be nil without data", raw)
		}
	}
}

func TestDecodeTokenPoolsFixture(t *testing.T) {
	pools, err := DecodePools(open(t, "token_pools.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(pools) != 20 {
		t.Fatalf("got %d pools", len(pools))
	}
	p := pools[0]
	if p.Name != "USO / USDG 0.3%" || p.BaseToken.Symbol != "USO" || p.QuoteToken.Symbol != "USDG" {
		t.Errorf("pool = %+v", p)
	}
	if p.BasePriceQuote == nil || p.BasePriceUSD == nil || p.QuotePriceUSD == nil {
		t.Errorf("prices missing: %+v", p)
	}
}

func TestDecodeOHLCVFixture(t *testing.T) {
	cs, err := DecodeOHLCV(open(t, "ohlcv.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != 167 { // 168 bars, one duplicated timestamp
		t.Fatalf("got %d candles", len(cs))
	}
	for i := 1; i < len(cs); i++ {
		if !cs[i].Time.After(cs[i-1].Time) {
			t.Fatalf("candles not ascending at %d", i)
		}
	}
}

func TestDecodeOHLCVSkipsShortBars(t *testing.T) {
	cs, err := DecodeOHLCV(strings.NewReader(`{"data":{"attributes":{"ohlcv_list":[[2,1,1,1,1,1],[1,2],[1,3,3,3,3,3],[1,9,9,9,9,9]]}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != 2 || cs[0].Close != 3 || cs[1].Close != 1 {
		t.Errorf("candles = %+v", cs)
	}
}

func TestTokenEndpointsURLs(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.RequestURI())
		switch {
		case strings.HasSuffix(r.URL.Path, "/info"):
			w.Write([]byte(`{"data":{"attributes":{}}}`))
		case strings.HasSuffix(r.URL.Path, "/pools"):
			w.Write([]byte(`{"data":[]}`))
		case strings.Contains(r.URL.Path, "/ohlcv/"):
			w.Write([]byte(`{"data":{"attributes":{"ohlcv_list":[]}}}`))
		default:
			w.Write([]byte(`{"data":{"attributes":{"address":"0xA"}}}`))
		}
	}))
	defer srv.Close()

	c := New(WithBaseURL(srv.URL), WithMinInterval(time.Millisecond))
	ctx := context.Background()
	if _, err := c.Token(ctx, "robinhood", "0xa"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.TokenInfo(ctx, "robinhood", "0xa"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.TokenPools(ctx, "robinhood", "0xa", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := c.PoolOHLCV(ctx, "robinhood", "0xp", 168); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"/networks/robinhood/tokens/0xa",
		"/networks/robinhood/tokens/0xa/info",
		"/networks/robinhood/tokens/0xa/pools?page=1&include=base_token%2Cquote_token%2Cdex",
		"/networks/robinhood/pools/0xp/ohlcv/hour?aggregate=1&limit=168&currency=token",
	}
	for i, w := range want {
		if i >= len(paths) || paths[i] != w {
			t.Errorf("request %d = %q, want %q", i, paths, w)
		}
	}
}
