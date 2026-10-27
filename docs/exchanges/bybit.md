# Exchange feasibility brief: Bybit

Template: [TEMPLATE.md](TEMPLATE.md). Written 2026-10-27 by the Dev agent, which has **no web access**: nothing below was read from a live page. Every line is therefore labelled `assumption` (from prior knowledge of Bybit's V5 API) with the page or public endpoint a person checks. Status: draft, not yet verified by a person. Public documentation only; no account, key or order was used.

Documentation root: https://bybit-exchange.github.io/docs/v5/intro

## 1. Option products offered
- Options are category `option` of the V5 API; underlyings BTC, ETH and SOL, USDC-settled, cash-settled at expiry; margined in USDC (a linear contract, not coin-margined like Deribit's). assumption — verify: [instruments-info](https://bybit-exchange.github.io/docs/v5/market/instrument) and `GET https://api.bybit.com/v5/market/instruments-info?category=option&baseCoin=BTC`, 2026-10-27.
- Expiries: daily, weekly, monthly and quarterly; strikes per expiry from the same endpoint. assumption — verify: the `deliveryTime` and strike spread of the list above, 2026-10-27.
- Consequence: premium and P&L are in USDC, so the bot's coin-denominated accounting (see [exchange-adapter.md §2](../exchange-adapter.md#2-deribit-specific-assumptions-outside-the-gateway), "settle later") changes meaning. assumption — verify: [exchange-adapter.md](../exchange-adapter.md), 2026-10-27.

## 2. Liquidity
- Open interest and spreads on the 0.16 delta strikes of 7–50 DTE are not measured here. assumption — verify: sample `GET https://api.bybit.com/v5/market/tickers?category=option&baseCoin=BTC` (fields `bid1Price`, `ask1Price`, `openInterest`) as `cmd/usdcstudy` does for Deribit's books, 2026-10-27.
- Expectation to test: thinner than Deribit for BTC and ETH, thin for SOL. assumption — verify: the same tickers response compared with the recorded Deribit books (`data/market`), 2026-10-27.

## 3. Margin model
- Unified Trading Account with cross collateral; a portfolio-margin mode exists for options. assumption — verify: [account info and margin mode](https://bybit-exchange.github.io/docs/v5/account/account-info), 2026-10-27.
- A pre-trade margin simulator like `private/simulate_portfolio`: none known. assumption — verify: search the V5 documentation index for "estimate margin" or "pre-check order", 2026-10-27.

## 4. Order types
- `POST /v5/order/create` with `orderType` Limit or Market, `timeInForce` GTC, IOC, FOK or PostOnly. assumption — verify: [place order](https://bybit-exchange.github.io/docs/v5/order/create-order), 2026-10-27.
- Amend in place: `POST /v5/order/amend`. assumption — verify: [amend order](https://bybit-exchange.github.io/docs/v5/order/amend-order), 2026-10-27.
- Label: `orderLinkId` (a client order id, with a length limit smaller than Deribit's 64), cancel by it through `POST /v5/order/cancel`; cancel all by category and base coin through `POST /v5/order/cancel-all`. assumption — verify: [cancel order](https://bybit-exchange.github.io/docs/v5/order/cancel-order) and [cancel all](https://bybit-exchange.github.io/docs/v5/order/cancel-all), 2026-10-27.

## 5. Public market data and open interest for GEX
- Chain: `instruments-info`; tickers with bid, ask, mark, IV and greeks: `market/tickers?category=option`; open interest per instrument in the ticker; index price in the same response and the WebSocket `tickers` topic. assumption — verify: [tickers](https://bybit-exchange.github.io/docs/v5/market/tickers), 2026-10-27.
- A volatility index like DVOL: none known (the bot would keep reading Deribit's DVOL over a public connection). assumption — verify: the market section of the V5 docs, 2026-10-27.
- GEX itself keeps reading Deribit mainnet ([exchange-adapter.md §2](../exchange-adapter.md#2-deribit-specific-assumptions-outside-the-gateway)); the liquidity cap would need Bybit's own open interest. assumption — verify: the tickers page above, 2026-10-27.

## 6. Rate limits
- An IP limit on HTTP requests per 5-second window and per-UID limits per endpoint group; option order endpoints have their own, lower per-second limits. assumption — verify: [rate limit](https://bybit-exchange.github.io/docs/v5/rate-limit), 2026-10-27.
- Mapping: matching-engine pool = create, amend, cancel, cancel-all; non-matching = everything else. Bybit's limits are per-second counters, not Deribit's credits, so the limiter would take a different configuration. assumption — verify: the table in the page above, 2026-10-27.

## 7. Testnet
- A separate testnet (`api-testnet.bybit.com`) with its own sign-up. Whether its option books mirror mainnet prices is not known. assumption — verify: [integration guide](https://bybit-exchange.github.io/docs/v5/guide) and `GET https://api-testnet.bybit.com/v5/market/tickers?category=option`, 2026-10-27.

## 8. API key scopes
- Keys carry separate permissions (read, trade, withdrawal) and can be bound to an IP; a trade-only key without withdrawal is possible. assumption — verify: [integration guide](https://bybit-exchange.github.io/docs/v5/guide), 2026-10-27.

## 9. Terms of use for automated trading
- API trading is allowed for personal accounts; market-maker rules and rate allowances apply to a separate programme. assumption — verify: [Bybit Terms of Service](https://www.bybit.com/en/help-center/article/Terms-of-Service), 2026-10-27.

## 10. Regional restrictions
- **Needs legal review.** Bybit restricts several jurisdictions (the United States among those known); Brazil's status for derivatives, and the effect on customers using this bot, are unknown. assumption — verify: Bybit's restricted-regions help article and counsel under `docs/legal/`, 2026-10-27.

## 11. Adapter capabilities missing and verdict
Against the capabilities of [exchange-adapter.md §3](../exchange-adapter.md#3-proposed-interface):
- `MarginSimulator`: missing (no known pre-trade simulator); a local margin model is needed ("settle later"). assumption — verify: section 3 above, 2026-10-27.
- `VolIndex`: missing; Deribit's DVOL stays the source over a public connection. assumption — verify: section 5 above, 2026-10-27.
- Coin-margined accounting: Bybit's options are USDC; premium, fees and "% of equity" change meaning. assumption — verify: section 1 above, 2026-10-27.
- `CancelByLabel`: present through `orderLinkId`, with a shorter label limit than `orders.MaxLabelLen`. assumption — verify: section 4 above, 2026-10-27.
- `AmendInPlace`, `CancelAllByCurrency`: present (category and base-coin scope). assumption — verify: section 4 above, 2026-10-27.
- Rate limits: a different shape (per-second counters), so the two-pool limiter needs another configuration. assumption — verify: section 6 above, 2026-10-27.

**Verdict: feasible with a gap** (provisional until a person verifies the lines above): the order, data and key needs look met; the missing simulator, the stablecoin settlement and liquidity not yet measured are the gaps, and the regional position needs counsel.
