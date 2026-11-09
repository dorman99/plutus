package lpplan

import "github.com/dorman99/plutus/pkg/lpscan"

// Legend explains verdicts, reason codes, info flags and the strategy terms.
// It is data so each presenter can render it in its own format.
type Legend struct {
	Verdicts   []lpscan.Definition `json:"verdicts"`
	Reasons    []lpscan.Definition `json:"reasons"`
	InfoFlags  []lpscan.Definition `json:"info_flags"`
	Range      string              `json:"range"`
	IL         string              `json:"il"`
	Fees       string              `json:"fees"`
	Chosen     string              `json:"chosen"`
	Disclaimer string              `json:"disclaimer"`
}

// LegendData returns the legend for the current thresholds.
func LegendData() Legend {
	d := func(c, m string) lpscan.Definition { return lpscan.Definition{Code: c, Meaning: m} }
	return Legend{
		Verdicts: []lpscan.Definition{
			d("GO", "no blocking signal"),
			d("WAIT", "a signal says hold off; strategy shown as \"if you still enter\""),
			d("AVOID", "a hard blocker; no strategy"),
		},
		Reasons: []lpscan.Definition{
			d(ReasonHoneypot, "avoid: GeckoTerminal marks the token a honeypot"),
			d(ReasonNoDepth, "avoid: no pool with at least $50K liquidity"),
			d(ReasonSkew, "avoid: buys or sells above 70% of 24h transactions"),
			d(ReasonWeak, "avoid: 24h score below 0.1"),
			d(ReasonSuspect, "wait: 24h score above 5, turnover may not be organic"),
			d(ReasonSwing, "wait: |24h price change| above 30%"),
			d(ReasonNew, "wait: pool younger than 7 days"),
			d(ReasonFading, "wait: 6h turnover/d below half of 24h"),
			d(ReasonFeeUnknown, "wait: fee tier not in the pool name"),
			d(ReasonILGTFees, "wait: base 7-day fees (before concentration) below IL at the range edge"),
			d(ReasonNoHistory, "wait: fewer than 24 hourly candles"),
			d(ReasonLowTurnover, "wait: chosen pool turnover/d below the 0.5 exit threshold (no pool clears it)"),
		},
		InfoFlags: []lpscan.Definition{
			d("TOP10", "top 10 holders own more than 80% (shown, does not decide)"),
			d("MINT", "mint authority set"),
			d("FREEZE", "freeze authority set"),
			d("HONEYPOT_UNKNOWN", "honeypot status not known"),
		},
		Range:      "safe ±2σ · balanced ±1σ · aggressive ±0.5σ of 7-day volatility (hourly log returns × √168)",
		IL:         "loss vs holding if price ends at that range edge (full range: at the same ±kσ move)",
		Fees:       "base = pool turnover/d × fee; est. = base × concentration (capped ×3, assumes other LPs are wider)",
		Chosen:     "pool the plan is built on",
		Disclaimer: "A decision brief, not financial advice: rug pulls and honeypots are not detected.",
	}
}
