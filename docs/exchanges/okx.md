# Exchange feasibility brief: OKX

Template: [TEMPLATE.md](TEMPLATE.md). Written 2026-10-29 by the Dev agent, which has **no web access**: nothing below was read from a live page.
Every line is therefore labelled `assumption` (from prior knowledge of OKX's V5 API) with the page or public endpoint a person checks.
Status: draft, not yet verified by a person. Public documentation only; no account, key or order was used.

Documentation root: https://www.okx.com/docs-v5/en/

## 1. Option products offered
- Options are instrument type `OPTION` of the V5 API; underlyings BTC-USD and ETH-USD (instrument families), coin-margined and settled in the coin, European, cash-settled at expiry. assumption — verify: [public instruments](https://www.okx.com/docs-v5/en/#public-data-rest-api-get-instruments) and `GET https://www.okx.com/api/v5/public/instruments?instType=OPTION&instFamily=BTC-USD`, 2026-10-29.
- Expiries: daily, weekly, monthly and quarterly; strikes per expiry from the same endpoint. assumption — verify: the `expTime` and strike spread of the list above, 2026-10-29.
- Consequence: like Deribit the premium is in coin, so the bot's coin accounting would largely carry over. assumption — verify: `ctValCcy` and `settleCcy` fields of the list above, 2026-10-29.

## 2. Liquidity
- Open interest and spreads on the 0.16 delta strikes of 7–50 DTE are not measured here. assumption — verify: sample `GET https://www.okx.com/api/v5/market/tickers?instType=OPTION&instFamily=BTC-USD` (fields `bidPx`, `askPx`) and `GET https://www.okx.com/api/v5/public/open-interest?instType=OPTION&instFamily=BTC-USD`, as `cmd/usdcstudy` does for Deribit's books, 2026-10-29.
- Expectation to test: clearly thinner than Deribit for BTC and ETH. assumption — verify: the same responses compared with the recorded Deribit books (`data/market`), 2026-10-29.

## 3. Margin model
- Account modes include a portfolio-margin mode that covers options, with cross collateral across coins. assumption — verify: [account mode](https://www.okx.com/docs-v5/en/#trading-account-rest-api-set-account-mode), 2026-10-29.
- A pre-trade margin simulator: a "simulated margin" endpoint exists in the account section, taking hypothetical positions. Whether it matches what `private/simulate_portfolio` gives the bot (post-trade IM and MM for a new order) is not known. assumption — verify: [get simulated margin](https://www.okx.com/docs-v5/en/#trading-account-rest-api-get-simulated-margin), 2026-10-29.

## 4. Order types
- `POST /api/v5/trade/order` with `ordType` limit, market, post_only, fok or ioc. assumption — verify: [place order](https://www.okx.com/docs-v5/en/#order-book-trading-trade-post-place-order), 2026-10-29.
- Amend in place: `POST /api/v5/trade/amend-order`. assumption — verify: [amend order](https://www.okx.com/docs-v5/en/#order-book-trading-trade-post-amend-order), 2026-10-29.
- Label: `clOrdId` (a client order id with a short length limit, letters and digits only, shorter than Deribit's 64 and without separators), cancel by it through `POST /api/v5/trade/cancel-order`. assumption — verify: [cancel order](https://www.okx.com/docs-v5/en/#order-book-trading-trade-post-cancel-order), 2026-10-29.
- Cancel all by currency: a mass-cancel endpoint exists for options, scoped by instrument family rather than by coin. assumption — verify: [mass cancel](https://www.okx.com/docs-v5/en/#order-book-trading-trade-post-cancel-all-after) and the mass-cancel entry next to it, 2026-10-29.

## 5. Public market data and open interest for GEX
- Chain: `public/instruments`; option summary with mark, IV and greeks per instrument: `public/opt-summary`; tickers with bid and ask: `market/tickers`; index price: `market/index-tickers`; WebSocket channels for tickers, option summary and index. assumption — verify: [option summary](https://www.okx.com/docs-v5/en/#public-data-rest-api-get-option-market-data), 2026-10-29.
- Open interest per instrument: `public/open-interest`. assumption — verify: [open interest](https://www.okx.com/docs-v5/en/#public-data-rest-api-get-open-interest), 2026-10-29.
- A volatility index like DVOL: none known on the public API (the bot would keep reading Deribit's DVOL over a public connection). assumption — verify: the market-data section of the V5 docs, 2026-10-29.
- GEX itself keeps reading Deribit mainnet ([exchange-adapter.md §2](../exchange-adapter.md#2-deribit-specific-assumptions-outside-the-gateway)); the liquidity cap would need OKX's own open interest. assumption — verify: the open-interest page above, 2026-10-29.

## 6. Rate limits
- Limits are per endpoint, per user id for private calls and per IP for public ones, counted per 2-second window; order endpoints have their own, tighter limits and sub-account limits. The numbers are not quoted here. assumption — verify: [rate limits](https://www.okx.com/docs-v5/en/#overview-rate-limit), 2026-10-29.
- Mapping: matching-engine pool = place, amend, cancel, mass-cancel; non-matching = everything else. Windowed counters, not Deribit's credits, so the limiter would take another configuration. assumption — verify: the table in the page above, 2026-10-29.

## 7. Testnet
- A demo-trading mode on the same host selected by the header `x-simulated-trading: 1`, with its own demo API keys. Whether its option books mirror mainnet prices and carry option liquidity is not known. assumption — verify: [demo trading](https://www.okx.com/docs-v5/en/#overview-demo-trading-services) and the instruments request of section 1 sent with that header, 2026-10-29.

## 8. API key scopes
- Keys carry separate permissions (read, trade, withdraw) and can be bound to an IP allow-list, with a passphrase; a trade-only key without withdraw is possible. assumption — verify: [REST authentication](https://www.okx.com/docs-v5/en/#overview-rest-authentication), 2026-10-29.

## 9. Terms of use for automated trading
- API trading is allowed for personal accounts; market-maker programmes are separate. assumption — verify: [OKX Terms of Service](https://www.okx.com/help/terms-of-service), 2026-10-29.
- Fees: maker and taker fees for options by tier, charged in coin; not quoted here. assumption — verify: [fee schedule](https://www.okx.com/fees), 2026-10-29.
- **Needs legal review:** whether a third party may run this bot on other people's OKX accounts under those terms. assumption — verify: counsel under `docs/legal/` reading the terms above, 2026-10-29.

## 10. Regional restrictions
- **Needs legal review.** OKX restricts several jurisdictions (the United States among those known) and runs regional entities with different product sets; Brazil's status for derivatives, and the effect on customers using this bot, are unknown. assumption — verify: OKX's restricted-regions help article and counsel under `docs/legal/`, 2026-10-29.

## 11. Adapter capabilities missing and verdict
Against the capabilities of [exchange-adapter.md §3](../exchange-adapter.md#3-proposed-interface):
- `MarginSimulator`: missing in the form the bot uses (a simulated-margin endpoint exists but is unproven for post-trade IM/MM); a local margin model may still be needed ("settle later"). Touches order sizing, so a person decides. assumption — verify: section 3 above, 2026-10-29.
- `VolIndex`: missing; Deribit's DVOL stays the source over a public connection. assumption — verify: section 5 above, 2026-10-29.
- `CancelByLabel`: present through `clOrdId`, with a shorter, alphanumeric-only label than `orders.MaxLabelLen`; unknown-outcome handling (`orders.MaybePlaced`) depends on it, so a person reviews that change. assumption — verify: section 4 above, 2026-10-29.
- `CancelAllByCurrency`: scoped by instrument family, not coin; the startup "cancel every order" step and the kill switch would change, so a person reviews it. assumption — verify: section 4 above, 2026-10-29.
- `AmendInPlace`: present. assumption — verify: section 4 above, 2026-10-29.
- Positions and reconcile: position quantities in contracts with a contract value, not Deribit's coin amounts; reconcile and `capToExchange` need a unit conversion, so a person reviews it. assumption — verify: `ctVal` in the instruments list of section 1, 2026-10-29.
- Rate limits: windowed counters, so the two-pool limiter needs another configuration. assumption — verify: section 6 above, 2026-10-29.

**Verdict: feasible with a gap** (provisional until a person verifies the lines above): coin-margined options, portfolio margin, a possible simulator and trade-only keys fit; liquidity not yet measured, label and position units touching order handling and reconcile, and the regional and third-party-account position (needs legal review) are the gaps.
