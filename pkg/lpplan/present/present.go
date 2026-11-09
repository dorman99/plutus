// Package present renders an lpplan.Plan. It never fetches: callers pass a
// finished Plan, so other UIs can add their own presenter alongside these.
package present

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/dorman99/plutus/pkg/lpplan"
	"github.com/dorman99/plutus/pkg/lpscan"
	scan "github.com/dorman99/plutus/pkg/lpscan/present"
)

// ChosenMarker prefixes the chosen pool.
const ChosenMarker = "★"

// JSON writes p as indented JSON.
func JSON(w io.Writer, p *lpplan.Plan) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(p)
}

// Warnings writes one "warning: ..." line per warning (the CLI sends these
// to stderr).
func Warnings(w io.Writer, p *lpplan.Plan) error {
	for _, msg := range p.Warnings {
		if _, err := fmt.Fprintf(w, "warning: %s\n", msg); err != nil {
			return err
		}
	}
	return nil
}

// Table writes the full brief: token, pools, verdict, strategy (not on
// avoid), exit signals, legend and disclaimer.
func Table(w io.Writer, p *lpplan.Plan) error {
	ew := &errWriter{w: w}
	t := p.Token
	amount := "-"
	if p.Params.Amount != nil {
		amount = scan.Money(*p.Params.Amount)
	}
	ew.printf("lpplan  network=%s  risk=%s  amount=%s\n\n", p.Network, p.Params.Risk, amount)

	ew.printf("TOKEN  %s  %s\n", orDash(t.Symbol), orDash(t.Name))
	ew.printf("  CA         %s\n", t.Address)
	ew.printf("  price      %s   mcap %s%s\n", usd(t.PriceUSD), money(t.McapUSD), mcapNote(t.McapSource))
	ew.printf("  GT score   %s   holders %s   top-10 %s\n", num(t.GTScore, 1), intOr(t.HoldersCount), pct(t.Top10Pct, 1))
	ew.printf("  honeypot   %s   mint %s   freeze %s\n", t.IsHoneypot, strOr(t.MintAuthority, "none"), strOr(t.FreezeAuthority, "none"))
	for _, n := range p.InfoFlags {
		ew.printf("  note %-16s %s\n", n.Code, n.Message)
	}

	ew.printf("\nPOOLS (%d)\n", len(p.Pools))
	if len(p.Pools) == 0 {
		ew.printf("  no pools found for this token\n")
	} else {
		tw := tabwriter.NewWriter(ew, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "\tPAIR\tDEX\tFEE\tLIQUIDITY\tVOL(24h)\tTURNOVER/d\tEST YIELD/d\tCHG(24h)\tGRADE\tFLAGS\tPOOL\n")
		for _, r := range p.Pools {
			mark := ""
			if r.Chosen {
				mark = ChosenMarker
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%.2f\t%s\t%s\t%s\t%s\t%s\n",
				mark, r.Pair, r.Dex, pct(r.FeePct, -1), scan.Money(r.LiquidityUSD), scan.Money(r.Volume24hUSD),
				r.TurnoverD, pct(r.EstFeeYieldD, 2), signedPct(r.PriceChange24hPct), r.Grade, flags(r.Flags), r.PoolAddress)
		}
		tw.Flush()
	}

	ew.printf("\nVERDICT  %s\n", strings.ToUpper(string(p.Verdict.Decision)))
	if len(p.Verdict.Reasons) == 0 {
		ew.printf("  no blocking signal\n")
	}
	for _, r := range p.Verdict.Reasons {
		ew.printf("  %-12s (%s) %s\n", r.Code, r.Tier, r.Message)
	}

	if s := p.Strategy; s != nil {
		title := "STRATEGY"
		if p.Verdict.Decision == lpplan.DecisionWait {
			title = "IF YOU STILL ENTER"
		}
		ew.printf("\n%s  (risk %s, k=%g)\n", title, s.Risk, s.K)
		ew.printf("  price      %.6g %s\n", s.Price, s.PriceUnit)
		if s.Mode == lpplan.ModeConcentrated {
			ew.printf("  range      %.6g – %.6g  (%+.1f%% / %+.1f%%)\n", *s.RangeLow, *s.RangeHigh, *s.RangeLowPct, *s.RangeHighPct)
		} else {
			ew.printf("  range      full range: %s\n", strOr(s.FullRangeReason, ""))
		}
		ew.printf("  σ          7d %s   1d %s\n", pct(s.Sigma7dPct, 1), pct(s.SigmaDayPct, 1))
		ew.printf("  fees       base %s/d · est. %s/d with concentration ×%.2f (%s over 7d)\n", pct(s.BaseFeeYieldD, 2), pct(s.EstFeeYieldD, 2), s.Concentration, pct(s.EstFee7dPct, 2))
		ew.printf("  IL         at low edge %s   at high edge %s\n", signedPct(s.ILLowPct), signedPct(s.ILHighPct))
		if d := s.Deposit; d != nil {
			ew.printf("  deposit    %s = %s %s (%s) + %s %s (%s)\n", scan.Money(d.AmountUSD),
				amt(d.BaseAmount), d.BaseSymbol, scan.Money(d.BaseUSD), amt(d.QuoteAmount), d.QuoteSymbol, scan.Money(d.QuoteUSD))
			ew.printf("             est. fees %s/d   IL %s (low) / %s (high)\n", money(d.EstFeesDUSD), money(d.ILLowUSD), money(d.ILHighUSD))
		}
	}

	if len(p.ExitSignals) > 0 {
		ew.printf("\nEXIT / REBALANCE WHEN\n")
		for _, s := range p.ExitSignals {
			ew.printf("  - %s\n", s.Message)
		}
	}

	lg := p.Legend
	ew.printf("\nLegend:\n")
	for _, v := range lg.Verdicts {
		ew.printf("  %-16s %s\n", v.Code, v.Meaning)
	}
	for _, r := range lg.Reasons {
		ew.printf("  %-16s %s\n", r.Code, r.Meaning)
	}
	for _, f := range lg.InfoFlags {
		ew.printf("  %-16s %s\n", f.Code, f.Meaning)
	}
	ew.printf("  %-16s %s\n", "RANGE", lg.Range)
	ew.printf("  %-16s %s\n", "IL", lg.IL)
	ew.printf("  %-16s %s\n", "FEES", lg.Fees)
	ew.printf("  %-16s %s\n", ChosenMarker, lg.Chosen)
	ew.printf("%s\n", lg.Disclaimer)
	return ew.err
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func strOr(s *string, def string) string {
	if s == nil {
		return def
	}
	return *s
}

func intOr(v *int) string {
	if v == nil {
		return "—"
	}
	return strconv.Itoa(*v)
}

func num(v *float64, prec int) string {
	if v == nil {
		return "—"
	}
	return strconv.FormatFloat(*v, 'f', prec, 64)
}

func money(v *float64) string {
	if v == nil {
		return "—"
	}
	return scan.Money(*v)
}

func usd(v *float64) string {
	if v == nil {
		return "—"
	}
	return "$" + strconv.FormatFloat(*v, 'g', 6, 64)
}

func amt(v *float64) string {
	if v == nil {
		return "?"
	}
	return strconv.FormatFloat(*v, 'g', 6, 64)
}

func mcapNote(src *string) string {
	if src != nil && *src == lpscan.McapSourceFDV {
		return " (FDV)"
	}
	return ""
}

func pct(v *float64, prec int) string {
	if v == nil {
		return "—"
	}
	return strconv.FormatFloat(*v, 'f', prec, 64) + "%"
}

func signedPct(v *float64) string {
	if v == nil {
		return "—"
	}
	return fmt.Sprintf("%+.2f%%", *v)
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
