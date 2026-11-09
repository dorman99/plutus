// Command lpplan prints an LP decision brief for one Robinhood Chain coin.
// It wires arguments to lpplan.Plan and a presenter.
//
// Usage: lpplan <CA> [--amount USD] [--risk safe|balanced|aggressive] [--json] [--timeout 120s]
//
// Exit codes: 0 success (including an avoid verdict), 1 API or network
// error, 2 invalid argument.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/dorman99/plutus/pkg/geckoterminal"
	"github.com/dorman99/plutus/pkg/lpplan"
	"github.com/dorman99/plutus/pkg/lpplan/present"
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
func run(ctx context.Context, args []string, stdout, stderr io.Writer, src lpplan.Source) int {
	var (
		p       lpplan.Params
		amount  float64
		risk    = string(lpplan.RiskBalanced)
		asJSON  bool
		timeout = 120 * time.Second
	)

	fs := flag.NewFlagSet("lpplan", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Float64Var(&amount, "amount", 0, "optional deposit in USD; adds the token split and $ estimates")
	fs.StringVar(&risk, "risk", risk, "range width: safe (±2σ), balanced (±1σ) or aggressive (±0.5σ)")
	fs.BoolVar(&asJSON, "json", false, "print JSON instead of the brief")
	fs.DurationVar(&timeout, "timeout", timeout, "total timeout")

	// Accept the CA before or after the flags.
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		p.CA, args = args[0], args[1:]
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fs.SetOutput(stdout)
			fmt.Fprintln(stdout, "Usage: lpplan <CA> [flags]")
			fs.PrintDefaults()
			return exitOK
		}
		fmt.Fprintf(stderr, "lpplan: %v\n", err)
		return exitParam
	}
	rest := fs.Args()
	if p.CA == "" && len(rest) > 0 {
		p.CA, rest = rest[0], rest[1:]
	}
	if len(rest) > 0 {
		fmt.Fprintf(stderr, "lpplan: unexpected argument %q (one CA only)\n", rest[0])
		return exitParam
	}
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "amount" {
			p.Amount = &amount
		}
	})
	p.Risk = lpplan.Risk(risk)
	p = p.Normalize()
	if err := p.Validate(); err != nil {
		fmt.Fprintf(stderr, "lpplan: %v\n", err)
		return exitParam
	}
	if timeout <= 0 {
		fmt.Fprintln(stderr, "lpplan: --timeout must be > 0")
		return exitParam
	}

	if src == nil {
		src = lpplan.GeckoSource(geckoterminal.New())
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	plan, err := lpplan.New(src).Plan(ctx, p)
	if err != nil {
		var pe *lpplan.ParamError
		if errors.As(err, &pe) {
			fmt.Fprintf(stderr, "lpplan: %v\n", err)
			return exitParam
		}
		fmt.Fprintf(stderr, "lpplan: %v\n", err)
		return exitAPI
	}

	if asJSON {
		err = present.JSON(stdout, plan)
	} else {
		present.Warnings(stderr, plan)
		err = present.Table(stdout, plan)
	}
	if err != nil {
		fmt.Fprintf(stderr, "lpplan: write output: %v\n", err)
		return exitAPI
	}
	return exitOK
}
