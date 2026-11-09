// Command lpscan prints the top Robinhood Chain coins worth reviewing for a
// liquidity position. It wires flags to lpscan.Scan and a presenter.
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
	"github.com/dorman99/plutus/pkg/lpscan"
	"github.com/dorman99/plutus/pkg/lpscan/present"
)

const (
	exitOK    = 0
	exitAPI   = 1
	exitParam = 2
)

func main() {
	os.Exit(run(context.Background(), os.Args[1:], os.Stdout, os.Stderr, nil))
}

// run is main without globals. src is nil in production (GeckoTerminal).
func run(ctx context.Context, args []string, stdout, stderr io.Writer, src lpscan.PoolSource) int {
	p := lpscan.DefaultParams()
	var (
		window  = string(p.Window)
		asJSON  bool
		timeout = 120 * time.Second
	)

	fs := flag.NewFlagSet("lpscan", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Float64Var(&p.MaxMcap, "max-mcap", p.MaxMcap, "skip coins above this market cap (USD); FDV used when market cap is missing")
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
	if err := p.Validate(); err != nil {
		fmt.Fprintf(stderr, "lpscan: %v\n", err)
		return exitParam
	}
	if timeout <= 0 {
		fmt.Fprintln(stderr, "lpscan: --timeout must be > 0")
		return exitParam
	}

	if src == nil {
		src = lpscan.GeckoSource(geckoterminal.New())
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	res, err := lpscan.New(src).Scan(ctx, p)
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
