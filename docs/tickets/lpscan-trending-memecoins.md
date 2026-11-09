# lpscan: trending memecoins with full LP plans

## Story

Repo: `plutus` (`/Users/sunday/Documents/personal/www/go/plutus`)

As a liquidity provider on Robinhood Chain, I want `lpscan` to list trending
memecoins between $1M and $20M market cap, each with a full `lpplan` analysis,
so that I find more coins like DELTA, RAETO and VRAX without the shortlist
being crowded out by tokenized stocks such as USO, AMD, HIMS and INTC.

Context: the coins that work for me come from GeckoTerminal's trending list
and sit above $1M mcap. On 2026-11-09 the default scan's top 5 were 4
tokenized stocks (high turnover scores), and DELTA only showed up at rank 3
in a run where the API rate-limited page 3. Comparing six `lpplan` runs
(USO, DELTA, ORBIO, RAETO, VRAX, HOOKR) side by side was the useful view; the
scan should produce that view directly. HOOKR exposed a planner gap: GO
verdict on a pool whose turnover (0.37) was already below its own 0.5 exit
threshold, while a 0.88-turnover pool was available.

## Acceptance Criteria

### Source and filters (lpscan)
- [ ] Default source is `trending_pools` only; `--source trending|all` (`all` = pools + trending + new, today's behaviour).
- [ ] New `--min-mcap` flag, default `1000000`; `--max-mcap` default changes from `5000000` to `20000000`; min > max is a `*ParamError` (exit 2).
- [ ] A coin whose base token name ends with `• Robinhood Token` is `kind: "stock"`, else `"meme"`. Verify against live data that AMD, HIMS, INTC, SPY, NVDA follow the pattern before relying on it.
- [ ] `--kind meme|stock|all`, default `meme`; every row has a `kind` field. A meme paired with a stock (e.g. BONER/HIMS) stays `meme`.
- [ ] Optional `--max-top10 <pct>` (off by default) drops rows whose top-10 holder share exceeds it (requires detail).

### Per-coin detail
- [ ] After ranking by today's score, the top N rows are enriched. `--detail full` (default) reuses `lpplan.Planner` so each row carries the same verdict, reasons, chosen pool, σ 7d, mode, est. yield/d, est. fees 7d, IL and exit signals as `/lp-plan`. `--detail basic` adds only token info (holders, top10 %, honeypot, GT score, mint/freeze).
- [ ] Full detail is capped at 10 rows; rows past 10 get basic detail and a warning.
- [ ] A per-coin enrichment failure becomes a warning on that row, not a failed scan.
- [ ] Defaults: `--top 5`, `--timeout 360s`.

### Ordering and pick
- [ ] Final order: verdict (GO, WAIT, AVOID), then `net_7d_pct` = `est_fee_7d_pct + il_low_pct` (new field), then existing tie-breaks.
- [ ] ★ pick = highest `net_7d_pct` GO row, excluding rows with `TOP10` (> 80%) or `is_honeypot: "unknown"`; null when none qualifies, with the reason stated.
- [ ] AVOID rows stay in the list (at the bottom) with their reasons.

### lpplan changes (shared by /lp-plan)
- [ ] Chosen pool: among pools with liquidity ≥ $50K, prefer turnover/d ≥ 0.5, then highest est. yield/d; fall back to the current rule only when none clears 0.5. Test: HOOKR-shaped fixture picks the 0.88-turnover WETH 1% pool over the 0.37-turnover USDG 3.04% pool.
- [ ] New WAIT reason `LOW_TURNOVER` when the chosen pool's turnover/d is below the exit threshold (0.5); in the legend and README.

### Output and docs
- [ ] CLI table and `lp-scan` skill: one row per coin with verdict, mcap, GT score, holders, top 10 %, honeypot, ★ pool (pair, fee, liquidity), σ 7d, mode, yield/d, fees 7d, IL, net 7d, exit signals already triggered, full CA. No per-coin pool tables (use `/lp-plan`). Then GO/WAIT/AVOID counts with reasons, ★ pick, legend, disclaimer.
- [ ] JSON keeps the chosen pool address per row.
- [ ] README, `lp-scan` and `lp-plan` SKILL.md updated for the new flags, fields, ordering and `LOW_TURNOVER`.
- [ ] `go test ./...` passes on fixtures only (no network), covering: kind detection, mcap min/max, source selection, ordering, pick exclusions, detail cap, pool-choice rule, `LOW_TURNOVER`.
