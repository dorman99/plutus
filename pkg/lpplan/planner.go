package lpplan

import (
	"context"
	"fmt"
	"time"

	"github.com/dorman99/plutus/pkg/geckoterminal"
)

// Source fetches the data a plan needs.
type Source interface {
	Token(ctx context.Context, ca string) (*geckoterminal.TokenDetail, error)
	TokenInfo(ctx context.Context, ca string) (*geckoterminal.TokenInfo, error)
	TokenPools(ctx context.Context, ca string) ([]geckoterminal.Pool, error)
	PoolOHLCV(ctx context.Context, pool string) ([]geckoterminal.Candle, error)
}

type geckoSource struct{ c *geckoterminal.Client }

// GeckoSource reads Robinhood Chain data through a GeckoTerminal client.
func GeckoSource(c *geckoterminal.Client) Source { return geckoSource{c} }

func (g geckoSource) Token(ctx context.Context, ca string) (*geckoterminal.TokenDetail, error) {
	return g.c.Token(ctx, Network, ca)
}
func (g geckoSource) TokenInfo(ctx context.Context, ca string) (*geckoterminal.TokenInfo, error) {
	return g.c.TokenInfo(ctx, Network, ca)
}
func (g geckoSource) TokenPools(ctx context.Context, ca string) ([]geckoterminal.Pool, error) {
	return g.c.TokenPools(ctx, Network, ca, 1)
}
func (g geckoSource) PoolOHLCV(ctx context.Context, pool string) ([]geckoterminal.Candle, error) {
	return g.c.PoolOHLCV(ctx, Network, pool, CandleLimit)
}

// Planner fetches data and analyzes it. It holds no per-plan state.
type Planner struct {
	src Source
	now func() time.Time
}

// New returns a Planner reading from src.
func New(src Source) *Planner { return &Planner{src: src, now: time.Now} }

// Plan makes up to four calls: token, info, pools, then candles for the
// chosen pool only. Token or pools failing is an error; info or candles
// failing becomes a warning. The budget is ctx's deadline.
func (pl *Planner) Plan(ctx context.Context, p Params) (*Plan, error) {
	p = p.Normalize()
	if err := p.Validate(); err != nil {
		return nil, err
	}

	var in Input
	warnings := []string{}
	var err error

	if in.Token, err = pl.src.Token(ctx, p.CA); err != nil {
		return nil, fmt.Errorf("token %s: %w", p.CA, err)
	}
	if in.Info, err = pl.src.TokenInfo(ctx, p.CA); err != nil {
		warnings = append(warnings, fmt.Sprintf("token info unavailable, holder and honeypot data missing: %v", err))
		in.Info = nil
	}
	if in.Pools, err = pl.src.TokenPools(ctx, p.CA); err != nil {
		return nil, fmt.Errorf("pools for %s: %w", p.CA, err)
	}

	now := pl.now()
	if rows := Rows(in.Pools, p.CA, now); Choose(rows) >= 0 {
		addr := rows[Choose(rows)].PoolAddress
		if in.Candles, err = pl.src.PoolOHLCV(ctx, addr); err != nil {
			warnings = append(warnings, fmt.Sprintf("price history unavailable for %s: %v", addr, err))
			in.Candles = nil
		}
	}

	plan := Analyze(in, p, now)
	plan.Warnings = append(warnings, plan.Warnings...)
	return plan, nil
}
