# internal/marketdata

Folder notes for `CLAUDE.md` (the root file's trading rules apply here too).

- **`internal/marketdata`** — option chain, subscriptions to the index, DVOL (`deribit_volatility_index.btc_usd`) and tickers of tradable expiries only:
  - every expiry a slot could take — its `ExpiryWindow` and the stretch up to `StretchHi` — the same ranges `marketdata.PickExpiry` uses.
  The manager holds no Deribit method or channel name: `public/get_instruments`, `public/get_volatility_index_data` and the channels live in `deribit.ChainSource`, behind the `ChainSource` interface in `source.go` (#710).
  `marketdata` never imports `internal/exchange` or `internal/exchange/deribit` (`TestExchangeCoupling_MarketDataDoesNotImportTheExchange`); `New` takes the source and `main` builds it.
  Valid ticker intervals are `100ms` and `raw` only.
  `DVOLTracker` keeps one value per UTC day.
  A coin without DVOL (`HasVolIndex` false: SOL, XRP, #834) subscribes to no DVOL channel and asks no DVOL history: `ReadChainIV` (every minute) feeds the same tracker with `ChainATMIV30` (pure, takes `now`: the ATM call/put mean mark IV, by variance to 30 days, one expiry → its own),
  and its daily closes persist in `data/chain_iv_history.jsonl` (`IVHistory`, `UseIVHistory` from `main`), so `DVOL()`, `IVPercentile()` and `DVOLDaily()` carry it to every rule; `IVSource()` names it (`ChainIVSource`), `IVCloses` counts its closes.
  Readers get copies.
  `Track(ctx, names)` subscribes to the tickers of held instruments outside those expiries (positions loaded at startup); the strategy calls it every cycle.
  `Instrument.HasQuote()` is false until the first ticker: never use such an instrument's zero prices as a mark.
  `Instrument.Settlement` (the coin, or USDC) and `ContractSize` (1 BTC/ETH, 10 SOL, 1,000 XRP) come from `public/get_instruments` (#838); `InstrumentFromName` splits `SOL_USDC` into SOL settled in USDC, while `ParseOptionName` keeps the part whole for the research tools.
