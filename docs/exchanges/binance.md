# Exchange feasibility brief: Binance

Template: [TEMPLATE.md](TEMPLATE.md). Written 2026-10-29 by the Dev agent, which has **no web access**: nothing below was read from a live page.
Every line is therefore labelled `assumption` (from prior knowledge of Binance's European Options API) with the page or public endpoint a person checks.
Status: draft, not yet verified by a person. Public documentation only; no account, key or order was used.

Documentation root: https://developers.binance.com/docs/derivatives/option-trading

## 1. Option products offered
- European options on the international platform through the options API (host `eapi.binance.com`); underlyings include BTC and ETH among a few others, quoted and settled in USDT, cash-settled at expiry. assumption — verify: [exchange information](https://developers.binance.com/docs/derivatives/option-trading/market-data/Exchange-Information) and `GET https://eapi.binance.com/eapi/v1/exchangeInfo`, 2026-10-29.
- Expiries: daily, weekly, monthly and quarterly; strikes per expiry from the same endpoint. assumption — verify: the `expiryDate` and strike spread of the list above, 2026-10-29.
- Consequence: premium and P&L are in USDT, so the bot's coin-denominated accounting changes meaning, as for Bybit ([exchange-adapter.md §2](../exchange-adapter.md#2-deribit-specific-assumptions-outside-the-gateway)). assumption — verify: the `quoteAsset` field of the list above, 2026-10-29.

## 2. Liquidity
- Open interest and spreads on the 0.16 delta strikes of 7–50 DTE are not measured here. assumption — verify: sample `GET https://eapi.binance.com/eapi/v1/ticker` (bid and ask) and `GET https://eapi.binance.com/eapi/v1/openInterest` per underlying and expiry, as `cmd/usdcstudy` does for Deribit's books, 2026-10-29.
- Expectation to test: thinner than Deribit for BTC and ETH. assumption — verify: the same responses compared with the recorded Deribit books (`data/market`), 2026-10-29.

## 3. Margin model
- An options margin account, and a portfolio-margin programme for eligible accounts that is separate from the options account. Whether options are included in it is not known. assumption — verify: [portfolio margin](https://developers.binance.com/docs/derivatives/portfolio-margin/general-info) and [options account](https://developers.binance.com/docs/derivatives/option-trading/account), 2026-10-29.
- A pre-trade margin simulator: none known. assumption — verify: search the options and portfolio-margin documentation for "estimate" or "pre-check", 2026-10-29.

## 4. Order types
- `POST /eapi/v1/order` with `type` LIMIT or MARKET and `timeInForce` GTC, IOC or FOK; a post-only option exists for limit orders. assumption — verify: [new order](https://developers.binance.com/docs/derivatives/option-trading/trade/New-Order), 2026-10-29.
- Amend in place: none known (cancel and replace). assumption — verify: the trade section of the [options docs](https://developers.binance.com/docs/derivatives/option-trading/trade/New-Order), 2026-10-29.
- Label: `clientOrderId`, cancel by it through `DELETE /eapi/v1/order`; cancel all open orders of an underlying through `DELETE /eapi/v1/allOpenOrders`. assumption — verify: [cancel order](https://developers.binance.com/docs/derivatives/option-trading/trade/Cancel-Option-Order), 2026-10-29.

## 5. Public market data and open interest for GEX
- Chain: `exchangeInfo`; mark price with IV and greeks: `GET /eapi/v1/mark`; tickers: `GET /eapi/v1/ticker`; index price: `GET /eapi/v1/index`; WebSocket streams for the same. assumption — verify: [mark price](https://developers.binance.com/docs/derivatives/option-trading/market-data/Option-Mark-Price), 2026-10-29.
- Open interest per underlying and expiry (not per instrument in one call). assumption — verify: [open interest](https://developers.binance.com/docs/derivatives/option-trading/market-data/Open-Interest), 2026-10-29.
- A volatility index: Binance publishes a BVOL index for BTC and ETH; whether it is on the public API and comparable to DVOL is not known. assumption — verify: the options market-data section and Binance's BVOL page, 2026-10-29.
- GEX itself keeps reading Deribit mainnet; the liquidity cap would need Binance's own open interest per strike. assumption — verify: the open-interest page above, 2026-10-29.

## 6. Rate limits
- Request weights per IP per minute and order-count limits per account per window, listed in `exchangeInfo`. The numbers are not quoted here. assumption — verify: [options general info, limits](https://developers.binance.com/docs/derivatives/option-trading/general-info) and `rateLimits` in `GET /eapi/v1/exchangeInfo`, 2026-10-29.
- Mapping: matching-engine pool = new order, cancel, cancel all (order counts); non-matching = everything else (weights). A different shape from Deribit's credits. assumption — verify: the page above, 2026-10-29.

## 7. Testnet
- Testnets exist for spot and futures; one for options is not known. assumption — verify: [options general info](https://developers.binance.com/docs/derivatives/option-trading/general-info) and a request to a testnet options host, 2026-10-29.

## 8. API key scopes
- Keys carry separate permissions (read, trading, withdrawals, and an options-specific one) and an IP allow-list; a trade-only key without withdrawals is possible. assumption — verify: [how to create API keys](https://www.binance.com/en/support/faq/how-to-create-api-keys-on-binance-360002502072), 2026-10-29.

## 9. Terms of use for automated trading
- API trading is allowed for personal accounts; the API terms add usage conditions. assumption — verify: [Binance Terms of Use](https://www.binance.com/en/terms), 2026-10-29.
- Fees: maker and taker fees for options by tier, in USDT; not quoted here. assumption — verify: [fee schedule](https://www.binance.com/en/fee/schedule), 2026-10-29.
- **Needs legal review:** whether a third party may run this bot on other people's Binance accounts under those terms. assumption — verify: counsel under `docs/legal/` reading the terms above, 2026-10-29.

## 10. Regional restrictions
- **Needs legal review.** The international platform restricts several jurisdictions (the United States is served by a separate entity without derivatives, as known); Brazil's status for derivatives, and the effect on customers using this bot, are unknown. assumption — verify: Binance's restricted-regions help article and counsel under `docs/legal/`, 2026-10-29.

## 11. Adapter capabilities missing and verdict
Against the capabilities of [exchange-adapter.md §3](../exchange-adapter.md#3-proposed-interface):
- `MarginSimulator`: missing (no known pre-trade simulator); a local margin model is needed ("settle later"), and it sizes every order, so a person decides. assumption — verify: section 3 above, 2026-10-29.
- `VolIndex`: missing on the API as far as known; Deribit's DVOL stays the source over a public connection. assumption — verify: section 5 above, 2026-10-29.
- Coin-margined accounting: options are USDT; premium, fees and "% of equity" change meaning. assumption — verify: section 1 above, 2026-10-29.
- `AmendInPlace`: missing; the entry price step-down (`repriceLegs`) would cancel and re-place, which changes order handling and the unknown-outcome path, so a person reviews it. assumption — verify: section 4 above, 2026-10-29.
- `CancelByLabel`, `CancelAllByCurrency`: present (`clientOrderId`, per-underlying cancel all), with a label limit to compare with `orders.MaxLabelLen`. assumption — verify: section 4 above, 2026-10-29.
- Open interest per instrument: not in one call, so the GEX and liquidity inputs need several. assumption — verify: section 5 above, 2026-10-29.
- Testnet: none known for options, so the testnet-first rule has no counterpart. assumption — verify: section 7 above, 2026-10-29.
- Rate limits: weights and order counts, so the two-pool limiter needs another configuration. assumption — verify: section 6 above, 2026-10-29.

**Verdict: feasible with a gap** (provisional until a person verifies the lines above): keys, orders and data look met; no simulator, no amend, no known options testnet and USDT settlement are real gaps, several of them touching order handling, and the regional and third-party-account position needs counsel.
