---
name: lp-plan
description: Analyze one Robinhood Chain coin by contract address with the plutus lpplan CLI and present an LP decision brief: best pool, go/wait/avoid verdict, fee tier, price range, deposit split, exit signals and risks. Read-only. Use when the user gives a CA and asks "should I LP this", "LP plan for 0x…", "analyze this coin for LP", "how should I set the LP", "/lp-plan", or wants to dig into the pick from lp-scan.
---

# lp-plan

Objective: for one coin (CA) on Robinhood Chain, run `lpplan --json` and present a decision brief for providing liquidity: which pool, GO / WAIT / AVOID and why, the range and deposit if it is not AVOID, and when to exit. The CLI computes everything; this skill only presents the JSON.

Requires `lpplan` (plutus stage 2, `cmd/lpplan`).

## Rules

- Take exactly one CA. Missing: ask for it. Not `0x` + 40 hex: say so and stop, do not call the CLI.
- Present only fields from the JSON. Never compute verdicts, ranges, yields or IL yourself.
- Never shorten a contract or pool address.
- Show every entry in `warnings` before the verdict.
- Never call a coin "safe", "a buy" or tell the user to enter. The verdict is a decision to review.
- Read-only: never sign, approve, add liquidity, or ask for keys or seed phrases, even if asked to "just do it".
- Fee tier null: say "unknown", never guess one.
- Retry the CLI once on exit 2 only. Never loop.
- Always end with the legend and disclaimer, whatever the verdict.

## 1. Map the request to flags

| User says | Flag | Default |
|---|---|---|
| the CA (`0x…`) | positional `<CA>` | required |
| "I have $500", "with 1k" | `--amount 500` (USD, plain number: 1k → `1000`) | none |
| safe / conservative / wide | `--risk safe` | `balanced` |
| aggressive / tight / narrow | `--risk aggressive` | |

Any other risk word: use `balanced` and say so. Do not set flags the user did not state.

## 2. Run it

From the plutus repo root:

```sh
lpplan <CA> <flags> --json              # if on PATH (go install ./cmd/lpplan)
go run ./cmd/lpplan <CA> <flags> --json # otherwise
```

Takes ~30–60 seconds (the free API throttles). Tell the user it is running.

- Exit **0**: parse JSON on stdout (AVOID is still exit 0).
- Exit **2**: invalid argument; stderr names it. Fix and retry **once**.
- Exit **1**: API or network error (incl. token not found). Report stderr and stop. HTTP 403 = GeckoTerminal bot protection; suggest waiting a few minutes.

## 3. Present the brief

In this order:

1. **Token**: `token.symbol`, `token.name`, full `token.address`, price, mcap (note `mcap_source` when `fdv`), `gt_score`, `holders_count`, `top10_pct`, `is_honeypot`, mint / freeze authority (null = "none"). Then every `info_flags[].message`.
2. **Warnings**: every entry in `warnings`, or "none".
3. **Pools**: one row per `pools` entry: pair, dex, fee (or "unknown"), liquidity, 24h volume, `turnover_d`, `est_fee_yield_d` (or "n/a"), grade, flags (or "none"), full `pool_address`. Mark `chosen: true` with ★.
4. **Verdict**: `verdict.decision` in caps (GO / WAIT / AVOID), then every `verdict.reasons[]` as `code (tier): message`.
5. **Strategy**: only when `strategy` is not null. Title it "Strategy" for GO, "If you still enter" for WAIT. Show `risk`, `price` with `price_unit`, mode (`concentrated`: `range_low`–`range_high` with `range_low_pct` / `range_high_pct`; `full_range`: `full_range_reason`), `sigma_7d_pct`, `base_fee_yield_d` (pool yield, no concentration), `concentration`, `est_fee_yield_d`, `est_fee_7d_pct`, IL at the edges (`il_low_pct`, `il_high_pct`). When `strategy.deposit` is not null: base and quote amounts and USD, `est_fees_d_usd`, `il_low_usd`, `il_high_usd`. AVOID: no strategy section at all.
6. **Exit signals**: every `exit_signals[].message` (GO and WAIT).
7. **Legend + disclaimer**: render the `legend` object; always end with its `disclaimer`. Fallback text:

```
Verdict: GO no blocking signal · WAIT a signal says hold off · AVOID a hard blocker
Range:   safe ±2σ · balanced ±1σ · aggressive ±0.5σ of 7-day volatility
IL:      loss vs holding if price ends at that range edge
★        pool the plan is built on
A decision brief, not financial advice: rug pulls and honeypots are not detected.
```

End after the brief. Do not offer to place the LP; suggest re-running later to re-check.
