package lpplan

import (
	"time"

	"github.com/dorman99/plutus/pkg/lpscan"
)

// Plan is the decision brief and the JSON contract shared by the CLI, the
// lp-plan skill and any future UI. Percent fields are percentages (0.3 means
// 0.3%); IL percentages are negative for a loss versus holding.
type Plan struct {
	ScannedAt   time.Time    `json:"scanned_at"`
	Network     string       `json:"network"`
	Params      Params       `json:"params"`
	Token       TokenSummary `json:"token"`
	Pools       []PoolRow    `json:"pools"`
	ChosenPool  *string      `json:"chosen_pool"`
	Verdict     Verdict      `json:"verdict"`
	Strategy    *Strategy    `json:"strategy"`
	ExitSignals []Signal     `json:"exit_signals"`
	InfoFlags   []Note       `json:"info_flags"`
	Warnings    []string     `json:"warnings"`
	Legend      Legend       `json:"legend"`
}

// TokenSummary is the coin's market data and metadata. Info fields are null
// when the info endpoint failed or did not send them.
type TokenSummary struct {
	Address         string   `json:"address"`
	Symbol          string   `json:"symbol"`
	Name            string   `json:"name"`
	PriceUSD        *float64 `json:"price_usd"`
	McapUSD         *float64 `json:"mcap_usd"`
	McapSource      *string  `json:"mcap_source"` // "market_cap" | "fdv" | null
	GTScore         *float64 `json:"gt_score"`
	GTVerified      *bool    `json:"gt_verified"`
	HoldersCount    *int     `json:"holders_count"`
	Top10Pct        *float64 `json:"top10_pct"`
	IsHoneypot      string   `json:"is_honeypot"` // yes | no | unknown
	MintAuthority   *string  `json:"mint_authority"`
	FreezeAuthority *string  `json:"freeze_authority"`
}

// PoolRow is one pool trading the coin, measured on 24h data.
type PoolRow struct {
	PoolAddress       string        `json:"pool_address"`
	Pair              string        `json:"pair"`
	BaseSymbol        string        `json:"base_symbol"`
	QuoteSymbol       string        `json:"quote_symbol"`
	Dex               string        `json:"dex"`
	FeePct            *float64      `json:"fee_pct"`
	LiquidityUSD      float64       `json:"liquidity_usd"`
	Volume24hUSD      float64       `json:"volume_24h_usd"`
	TurnoverD         float64       `json:"turnover_d"`
	EstFeeYieldD      *float64      `json:"est_fee_yield_d"`
	PriceChange24hPct *float64      `json:"price_change_24h_pct"`
	Score             float64       `json:"score"`
	Grade             lpscan.Grade  `json:"grade"`
	Flags             []lpscan.Flag `json:"flags"`
	PoolCreatedAt     *time.Time    `json:"pool_created_at"`
	Chosen            bool          `json:"chosen"`
	URL               string        `json:"url"`

	turnover6hD float64 // for FADING
	skew        bool
	basePrice   *float64 // base priced in quote
	baseUSD     *float64
	quoteUSD    *float64
}

// Decision is the verdict.
type Decision string

const (
	DecisionGo    Decision = "go"
	DecisionWait  Decision = "wait"
	DecisionAvoid Decision = "avoid"
)

// Verdict is the decision and every reason that matched.
type Verdict struct {
	Decision Decision `json:"decision"`
	Reasons  []Reason `json:"reasons"`
}

// Reason is one matched verdict rule.
type Reason struct {
	Code    string   `json:"code"`
	Tier    Decision `json:"tier"`
	Message string   `json:"message"`
}

// Strategy is how to set the LP if the user enters. Null on avoid.
type Strategy struct {
	Risk            Risk     `json:"risk"`
	K               float64  `json:"k"`
	Mode            string   `json:"mode"` // concentrated | full_range
	FullRangeReason *string  `json:"full_range_reason"`
	Price           float64  `json:"price"`
	PriceUnit       string   `json:"price_unit"` // "QUOTE per BASE"
	Sigma7dPct      *float64 `json:"sigma_7d_pct"`
	SigmaDayPct     *float64 `json:"sigma_day_pct"`
	RangeLow        *float64 `json:"range_low"`
	RangeHigh       *float64 `json:"range_high"`
	RangeLowPct     *float64 `json:"range_low_pct"`
	RangeHighPct    *float64 `json:"range_high_pct"`
	Concentration   float64  `json:"concentration"`
	BaseFeeYieldD   *float64 `json:"base_fee_yield_d"` // pool turnover/d × fee, no concentration
	EstFeeYieldD    *float64 `json:"est_fee_yield_d"`  // base × concentration
	EstFee7dPct     *float64 `json:"est_fee_7d_pct"`
	ILLowPct        *float64 `json:"il_low_pct"`
	ILHighPct       *float64 `json:"il_high_pct"`
	Deposit         *Deposit `json:"deposit"`
}

// Deposit is the split and dollar estimates for --amount.
type Deposit struct {
	AmountUSD   float64  `json:"amount_usd"`
	BaseSymbol  string   `json:"base_symbol"`
	BaseAmount  *float64 `json:"base_amount"`
	BaseUSD     float64  `json:"base_usd"`
	QuoteSymbol string   `json:"quote_symbol"`
	QuoteAmount *float64 `json:"quote_amount"`
	QuoteUSD    float64  `json:"quote_usd"`
	EstFeesDUSD *float64 `json:"est_fees_d_usd"`
	ILLowUSD    *float64 `json:"il_low_usd"`
	ILHighUSD   *float64 `json:"il_high_usd"`
}

// Signal is an exit or rebalance trigger; Value is its threshold.
type Signal struct {
	Code    string   `json:"code"`
	Message string   `json:"message"`
	Value   *float64 `json:"value"`
}

// Note is an informational flag that never decides the verdict.
type Note struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
