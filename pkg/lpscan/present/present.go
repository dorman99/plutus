// Package present renders an lpscan.Result. It never scans: callers pass a
// finished Result, so other UIs can add their own presenter alongside these.
package present

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/dorman99/plutus/pkg/lpscan"
)

// NoMatches is printed instead of an empty table.
const NoMatches = "No coins matched. Try a higher --max-mcap or lower --min-liquidity / --min-volume."

// PickMarker prefixes the picked row's rank.
const PickMarker = "★"

// JSON writes r as indented JSON.
func JSON(w io.Writer, r *lpscan.Result) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

// Warnings writes one "warning: ..." line per warning. The CLI sends these
// to stderr so the table stays clean when piped or pasted.
func Warnings(w io.Writer, r *lpscan.Result) error {
	for _, msg := range r.Warnings {
		if _, err := fmt.Fprintf(w, "warning: %s\n", msg); err != nil {
			return err
		}
	}
	return nil
}

// Table writes the header, stats line, ranked table (or the no-match
// message), pick line, legend and disclaimer.
func Table(w io.Writer, r *lpscan.Result) error {
	ew := &errWriter{w: w}
	p := r.Params
	win := string(p.Window)

	ew.printf("lpscan  network=%s  window=%s  max-mcap=%s  min-liq=%s  min-vol=%s  top=%d\n",
		r.Network, win, Money(p.MaxMcap), Money(p.MinLiquidity), Money(p.MinVolume), p.Top)
	ew.printf("scanned %d pools -> %d passed filters -> %d coins\n\n", r.Stats.Scanned, r.Stats.Passed, r.Stats.Coins)

	if len(r.Results) == 0 {
		ew.printf("%s\n", NoMatches)
	} else {
		tw := tabwriter.NewWriter(ew, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "#\tCOIN\tPAIR\tLIQUIDITY\tVOL(%s)\tTURNOVER/d\tFEE\tEST YIELD/d\tCHG(%s)\tMCAP\tSCORE\tGRADE\tFLAGS\tCA\n", win, win)
		for _, row := range r.Results {
			rank := strconv.Itoa(row.Rank)
			if r.Pick != nil && r.Pick.Rank == row.Rank {
				rank = PickMarker + rank
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%.2f\t%s\t%s\t%+.1f%%\t%s\t%.2f\t%s\t%s\t%s\n",
				rank, row.CoinSymbol, row.Pair, Money(row.LiquidityUSD), Money(row.VolumeUSD),
				row.TurnoverD, pct(row.FeePct, -1), pct(row.EstFeeYieldD, 2), row.PriceChangePct,
				Money(row.McapUSD), row.Score, row.Grade, flags(row.Flags), row.CoinAddress)
		}
		tw.Flush()
		ew.printf("\n")
		if r.Pick != nil {
			ew.printf("%s pick: %s (%s) — %s\n", PickMarker, r.Pick.CoinSymbol, r.Pick.CoinAddress, r.Pick.Reason)
		} else {
			ew.printf("%s pick: none — no clear candidate this run\n", PickMarker)
		}
	}

	lg := r.Legend
	ew.printf("\nLegend:\n")
	for _, f := range lg.Flags {
		ew.printf("  %-6s %s\n", f.Code, f.Meaning)
	}
	grades := make([]string, 0, len(lg.Grades))
	for _, g := range lg.Grades {
		grades = append(grades, g.Code+" "+g.Meaning)
	}
	ew.printf("  %-6s %s (per-day score)\n", "GRADE", strings.Join(grades, " · "))
	ew.printf("  %-6s %s\n", PickMarker, lg.Pick)
	ew.printf("%s\n", lg.Disclaimer)
	return ew.err
}

// Money abbreviates a USD amount: $950, $412.3K, $1.9M, $2.1B.
func Money(v float64) string {
	sign := ""
	if v < 0 {
		sign, v = "-", -v
	}
	units := []struct {
		div    float64
		suffix string
	}{{1e9, "B"}, {1e6, "M"}, {1e3, "K"}}
	for _, u := range units {
		// also step up when the smaller unit would round to 1000 (999,960 is $1.0M, not $1000.0K)
		if v >= u.div || math.Round(v/(u.div/1000)*10)/10 >= 1000 {
			return fmt.Sprintf("%s$%.1f%s", sign, v/u.div, u.suffix)
		}
	}
	return fmt.Sprintf("%s$%.0f", sign, v)
}

// pct formats a nullable percentage; prec -1 keeps the shortest exact form.
func pct(v *float64, prec int) string {
	if v == nil {
		return "—"
	}
	return strconv.FormatFloat(*v, 'f', prec, 64) + "%"
}

func flags(fs []lpscan.Flag) string {
	if len(fs) == 0 {
		return "-"
	}
	s := make([]string, len(fs))
	for i, f := range fs {
		s[i] = string(f)
	}
	return strings.Join(s, ",")
}

type errWriter struct {
	w   io.Writer
	err error
}

func (e *errWriter) Write(b []byte) (int, error) {
	if e.err != nil {
		return 0, e.err
	}
	n, err := e.w.Write(b)
	e.err = err
	return n, err
}

func (e *errWriter) printf(format string, a ...any) { fmt.Fprintf(e, format, a...) }
