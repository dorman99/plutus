package lpscan

import "time"

// Result is the scan output and the JSON contract shared by the CLI, the
// lp-scan skill and any future UI.
type Result struct {
	ScannedAt time.Time `json:"scanned_at"`
	Network   string    `json:"network"`
	Params    Params    `json:"params"`
	Stats     Stats     `json:"stats"`
	Results   []Row     `json:"results"`
	Pick      *Pick     `json:"pick"`
	Warnings  []string  `json:"warnings"`
	Legend    Legend    `json:"legend"`
}

// Stats counts pools through the pipeline.
type Stats struct {
	Scanned int `json:"scanned"` // unique pools fetched
	Passed  int `json:"passed"`  // pools that passed every hard filter
	Coins   int `json:"coins"`   // distinct coins among passed pools
}

// Row is one coin, represented by its highest-scoring pool.
// Percent fields are percentages (0.3 means 0.3%).
type Row struct {
	Rank           int        `json:"rank"`
	CoinSymbol     string     `json:"coin_symbol"`
	CoinAddress    string     `json:"coin_address"`
	Pair           string     `json:"pair"`
	Dex            string     `json:"dex"`
	LiquidityUSD   float64    `json:"liquidity_usd"`
	VolumeUSD      float64    `json:"volume_usd"`
	TurnoverD      float64    `json:"turnover_d"`
	FeePct         *float64   `json:"fee_pct"`
	EstFeeYieldD   *float64   `json:"est_fee_yield_d"`
	PriceChangePct float64    `json:"price_change_pct"`
	McapUSD        float64    `json:"mcap_usd"`
	McapSource     string     `json:"mcap_source"`
	Score          float64    `json:"score"`
	Grade          Grade      `json:"grade"`
	Flags          []Flag     `json:"flags"`
	Buys           int        `json:"buys"`
	Sells          int        `json:"sells"`
	Buyers         int        `json:"buyers"`
	Sellers        int        `json:"sellers"`
	PoolCreatedAt  *time.Time `json:"pool_created_at"`
	PoolAddress    string     `json:"pool_address"`
	URL            string     `json:"url"`
}

// HasFlag reports whether the row carries f.
func (r Row) HasFlag(f Flag) bool {
	for _, x := range r.Flags {
		if x == f {
			return true
		}
	}
	return false
}

// Pick is the single coin to review first, or nil when none qualifies.
type Pick struct {
	Rank        int    `json:"rank"`
	CoinSymbol  string `json:"coin_symbol"`
	CoinAddress string `json:"coin_address"`
	Reason      string `json:"reason"`
}

const (
	McapSourceMarketCap = "market_cap"
	McapSourceFDV       = "fdv"
)
