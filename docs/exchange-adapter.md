# Exchange adapter boundary: what the bot asks of Deribit, and the interface that would hide it

E8 (PRD "Multi-customer, multi-account, multi-exchange") adds exchanges without changing the strategy. This study (#325, with #253 and #181) lists everything the bot asks of Deribit today, every Deribit habit found outside the gateway, and proposes the smallest adapter interface that hides it, with a migration in small steps. **Design only: no Go code moves with it.** The interfaces that already name the boundary live in `internal/exchange` ([architecture.md §15](architecture.md#15-exchange-adapters)); this document is their inventory and their next step.

`tests/exchange_coupling_test.go` pins section 1: it reads every `"public/…"` and `"private/…"` string literal under `internal/` and fails when one is missing from its table (which mirrors the table below), when a method shows up in a package the table does not allow, or when any such literal appears under `internal/strategy`. A new Deribit call is added to both, in the same PR.

Contents: [1. Inventory](#1-inventory-every-deribit-method-and-subscription) · [2. Deribit-specific assumptions](#2-deribit-specific-assumptions-outside-the-gateway) · [3. Proposed interface](#3-proposed-interface) · [4. Migration](#4-migration-sequence) · [5. Proposals for a person](#5-proposals-for-a-person-order-handling-and-reconcile)

## 1. Inventory: every Deribit method and subscription

How to read the columns:

- **Access**: `public` methods need no credentials; `private` ones need `public/auth` and are refused by a `PublicOnly()` gateway (`ErrPrivateOnPublic`).
- **Pool**: Deribit charges each request to one of two credit pools (`gateway.IsMatchingEngine`): **matching** (`private/buy|sell|edit|close_position` and every `private/cancel*`) or **non-matching** (everything else).
- **Retried**: the gateway retries a rate-limited request only when `isIdempotent` (`public/*`, `private/get_*`). Orders and cancels are never retried: a "failed" order may already be on the book.
- **Lane**: the gateway's priority queue (`gateway.PriorityHigh` is drained first and skips the circuit breaker).
- **Connection**: `trading` (the account's gateway, testnet or live) or `market` (mainnet public-only on testnet, the trading gateway on live — `marketGW` in `cmd/bot/main.go`).

### RPC methods

| Method | Package / file | Purpose | Access | Pool | Retried | Lane | Connection | Strategy loses without it |
|---|---|---|---|---|---|---|---|---|
| `public/auth` | gateway / `gateway.go` | client-credentials login on connect and reconnect | public | non-matching | yes | high | trading | every private call |
| `public/set_heartbeat` | gateway / `gateway.go` | asks Deribit to send heartbeats | public | non-matching | yes | low | both | early detection of a dead socket (falls back to its own ping) |
| `public/test` | gateway / `gateway.go` | answer to Deribit's `test_request` (written on the socket, outside the queue) and the gateway's own keep-alive ping | public | non-matching | yes | low | both | the connection (Deribit closes it, code 4000) |
| `public/subscribe` | gateway / `gateway.go` | subscribes channels (below), in chunks; restored after a reconnect | public | non-matching | yes | low | trading | live index, DVOL and tickers |
| `public/get_instruments` | marketdata / `marketdata.go` | the option chain at start (names, strike, expiry, tick sizes, minimum size) | public | non-matching | yes | low | trading | every instrument: no entry, no mark |
| `public/get_volatility_index_data` | marketdata / `marketdata.go` | DVOL daily history to seed the IV percentile | public | non-matching | yes | low | trading | the margin band starts at percentile 50 until history builds up |
| `public/get_book_summary_by_currency` | gex / `manager.go`; recorder / `linear.go` | mainnet open interest and marks for GEX and the liquidity cap (gex, every 60 s); the USDC-settled book for the recorder | public | non-matching | yes | low | market | GEX regime and flip (no new risk: `gex_wait`), the liquidity cap (nothing sent) |
| `public/get_tradingview_chart_data` | orders / `executor.go` | daily closes of `<COIN>-PERPETUAL` that seed the SMA trend and swings | public | non-matching | yes | low | trading | the trend at startup (it builds from live prices instead) |
| `private/buy` | orders / `executor.go`; gateway / `ratelimiter.go` (pool) | buy-backs: stops, rolls, delta exits, MM cut, balancing, kill switch | private | matching | no | high for stop-loss, kill switch, GEX close; else low | trading | every exit |
| `private/sell` | orders / `executor.go`; gateway / `ratelimiter.go` (pool) | entries, repairs, top-ups, `close_long` | private | matching | no | low | trading | every new short |
| `private/edit` | orders / `executor.go`; gateway / `ratelimiter.go` (pool) | re-prices a resting entry/repair (`AmendOrder`, step-down) | private | matching | no | low | trading | the step-down (cancel and re-send instead) |
| `private/cancel` | orders / `executor.go`; gateway / `ratelimiter.go` (pool prefix) | cancels one order by ID (timeouts, freeze, shed, resting closes) | private | matching | no | high | trading | every timeout and freeze |
| `private/cancel_by_label` | orders / `executor.go` | cancels an order whose submit outcome is unknown (`orders.MaybePlaced`) | private | matching | no | high | trading | the unknown-outcome guard: slot or instrument stays blocked |
| `private/cancel_all_by_currency` | orders / `executor.go` | startup reconcile and kill switch | private | matching | no | high | trading | a clean start and the kill switch |
| `private/cancel_all_by_instrument` | orders / `executor.go` | `Executor.CancelAll` — **not called** by any package today | private | matching | no | high | trading | nothing |
| `private/get_order_state` | orders / `executor.go` | fill tracking of resting orders, read-back after cancel | private | non-matching | yes | low | trading | fills of resting orders |
| `private/get_user_trades_by_order` | orders / `executor.go` | the fees of an order's fills (`OrderFee`), booked into net P&L | private | non-matching | yes | low | trading | net P&L (gross only) |
| `private/get_positions` | orders / `executor.go`; account / `account.go` | option shorts for reconcile and the position check (orders, `kind=option`); every position of each held currency for the monitor (account) | private | non-matching | yes | low | trading | reconcile, the position check, adoption of orphans |
| `private/get_account_summary` | orders / `executor.go`; account / `account.go` | IM, MM, margin balance (`extended`) for the margin policy; per-currency fallback of the monitor's account view | private | non-matching | yes | low | trading | the margin policy: no new risk while margin is unknown |
| `private/get_account_summaries` | account / `account.go` | every currency's summary at once for `/api/account` (monitor only) | private | non-matching | yes | low | trading | nothing in trading (monitor only) |
| `private/simulate_portfolio` | orders / `executor.go` | sizing of entries and top-ups and the size cap (`SimulatePortfolio`, `SimulateAlone`, ≤ 1 call/s spaced in the executor) | private | non-matching | no (not `get_`) | low | trading | every entry and top-up (a failed simulation → no new risk) |
| `private/close_position` | gateway / `ratelimiter.go` | named only in the matching-pool list (`IsMatchingEngine`) — **not called** by any package today | private | matching | no | — | — | nothing |
| `private/get_margins` | orders / `executor.go` | margin of one order (`GetMargins`) — in the strategy's `AccountReader` but **not called** today | private | non-matching | yes | low | trading | nothing today |

The gateway also matches on method prefixes, which the test lists as gateway-only entries: `"public/"` and `"private/get_"` (`retry.go`, idempotent), `"private/cancel"` (`ratelimiter.go`, matching pool) and `"private/"` (`gateway.go`, refused on a public-only gateway).

### Subscriptions (`public/subscribe` channels)

| Channel | Package / file | Purpose | Connection |
|---|---|---|---|
| `deribit_price_index.<coin>_usd` | marketdata / `marketdata.go` | the underlying price (reliable on testnet where options are unquoted) | trading |
| `deribit_volatility_index.<coin>_usd` | marketdata / `marketdata.go` | DVOL: margin bands, flip buffer, `sma9_band_sd` | trading |
| `ticker.<instrument>.100ms` | marketdata / `marketdata.go` | bid, ask, mark, greeks of every tradable expiry and of held instruments (`Track`) | trading |

Heartbeats arrive as `heartbeat` notifications; `test_request` is answered with `public/test` on the socket (above).

**Count (the coupling metric):** 23 RPC methods (20 called, 3 named but not called: `private/close_position`, `private/cancel_all_by_instrument`, `private/get_margins`), 3 subscription channels, 4 gateway prefix rules, in 6 packages (gateway, marketdata, gex, recorder, orders, account). None under `internal/strategy`.

## 2. Deribit-specific assumptions outside the gateway

Each row is marked: **adapter** (moves into the adapter), **stays** (exchange-neutral already, or the bot's own), or **settle later** (a strategy assumption a person decides when a second exchange is chosen).

| Assumption | Where it lives | Behind a `deps.go` interface today? | Mark |
|---|---|---|---|
| Instrument naming `BTC-4OCT26-60000-P` (one-digit days for dailies) | `marketdata.ParseOptionName`, called by gex, recorder, replay, evidence, idlemargin, deployeffect and strategy (`orphans.go`, `pending.go`, `shed.go`, `stopped.go`) | no — a free function | **adapter**: the adapter fills `Instrument{Strike, Expiry, OptionType}`; the strategy's four callers read the instrument instead (step 7). Research tools keep the parser for recorded Deribit files |
| Coin-margined (inverse) premiums: prices, fees and P&L in coin, USD = coin × spot | `orders` (journal, `ClosedNetPnL`), `strategy` (sizing, `pnl.go`), `perf`, `evidence`, `api` | no — a unit everywhere | **settle later**: a linear (USDC) exchange changes what "premium" and "% of equity" mean; the recorder already records Deribit's USDC book to study it |
| The margin simulator `private/simulate_portfolio` (portfolio margin, `add_positions`) | `orders.Executor`, used by `strategy/limits.go` sizing and `CapLots` | yes (`AccountReader.SimulatePortfolio/SimulateAlone`) | **adapter** + capability `MarginSimulator`; an exchange without it needs a local margin model (**settle later**) |
| Account figures (`initial_margin`, `maintenance_margin`, `margin_balance`, cross collateral totals) | `orders.AccountSummary`, `MarginUsage`, `risk` | yes (`GetAccountSummary`) | **adapter** maps to `AccountSummary`; the margin policy reads the result (**stays**) |
| Order labels ≤ 64 chars (`orders.MaxLabelLen`), `uniqueOrderLabel`, cancel by label | `orders/executor.go`, `strategy/orphans.go` | yes (`CancelByLabel`) | **adapter** (label = client order ID); exchanges without cancel-by-client-ID → [§5](#5-proposals-for-a-person-order-handling-and-reconcile) |
| Two rate-limit pools (matching / non-matching) and their credits in `.env` | `gateway/ratelimiter.go`, `IsMatchingEngine` | below the interfaces (gateway) | **adapter**: each adapter's transport owns its pools |
| Tick sizes: `Instrument.TickSize`/`TickSizeSteps` from the chain, and the executor's fallback `tickSize = 0.0001` (`RoundToTick`, used by `AmendOrder`) | `marketdata/types.go`, `orders/executor.go`, `strategy/pricing.go` | partly (the instrument travels through `MarketData`) | instrument ticks **stay** (adapter fills them); the 0.0001 fallback → [§5](#5-proposals-for-a-person-order-handling-and-reconcile) |
| `MinTradeAmount` lots (0.1 BTC, 1 ETH) | `marketdata.Instrument`, strategy sizing | yes (instrument) | **stays** (adapter fills it) |
| Fee formula 0.03 % of the underlying, cap 12.5 % of the price | `orders.OptionFee` (backtest, stress); live books `get_user_trades_by_order` | live: yes (`OrderFee`); model: no | **adapter** (`FeeModel` per exchange) |
| Index and DVOL names `deribit_price_index.<coin>_usd`, `deribit_volatility_index.<coin>_usd` | `marketdata.go` | yes (`MarketData`) | **adapter** |
| DVOL as the volatility index (margin bands, flip buffer, worst case) | `marketdata.DVOLTracker`, `risk`, `gamma.go`, `api/summary.go` | yes (`VolIndex`) | **adapter** + capability `VolIndex`; without one, read Deribit's DVOL over a public connection (**settle later**) |
| Ticker interval `100ms` (only `100ms` and `raw` valid) | `marketdata.go` | yes | **adapter** |
| Greeks from the ticker; position greeks size-weighted (`strategy.PerOptionGreeks`) | `marketdata`, `strategy/reconcile.go` | partly | ticker greeks **adapter**; `PerOptionGreeks` is reconcile → [§5](#5-proposals-for-a-person-order-handling-and-reconcile) |
| A strike shared by two strangles comes back as one position (#156 split) | `strategy` `GroupPositions` | no | reconcile → [§5](#5-proposals-for-a-person-order-handling-and-reconcile) |
| Currency-scoped cancel-all at startup and kill switch | `orders.CancelAllOrders`, `strategy` reconcile, `killswitch.go` | yes | **adapter**, plus capability `CancelAllByCurrency`; another scope → [§5](#5-proposals-for-a-person-order-handling-and-reconcile) |
| Amend in place (`private/edit`) | `orders.AmendOrder`, `strategy` step-down | yes | **adapter** + capability `AmendInPlace`; cancel-and-replace → [§5](#5-proposals-for-a-person-order-handling-and-reconcile) |
| Order types `limit`, `market`, IOC/GTC `time_in_force` | `orders.Order`, executor params | yes (`Submit`) | **adapter** maps them; the rules that choose them **stay** |
| Unknown outcome = no RPC error (`orders.MaybePlaced` reads `gateway.RPCError`, `ErrCircuitOpen`) | `orders/executor.go` | the function, no | **adapter** must keep the same meaning (placed / not placed / maybe) |
| GEX from **mainnet** open interest (testnet OI is test accounts') | `cmd/bot/main.go` `marketGW`, `gex.Manager` | yes (`GEXSource`, `OISource`) | **stays**: GEX keeps reading Deribit mainnet whatever the trading exchange; the liquidity cap's OI → **settle later** (it must be the trading venue's) |
| Perpetual `<COIN>-PERPETUAL` for daily closes and the hedge suggestion | `strategy.suggestedHedgeInst` | no — a literal format | **adapter** (`Candles` takes the coin, the adapter names its instrument) |
| Endpoints `wss://www.deribit.com`, `wss://test.deribit.com`, `DERIBIT_ENV` | `config.go`, `.env` | n/a | **adapter** (per-exchange `.env` keys, testnet default kept) |

## 3. Proposed interface

Four groups — connection, market data, orders, positions and margin — plus a capability list. The method sets of groups 2–4 are the ones `internal/strategy/deps.go` uses today, regrouped; the block compiles against today's types (`TestExchangeCoupling_ProposedInterfaceCompiles` copies it into a scratch package of this module and runs `go vet`; the last block of `var _` lines are the checks, `deps.go` included). It is a proposal, so it lives here, not in `internal/`.

```go
// Package adapter is a proposal (docs/exchange-adapter.md): the boundary one
// exchange implements. Data types are the bot's own; an adapter converts.
package adapter

import (
	"context"

	"optionsbot/internal/gateway"
	"optionsbot/internal/gex"
	"optionsbot/internal/marketdata"
	"optionsbot/internal/orders"
	"optionsbot/internal/strategy"
)

// ── 1. Connection ────────────────────────────────────────────────────────────
// The adapter owns its transport (rate-limit pools, priorities, retries,
// circuit breaker, reconnect). Method names and wire format stay inside it:
// no caller passes "private/…" strings any more.

type Connection interface {
	Connect(ctx context.Context) error
	Close() error
	// Fatal reports that the connection gave up reconnecting.
	Fatal() <-chan error
	ConnStats() gateway.ConnStats
}

// ── 2. Market data ───────────────────────────────────────────────────────────

// OptionChain: the chain, live quotes and the underlying's index.
type OptionChain interface {
	UnderlyingPrice() float64
	GetInstrument(name string) (*marketdata.Instrument, bool)
	AllInstruments() []*marketdata.Instrument
	Track(ctx context.Context, instruments []string) error
}

// VolIndex: the implied-volatility index (Deribit: DVOL). Optional capability.
type VolIndex interface {
	DVOL() float64
	IVPercentile() float64
	DVOLDaily() ([]marketdata.DayIV, marketdata.DayIV)
}

// Candles: daily closes for the trend. Today on the Executor
// (public/get_tradingview_chart_data); proposed home: market data.
type Candles interface {
	GetDailyCloses(ctx context.Context, instrument string, days int) ([]orders.DailyClose, error)
}

// BookSummary: open interest and marks of every option of a currency.
// NEW: today an unexported fetch in gex.Manager and recorder.LinearBook.
type BookSummary interface {
	BookSummary(ctx context.Context, currency string) ([]gex.SummaryRow, error)
}

type MarketData interface {
	OptionChain
	VolIndex
}

// ── 3. Orders ────────────────────────────────────────────────────────────────

type Orders interface {
	Submit(ctx context.Context, order orders.Order) (orders.Fill, error)
	Cancel(ctx context.Context, orderID string) error
	CancelAllOrders(ctx context.Context, currency string) error
	CancelByLabel(ctx context.Context, currency, label string) (int, error)
	GetOrderState(ctx context.Context, orderID string) (orders.OrderStateInfo, error)
	AmendOrder(ctx context.Context, orderID string, qty, price float64) error
	OrderFee(ctx context.Context, orderID string) (float64, error)
}

// ── 4. Positions and margin ──────────────────────────────────────────────────

type PositionsAndMargin interface {
	GetAccountSummary(ctx context.Context, currency string) (orders.AccountSummary, error)
	GetPositions(ctx context.Context, currency string) ([]orders.RawPosition, error)
	GetMargins(ctx context.Context, instrument string, amount, price float64) (orders.MarginInfo, error)
}

// MarginSimulator prices hypothetical positions in the exchange's margin
// model. Optional capability (Deribit: private/simulate_portfolio).
type MarginSimulator interface {
	SimulatePortfolio(ctx context.Context, currency string, positions map[string]float64) (orders.AccountSummary, error)
	SimulateAlone(ctx context.Context, currency string, positions map[string]float64) (orders.AccountSummary, error)
}

// ── Capabilities ─────────────────────────────────────────────────────────────
// What an exchange may lack. The bot refuses to trade on an adapter missing a
// capability it has no fallback for; each fallback is a person's decision (§5).

type Capabilities struct {
	MarginSimulator     bool // portfolio margin simulation before an order
	VolIndex            bool // an implied-volatility index like DVOL
	CancelByLabel       bool // cancel/look up an order by client order ID
	AmendInPlace        bool // edit price and size of a resting order
	CancelAllByCurrency bool // cancel every order of one underlying
	PortfolioMargin     bool // margin offsets across the book
	InverseSettlement   bool // premiums and P&L in the coin, not USD(C)
	MainnetOpenInterest bool // real open interest on the same venue
}

// Exchange is one adapter. NEW: no type satisfies it yet (Name, Capabilities
// and BookSummary are new; Deribit's would compose Executor, Manager, Gateway).
type Exchange interface {
	Name() string
	Capabilities() Capabilities
	Connection
	MarketData
	Candles
	BookSummary
	Orders
	PositionsAndMargin
	MarginSimulator
}

// ── Checks against today's types ─────────────────────────────────────────────
var (
	_ Connection         = (*gateway.Gateway)(nil)
	_ MarketData         = (*marketdata.Manager)(nil)
	_ Orders             = (*orders.Executor)(nil)
	_ PositionsAndMargin = (*orders.Executor)(nil)
	_ MarginSimulator    = (*orders.Executor)(nil)
	_ Candles            = (*orders.Executor)(nil)

	// The strategy's deps.go asks for exactly these groups, both ways.
	_ strategy.Exchange = (interface {
		Orders
		PositionsAndMargin
		MarginSimulator
		Candles
	})(nil)
	_ strategy.MarketData = MarketData(nil)
)
```

**Types that change** (none in this PR): `Exchange` and `BookSummary` are new; `GetDailyCloses` moves from the account reader to market data (`Candles`); `Capabilities` is new and Deribit's value is all `true`. `internal/exchange` keeps its current names until step 6 renames or aliases them to these groups; `strategy/deps.go` names the boundary's interfaces as aliases (#182: `Exchange = exchange.Trading`, `MarketData = exchange.MarketData`, `OISource = exchange.OpenInterest`), so a change here is a change to what the strategy asks.

**Deribit's capabilities**: all true. A second exchange's row of this table is the E8 feasibility brief's job (#183).

## 4. Migration sequence

Each step is one PR that keeps `make check` green and `tests/restart_invariance_test.go` passing, sends the same requests in the same order to Deribit, and changes no order or reconcile behaviour. Order handling and reconcile changes are in §5, not here.

1. **Inventory pinned** (this PR): this document and `tests/exchange_coupling_test.go`.
2. **Capabilities**: add `Capabilities` to `internal/exchange` with Deribit's value (all true), read by nothing. A test pins Deribit's value.
3. **One book-summary fetch**: `gex.Manager` and `recorder.LinearBook` call one `BookSummary(ctx, currency)` (the method moves to a Deribit package; same request, same gateway). `public/get_book_summary_by_currency` then has one call site.
4. **Chain and channels behind the adapter**: `public/get_instruments`, `public/get_volatility_index_data` and the three channel names move from `marketdata.Manager` into a Deribit chain source the manager consumes; `Manager` keeps its methods (it still satisfies `MarketData`).
5. **Account view**: `account.Poller`'s three calls go through the same Deribit package (monitor only, no trading effect).
6. **`internal/exchange/deribit`**: an `Adapter` type that composes today's `Gateway`, `Executor` and `Manager` and satisfies `Exchange`; `main` builds `strategy.Deps` from it. No call changes; the coupling test's allowed packages move to `exchange/deribit` as each step lands.
7. **Instrument fields instead of name parsing in the strategy**: `orphans.go`, `pending.go`, `shed.go`, `stopped.go` read `Instrument.Expiry/Strike/OptionType` (from the chain or the journal) instead of `ParseOptionName`; research tools keep the parser for recorded Deribit files. Restart invariance covers the journal-restore paths (`stopped.go`, `shed.go`).
8. **Name-free strategy literals**: `suggestedHedgeInst` (`<COIN>-PERPETUAL`) and the daily-close instrument come from the adapter.

After step 8 the coupling test can require that every `"public/"`/`"private/"` literal lives in `gateway` or `exchange/deribit` only. A second exchange is then an adapter plus the person's decisions in §5.

## 5. Proposals for a person (order handling and reconcile)

Core policy areas: the study found these; none is planned for the agents. Each needs a person's decision once a second exchange is chosen, and none matters while Deribit is the only one.

- **Unknown order outcomes without cancel-by-label** — today an order whose submit got no answer is cancelled by label each cycle (`private/cancel_by_label`) and its slot blocked. An exchange without it needs a lookup by client order ID, or the bot refuses to trade there.
- **No amend in place** — the step-down (`AmendOrder`) would become cancel-and-replace, which can leave both or neither order working for a moment and changes the fill risk of every entry and repair.
- **Cancel-all scope** — startup reconcile and the kill switch cancel *one currency's* orders so the BTC bot leaves ETH's alone. An exchange with only per-symbol or per-account cancel needs another way to keep the two bots apart (e.g. cancel by label prefix).
- **Executor tick fallback** — `AmendOrder` rounds with the executor's fixed `tickSize = 0.0001`, not the instrument's `TickSizeSteps` as `Submit` does. Correct on Deribit today; wrong on any venue with another tick. Proposal: pass the instrument's tick to `AmendOrder`.
- **Position aggregation in reconcile** — `GroupPositions` splits a strike shared by two strangles because Deribit reports one position per instrument (#156), and `PerOptionGreeks` undoes Deribit's size-weighted greeks. Another exchange's position report needs both rechecked against `restart_invariance_test.go`.
- **"Not placed" errors** — `orders.MaybePlaced` treats an RPC error or an open breaker as a definite no; each adapter must map its errors to the same three outcomes (placed, not placed, maybe), or orders can be doubled.
- **Margin without a simulator** — entry and top-up sizing and the size cap need `simulate_portfolio`; the backtest's `ApproxIMLimitPct` is labelled an approximation and is not good enough for live sizing.
- **Settlement unit** — every P&L, fee and "% of equity" is in coin; a linear venue changes the units of the journal and the monitor.
