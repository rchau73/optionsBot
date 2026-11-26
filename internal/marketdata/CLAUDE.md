# internal/marketdata

Folder notes for `CLAUDE.md` (the root file's trading rules apply here too).

- **`internal/marketdata`** — option chain, subscriptions to the index, DVOL (`deribit_volatility_index.btc_usd`) and tickers of tradable expiries only:
  - every expiry a slot could take — its `ExpiryWindow` and the stretch up to `StretchHi` — the same ranges `marketdata.PickExpiry` uses.
  The manager holds no Deribit method or channel name: `public/get_instruments`, `public/get_volatility_index_data` and the channels live in `deribit.ChainSource`, behind the `ChainSource` interface in `source.go` (#710).
  `marketdata` never imports `internal/exchange` or `internal/exchange/deribit` (`TestExchangeCoupling_MarketDataDoesNotImportTheExchange`); `New` takes the source and `main` builds it.
  Valid ticker intervals are `100ms` and `raw` only.
  `DVOLTracker` keeps one value per UTC day.
  A live DVOL push reaches it only through the sanity gate (`dvolgate.go`, pure `CheckDVOL`/`ATMMarkIV`, #796): more than ×/÷ 1.5 from the fresh ATM mark IV of the expiry nearest 30 DTE, or without marks a jump over 25 % with spot under 1 %, is rejected (≤ 30 min without marks);
  the first of an episode (and one every 30 min) is queued for `TakeDVOLRejections`, which the strategy journals as `dvol_rejected`.
  The startup history seed is not gated.
  The DVOL feed is the trading connection's, so on testnet it is testnet's own index (the 2026-11-14/15 swings).
  Readers get copies.
  `Track(ctx, names)` subscribes to the tickers of held instruments outside those expiries (positions loaded at startup); the strategy calls it every cycle.
  `Instrument.HasQuote()` is false until the first ticker: never use such an instrument's zero prices as a mark.
  `Instrument.Settlement` (the coin, or USDC) and `ContractSize` (1 BTC/ETH, 10 SOL, 1,000 XRP) come from `public/get_instruments` (#838); `InstrumentFromName` splits `SOL_USDC` into SOL settled in USDC, while `ParseOptionName` keeps the part whole for the research tools.
  `Instrument.OrderStep()` is every order's size grid (#832): the minimum for BTC/ETH, whole contracts for a USDC option (2,700 XRP → 2,000); `SettlementOf(name)` and `USDPerUnit` (1 for USDC, else spot) give an amount's unit and dollar value.
  The linear coin list itself is `config.IsLinear` (config names `min_premium`'s unit with it, `Config.PremiumUnit`).
