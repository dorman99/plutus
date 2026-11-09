package lpscan

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/dorman99/plutus/pkg/geckoterminal"
)

// Majors are base tokens that are never ranked as a "coin": the pool's
// market cap fields describe the base token, and WETH or a stablecoin is not
// an LP candidate. Lowercase addresses on Robinhood Chain.
var Majors = map[string]string{
	"0x0bd7d308f8e1639fab988df18a8011f41eacad73": "WETH",
	"0x5fc5360d0400a0fd4f2af552add042d716f1d168": "USDG",
}

const poolURLFormat = "https://www.geckoterminal.com/%s/pools/%s"

// Rank turns raw pools into a Result: dedupe, hard filters (kind, market
// cap, liquidity, volume), score, grade, one row per coin, top N and the
// pick. It is pure; now is the scan time
// used for the NEW flag and Result.ScannedAt.
func Rank(pools []geckoterminal.Pool, p Params, now time.Time) *Result {
	res := &Result{
		ScannedAt: now.UTC(),
		Network:   Network,
		Params:    p,
		Results:   []Row{},
		Warnings:  []string{},
		Legend:    LegendData(),
	}

	seen := make(map[string]bool, len(pools))
	best := make(map[string]Row) // coin address -> best row
	for _, pool := range pools {
		if seen[pool.Address] {
			continue
		}
		seen[pool.Address] = true
		res.Stats.Scanned++

		row, ok := evaluate(pool, p, now)
		if !ok {
			continue
		}
		res.Stats.Passed++
		if cur, exists := best[row.CoinAddress]; !exists || better(row, cur) {
			best[row.CoinAddress] = row
		}
	}
	res.Stats.Coins = len(best)

	rows := make([]Row, 0, len(best))
	for _, r := range best {
		rows = append(rows, r)
	}
	sort.Slice(rows, func(i, j int) bool { return better(rows[i], rows[j]) })
	if len(rows) > p.Top {
		rows = rows[:p.Top]
	}
	res.Results = rows
	Finalize(res)
	return res
}

// better orders rows by score, then liquidity, then address for stability.
func better(a, b Row) bool {
	if a.Score != b.Score {
		return a.Score > b.Score
	}
	if a.LiquidityUSD != b.LiquidityUSD {
		return a.LiquidityUSD > b.LiquidityUSD
	}
	return a.PoolAddress < b.PoolAddress
}

// evaluate applies the hard filters and, if the pool passes, builds its row.
// Missing data fails closed: a pool that cannot be checked is dropped.
func evaluate(pool geckoterminal.Pool, p Params, now time.Time) (Row, bool) {
	if _, major := Majors[pool.BaseToken.Address]; major || pool.BaseToken.Address == "" {
		return Row{}, false
	}

	var flags []Flag
	mcap, src := positive(pool.MarketCapUSD), McapSourceMarketCap
	if mcap == 0 {
		mcap, src = positive(pool.FDVUSD), McapSourceFDV
		if mcap == 0 {
			return Row{}, false
		}
		flags = append(flags, FlagFDV)
	}
	if mcap > p.MaxMcap || mcap < p.MinMcap {
		return Row{}, false
	}
	kind := KindOf(pool.BaseToken.Name)
	if p.Kind != KindAll && kind != p.Kind {
		return Row{}, false
	}

	key := p.Window.apiKey()
	liq := positive(pool.ReserveUSD)
	vol := positive(pool.VolumeUSD[key])
	if liq == 0 || vol == 0 || liq < p.MinLiquidity || vol < p.MinVolume {
		return Row{}, false
	}
	pc := pool.PriceChangePct[key]
	if pc == nil || math.IsNaN(*pc) || math.IsInf(*pc, 0) {
		return Row{}, false
	}

	turnoverD := vol / liq * (24 * time.Hour).Hours() / p.Window.Duration().Hours()
	score := turnoverD / (1 + math.Abs(*pc)/100)

	if pool.CreatedAt != nil && now.Sub(*pool.CreatedAt) < NewPoolAgeDays*24*time.Hour {
		flags = append(flags, FlagNew)
	}
	var tx geckoterminal.Txns
	if t := pool.Transactions[key]; t != nil {
		tx = *t
		if total := tx.Buys + tx.Sells; total > 0 {
			if float64(tx.Buys)/float64(total) > SkewShare || float64(tx.Sells)/float64(total) > SkewShare {
				flags = append(flags, FlagSkew)
			}
		}
	}
	if math.Abs(*pc) > SwingPct {
		flags = append(flags, FlagSwing)
	}

	fee := ParseFeePct(pool.Name)
	var yield *float64
	if fee != nil {
		y := turnoverD * *fee
		yield = &y
	}

	baseSym, quoteSym := Symbols(pool)
	row := Row{
		CoinSymbol:     baseSym,
		CoinAddress:    pool.BaseToken.Address,
		Kind:           kind,
		Pair:           baseSym + "/" + quoteSym,
		Dex:            pool.DexID,
		LiquidityUSD:   liq,
		VolumeUSD:      vol,
		TurnoverD:      turnoverD,
		FeePct:         fee,
		EstFeeYieldD:   yield,
		PriceChangePct: *pc,
		McapUSD:        mcap,
		McapSource:     src,
		Score:          score,
		Flags:          flags,
		Buys:           tx.Buys,
		Sells:          tx.Sells,
		Buyers:         tx.Buyers,
		Sellers:        tx.Sellers,
		PoolCreatedAt:  pool.CreatedAt,
		PoolAddress:    pool.Address,
		URL:            fmt.Sprintf(poolURLFormat, Network, pool.Address),
	}
	if row.Flags == nil {
		row.Flags = []Flag{}
	}
	row.Grade = grade(row)
	return row, true
}

func grade(r Row) Grade { return GradeFor(r.Score, r.HasFlag(FlagSkew)) }

// GradeFor grades a per-day score; skew forces suspect. Shared with lpplan
// so a pool is graded the same way in both stages.
func GradeFor(score float64, skew bool) Grade {
	switch {
	case score > GradeSuspectMax || skew:
		return GradeSuspect
	case score >= GradeStrongMin:
		return GradeStrong
	case score >= GradeDecentMin:
		return GradeDecent
	default:
		return GradeWeak
	}
}

// stockNameSuffix ends the name of every tokenized stock or ETF on
// Robinhood Chain ("NVIDIA • Robinhood Token", "Intel • Robinhood Token").
const stockNameSuffix = "Robinhood Token"

// KindOf classifies a base token by its name.
func KindOf(name string) Kind {
	if strings.HasSuffix(strings.TrimSpace(name), stockNameSuffix) {
		return KindStock
	}
	return KindMeme
}

var feeRe = regexp.MustCompile(`(\d+(?:\.\d+)?)%\s*$`)

// ParseFeePct reads a trailing fee tier from a pool name
// ("NVDA / WETH 0.05%" -> 0.05). Nil when the name has none.
func ParseFeePct(name string) *float64 {
	m := feeRe.FindStringSubmatch(strings.TrimSpace(name))
	if m == nil {
		return nil
	}
	v, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return nil
	}
	return &v
}

// Symbols returns base and quote symbols, falling back to the pool name
// ("ABC / WETH 0.3%") when the API did not include the tokens.
func Symbols(pool geckoterminal.Pool) (string, string) {
	base, quote := pool.BaseToken.Symbol, pool.QuoteToken.Symbol
	if base == "" || quote == "" {
		name := feeRe.ReplaceAllString(strings.TrimSpace(pool.Name), "")
		if parts := strings.SplitN(name, "/", 2); len(parts) == 2 {
			if base == "" {
				base = strings.TrimSpace(parts[0])
			}
			if quote == "" {
				quote = strings.TrimSpace(parts[1])
			}
		}
	}
	if base == "" {
		base = "?"
	}
	if quote == "" {
		quote = "?"
	}
	return base, quote
}

func positive(v *float64) float64 {
	if v == nil || math.IsNaN(*v) || math.IsInf(*v, 0) || *v <= 0 {
		return 0
	}
	return *v
}
