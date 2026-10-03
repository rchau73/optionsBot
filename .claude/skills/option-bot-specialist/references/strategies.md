# Strategy playbook

One entry per structure: what it is, the edge, the Greeks, how each short leg is covered, exits, what the backtest must include, and how it usually loses money. Prices and Greeks on Deribit's inverse options are quoted in the underlying (BTC/ETH); keep coin vs USD explicit in every number.

## Contents
1. Short strangle, delta-hedged (current bot)
2. Iron condor
3. Credit spreads (put / call)
4. Broken-wing butterfly and jade lizard
5. Open-interest / max-pain pinning (7–30 DTE)
6. GEX-regime filters and gamma-wall fades
7. Calendars and diagonals
8. Covered calls and cash-secured puts (Deribit spot)
9. Put-call parity and futures basis
10. Box spreads
11. Wide-spread liquidity capture

---

## 1. Short strangle, delta-hedged (what the bot runs today)
- **Structure:** sell OTM call + OTM put, same expiry; today |delta| ≈ 0.16–0.18 at 25/45/60 DTE, rolled at `rollout_dte`, stop at `stop_loss_multiplier` × premium.
- **Edge:** variance risk premium — implied vol on average exceeds realised vol in crypto; theta decay accelerates inside 30 DTE.
- **Greeks:** short gamma, short vega, long theta; delta ≈ 0 at entry, drifts with spot.
- **Coverage:** no wings, so cover with a **perpetual delta hedge** when |net delta| exceeds a band (e.g. 0.10–0.20 coin per coin of short notional), plus the hard stop-loss. The bot today only *reports* hedges (`hedge_report.json`); automating them is a product decision.
- **Exits:** ROI take-profit (50%), delta-drift roll, DTE roll, stop-loss, GEX regime shedding.
- **Backtest must include:** at least one crash (Mar 2020, May 2021, Jun/Nov 2022) and one squeeze; hedge costs and perp funding; market-order stop slippage in a fast market.
- **How it loses:** gap moves through a short strike with gamma highest near expiry; stops filled far through the trigger; whipsaw hedging (buy high, sell low) in a choppy, high-realised-vol range.

## 2. Iron condor
- **Structure:** short strangle + long further-OTM wings (e.g. short 0.16Δ, long 0.05Δ), same expiry. Max loss = wing width − credit.
- **Edge:** same premium as the strangle, but you pay away part of it to cap the tail; often the right first step from "naked" to "covered".
- **Greeks:** smaller gamma/vega than the strangle; much smaller worst case.
- **Coverage:** the wings. No hedge needed for solvency; an optional delta hedge reduces path P&L.
- **Exits:** 50% of max profit, or close/roll the tested side at a delta threshold; close all by ~7 DTE to avoid pin and gamma risk at 08:00 UTC settlement.
- **Deribit notes:** prefer combo orders so the four legs fill together; check wing liquidity — far OTM strikes can be quoted only at 0.0001–0.0005 with thin size.
- **Backtest must include:** realistic wing fills (wide spreads), the cost of the wings vs the reduction in stop-outs.
- **How it loses:** paying too much for wings in low IV; repeated small losses when one side is tested each cycle.

## 3. Credit spreads (put / call)
- **Structure:** sell one OTM option, buy a further-OTM option of the same type. Directional tilt with defined risk.
- **When:** pair with a regime view — e.g. when GEX says shed puts, the bot could sell only call spreads instead of a lone call.
- **Coverage:** the long leg. **Exits:** 50–70% of max profit, or at a delta/price threshold on the short strike.
- **How it loses:** a trend through the short strike; the payoff is skewed (small wins, defined but larger losses).

## 4. Broken-wing butterfly and jade lizard
- **Broken-wing butterfly:** long 1, short 2, long 1 with an asymmetric (wider) far wing, entered for a credit — no risk on one side, defined risk on the other.
- **Jade lizard:** short put + short call spread, with credit ≥ call spread width, so there is no upside risk; downside is a short put (cover with a put wing or a perp hedge rule).
- **Use:** expressing skew views. Crypto put skew is often rich after sell-offs; call skew is rich in euphoric rallies.
- **How it loses:** mis-sized short leg in a crash; complex fills — use combos.

## 5. Open-interest / max-pain pinning (7–30 DTE)
- **Idea:** large OI concentrations (and the max-pain strike) can act as magnets into expiry when dealers are long gamma there; sell premium around the pin, buy wings outside.
- **Strike selection:** for each expiry in 7–30 DTE, rank strikes by OI (calls + puts) from `public/get_book_summary_by_currency`; compute max pain; structure an iron condor or iron fly centred on the pin with wings outside the next large OI walls.
- **Signal hygiene:** only when GEX regime is positive/pinning and spot sits between the call wall and put wall; stand aside in negative-gamma regimes.
- **Coverage:** wings (iron fly/condor). Avoid naked pin trades — pins fail violently.
- **Backtest must include:** daily OI snapshots (not just today's), regime labels, and a comparison against the same structure placed without the OI rule (does the rule add anything?).
- **How it loses:** pin breaks on news or liquidations; OI is a stale signal once a large holder rolls.

## 6. GEX-regime filters and gamma-wall fades
- **Filter (in the bot today):** classify market-wide dealer gamma by spot vs gamma flip; in confirmed negative regimes shed the leg in the trend's direction.
- **Extensions to test:** size by regime (smaller in negative gamma); fade moves into large gamma walls with call/put spreads in positive regimes.
- **How it loses:** GEX is an estimate (dealer positioning is assumed, not observed); regimes flip fast around the flip — keep hysteresis.

## 7. Calendars and diagonals
- **Structure:** sell near expiry, buy a later one (same strike = calendar; different strike = diagonal).
- **Edge:** term-structure richness — front-month IV spikes around events; long back month keeps you long vega overall.
- **Greeks:** long vega, short gamma in the front, positive theta while spot stays near the strike.
- **Coverage:** the long back-month option covers the short front.
- **How it loses:** large moves away from the strike; front/back IV spread collapses the wrong way.

## 8. Covered calls and cash-secured puts (Deribit spot)
- **Covered call:** hold BTC/ETH (Deribit spot or as collateral) and sell OTM calls. In inverse options the collateral already *is* the coin — check how margin treats it.
- **Cash-secured put:** hold USDC and sell puts (USDC-linear options where available), or hold coin and accept the double-loss in a crash on inverse puts.
- **Edge:** monetise holdings; good for a long-term holder's mandate.
- **How it loses:** capped upside in rallies (calls), full downside exposure (puts).

## 9. Put-call parity and futures basis
- **Parity check:** C − P should equal the discounted (F − K) using the Deribit future of the same expiry. Persistent deviations net of fees/spreads → conversion/reversal (option combo + future hedge).
- **Basis / cash-and-carry:** long Deribit spot, short dated Deribit future when annualised basis exceeds funding and fees; hold to expiry.
- **Reality check:** these edges are usually tiny and competed away; the bot's latency and fees may make them negative. Treat as a measurement project first ("how often and how big are deviations after costs?") before trading.
- **How it loses:** legging risk (one leg fills, the other doesn't), fees, margin on both legs.

## 10. Box spreads
- **Structure:** bull call spread + bear put spread, same strikes/expiry; payoff fixed at strike width. Price vs width implies an interest rate.
- **Use:** detecting financing mispricing; mostly educational on Deribit given fees and European settlement.

## 11. Wide-spread liquidity capture
- **Idea:** some strikes quote very wide; a resting limit inside the spread can earn part of it when the other side crosses.
- **Rules:** only quote where you would happily hold the position (it must fit an approved structure above); cap inventory per strike; cancel on volatility spikes; account for adverse selection — you get filled most when the price is about to move against you.
- **Bot impact:** many amends → request budget and matching-engine credits; post-only orders.
- **How it loses:** adverse selection, stale quotes during fast moves, inventory piling up on one side.
