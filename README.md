# plutus

On-chain tooling for liquidity providers. Stage 1 is **lpscan**: a read-only
scanner that ranks Robinhood Chain coins worth reviewing for a liquidity
position, using the free GeckoTerminal public API (no key, no wallet).

## Layout

```
pkg/geckoterminal/   rate-limited GeckoTerminal client (shared by every stage)
pkg/lpscan/          scan logic: Params, Scanner.Scan, pure Rank, Result. Data only, never prints
pkg/lpscan/present/  presenters: Table, JSON, Warnings. Render a Result, never scan
cmd/lpscan/          CLI: flags + exit codes, wires Scan to a presenter
.claude/skills/lp-scan/  Claude Code skill that runs lpscan --json
```

Using the scanner from Go (any UI):

```go
res, err := lpscan.New(lpscan.GeckoSource(geckoterminal.New())).Scan(ctx, lpscan.DefaultParams())
// render res however you like, or use present.Table / present.JSON
```

## Install

```sh
go install ./cmd/lpscan     # Go 1.22+, no third-party dependencies
```

## Usage

```sh
lpscan                                   # defaults
lpscan --max-mcap 2000000 --window 1h --min-liquidity 30000 --top 10
lpscan --json                            # for the skill / scripts
```

| Flag | Default | Meaning |
|---|---|---|
| `--max-mcap` | 5000000 | Skip coins above this market cap (USD). FDV used when market cap is missing (`FDV` flag). |
| `--window` | 6h | `5m`, `1h`, `6h` or `24h`: window for volume, price change and transactions. |
| `--min-liquidity` | 50000 | Minimum pool liquidity (USD). |
| `--min-volume` | 25000 | Minimum volume inside `--window` (USD). |
| `--top` | 5 | Coins to print (1–50). |
| `--pages` | 3 | Pages per endpoint (1–10); 3 endpoints × pages API calls. |
| `--timeout` | 120s | Total scan budget. A full 3-page scan takes ~1.5–2 min because the free API throttles to roughly one call per 10s. |
| `--json` | off | Print JSON instead of the table. |

Exit codes: `0` success (including zero results), `1` API or network error
(nothing fetched), `2` invalid parameter.

## How coins are ranked

1. Fetch `pools`, `trending_pools`, `new_pools` (interleaved by page), de-duplicate by pool address.
2. Skip pools whose base token is WETH or USDG; the coin is the pool's base token.
3. Hard filters (missing data drops the pool): market cap (or FDV) ≤ max, liquidity ≥ min, window volume ≥ min, price change present.
4. `turnover/d = volume / liquidity × (24h / window)`, `score = turnover/d / (1 + |price change %| / 100)`.
5. One row per coin (its best pool), sorted by score, top N.
6. Fee tier is parsed from the pool name when present; `est yield/d = turnover/d × fee %`. Display and pick only, not in the score.
7. Grade: `weak` <0.1, `decent` 0.1–0.5, `strong` ≥0.5, `suspect` >5 or `SKEW`.
8. ★ Pick: among `decent`/`strong` rows without `SKEW`, `SWING` or `FDV`, the highest est. yield/d (or highest score if no fee is known). None if nothing qualifies.

Flags are warnings, not filters: `FDV`, `NEW` (pool < 7 days), `SKEW` (buys or
sells > 70% of transactions), `SWING` (|price change| > 30%).

JSON percent fields (`fee_pct`, `est_fee_yield_d`, `price_change_pct`) are
percentages: `0.3` means 0.3%.

**This is a list of candidates to review, not a safety check.** Rug pulls and
honeypots are not detected.

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
