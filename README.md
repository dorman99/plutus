# plutus

On-chain tooling for liquidity providers on Robinhood Chain, read-only, using
the free GeckoTerminal public API (no key, no wallet).

- Stage 1, **lpscan**: ranks coins worth reviewing for a liquidity position. By
  default: trending meme coins between $1M and $20M market cap, each with its
  full lpplan verdict.
- Stage 2, **lpplan**: for one coin (CA), picks the pool and gives a GO / WAIT /
  AVOID verdict with a range, fee and IL estimate, deposit split and exit signals.

## Layout

```
pkg/geckoterminal/   rate-limited GeckoTerminal client (shared by every stage)
pkg/lpscan/          scan logic: Params, Scanner.Scan, pure Rank, Result. Data only, never prints
pkg/lpscan/present/  presenters: Table, JSON, Warnings. Render a Result, never scan
cmd/lpscan/          CLI: flags + exit codes, wires Scan to a presenter
pkg/lpplan/          plan logic: Params, Planner.Plan, pure Analyze, Plan. Data only, never prints
pkg/lpplan/present/  presenters: Table, JSON, Warnings for a Plan
cmd/lpplan/          CLI: lpplan <CA> [flags]
.claude/skills/lp-scan/  Claude Code skill that runs lpscan --json
.claude/skills/lp-plan/  Claude Code skill that runs lpplan --json
```

Using the scanner from Go (any UI):

```go
res, err := lpscan.New(lpscan.GeckoSource(geckoterminal.New())).Scan(ctx, lpscan.DefaultParams())
// render res however you like, or use present.Table / present.JSON
```

## Install

```sh
go install ./cmd/lpscan ./cmd/lpplan   # Go 1.22+, no third-party dependencies
```

## Usage

```sh
lpscan                                   # trending memes, $1M-$20M, top 5 with full plans
lpscan --max-mcap 30000000 --max-top10 60 --top 8
lpscan --source all --kind all --detail basic   # stage-1 style: every listing, stocks too
lpscan --json                            # for the skill / scripts
```

| Flag | Default | Meaning |
|---|---|---|
| `--min-mcap` | 1000000 | Skip coins below this market cap (USD). |
| `--max-mcap` | 20000000 | Skip coins above this market cap (USD). FDV used when market cap is missing (`FDV` flag). |
| `--source` | trending | `trending` (`trending_pools` only) or `all` (`pools`, `trending_pools`, `new_pools`). |
| `--kind` | meme | `meme`, `stock` (base token name ends with "Robinhood Token": tokenized stocks and ETFs) or `all`. |
| `--detail` | full | `full`: the lpplan analysis per coin (~4 calls each, first 10 coins; the rest get basic). `basic`: token info only (1 call). |
| `--max-top10` | off | Drop coins whose top-10 holders own more than this % (e.g. `60`). |
| `--window` | 6h | `5m`, `1h`, `6h` or `24h`: window for volume, price change and transactions. |
| `--min-liquidity` | 50000 | Minimum pool liquidity (USD). |
| `--min-volume` | 25000 | Minimum volume inside `--window` (USD). |
| `--top` | 5 | Coins to print (1–50). |
| `--pages` | 3 | Pages per endpoint (1–10): pages API calls with `trending`, 3 × pages with `all`. |
| `--timeout` | 360s | Total budget for listings and detail. The default scan takes ~4 min because the free API throttles to roughly one call per 10s. |
| `--json` | off | Print JSON instead of the table. |

Exit codes: `0` success (including zero results), `1` API or network error
(nothing fetched), `2` invalid parameter.

## How coins are ranked

1. Fetch `trending_pools` (or, with `--source all`, `pools`, `trending_pools`, `new_pools` interleaved by page), de-duplicate by pool address.
2. Skip pools whose base token is WETH or USDG; the coin is the pool's base token.
3. Hard filters (missing data drops the pool): kind (`--kind`), market cap (or FDV) within `--min-mcap`–`--max-mcap`, liquidity ≥ min, window volume ≥ min, price change present.
4. `turnover/d = volume / liquidity × (24h / window)`, `score = turnover/d / (1 + |price change %| / 100)`.
5. One row per coin (its best pool), sorted by score, top N: these are the candidates.
6. Fee tier is parsed from the pool name when present; `est yield/d = turnover/d × fee %`.
7. Grade: `weak` <0.1, `decent` 0.1–0.5, `strong` ≥0.5, `suspect` >5 or `SKEW`.
8. Detail: each candidate gets its lpplan analysis (`--detail full`: verdict, reasons, chosen pool, σ, mode, yield, 7-day fees, IL, exit signals, holders, top 10, honeypot). The plan may use a different pool of the coin than step 5. A coin whose lookup fails keeps its row without detail, with a warning.
9. `--max-top10` drops coins above that holder share. Final order: verdict (GO, WAIT, AVOID, no plan), then `net_7d_pct = est_fee_7d_pct + il_low_pct` (fees for a week minus IL at the low edge).
10. ★ Pick: the first GO coin in that order whose top-10 holders own ≤ 80% and whose honeypot check is `no`. None if nothing qualifies. Without detail, the stage-1 rule applies: among `decent`/`strong` rows without `SKEW`, `SWING` or `FDV`, the highest est. yield/d (or highest score if no fee is known).

Flags are warnings, not filters: `FDV`, `NEW` (pool < 7 days), `SKEW` (buys or
sells > 70% of transactions), `SWING` (|price change| > 30%).

JSON percent fields (`fee_pct`, `est_fee_yield_d`, `price_change_pct`) are
percentages: `0.3` means 0.3%.

**This is a list of candidates to review, not a safety check.** Rug pulls and
honeypots are not detected.

## lpplan

```sh
lpplan 0xa30fa36db767ad9ed3f7a60fc79526fb4d56d344                 # balanced, % only
lpplan 0xa30f… --amount 500 --risk safe --json                     # $ figures + token split
```

| Flag | Default | Meaning |
|---|---|---|
| `<CA>` | required | Coin contract address (before or after the flags). |
| `--amount` | none | Deposit in USD: adds the token split, fees $/day and IL in $. |
| `--risk` | balanced | Range width: `safe` ±2σ, `balanced` ±1σ, `aggressive` ±0.5σ of 7-day volatility. |
| `--json` | off | Print JSON instead of the brief. |
| `--timeout` | 120s | Total budget. Four API calls: ~30–60 s on the free tier. |

How the plan is built:

1. Fetch token, token info (holders, GT score, honeypot), the token's pools (base or quote side), then hourly candles (7 days, priced in the quote token) for the chosen pool only. Info or candles failing is a warning; token or pools failing is exit 1.
2. Every pool is measured on 24h data like lpscan: turnover/d, score, grade, `NEW`/`SKEW`/`SWING`, fee from the name, est. yield/d.
3. ★ Chosen pool: liquidity ≥ $50K, preferring pools with turnover/d ≥ 0.5 (the exit threshold), then the highest est. yield/d (highest turnover/d if no fee is known). Only when no pool clears 0.5 are the others considered.
4. `σ_7d = stdev(hourly log returns) × √168`; range `price × e^(±k·σ_7d)`. Full range on v2 pools, without enough history, or when the range would exceed +60%.
5. Base fee yield = turnover/d × fee; est. yield = base × concentration factor `1/(1 − e^(−kσ/2))`, capped at ×3 (in practice concentrated plans hit the cap). IL at each range edge from Uniswap v3 maths.
6. Verdict: AVOID on `HONEYPOT`, `NO_DEPTH`, `SKEW`, `WEAK`; WAIT on `SUSPECT`, `SWING`, `NEW`, `FADING`, `FEE_UNKNOWN`, `IL_GT_FEES` (base 7-day fees below edge IL), `NO_HISTORY`, `LOW_TURNOVER` (chosen pool's turnover/d below 0.5, i.e. no pool clears it); otherwise GO. Every matched reason is listed. No strategy on AVOID.
7. Holder concentration (`TOP10` > 80%), mint/freeze authority and unknown honeypot status are shown but never decide the verdict.

Exit codes: `0` success (including AVOID), `1` API or network error, `2` invalid argument.

**A decision brief, not financial advice.** The concentrated fee estimate assumes the
rest of the pool is spread wider; competing concentrated LPs lower real yield,
so the verdict's IL check uses the base yield.

## Limits

At least 2s between API calls, 10s per request, 429/5xx retried 3× (2s, 4s,
8s) but never past the scan deadline. If time runs out after some data was
fetched, the result is printed with a warning and exit 0. GeckoTerminal sits
behind bot protection; bursts of requests can get a temporary HTTP 403, which
is not retried.

## Test

```sh
go test ./...   # fixtures only, no network
```
