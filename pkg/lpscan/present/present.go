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
const NoMatches = "No coins matched. Try a wider --min-mcap / --max-mcap, --kind all, --source all or lower --min-liquidity / --min-volume."

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
// message), verdict summary, pick line, legend and disclaimer. Rows with a
// plan get the detailed layout; without, the score layout.
func Table(w io.Writer, r *lpscan.Result) error {
	ew := &errWriter{w: w}
	p := r.Params
	win := string(p.Window)

	ew.printf("lpscan  network=%s  source=%s  kind=%s  window=%s  mcap=%s-%s  min-liq=%s  min-vol=%s  top=%d  detail=%s",
		r.Network, p.Source, p.Kind, win, Money(p.MinMcap), Money(p.MaxMcap), Money(p.MinLiquidity), Money(p.MinVolume), p.Top, p.Detail)
	if p.MaxTop10 != nil {
		ew.printf("  max-top10=%g%%", *p.MaxTop10)
	}
	ew.printf("\n")
	ew.printf("scanned %d pools -> %d passed filters -> %d coins\n\n", r.Stats.Scanned, r.Stats.Passed, r.Stats.Coins)

	detailed := hasPlans(r)
	switch {
	case len(r.Results) == 0:
		ew.printf("%s\n", NoMatches)
	case detailed:
		detailTable(ew, r)
	default:
		scoreTable(ew, r, win)
	}
	if len(r.Results) > 0 {
		if r.Pick != nil {
			ew.printf("%s pick: %s (%s) — %s\n", PickMarker, r.Pick.CoinSymbol, r.Pick.CoinAddress, r.Pick.Reason)
		} else {
			ew.printf("%s pick: none — no clear candidate this run\n", PickMarker)
		}
	}

	lg := r.Legend
	ew.printf("\nLegend:\n")
	if detailed {
		verdicts := make([]string, 0, len(lg.Verdicts))
		for _, v := range lg.Verdicts {
			verdicts = append(verdicts, v.Code+" "+v.Meaning)
		}
		ew.printf("  %-7s %s\n", "VERDICT", strings.Join(verdicts, " · "))
		ew.printf("  %-7s %s\n", "NET 7d", lg.Net7d)
	}
	for _, f := range lg.Flags {
		ew.printf("  %-7s %s\n", f.Code, f.Meaning)
	}
	grades := make([]string, 0, len(lg.Grades))
	for _, g := range lg.Grades {
		grades = append(grades, g.Code+" "+g.Meaning)
	}
	ew.printf("  %-7s %s (per-day score)\n", "GRADE", strings.Join(grades, " · "))
	ew.printf("  %-7s %s\n", "KIND", lg.Kind)
	ew.printf("  %-7s %s; %s\n", PickMarker, lg.Pick, lg.PickGuard)
	ew.printf("%s\n", lg.Disclaimer)
	return ew.err
}

func hasPlans(r *lpscan.Result) bool {
	for _, row := range r.Results {
		if row.Detail != nil && row.Detail.Plan != nil {
			return true
		}
	}
	return false
}

func rankCell(r *lpscan.Result, row lpscan.Row) string {
	rank := strconv.Itoa(row.Rank)
	if r.Pick != nil && r.Pick.Rank == row.Rank {
		rank = PickMarker + rank
	}
	return rank
}

func scoreTable(ew *errWriter, r *lpscan.Result, win string) {
	tw := tabwriter.NewWriter(ew, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "#\tCOIN\tPAIR\tLIQUIDITY\tVOL(%s)\tTURNOVER/d\tFEE\tEST YIELD/d\tCHG(%s)\tMCAP\tSCORE\tGRADE\tFLAGS\tCA\n", win, win)
	for _, row := range r.Results {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%.2f\t%s\t%s\t%+.1f%%\t%s\t%.2f\t%s\t%s\t%s\n",
			rankCell(r, row), row.CoinSymbol, row.Pair, Money(row.LiquidityUSD), Money(row.VolumeUSD),
			row.TurnoverD, pct(row.FeePct, -1), pct(row.EstFeeYieldD, 2), row.PriceChangePct,
			Money(row.McapUSD), row.Score, row.Grade, flags(row.Flags), row.CoinAddress)
	}
	tw.Flush()
	ew.printf("\n")
}

func detailTable(ew *errWriter, r *lpscan.Result) {
	tw := tabwriter.NewWriter(ew, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "#\tCOIN\tVERDICT\tMCAP\tGT\tHOLDERS\tTOP10\tHONEYPOT\tPOOL\tσ 7d\tMODE\tYIELD/d\tFEES 7d\tIL\tNET 7d\tEXITS NOW\tCA\n")
	counts := map[string]int{}
	for _, row := range r.Results {
		d := row.Detail
		verdict, gt, holders, top10, hp := "—", "—", "—", "—", "—"
		pool, sigma, mode, yield, fees, il, net, exits := "—", "—", "—", "—", "—", "—", "—", "-"
		if d != nil {
			gt, top10, hp = num(d.GTScore, 1), pct(d.Top10Pct, 1), d.IsHoneypot
			if d.HoldersCount != nil {
				holders = strconv.Itoa(*d.HoldersCount)
			}
		}
		if d != nil && d.Plan != nil {
			pd := d.Plan
			verdict = strings.ToUpper(pd.Verdict)
			counts[verdict]++
			if pd.ChosenPool != nil {
				pool = pd.ChosenPair + " " + pct(pd.ChosenFeePct, -1)
				if pd.ChosenLiquidityUSD != nil {
					pool += " " + Money(*pd.ChosenLiquidityUSD)
				}
			}
			sigma, yield, fees, il, net = pct(pd.Sigma7dPct, 1), pct(pd.EstFeeYieldD, 2), pct(pd.EstFee7dPct, 1), pct(pd.ILLowPct, 1), pct(pd.Net7dPct, 1)
			if pd.Mode != "" {
				mode = strings.Replace(pd.Mode, "_", " ", 1)
			}
			if len(pd.TriggeredExits) > 0 {
				exits = strings.Join(pd.TriggeredExits, ",")
			}
		} else {
			counts["NO PLAN"]++
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			rankCell(r, row), row.CoinSymbol, verdict, Money(row.McapUSD), gt, holders, top10, hp,
			pool, sigma, mode, yield, fees, il, net, exits, row.CoinAddress)
	}
	tw.Flush()

	var parts []string
	for _, k := range []string{"GO", "WAIT", "AVOID", "NO PLAN"} {
		if counts[k] > 0 {
			parts = append(parts, fmt.Sprintf("%s %d", k, counts[k]))
		}
	}
	ew.printf("\n%s\n", strings.Join(parts, " · "))
	for _, row := range r.Results {
		if row.Detail == nil || row.Detail.Plan == nil {
			continue
		}
		for _, rs := range row.Detail.Plan.Reasons {
			ew.printf("  %s: %s (%s): %s\n", row.CoinSymbol, rs.Code, rs.Tier, rs.Message)
		}
	}
	ew.printf("\n")
}

func num(v *float64, prec int) string {
	if v == nil {
		return "—"
	}
	return strconv.FormatFloat(*v, 'f', prec, 64)
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
