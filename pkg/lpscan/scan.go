package lpscan

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/dorman99/plutus/pkg/geckoterminal"
)

// PoolSource fetches one page of one pool listing endpoint.
type PoolSource interface {
	Pools(ctx context.Context, ep geckoterminal.Endpoint, page int) ([]geckoterminal.Pool, error)
}

// PoolSourceFunc adapts a function to PoolSource.
type PoolSourceFunc func(ctx context.Context, ep geckoterminal.Endpoint, page int) ([]geckoterminal.Pool, error)

func (f PoolSourceFunc) Pools(ctx context.Context, ep geckoterminal.Endpoint, page int) ([]geckoterminal.Pool, error) {
	return f(ctx, ep, page)
}

// GeckoSource reads Robinhood Chain pools through a GeckoTerminal client.
func GeckoSource(c *geckoterminal.Client) PoolSource {
	return PoolSourceFunc(func(ctx context.Context, ep geckoterminal.Endpoint, page int) ([]geckoterminal.Pool, error) {
		return c.Pools(ctx, Network, ep, page)
	})
}

// Endpoints are fetched in this order on every page with --source all.
var Endpoints = []geckoterminal.Endpoint{
	geckoterminal.EndpointPools,
	geckoterminal.EndpointTrending,
	geckoterminal.EndpointNew,
}

// endpoints returns the listings fetched for src.
func endpoints(src Source) []geckoterminal.Endpoint {
	if src == SourceTrending {
		return []geckoterminal.Endpoint{geckoterminal.EndpointTrending}
	}
	return Endpoints
}

// FetchError means no pool data could be fetched at all.
type FetchError struct{ Errs []error }

func (e *FetchError) Error() string {
	if len(e.Errs) == 0 {
		return "no pool data fetched"
	}
	return fmt.Sprintf("no pool data fetched: %v", errors.Join(e.Errs...))
}

func (e *FetchError) Unwrap() []error { return e.Errs }

// Scanner fetches pools and ranks them. It holds no per-scan state, so one
// Scanner can be reused across scans (e.g. by a scheduled watcher).
type Scanner struct {
	src    PoolSource
	detail Detailer
	now    func() time.Time
}

// New returns a Scanner reading from src.
func New(src PoolSource) *Scanner { return &Scanner{src: src, now: time.Now} }

// WithDetailer makes Scan add per-coin detail (Params.Detail) to the top
// rows and order them by it. Without one, rows carry no detail.
func (s *Scanner) WithDetailer(d Detailer) *Scanner { s.detail = d; return s }

// Scan fetches up to (endpoints for p.Source) × p.Pages pages, interleaved by page so
// running out of time loses the deepest pages rather than a whole endpoint.
// The scan budget is ctx's deadline.
//
// A failing endpoint or a timeout after some data was fetched becomes a
// warning on the Result. Scan returns an error only for invalid params
// (*ParamError) or when nothing at all was fetched (*FetchError).
func (s *Scanner) Scan(ctx context.Context, p Params) (*Result, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}

	var (
		pools    []geckoterminal.Pool
		warnings []string
		errs     []error
		fetched  int
		eps      = endpoints(p.Source)
		planned  = len(eps) * p.Pages
		done     = map[geckoterminal.Endpoint]bool{} // failed or exhausted
		timedOut bool
	)

fetch:
	for page := 1; page <= p.Pages; page++ {
		for _, ep := range eps {
			if done[ep] {
				continue
			}
			if ctx.Err() != nil {
				timedOut = true
				break fetch
			}
			got, err := s.src.Pools(ctx, ep, page)
			if err != nil {
				if ctx.Err() != nil || geckoterminal.IsDeadline(err) {
					timedOut = true
					errs = append(errs, err)
					break fetch
				}
				done[ep] = true
				errs = append(errs, err)
				warnings = append(warnings, fmt.Sprintf("%s page %d failed, skipping this endpoint: %v", ep, page, err))
				continue
			}
			fetched++
			pools = append(pools, got...)
			if len(got) == 0 {
				done[ep] = true
			}
		}
	}

	if fetched == 0 {
		return nil, &FetchError{Errs: errs}
	}
	if timedOut {
		warnings = append(warnings, fmt.Sprintf("scan timed out after %d/%d requests; results may be incomplete", fetched, planned))
	}

	res := Rank(pools, p, s.now())
	res.Warnings = append(res.Warnings, warnings...)
	if s.detail != nil && len(res.Results) > 0 {
		enrich(ctx, s.detail, res)
		Finalize(res)
	}
	return res, nil
}
