// Package geckoterminal is a small, rate-limited client for the free
// GeckoTerminal public API (https://api.geckoterminal.com/api/v2).
//
// It is shared by every plutus stage that needs on-chain pool or token data,
// so the free-tier limits are enforced here, once, for all callers.
package geckoterminal

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

// Endpoint is a network pool listing endpoint.
type Endpoint string

const (
	EndpointPools    Endpoint = "pools"          // top pools by liquidity and activity
	EndpointTrending Endpoint = "trending_pools" // pools with rising attention
	EndpointNew      Endpoint = "new_pools"      // recently created pools
)

// Token is the subset of token attributes plutus uses.
type Token struct {
	Address string `json:"address"`
	Symbol  string `json:"symbol"`
	Name    string `json:"name"`
}

// Txns holds transaction counts for one time window.
type Txns struct {
	Buys    int `json:"buys"`
	Sells   int `json:"sells"`
	Buyers  int `json:"buyers"`
	Sellers int `json:"sellers"`
}

// Pool is a decoded pool. Numeric fields are nil when the API sent null,
// an empty value or something unparseable. A zero value is kept as zero;
// deciding whether zero means "missing" is the caller's job.
//
// Window-keyed maps use the API keys: "m5", "m15", "m30", "h1", "h6", "h24".
type Pool struct {
	Address        string
	Name           string
	DexID          string
	CreatedAt      *time.Time
	MarketCapUSD   *float64
	FDVUSD         *float64
	ReserveUSD     *float64
	VolumeUSD      map[string]*float64
	PriceChangePct map[string]*float64
	Transactions   map[string]*Txns
	BaseToken      Token
	QuoteToken     Token
}

// DecodePools decodes a JSON:API pool listing response, resolving base and
// quote tokens from the "included" section when present.
func DecodePools(r io.Reader) ([]Pool, error) {
	var resp rawResponse
	if err := json.NewDecoder(r).Decode(&resp); err != nil {
		return nil, fmt.Errorf("decode pools: %w", err)
	}

	tokens := make(map[string]Token, len(resp.Included))
	for _, inc := range resp.Included {
		if inc.Type != "token" {
			continue
		}
		tokens[inc.ID] = Token{
			Address: strings.ToLower(inc.Attributes.Address),
			Symbol:  inc.Attributes.Symbol,
			Name:    inc.Attributes.Name,
		}
	}

	pools := make([]Pool, 0, len(resp.Data))
	for _, d := range resp.Data {
		a := d.Attributes
		p := Pool{
			Address:        strings.ToLower(a.Address),
			Name:           a.Name,
			DexID:          d.Relationships.Dex.id(),
			CreatedAt:      parseTime(a.PoolCreatedAt),
			MarketCapUSD:   a.MarketCapUSD.Val,
			FDVUSD:         a.FDVUSD.Val,
			ReserveUSD:     a.ReserveInUSD.Val,
			VolumeUSD:      unwrap(a.VolumeUSD),
			PriceChangePct: unwrap(a.PriceChangePercentage),
			Transactions:   a.Transactions,
			BaseToken:      resolveToken(tokens, d.Relationships.BaseToken.id()),
			QuoteToken:     resolveToken(tokens, d.Relationships.QuoteToken.id()),
		}
		pools = append(pools, p)
	}
	return pools, nil
}

// resolveToken looks the token up in "included"; without it, the address is
// recovered from the relationship id ("robinhood_0xabc..." -> "0xabc...").
func resolveToken(tokens map[string]Token, id string) Token {
	if t, ok := tokens[id]; ok {
		return t
	}
	if i := strings.LastIndex(id, "_"); i >= 0 {
		return Token{Address: strings.ToLower(id[i+1:])}
	}
	return Token{Address: strings.ToLower(id)}
}

func parseTime(s *string) *time.Time {
	if s == nil || *s == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339, *s)
	if err != nil {
		return nil
	}
	return &t
}

func unwrap(m map[string]flexFloat) map[string]*float64 {
	out := make(map[string]*float64, len(m))
	for k, v := range m {
		out[k] = v.Val
	}
	return out
}

type rawResponse struct {
	Data     []rawPool     `json:"data"`
	Included []rawIncluded `json:"included"`
}

type rawPool struct {
	ID         string `json:"id"`
	Type       string `json:"type"`
	Attributes struct {
		Address               string               `json:"address"`
		Name                  string               `json:"name"`
		PoolCreatedAt         *string              `json:"pool_created_at"`
		MarketCapUSD          flexFloat            `json:"market_cap_usd"`
		FDVUSD                flexFloat            `json:"fdv_usd"`
		ReserveInUSD          flexFloat            `json:"reserve_in_usd"`
		VolumeUSD             map[string]flexFloat `json:"volume_usd"`
		PriceChangePercentage map[string]flexFloat `json:"price_change_percentage"`
		Transactions          map[string]*Txns     `json:"transactions"`
	} `json:"attributes"`
	Relationships struct {
		BaseToken  rawRel `json:"base_token"`
		QuoteToken rawRel `json:"quote_token"`
		Dex        rawRel `json:"dex"`
	} `json:"relationships"`
}

type rawRel struct {
	Data *struct {
		ID   string `json:"id"`
		Type string `json:"type"`
	} `json:"data"`
}

func (r rawRel) id() string {
	if r.Data == nil {
		return ""
	}
	return r.Data.ID
}

type rawIncluded struct {
	ID         string `json:"id"`
	Type       string `json:"type"`
	Attributes struct {
		Address string `json:"address"`
		Name    string `json:"name"`
		Symbol  string `json:"symbol"`
	} `json:"attributes"`
}

// flexFloat accepts a JSON number, a numeric string or null. Anything it
// cannot parse becomes nil (missing) instead of failing the whole response.
type flexFloat struct{ Val *float64 }

func (f *flexFloat) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || bytes.Equal(b, []byte("null")) {
		f.Val = nil
		return nil
	}
	s := string(b)
	if b[0] == '"' {
		if err := json.Unmarshal(b, &s); err != nil {
			f.Val = nil
			return nil
		}
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		f.Val = nil
		return nil
	}
	f.Val = &v
	return nil
}
