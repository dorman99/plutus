package geckoterminal

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"sort"
	"strings"
	"time"
)

// TokenDetail is the subset of /tokens/{address} attributes plutus uses.
type TokenDetail struct {
	Address         string
	Symbol          string
	Name            string
	PriceUSD        *float64
	MarketCapUSD    *float64
	FDVUSD          *float64
	TotalReserveUSD *float64
	Volume24hUSD    *float64
}

// TokenInfo is the subset of /tokens/{address}/info attributes plutus uses.
// Every field is nil (or "unknown") when the API did not send it.
type TokenInfo struct {
	GTScore         *float64
	GTVerified      bool
	HoldersCount    *int
	Top10Pct        *float64
	IsHoneypot      string // "yes", "no" or "unknown"
	MintAuthority   *string
	FreezeAuthority *string
}

// Candle is one OHLCV bar.
type Candle struct {
	Time                   time.Time
	Open, High, Low, Close float64
	Volume                 float64
}

// Token fetches one token's market data.
func (c *Client) Token(ctx context.Context, network, address string) (*TokenDetail, error) {
	body, err := c.get(ctx, fmt.Sprintf("%s/networks/%s/tokens/%s", c.baseURL, url.PathEscape(network), url.PathEscape(address)))
	if err != nil {
		return nil, err
	}
	defer body.Close()
	return DecodeToken(body)
}

// TokenInfo fetches one token's metadata: holders, GT score, honeypot status.
func (c *Client) TokenInfo(ctx context.Context, network, address string) (*TokenInfo, error) {
	body, err := c.get(ctx, fmt.Sprintf("%s/networks/%s/tokens/%s/info", c.baseURL, url.PathEscape(network), url.PathEscape(address)))
	if err != nil {
		return nil, err
	}
	defer body.Close()
	return DecodeTokenInfo(body)
}

// TokenPools fetches one page of the pools that trade a token (as base or
// quote), with base token, quote token and dex included.
func (c *Client) TokenPools(ctx context.Context, network, address string, page int) ([]Pool, error) {
	u := fmt.Sprintf("%s/networks/%s/tokens/%s/pools?page=%d&include=%s",
		c.baseURL, url.PathEscape(network), url.PathEscape(address), page, url.QueryEscape("base_token,quote_token,dex"))
	body, err := c.get(ctx, u)
	if err != nil {
		return nil, err
	}
	defer body.Close()
	return DecodePools(body)
}

// PoolOHLCV fetches up to limit hourly candles for a pool, oldest first.
// Prices are the base token priced in the quote token (currency=token), the
// pool ratio that drives impermanent loss.
func (c *Client) PoolOHLCV(ctx context.Context, network, pool string, limit int) ([]Candle, error) {
	u := fmt.Sprintf("%s/networks/%s/pools/%s/ohlcv/hour?aggregate=1&limit=%d&currency=token",
		c.baseURL, url.PathEscape(network), url.PathEscape(pool), limit)
	body, err := c.get(ctx, u)
	if err != nil {
		return nil, err
	}
	defer body.Close()
	return DecodeOHLCV(body)
}

// DecodeToken decodes a /tokens/{address} response.
func DecodeToken(r io.Reader) (*TokenDetail, error) {
	var resp struct {
		Data struct {
			Attributes struct {
				Address         string    `json:"address"`
				Symbol          string    `json:"symbol"`
				Name            string    `json:"name"`
				PriceUSD        flexFloat `json:"price_usd"`
				MarketCapUSD    flexFloat `json:"market_cap_usd"`
				FDVUSD          flexFloat `json:"fdv_usd"`
				TotalReserveUSD flexFloat `json:"total_reserve_in_usd"`
				VolumeUSD       struct {
					H24 flexFloat `json:"h24"`
				} `json:"volume_usd"`
			} `json:"attributes"`
		} `json:"data"`
	}
	if err := json.NewDecoder(r).Decode(&resp); err != nil {
		return nil, fmt.Errorf("decode token: %w", err)
	}
	a := resp.Data.Attributes
	if a.Address == "" {
		return nil, fmt.Errorf("decode token: response has no token")
	}
	return &TokenDetail{
		Address:         strings.ToLower(a.Address),
		Symbol:          a.Symbol,
		Name:            a.Name,
		PriceUSD:        a.PriceUSD.Val,
		MarketCapUSD:    a.MarketCapUSD.Val,
		FDVUSD:          a.FDVUSD.Val,
		TotalReserveUSD: a.TotalReserveUSD.Val,
		Volume24hUSD:    a.VolumeUSD.H24.Val,
	}, nil
}

// DecodeTokenInfo decodes a /tokens/{address}/info response.
func DecodeTokenInfo(r io.Reader) (*TokenInfo, error) {
	var resp struct {
		Data struct {
			Attributes struct {
				GTScore    flexFloat `json:"gt_score"`
				GTVerified bool      `json:"gt_verified"`
				Holders    *struct {
					Count        *int `json:"count"`
					Distribution struct {
						Top10 flexFloat `json:"top_10"`
					} `json:"distribution_percentage"`
				} `json:"holders"`
				IsHoneypot      json.RawMessage `json:"is_honeypot"`
				MintAuthority   *string         `json:"mint_authority"`
				FreezeAuthority *string         `json:"freeze_authority"`
			} `json:"attributes"`
		} `json:"data"`
	}
	if err := json.NewDecoder(r).Decode(&resp); err != nil {
		return nil, fmt.Errorf("decode token info: %w", err)
	}
	a := resp.Data.Attributes
	info := &TokenInfo{
		GTScore:         a.GTScore.Val,
		GTVerified:      a.GTVerified,
		IsHoneypot:      honeypot(a.IsHoneypot),
		MintAuthority:   nonEmpty(a.MintAuthority),
		FreezeAuthority: nonEmpty(a.FreezeAuthority),
	}
	if a.Holders != nil {
		info.HoldersCount = a.Holders.Count
		info.Top10Pct = a.Holders.Distribution.Top10.Val
	}
	return info, nil
}

// honeypot normalizes is_honeypot, which may be a bool, a string or null.
func honeypot(raw json.RawMessage) string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return "unknown"
	}
	var b bool
	if err := json.Unmarshal(raw, &b); err == nil {
		if b {
			return "yes"
		}
		return "no"
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		switch strings.ToLower(strings.TrimSpace(s)) {
		case "yes", "true":
			return "yes"
		case "no", "false":
			return "no"
		}
	}
	return "unknown"
}

func nonEmpty(s *string) *string {
	if s == nil || strings.TrimSpace(*s) == "" {
		return nil
	}
	return s
}

// DecodeOHLCV decodes an ohlcv response into candles sorted oldest first.
// Bars with fewer than six values are skipped, and a repeated timestamp
// keeps its first bar (the live API occasionally sends one twice).
func DecodeOHLCV(r io.Reader) ([]Candle, error) {
	var resp struct {
		Data struct {
			Attributes struct {
				List [][]float64 `json:"ohlcv_list"`
			} `json:"attributes"`
		} `json:"data"`
	}
	if err := json.NewDecoder(r).Decode(&resp); err != nil {
		return nil, fmt.Errorf("decode ohlcv: %w", err)
	}
	out := make([]Candle, 0, len(resp.Data.Attributes.List))
	seen := map[int64]bool{}
	for _, b := range resp.Data.Attributes.List {
		if len(b) < 6 || seen[int64(b[0])] {
			continue
		}
		seen[int64(b[0])] = true
		out = append(out, Candle{
			Time: time.Unix(int64(b[0]), 0).UTC(),
			Open: b[1], High: b[2], Low: b[3], Close: b[4], Volume: b[5],
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Time.Before(out[j].Time) })
	return out, nil
}
