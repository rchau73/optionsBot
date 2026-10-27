# Exchange feasibility brief: <exchange>

Copy this file to `docs/exchanges/<exchange>.md` and answer every section. Rules (#327, E8):

- Public documentation and public endpoints only. No account, key, token or order. Private competitor or commercial notes go in the gitignored `data/product/`.
- Every factual answer line is a bullet that ends with a source link and the date it was read, e.g. `([docs](https://…), 2026-10-27)`; or it carries the label `assumption` and says how to verify it, e.g. `assumption — verify: <page or public endpoint>`. A line with neither is not an answer.
- Compare like with like: the sections and their order are fixed so briefs can be read side by side. What the bot needs from an exchange is in [../exchange-adapter.md](../exchange-adapter.md).

Status: <draft | verified by a person on YYYY-MM-DD>

## 1. Option products offered
Coins, expiries (daily, weekly, monthly, quarterly), strike grid, settlement (cash or physical), coin- or stablecoin-margined.

Answer:

## 2. Liquidity
Open interest and bid-ask spreads on the strikes the strategy uses (about 0.16 delta, 7–50 DTE), from public data.

Answer:

## 3. Margin model
Portfolio margin or not; cross collateral; whether a pre-trade margin simulator exists (what `private/simulate_portfolio` gives the bot).

Answer:

## 4. Order types
Post-only/GTC limit, IOC, market, edit in place, cancel by label or client id, cancel all by currency.

Answer:

## 5. Public market data and open interest for GEX
Chain, tickers with greeks, index price, a volatility index, open interest per strike.

Answer:

## 6. Rate limits
The limits and how they map to the gateway's two pools (matching engine / non-matching).

Answer:

## 7. Testnet
Availability, whether its prices and books mirror mainnet.

Answer:

## 8. API key scopes
Trade-only keys, withdrawal off, IP allow-list.

Answer:

## 9. Terms of use for automated trading
Bots, API use, market-making rules.

Answer:

## 10. Regional restrictions
**Needs legal review.** Excluded jurisdictions, residency and licensing, customers' ability to use it.

Answer:

## 11. Adapter capabilities missing and verdict
The capabilities of the adapter study ([../exchange-adapter.md §3](../exchange-adapter.md#3-proposed-interface)) the exchange lacks, and one verdict: **feasible**, **feasible with a gap** or **not now**.

Answer:
