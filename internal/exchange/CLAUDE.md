# internal/exchange

Folder notes for `CLAUDE.md` (the root file's trading rules apply here too).

- **`internal/exchange`** — the exchange boundary for E8 (#181):
  - interfaces only, with compile-time checks in `deribit.go` that today's Deribit types (`orders.Executor`, `marketdata.Manager`, `gex.Manager`, `gateway.Gateway`, `ParseOptionName`, `IsMatchingEngine`, `OptionFee`) satisfy them;
  - Deribit is the only adapter.
  The strategy depends on it (#182):
  - `deps.go`'s `Exchange`, `MarketData`, `OISource` are aliases of its `Trading`, `MarketData`, `OpenInterest` (`tests/exchange_test.go` checks `Deps` carries those types), so a method the strategy needs is added there.
  `marketdata` and `account` read the connection through `internal/exchange/transport` (`Caller`, `Streamer`; a subpackage because `internal/exchange` names `marketdata.Instrument`).
  Method → Deribit call map and what is Deribit-specific: `docs/architecture.md` §15.
  The full inventory, the proposed adapter interface and its migration steps: `docs/exchange-adapter.md` (#325)
  - — `tests/exchange_coupling_test.go` fails when a `"public/…"`/`"private/…"` literal under `internal/` is not in its table (`deribitMethods`, method → allowed packages) and the doc's §1, or appears under `internal/strategy`:
  - a new Deribit call needs a row in both.
