package lpscan

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/dorman99/plutus/pkg/geckoterminal"
)

func newTestScanner(src PoolSource) *Scanner {
	s := New(src)
	s.now = func() time.Time { return testNow }
	return s
}

func TestScanInterleavesAndDedupes(t *testing.T) {
	pools := loadFixture(t)
	var calls []string
	src := PoolSourceFunc(func(ctx context.Context, ep geckoterminal.Endpoint, page int) ([]geckoterminal.Pool, error) {
		calls = append(calls, fmt.Sprintf("%s:%d", ep, page))
		if page > 1 {
			return nil, nil // exhausted
		}
		return pools, nil // every endpoint returns the same pools
	})
	r, err := newTestScanner(src).Scan(context.Background(), DefaultParams())
	if err != nil {
		t.Fatal(err)
	}
	want := "pools:1,trending_pools:1,new_pools:1,pools:2,trending_pools:2,new_pools:2"
	if got := strings.Join(calls, ","); got != want {
		t.Errorf("calls = %s, want %s", got, want)
	}
	if r.Stats.Scanned != 15 || len(r.Warnings) != 0 {
		t.Errorf("stats=%+v warnings=%v", r.Stats, r.Warnings)
	}
}

func TestScanEndpointFailureIsAWarning(t *testing.T) {
	pools := loadFixture(t)
	var trendingCalls int
	src := PoolSourceFunc(func(ctx context.Context, ep geckoterminal.Endpoint, page int) ([]geckoterminal.Pool, error) {
		if ep == geckoterminal.EndpointTrending {
			trendingCalls++
			return nil, errors.New("HTTP 503")
		}
		return pools, nil
	})
	r, err := newTestScanner(src).Scan(context.Background(), DefaultParams())
	if err != nil {
		t.Fatal(err)
	}
	if trendingCalls != 1 {
		t.Errorf("failed endpoint called %d times, want 1", trendingCalls)
	}
	if len(r.Warnings) != 1 || !strings.Contains(r.Warnings[0], "trending_pools") {
		t.Errorf("warnings = %v", r.Warnings)
	}
	if len(r.Results) == 0 {
		t.Error("expected results from the other endpoints")
	}
}

func TestScanTimeoutKeepsPartialResults(t *testing.T) {
	pools := loadFixture(t)
	n := 0
	src := PoolSourceFunc(func(ctx context.Context, ep geckoterminal.Endpoint, page int) ([]geckoterminal.Pool, error) {
		n++
		if n == 1 {
			return pools, nil
		}
		<-ctx.Done()
		return nil, ctx.Err()
	})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	r, err := newTestScanner(src).Scan(ctx, DefaultParams())
	if err != nil {
		t.Fatalf("want partial result, got %v", err)
	}
	if len(r.Warnings) != 1 || !strings.Contains(r.Warnings[0], "timed out after 1/9 requests") {
		t.Errorf("warnings = %v", r.Warnings)
	}
	if len(r.Results) == 0 {
		t.Error("expected results from the first page")
	}
}

func TestScanNothingFetchedIsAnError(t *testing.T) {
	src := PoolSourceFunc(func(ctx context.Context, ep geckoterminal.Endpoint, page int) ([]geckoterminal.Pool, error) {
		return nil, errors.New("connection refused")
	})
	_, err := newTestScanner(src).Scan(context.Background(), DefaultParams())
	var fe *FetchError
	if !errors.As(err, &fe) {
		t.Fatalf("err = %v, want *FetchError", err)
	}
}

func TestScanRejectsInvalidParams(t *testing.T) {
	p := DefaultParams()
	p.Window = "4h"
	_, err := newTestScanner(nil).Scan(context.Background(), p)
	var pe *ParamError
	if !errors.As(err, &pe) || pe.Field != "window" {
		t.Fatalf("err = %v, want window ParamError", err)
	}
}
