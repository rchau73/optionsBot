# internal/gex

Folder notes for `CLAUDE.md` (the root file's trading rules apply here too).

- **`internal/gex`** — every 60 s computes dealer gamma exposure from **mainnet** open interest (always: testnet prices mirror mainnet but its open interest belongs to test accounts, so testnet GEX describes nothing real).
  `gex.Build(rows, now, params)` is pure;
  - `gex_method: script` (default) reproduces GestaoCarteira's `deribit_tc_export_v3.py` exactly (5 nearest expiries of the chain, strikes ±`gex_strike_range_pct` of each expiry's future, regime = sign of the weighted sum, flip = lowest crossing, no hysteresis)
  - — `tests/gex_parity_test.go` checks it against the script's own output on captured mainnet data;
  - one intended divergence (#660): a strike with no open interest is not a zero crossing — the script counts it, and on 2026-11-05 an empty 67,000 strike became BTC's flip (real ≈ 77,400) and shed every call; `tests/gex_test.go` pins it on that day's book.
  - `nearest_flip` is the original rule (crossing nearest spot, regime = spot vs flip, hysteresis `gamma_regime_band_pct`).
  The book summary comes from `deribit.BookReader` (`internal/exchange/deribit`, #687), the one fetch it shares with the recorder.
  Option names are parsed with `marketdata.ParseOptionName` (daily expiries have one-digit days, e.g. `4OCT26`).
- **SOL and XRP** (#835): built from the coin's USDC book (`BookReader` asks `currency=USDC` and keeps `<COIN>_USDC-`; `Build` matches the name's `SOL_USDC` part, `ownUnderlying`, so BTC never reads `BTC_USDC`).
  The reference method needs no change for linear options: Deribit gives their open interest in the coin (SOL_USDC: contracts × 10), as for BTC, so gamma × OI × spot² is dollar gamma for both.
  The same open interest feeds the liquidity cap (orders are sized in SOL too); the trend reads `<COIN>_USDC-PERPETUAL` (`PerpetualName`, #838).
  Each change of the live regime logs `gex_regime_changed` (slog, not the journal: BTC/ETH journal lines stay byte-identical).
  Recorded hourly books 2026-10-28 → 11-19: SOL 11 changes (0.51/day), XRP 16 (0.74/day), BTC 11 (0.50/day).
