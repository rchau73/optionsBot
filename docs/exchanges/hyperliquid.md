# Exchange feasibility brief: Hyperliquid

Template: [TEMPLATE.md](TEMPLATE.md). Written 2026-10-29 by the Dev agent, which has **no web access**: nothing below was read from a live page.
Every line is therefore labelled `assumption` (from prior knowledge of Hyperliquid's API) with the page or public endpoint a person checks.
Status: draft, not yet verified by a person. Public documentation only; no account, key or order was used.

Documentation root: https://hyperliquid.gitbook.io/hyperliquid-docs

## 1. Option products offered
- No listed options market is known: the exchange offers perpetual futures and spot on its own chain. assumption — verify: the public info request `{"type":"meta"}` posted to `https://api.hyperliquid.xyz/info` (the perpetual universe) and the [product list](https://hyperliquid.gitbook.io/hyperliquid-docs), 2026-10-29.
- Options on the exchange's own order book, if any, would come from third-party or builder-deployed markets; none is known to list BTC and ETH with expiries and strikes. assumption — verify: the builder-deployed markets section of the [documentation](https://hyperliquid.gitbook.io/hyperliquid-docs) and the info request `{"type":"allPerpMetas"}`, 2026-10-29.
- Consequence: the strategy (short strangles) cannot be run on a perpetual; the question is whether options exist at all. assumption — verify: the two lines above, 2026-10-29.

## 2. Liquidity
- Not measurable while no option instrument is known. assumption — verify: if section 1 finds option markets, sample the info request `{"type":"l2Book","coin":"<instrument>"}` and compare with the recorded Deribit books (`data/market`), 2026-10-29.

## 3. Margin model
- Cross and isolated margin on perpetuals, with a portfolio-margin account type announced for some assets. Whether it covers options is not known. assumption — verify: [margining](https://hyperliquid.gitbook.io/hyperliquid-docs/trading/margining) and [portfolio margin](https://hyperliquid.gitbook.io/hyperliquid-docs/trading/portfolio-margin), 2026-10-29.
- A pre-trade margin simulator: none known. assumption — verify: the request types listed in the [info endpoint](https://hyperliquid.gitbook.io/hyperliquid-docs/for-developers/api/info-endpoint) documentation, 2026-10-29.

## 4. Order types
- Orders are signed actions on the exchange endpoint: limit with time in force GTC, IOC or ALO (post-only); market as an aggressive IOC. assumption — verify: [exchange endpoint](https://hyperliquid.gitbook.io/hyperliquid-docs/for-developers/api/exchange-endpoint), 2026-10-29.
- Amend in place: a modify action exists. assumption — verify: [exchange endpoint](https://hyperliquid.gitbook.io/hyperliquid-docs/for-developers/api/exchange-endpoint), 2026-10-29.
- Label: a client order id (`cloid`, a 128-bit hex value, not a free-text label), cancel by it with the cancel-by-cloid action; cancel all: a schedule-cancel dead-man switch and per-order cancels, no cancel-all-by-currency known. assumption — verify: [exchange endpoint](https://hyperliquid.gitbook.io/hyperliquid-docs/for-developers/api/exchange-endpoint), 2026-10-29.

## 5. Public market data and open interest for GEX
- Public info requests and WebSocket subscriptions for books, trades, mid prices and perpetual context (mark, open interest, funding). Option chains, greeks and IV: none known. assumption — verify: [info endpoint](https://hyperliquid.gitbook.io/hyperliquid-docs/for-developers/api/info-endpoint) and [WebSocket subscriptions](https://hyperliquid.gitbook.io/hyperliquid-docs/for-developers/api/websocket/subscriptions), 2026-10-29.
- A volatility index like DVOL: none known. assumption — verify: [info endpoint](https://hyperliquid.gitbook.io/hyperliquid-docs/for-developers/api/info-endpoint), 2026-10-29.
- GEX itself keeps reading Deribit mainnet ([exchange-adapter.md §2](../exchange-adapter.md#2-deribit-specific-assumptions-outside-the-gateway)). assumption — verify: section 1 above, 2026-10-29.

## 6. Rate limits
- Per-IP weight limits on requests and per-address limits on actions that grow with traded volume. The numbers are not quoted here. assumption — verify: [rate limits and user limits](https://hyperliquid.gitbook.io/hyperliquid-docs/for-developers/api/rate-limits-and-user-limits), 2026-10-29.
- Mapping: matching-engine pool = order, modify, cancel actions; non-matching = info requests. A different shape from Deribit's credits. assumption — verify: [rate limits and user limits](https://hyperliquid.gitbook.io/hyperliquid-docs/for-developers/api/rate-limits-and-user-limits), 2026-10-29.

## 7. Testnet
- A public testnet with its own API host and faucet-funded accounts. Whether it lists options is not known. assumption — verify: the [API documentation](https://hyperliquid.gitbook.io/hyperliquid-docs/for-developers/api) and the info request `{"type":"meta"}` posted to `https://api.hyperliquid-testnet.xyz/info`, 2026-10-29.

## 8. API key scopes
- No API keys: a wallet signs every action. An "API wallet" (agent) can be approved to trade for a main account and cannot withdraw, which fits trade-only; no IP allow-list is known. assumption — verify: [nonces and API wallets](https://hyperliquid.gitbook.io/hyperliquid-docs/for-developers/api/nonces-and-api-wallets), 2026-10-29.
- Consequence: the "encrypted at rest, revocable" key rules of the PRD become agent-wallet rules (revoked on chain by the account owner). assumption — verify: [nonces and API wallets](https://hyperliquid.gitbook.io/hyperliquid-docs/for-developers/api/nonces-and-api-wallets) and PRD E8, 2026-10-29.

## 9. Terms of use for automated trading
- Trading by API is the normal access path; the terms bar listed jurisdictions. assumption — verify: [Hyperliquid terms of use](https://app.hyperliquid.xyz/terms), 2026-10-29.
- Fees: maker and taker fees by volume tier for perpetuals and spot; options fees not applicable while none are listed. assumption — verify: [fees](https://hyperliquid.gitbook.io/hyperliquid-docs/trading/fees), 2026-10-29.
- **Needs legal review:** whether a third party may operate an agent wallet on other people's accounts, and what a non-custodial on-chain venue means for the product. assumption — verify: counsel under `docs/legal/`, 2026-10-29.

## 10. Regional restrictions
- **Needs legal review.** The terms exclude listed jurisdictions (the United States among those known); a decentralised venue raises other questions for a regulated product in Brazil. assumption — verify: the [terms page](https://app.hyperliquid.xyz/terms) and counsel under `docs/legal/`, 2026-10-29.

## 11. Adapter capabilities missing and verdict
Against the capabilities of [exchange-adapter.md §3](../exchange-adapter.md#3-proposed-interface):
- The option chain, greeks and expiries the whole strategy reads: missing (no listed options known). assumption — verify: section 1 above, 2026-10-29.
- `MarginSimulator`: missing; a local margin model would be needed. assumption — verify: section 3 above, 2026-10-29.
- `VolIndex`: missing. assumption — verify: section 5 above, 2026-10-29.
- `CancelByLabel`, `CancelAllByCurrency`: partial (`cloid` is not a free label; no cancel-all-by-currency), which touches the unknown-outcome path and the kill switch, so a person reviews it. assumption — verify: section 4 above, 2026-10-29.
- Authentication: wallet signatures instead of API credentials, a new connection and key-handling model. assumption — verify: section 8 above, 2026-10-29.

**Verdict: not now** (provisional until a person verifies section 1): with no listed BTC and ETH options market known, there is nothing for the strategy to trade; revisit if the check of section 1 finds one.
