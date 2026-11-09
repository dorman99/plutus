package lpscan

// Flag is a per-row warning. Flags never filter a row out.
type Flag string

const (
	FlagFDV   Flag = "FDV"
	FlagNew   Flag = "NEW"
	FlagSkew  Flag = "SKEW"
	FlagSwing Flag = "SWING"
)

// Grade is a coarse reading of the per-day score.
type Grade string

const (
	GradeWeak    Grade = "weak"
	GradeDecent  Grade = "decent"
	GradeStrong  Grade = "strong"
	GradeSuspect Grade = "suspect"
)

// Tunable thresholds. Starting values; revisit after real scans.
const (
	GradeDecentMin  = 0.1  // score/d at or above: decent
	GradeStrongMin  = 0.5  // score/d at or above: strong
	GradeSuspectMax = 5.0  // score/d above: suspect
	SkewShare       = 0.70 // buys or sells share above: SKEW
	SwingPct        = 30.0 // |price change| above: SWING
	NewPoolAgeDays  = 7    // pool younger than: NEW
)

// Definition is one legend entry.
type Definition struct {
	Code    string `json:"code"`
	Meaning string `json:"meaning"`
}

// Legend explains every flag, grade and the pick marker. It is data so
// each presenter can render it in its own format.
type Legend struct {
	Flags      []Definition `json:"flags"`
	Grades     []Definition `json:"grades"`
	Verdicts   []Definition `json:"verdicts"`
	Kind       string       `json:"kind"`
	Net7d      string       `json:"net_7d"`
	Pick       string       `json:"pick"`
	PickGuard  string       `json:"pick_guard"`
	Disclaimer string       `json:"disclaimer"`
}

// LegendData returns the legend for the current thresholds.
func LegendData() Legend {
	return Legend{
		Flags: []Definition{
			{string(FlagFDV), "market cap missing, FDV used instead"},
			{string(FlagNew), "pool younger than 7 days"},
			{string(FlagSkew), "buys or sells above 70% of transactions in the window"},
			{string(FlagSwing), "absolute price change above 30% in the window"},
		},
		Grades: []Definition{
			{string(GradeWeak), "<0.1"},
			{string(GradeDecent), "0.1-0.5"},
			{string(GradeStrong), ">=0.5"},
			{string(GradeSuspect), ">5 or SKEW"},
		},
		Verdicts: []Definition{
			{"GO", "no blocking signal (same verdict as /lp-plan)"},
			{"WAIT", "a signal says hold off"},
			{"AVOID", "a hard blocker"},
		},
		Kind:       "stock = base token name ends with \"Robinhood Token\" (tokenized stock or ETF); meme = everything else",
		Net7d:      "est. 7-day fees + IL at the low edge (full range: at a -1σ move); rows sort by verdict, then net 7d",
		Pick:       "best candidate to review first, not a recommendation to enter",
		PickGuard:  "never ★: top 10 holders above 80% or honeypot status not confirmed \"no\"",
		Disclaimer: "Candidates to review, not a safety check: rug pulls and honeypots are not detected.",
	}
}
