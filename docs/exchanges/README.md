# Exchange feasibility briefs (E8)

One brief per candidate exchange, all on the fixed [TEMPLATE.md](TEMPLATE.md), so they read side by side. What the bot needs from an exchange is in [../exchange-adapter.md](../exchange-adapter.md).
Every figure carries a source link and a date, or the label `assumption` with how to verify it. The Dev agent has no web access: all four briefs are drafts of assumptions until a person verifies them.
Anything about offering the product on an exchange for other people's accounts **needs legal review** (section 9 and 10 of each brief; counsel under `docs/legal/`).

## Comparison

The verdicts are the template's terms: **feasible**, **feasible with a gap**, **not now**. Every cell is an `assumption` until a person verifies the brief.

| | Deribit (today) | [Bybit](bybit.md) | [OKX](okx.md) | [Binance](binance.md) | [Hyperliquid](hyperliquid.md) |
|---|---|---|---|---|---|
| Options on BTC and ETH | yes, coin-margined | yes, USDC | yes, coin-margined | yes, USDT | none known |
| Margin | portfolio margin | unified account, portfolio mode | portfolio-margin mode | options account, separate portfolio programme | perpetual margining only |
| Pre-trade margin simulator | `private/simulate_portfolio` | none known | endpoint exists, unproven | none known | none known |
| Amend in place | yes | yes | yes | none known | yes |
| Cancel by label | yes (64 characters) | `orderLinkId`, shorter | `clOrdId`, short, alphanumeric | `clientOrderId` | `cloid`, hex only |
| Volatility index | DVOL | none known | none known | BVOL, API unknown | none known |
| Testnet | yes | yes | demo mode, same host | none known for options | yes, options unknown |
| Key permissions | trade-only possible | trade-only possible | trade-only possible | trade-only possible | agent wallet, cannot withdraw |
| Third-party accounts and regions | — | needs legal review | needs legal review | needs legal review | needs legal review |
| Verdict | the adapter | feasible with a gap | feasible with a gap | feasible with a gap | not now |

Changes an adapter would force in order handling, the unknown-outcome path, the kill switch or reconcile are listed in section 11 of each brief and go to a person.
