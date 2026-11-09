// Command lpscan prints the top Robinhood Chain coins worth reviewing for a
// liquidity position: by default trending meme coins between $1M and $20M
// market cap, each with its full lpplan verdict. It wires flags to
// lpscan.Scan (with lpplan as the Detailer) and a presenter.
//
// Exit codes: 0 success (including zero results), 1 API or network error,
// 2 invalid parameter.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/dorman99/plutus/pkg/geckoterminal"
	"github.com/dorman99/plutus/pkg/lpplan"
	"github.com/dorman99/plutus/pkg/lpscan"
	"github.com/dorman99/plutus/pkg/lpscan/present"
)

const (
	exitOK    = 0
	exitAPI   = 1
	exitParam = 2
)

func main() {
	os.Exit(run(context.Background(), os.Args[1:], os.Stdout, os.Stderr, nil, nil))
}

// run is main without globals. src and det are nil in production: both then
// read GeckoTerminal through one client, so they share its rate limiter.
// A test passing src but no det scans without detail.
func run(ctx context.Context, args []string, stdout, stderr io.Writer, src lpscan.PoolSource, det lpscan.Detailer) int {
	p := lpscan.DefaultParams()
	var (
		window   = string(p.Window)
		source   = string(p.Source)
		kind     = string(p.Kind)
		detail   = string(p.Detail)
		maxTop10 float64
		asJSON   bool
		timeout  = 360 * time.Second
	)

	fs := flag.NewFlagSet("lpscan", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Float64Var(&p.MinMcap, "min-mcap", p.MinMcap, "skip coins below this market cap (USD)")
	fs.Float64Var(&p.MaxMcap, "max-mcap", p.MaxMcap, "skip coins above this market cap (USD); FDV used when market cap is missing")
	fs.StringVar(&source, "source", source, "pool listings: trending, or all (pools, trending, new)")
	fs.StringVar(&kind, "kind", kind, "coins to keep: meme, stock (tokenized stocks/ETFs) or all")
	fs.StringVar(&detail, "detail", detail, "per-coin detail: full (lpplan verdict, ~4 calls/coin, first 10) or basic (token info, 1 call)")
	fs.Float64Var(&maxTop10, "max-top10", 0, "drop coins whose top-10 holders own more than this % (0 = off)")
	fs.StringVar(&window, "window", window, "time window: 5m, 1h, 6h or 24h")
	fs.Float64Var(&p.MinLiquidity, "min-liquidity", p.MinLiquidity, "minimum pool liquidity (USD)")
	fs.Float64Var(&p.MinVolume, "min-volume", p.MinVolume, "minimum volume inside --window (USD)")
	fs.IntVar(&p.Top, "top", p.Top, "how many coins to print (1-50)")
	fs.IntVar(&p.Pages, "pages", p.Pages, "pool pages per endpoint (1-10)")
	fs.BoolVar(&asJSON, "json", false, "print JSON instead of the table")
	fs.DurationVar(&timeout, "timeout", timeout, "total scan timeout")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fs.SetOutput(stdout)
			fmt.Fprintln(stdout, "Usage: lpscan [flags]")
			fs.PrintDefaults()
			return exitOK
		}
		fmt.Fprintf(stderr, "lpscan: %v\n", err)
		return exitParam
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "lpscan: unexpected argument %q\n", fs.Arg(0))
		return exitParam
	}
	p.Window = lpscan.Window(window)
	p.Source, p.Kind, p.Detail = lpscan.Source(source), lpscan.Kind(kind), lpscan.DetailLevel(detail)
	if maxTop10 != 0 {
		p.MaxTop10 = &maxTop10
	}
	if err := p.Validate(); err != nil {
		fmt.Fprintf(stderr, "lpscan: %v\n", err)
		return exitParam
	}
	if timeout <= 0 {
		fmt.Fprintln(stderr, "lpscan: --timeout must be > 0")
		return exitParam
	}

	if src == nil {
		c := geckoterminal.New()
		src = lpscan.GeckoSource(c)
		if det == nil {
			det = lpplan.ScanDetailer(lpplan.GeckoSource(c))
		}
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	sc := lpscan.New(src)
	if det != nil {
		sc.WithDetailer(det)
	}
	res, err := sc.Scan(ctx, p)
	if err != nil {
		var pe *lpscan.ParamError
		if errors.As(err, &pe) {
			fmt.Fprintf(stderr, "lpscan: %v\n", err)
			return exitParam
		}
		fmt.Fprintf(stderr, "lpscan: %v\n", err)
		return exitAPI
	}

	if asJSON {
		err = present.JSON(stdout, res)
	} else {
		present.Warnings(stderr, res)
		err = present.Table(stdout, res)
	}
	if err != nil {
		fmt.Fprintf(stderr, "lpscan: write output: %v\n", err)
		return exitAPI
	}
	return exitOK
}
