---
name: lp-scan
description: Scan Robinhood Chain pools with the lpscan CLI and present a graded shortlist of coins to review for a liquidity position, with each coin's contract address, flags and one highlighted pick. Use when the user asks for LP coins, liquidity pool candidates, "top N coins to LP", or pool scans on Robinhood Chain, optionally with market cap, time window, liquidity, volume or count limits.
---

# lp-scan

Turn a plain-language request into `lpscan` flags, run it with `--json`, and present the result. The CLI flags and JSON fields are the contract; do not compute scores, grades or the pick yourself.

## 1. Map the request to flags

Only set what the user stated. Everything else keeps the CLI default.

| User says | Flag | Default |
|---|---|---|
| max / under N market cap | `--max-mcap N` (USD, plain number: 2M → `2000000`) | 5000000 |
| last 5 minutes / hour / 6 hours / day | `--window 5m\|1h\|6h\|24h` | 6h |
| at least N liquidity / TVL | `--min-liquidity N` | 50000 |
| at least N volume | `--min-volume N` | 25000 |
| show / top N | `--top N` (1–50) | 5 |

Other windows (4h, 12h, 1w) are not supported: pick the closest supported one and say so.

Examples:
- "top 5 LP coins on Robinhood Chain" → `lpscan --json`
- "under 2M market cap, last 6 hours" → `lpscan --max-mcap 2000000 --window 6h --json`
- "show 10, at least 100k liquidity" → `lpscan --top 10 --min-liquidity 100000 --json`

## 2. Run it

From the plutus repo root:

```sh
lpscan <flags> --json            # if lpscan is on PATH (go install ./cmd/lpscan)
go run ./cmd/lpscan <flags> --json   # otherwise
```

A full scan takes ~1.5–2 minutes (default `--timeout 120s`): the free API throttles hard and 429s are retried. Tell the user it is running.

- Exit **0**: parse the JSON on stdout (zero results is still exit 0).
- Exit **2**: invalid flag; stderr names it. Fix the flag and retry **once**.
- Exit **1**: API or network error. Report the stderr message; do not retry in a loop. An HTTP 403 means GeckoTerminal's bot protection blocked the request; suggest waiting a few minutes.

## 3. Present the result

In this order:

1. **Parameters used**: `params` (window, max mcap, min liquidity, min volume, top) and `stats` (`scanned` pools → `passed` → `coins`).
2. **Warnings**: every entry in `warnings` (failed endpoint, timeout = results may be incomplete).
3. **Shortlist**: for every row in `results`: rank, `coin_symbol`, `pair`, `grade`, `flags` (or "none"), liquidity, volume, `turnover_d`, `est_fee_yield_d` (when not null), price change, and the **full `coin_address` (CA)**, never shortened. Mark the pick with ★.
4. **Is this list good?** A short read from the grades: how many `strong` / `decent` / `weak` / `suspect`, and what the flags suggest (e.g. "2 of 5 are suspect: one-sided trading or implausible turnover"). If every row is `weak` or `suspect`, say the list is thin this run.
5. **Pick**: if `pick` is not null: "★ Best candidate to review first: SYMBOL (full CA)" plus `pick.reason`. If `pick` is null: "No clear candidate this run." To analyze any listed coin, run `/lp-plan <CA>`.
6. **Legend + disclaimer**, always, every time, even with zero results:

```
Legend:
  FDV    market cap missing, FDV used instead
  NEW    pool younger than 7 days
  SKEW   buys or sells above 70% of transactions in the window
  SWING  absolute price change above 30% in the window
  GRADE  weak <0.1 · decent 0.1-0.5 · strong >=0.5 · suspect >5 or SKEW (per-day score)
  ★      best candidate to review first, not a recommendation to enter
Candidates to review, not a safety check: rug pulls and honeypots are not detected.
```

Prefer the `legend` object from the JSON if it differs from the text above.

Zero results: say "No coins matched. Try a higher --max-mcap or lower --min-liquidity / --min-volume.", then the legend.

## Rules

- Never call a coin "safe", "good to buy" or a recommendation to enter. It is a list of candidates to review; the pick is the one to review first.
- Never shorten a contract address.
- Do not invent numbers or fields not in the JSON.
