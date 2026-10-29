# internal/marketdata

Folder notes for `CLAUDE.md` (the root file's trading rules apply here too).

- **`internal/marketdata`** — option chain, subscriptions to the index, DVOL (`deribit_volatility_index.btc_usd`) and tickers of tradable expiries only:
  - every expiry a slot could take — its `ExpiryWindow` and the stretch up to `StretchHi` — the same ranges `marketdata.PickExpiry` uses.
  Valid ticker intervals are `100ms` and `raw` only.
  `DVOLTracker` keeps one value per UTC day.
  Readers get copies.
  `Track(ctx, names)` subscribes to the tickers of held instruments outside those expiries (positions loaded at startup); the strategy calls it every cycle.
  `Instrument.HasQuote()` is false until the first ticker: never use such an instrument's zero prices as a mark.
