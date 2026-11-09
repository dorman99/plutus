---
name: lp-scan
description: Scan Robinhood Chain trending pools with the lpscan CLI and present a shortlist of meme coins to review for a liquidity position, each with its lpplan verdict (GO/WAIT/AVOID), holders, top-10 share, honeypot, chosen pool, volatility, yield, IL, net 7d, contract address and one highlighted pick. Use when the user asks for LP coins, liquidity pool candidates, "top N coins to LP", coins "like DELTA", trending scans or pool scans on Robinhood Chain, optionally with market cap, time window, liquidity, volume, holder or count limits.
---

# lp-scan

Turn a plain-language request into `lpscan` flags, run it with `--json`, and present the result. The CLI flags and JSON fields are the contract; do not compute scores, verdicts, net 7d or the pick yourself.

## 1. Map the request to flags

Only set what the user stated. Everything else keeps the CLI default.

| User says | Flag | Default |
|---|---|---|
| above / at least N market cap | `--min-mcap N` (USD, plain number: 1M → `1000000`) | 1000000 |
| max / under N market cap | `--max-mcap N` | 20000000 |
| include non-trending / all pools / new pools | `--source all` | trending |
| stocks / tokenized stocks / ETFs | `--kind stock` | meme |
| memes and stocks / everything | `--kind all` | |
| quick / fast / no plans | `--detail basic` | full |
| top 10 holders under N% / no bundles | `--max-top10 N` | off |
| last 5 minutes / hour / 6 hours / day | `--window 5m\|1h\|6h\|24h` | 6h |
| at least N liquidity / TVL | `--min-liquidity N` | 50000 |
| at least N volume | `--min-volume N` | 25000 |
| show / top N | `--top N` (1–50; full detail for the first 10 only) | 5 |

Other windows (4h, 12h, 1w) are not supported: pick the closest supported one and say so.

Examples:
- "top 5 LP coins on Robinhood Chain" → `lpscan --json`
- "coins like DELTA, up to 30M mcap" → `lpscan --max-mcap 30000000 --json`
- "show 8, no bundled holders" → `lpscan --top 8 --max-top10 60 --json`
- "scan tokenized stocks" → `lpscan --kind stock --json`

## 2. Run it

From the plutus repo root:

```sh
lpscan <flags> --json            # if lpscan is on PATH (go install ./cmd/lpscan)
go run ./cmd/lpscan <flags> --json   # otherwise
```

The default scan takes ~4 minutes (default `--timeout 360s`): 3 trending pages plus ~4 calls per coin for its plan, and the free API throttles hard. Tell the user it is running and roughly how long. With `--detail basic` it is ~1–1.5 minutes.

- Exit **0**: parse the JSON on stdout (zero results is still exit 0).
- Exit **2**: invalid flag; stderr names it. Fix the flag and retry **once**.
- Exit **1**: API or network error. Report the stderr message; do not retry in a loop. An HTTP 403 means GeckoTerminal's bot protection blocked the request; suggest waiting a few minutes.

## 3. Present the result

In this order:

1. **Parameters used**: `params` (source, kind, window, min–max mcap, min liquidity, min volume, top, detail, max_top10 when set) and `stats` (`scanned` pools → `passed` → `coins`).
2. **Warnings**: every entry in `warnings` (failed endpoint, timeout, a coin whose detail failed, detail cap).
3. **Shortlist**: one row per `results` entry, in the given order, mark the pick with ★. When `detail.plan` is present:

   | # | Coin | Verdict | Mcap | GT | Holders | Top 10 | Honeypot | ★ pool (`chosen_pair`, `chosen_fee_pct` or "unknown", `chosen_liquidity_usd`) | σ 7d | Mode | Yield/d | Fees 7d | IL (`il_low_pct`) | Net 7d | Exits now (`triggered_exits` or "none") | CA |

   Null values: "—". Without `detail.plan` (basic detail or a failed lookup), show the row with what it has (`pair`, `grade`, `flags`, liquidity, volume, `turnover_d`, `est_fee_yield_d`, plus holders / top 10 / honeypot / GT when `detail` is set) and say it has no plan. Always the **full `coin_address`**, never shortened. No per-coin pool tables: for those, run `/lp-plan <CA>`.
4. **Is this list good?** Count GO / WAIT / AVOID (and rows without a plan), then every `detail.plan.reasons[]` as `SYMBOL: code (tier): message`. Point out any `triggered_exits`, top 10 above 80% and honeypot not `no`. If no row is GO, say the list is thin this run.
5. **Pick**: if `pick` is not null: "★ Best candidate to review first: SYMBOL (full CA)" plus `pick.reason`. If `pick` is null: "No clear candidate this run." To see a coin's full brief (all pools, range, deposit split), run `/lp-plan <CA> [amount]`.
6. **Legend + disclaimer**, always, every time, even with zero results. Render the `legend` object (verdicts, net_7d, flags, grades, kind, pick, pick_guard) and end with its `disclaimer`. Fallback text:

```
Legend:
  VERDICT GO no blocking signal · WAIT a signal says hold off · AVOID a hard blocker
  NET 7d  est. 7-day fees + IL at the low edge; rows sort by verdict, then net 7d
  FDV     market cap missing, FDV used instead
  NEW     pool younger than 7 days
  SKEW    buys or sells above 70% of transactions in the window
  SWING   absolute price change above 30% in the window
  GRADE   weak <0.1 · decent 0.1-0.5 · strong >=0.5 · suspect >5 or SKEW (per-day score)
  KIND    stock = name ends with "Robinhood Token"; meme = everything else
  ★       best candidate to review first, not a recommendation to enter; never ★: top 10 above 80% or honeypot not "no"
Candidates to review, not a safety check: rug pulls and honeypots are not detected.
```

Zero results: say "No coins matched. Try a wider --min-mcap / --max-mcap, --kind all, --source all or lower --min-liquidity / --min-volume.", then the legend.

## Rules

- Never call a coin "safe", "good to buy" or a recommendation to enter. It is a list of candidates to review; the pick is the one to review first.
- Never shorten a contract or pool address.
- Do not invent numbers or fields not in the JSON; never recompute net 7d, verdicts or the pick.
- Read-only: never sign, approve, add liquidity, or ask for keys or seed phrases.
