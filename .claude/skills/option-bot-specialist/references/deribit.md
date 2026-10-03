# Deribit facts that change strategy design

Items marked **(verify)** drift over time or depend on account settings — confirm against the current Deribit documentation, the API (`public/get_instruments`, `private/get_account_summary`), or the user's account before relying on them.

## Instruments
- **Inverse options (BTC, ETH):** quoted and settled in the underlying coin; premium, P&L and margin are in coin. A USD P&L requires the spot conversion — always state which.
- **Linear options (USDC-settled)** exist for some underlyings **(verify which)** — margin and P&L in USDC, no coin-collateral convexity.
- **European exercise, cash-settled** against a time-weighted index average before expiry; **expiry at 08:00 UTC**. Dailies, weeklies (Friday), monthlies (last Friday) and quarterlies **(verify listings)**.
- **Futures** (dated, inverse and linear) and **perpetuals** for hedging; perpetuals pay/receive **funding** — include it in hedge cost.
- **Spot** pairs on Deribit for covered/cash-secured structures **(verify pairs)**.
- Strikes, tick sizes (`tick_size`, `tick_size_steps`) and `min_trade_amount` come from `public/get_instruments` — never hard-code them; the bot already reads them.

## Data the strategies need (public API)
- `public/get_book_summary_by_currency` — OI, mark IV, mark price per instrument (the bot's GEX input).
- `public/get_order_book` — depth and spread at the strikes you plan to trade.
- `public/ticker` / `ticker.{instrument}.100ms` subscriptions — bid/ask, mark, greeks, mark IV.
- `deribit_volatility_index.{btc_usd|eth_usd}` (DVOL) and `public/get_volatility_index_data` for history.
- `public/get_tradingview_chart_data` — underlying candles.
- Historical OI per strike is **not** served as a time series by the live API: a pinning backtest needs data collected over time or an external dataset — flag this in Research Requests.

## Orders and execution
- Order types: limit, market, stop variants; `time_in_force` (GTC, IOC, FOK); `post_only` for passive quoting; `reduce_only` for exits **(verify flags per instrument)**.
- **Combo orders / RFQ / block trades** let multi-leg structures trade as one — preferred for condors and spreads to avoid legging risk **(verify availability and minimums)**.
- Market orders on illiquid options can fill far from mid; exits in fast markets are where backtests most overstate results.

## Margin
- Standard Margin vs **Portfolio Margin** (the bot assumes PM): PM nets risk across the portfolio, so marginal margin depends on what is already on the book — size with `private/get_margins`, not a fixed rate.
- Inverse products: collateral value moves with spot, so margin headroom shrinks exactly when short puts lose.

## Limits and fees
- Rate limits are credit-based and split between matching-engine and non-matching requests **(verify current tiers for the account)**; the gateway enforces both pools.
- Fees: maker/taker per contract with a cap relative to option price, plus delivery fees **(verify the current schedule)** — model them explicitly in every backtest.

## Testnet
- `test.deribit.com` mirrors the API but liquidity, quotes and sometimes underlying prices are unrealistic (bid often 0; mark price fallback). Use testnet to prove *mechanics* (orders, fills, rolls, kill switch), not *profitability*.
