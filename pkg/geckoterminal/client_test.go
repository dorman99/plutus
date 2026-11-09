package geckoterminal

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const onePool = `{
 "data": [{
  "id": "robinhood_0xpool", "type": "pool",
  "attributes": {
   "address": "0xPOOL", "name": "ABC / WETH 0.3%",
   "pool_created_at": "2026-07-02T20:51:08Z",
   "market_cap_usd": null, "fdv_usd": "625380246.17", "reserve_in_usd": 13813862.49,
   "volume_usd": {"m5": "1569.9", "h6": "not-a-number", "h24": "0.0"},
   "price_change_percentage": {"h6": "-0.234"},
   "transactions": {"h6": {"buys": 29474, "sells": 30920, "buyers": 3004, "sellers": 3199}}
  },
  "relationships": {
   "base_token": {"data": {"id": "robinhood_0xABC", "type": "token"}},
   "quote_token": {"data": {"id": "robinhood_0xweth", "type": "token"}},
   "dex": {"data": {"id": "uniswap-v3-robinhood", "type": "dex"}}
  }
 }],
 "included": [
  {"id": "robinhood_0xABC", "type": "token", "attributes": {"address": "0xABC", "name": "Abc", "symbol": "ABC"}},
  {"id": "uniswap-v3-robinhood", "type": "dex", "attributes": {"name": "Uniswap V3"}}
 ]
}`

func TestDecodePools(t *testing.T) {
	pools, err := DecodePools(strings.NewReader(onePool))
	if err != nil {
		t.Fatal(err)
	}
	if len(pools) != 1 {
		t.Fatalf("got %d pools", len(pools))
	}
	p := pools[0]
	if p.Address != "0xpool" || p.DexID != "uniswap-v3-robinhood" || p.CreatedAt == nil {
		t.Errorf("pool = %+v", p)
	}
	if p.MarketCapUSD != nil {
		t.Errorf("null market cap decoded as %v", *p.MarketCapUSD)
	}
	if p.FDVUSD == nil || *p.FDVUSD != 625380246.17 || p.ReserveUSD == nil || *p.ReserveUSD != 13813862.49 {
		t.Errorf("fdv/reserve = %v/%v", p.FDVUSD, p.ReserveUSD)
	}
	if p.VolumeUSD["h6"] != nil {
		t.Errorf("unparseable volume decoded as %v", *p.VolumeUSD["h6"])
	}
	if v := p.VolumeUSD["h24"]; v == nil || *v != 0 {
		t.Errorf("zero volume = %v", v)
	}
	if v := p.PriceChangePct["h6"]; v == nil || *v != -0.234 {
		t.Errorf("price change = %v", v)
	}
	if tx := p.Transactions["h6"]; tx == nil || tx.Buyers != 3004 {
		t.Errorf("txns = %+v", tx)
	}
	if p.BaseToken != (Token{Address: "0xabc", Symbol: "ABC", Name: "Abc"}) {
		t.Errorf("base = %+v", p.BaseToken)
	}
	// not in "included": address recovered from the relationship id
	if p.QuoteToken.Address != "0xweth" || p.QuoteToken.Symbol != "" {
		t.Errorf("quote = %+v", p.QuoteToken)
	}
}

func fastClient(url string, opts ...Option) *Client {
	base := []Option{
		WithBaseURL(url),
		WithMinInterval(0),
		WithBackoff([]time.Duration{time.Millisecond, 2 * time.Millisecond, 4 * time.Millisecond}),
	}
	return New(append(base, opts...)...)
}

func TestClientRetries429(t *testing.T) {
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/networks/robinhood/pools" || r.URL.Query().Get("page") != "2" ||
			r.URL.Query().Get("include") != "base_token,quote_token,dex" {
			t.Errorf("unexpected request %s", r.URL)
		}
		if n.Add(1) <= 2 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Write([]byte(onePool))
	}))
	defer srv.Close()

	pools, err := fastClient(srv.URL).Pools(context.Background(), "robinhood", EndpointPools, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(pools) != 1 || n.Load() != 3 {
		t.Errorf("pools=%d attempts=%d", len(pools), n.Load())
	}
}

func TestClientGivesUpAfterThreeRetries(t *testing.T) {
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	_, err := fastClient(srv.URL).Pools(context.Background(), "robinhood", EndpointPools, 1)
	var he *HTTPError
	if !errors.As(err, &he) || he.Status != 502 || n.Load() != 4 {
		t.Fatalf("err=%v attempts=%d", err, n.Load())
	}
}

func TestClientDoesNotRetry4xx(t *testing.T) {
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	_, err := fastClient(srv.URL).Pools(context.Background(), "robinhood", EndpointPools, 1)
	if err == nil || n.Load() != 1 || !strings.Contains(err.Error(), "403") {
		t.Fatalf("err=%v attempts=%d", err, n.Load())
	}
}

func TestClientEnforcesMinInterval(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(onePool))
	}))
	defer srv.Close()

	c := fastClient(srv.URL, WithMinInterval(80*time.Millisecond))
	start := time.Now()
	for i := 0; i < 3; i++ {
		if _, err := c.Pools(context.Background(), "robinhood", EndpointPools, 1); err != nil {
			t.Fatal(err)
		}
	}
	if el := time.Since(start); el < 160*time.Millisecond {
		t.Errorf("3 calls took %v, want >= 160ms", el)
	}
}

func TestClientWontSleepPastDeadline(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	c := fastClient(srv.URL, WithBackoff([]time.Duration{10 * time.Second}))
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := c.Pools(ctx, "robinhood", EndpointPools, 1)
	if !IsDeadline(err) {
		t.Fatalf("err = %v, want deadline", err)
	}
	if el := time.Since(start); el > 150*time.Millisecond {
		t.Errorf("gave up after %v; should not wait for a backoff past the deadline", el)
	}
}
